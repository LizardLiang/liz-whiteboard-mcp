// Canvas element write tools (Wave 2 of the canvas-whiteboard-mcp-support
// tactical plan): create_canvas_element, update_canvas_element, and
// delete_canvas_element for the SHAPE and TEXT kinds.
//
// Three properties of this file are load-bearing.
//
//  1. Writes go over Socket.IO, never direct SQL. Every element mutation is
//     emitted on /canvas/<canvasBoardId> so connected browser clients re-render
//     without a reload — the same convention every ER write tool follows. A
//     direct INSERT would persist an element that no open client shows until it
//     is reloaded.
//  2. The gate is EDITOR+, not membership. authorizeCanvasEdit calls
//     auth.AssertSchemaEditAccess (the gate from a9b9e95), NOT the
//     loadAuthorizedCanvasBoard read gate, which only proves membership. The
//     collaboration server re-checks with requireCanvasBoardRole; both layers
//     stay, and the MCP's own check gives a clean error before a socket dial.
//  3. Server-owned fields are never sent. The server computes zIndex as
//     MAX(zIndex)+1, generates the id, forces rotation to 0, and honours
//     minRevision only on undo's restore path. Sending any of them buys nothing
//     and risks tripping that restore-gated branch.
//
// Connectors are NOT here: their props arm is a filled union with three
// cross-field invariants and gets its own tool. Group elements are out of scope.
package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// Bounds mirrored from the app's Zod schemas (src/data/schema.ts). Checked here
// so an out-of-range value fails as a named MCP validation error instead of an
// opaque Zod string returned over a socket ack.
const (
	// canvasCoordMax mirrors MAX_BOARD_COORD: positions are bounded ±10,000,000.
	canvasCoordMax = 10_000_000
	// canvasSizeMax mirrors width/height .positive().max(100_000).
	canvasSizeMax = 100_000
	// canvasZIndexMin/Max mirror CANVAS_ZINDEX_MIN/MAX.
	canvasZIndexMin = -1_000_000
	canvasZIndexMax = 1_000_000
	// canvasTextMaxLength mirrors CANVAS_TEXT_MAX_LENGTH.
	canvasTextMaxLength = 10_000
)

// canvasStyleInput is the caller-settable appearance of a canvas element.
//
// The key set is fixed and complete because the app's canvasElementStyleSchema
// is a z.strictObject: an unknown key makes the whole write fail server-side, so
// a free-form JSON style argument would be a trap for an LLM. Every field is
// optional, and the schema supplies its own default for each absent one — which
// means style is written WHOLE, not merged: a style naming only fill resets the
// other keys to the engine's defaults.
type canvasStyleInput struct {
	Fill          *string  `json:"fill,omitempty" jsonschema:"Fill color, any CSS color string"`
	Stroke        *string  `json:"stroke,omitempty" jsonschema:"Stroke color, any CSS color string"`
	StrokeWidth   *float64 `json:"strokeWidth,omitempty" jsonschema:"Stroke width, 0-64"`
	FontSize      *float64 `json:"fontSize,omitempty" jsonschema:"Font size, 1-512"`
	Color         *string  `json:"color,omitempty" jsonschema:"Text color, any CSS color string"`
	CornerRadius  *float64 `json:"cornerRadius,omitempty" jsonschema:"Corner rounding in world units, 0-512"`
	TextAlign     *string  `json:"textAlign,omitempty" jsonschema:"Horizontal text alignment: left, center, or right"`
	VerticalAlign *string  `json:"verticalAlign,omitempty" jsonschema:"Vertical text alignment: top, middle, or bottom"`
}

type createCanvasElementInput struct {
	CanvasBoardID string            `json:"canvasBoardId" jsonschema:"The canvas board UUID"`
	Kind          string            `json:"kind" jsonschema:"Element kind: rectangle, ellipse, diamond, triangle, or text"`
	PositionX     float64           `json:"positionX" jsonschema:"X position of the element's top-left corner in board coordinates"`
	PositionY     float64           `json:"positionY" jsonschema:"Y position of the element's top-left corner in board coordinates"`
	Width         float64           `json:"width" jsonschema:"Element width, greater than 0 and at most 100000"`
	Height        float64           `json:"height" jsonschema:"Element height, greater than 0 and at most 100000"`
	Text          *string           `json:"text,omitempty" jsonschema:"Label text drawn inside the element, at most 10000 characters"`
	Style         *canvasStyleInput `json:"style,omitempty" jsonschema:"Appearance; omitted keys fall back to the engine defaults"`
}

type updateCanvasElementInput struct {
	CanvasBoardID string            `json:"canvasBoardId" jsonschema:"The canvas board UUID the element belongs to"`
	ElementID     string            `json:"elementId" jsonschema:"The canvas element UUID"`
	PositionX     *float64          `json:"positionX,omitempty" jsonschema:"New X position in board coordinates"`
	PositionY     *float64          `json:"positionY,omitempty" jsonschema:"New Y position in board coordinates"`
	Width         *float64          `json:"width,omitempty" jsonschema:"New width, greater than 0 and at most 100000"`
	Height        *float64          `json:"height,omitempty" jsonschema:"New height, greater than 0 and at most 100000"`
	ZIndex        *int              `json:"zIndex,omitempty" jsonschema:"New paint order, -1000000 to 1000000; higher draws on top"`
	Text          *string           `json:"text,omitempty" jsonschema:"New label text, at most 10000 characters; pass an empty string to clear it"`
	Style         *canvasStyleInput `json:"style,omitempty" jsonschema:"Replacement appearance; omitted keys reset to the engine defaults"`
}

type deleteCanvasElementInput struct {
	CanvasBoardID string `json:"canvasBoardId" jsonschema:"The canvas board UUID the element belongs to"`
	ElementID     string `json:"elementId" jsonschema:"The canvas element UUID"`
}

// canvasEditFns is the injectable write gate, mirroring canvasBoardLoaderFns in
// canvas_read.go so the authz path is unit-testable without a database.
type canvasEditFns struct {
	resolveProject func(ctx context.Context, canvasBoardID string) (string, error)
	assertEdit     func(ctx context.Context, userID, projectID string) error
}

// prodCanvasEditFns is the production write gate.
//
// assertEdit MUST be auth.AssertSchemaEditAccess. auth.AssertProjectAccess would
// let a VIEWER mutate a board through the MCP and leave the refusal entirely to
// the collaboration server. TestProdCanvasEditFns_UseEditorGateAndCanvasResolver
// pins both stages.
func prodCanvasEditFns() canvasEditFns {
	return canvasEditFns{
		resolveProject: data.GetCanvasBoardProjectID,
		assertEdit:     auth.AssertSchemaEditAccess,
	}
}

// authorizeCanvasEditWithFns runs the shared canvas write gate: UUID validation,
// canvas project resolution, EDITOR+ check. An unknown board reports NOT_FOUND
// through canvasBoardNotFound, the same message the read tools use.
func authorizeCanvasEditWithFns(ctx context.Context, fns canvasEditFns, userID, canvasBoardID string) error {
	if e := validateUUID("canvasBoardId", canvasBoardID); e != nil {
		return e
	}
	projectID, err := fns.resolveProject(ctx, canvasBoardID)
	if err != nil {
		return err
	}
	if projectID == "" {
		return canvasBoardNotFound(canvasBoardID)
	}
	return fns.assertEdit(ctx, userID, projectID)
}

// authorizeCanvasEdit is the production entry point for the write gate. It
// returns the authenticated user id, which the caller needs for the socket dial.
func authorizeCanvasEdit(ctx context.Context, canvasBoardID string) (string, error) {
	userID := auth.UserID(ctx)
	return userID, authorizeCanvasEditWithFns(ctx, prodCanvasEditFns(), userID, canvasBoardID)
}

// checkCanvasCoord mirrors boardCoordSchema: finite and within ±MAX_BOARD_COORD.
func checkCanvasCoord(field string, v float64) *mcperr.McpError {
	if e := checkFinite(field, v); e != nil {
		return e
	}
	if v < -canvasCoordMax || v > canvasCoordMax {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be between %d and %d.", field, -canvasCoordMax, canvasCoordMax), field)
	}
	return nil
}

// checkCanvasSize mirrors z.number().positive().max(100_000) on width/height.
func checkCanvasSize(field string, v float64) *mcperr.McpError {
	if e := checkFinite(field, v); e != nil {
		return e
	}
	if e := checkPositive(field, v); e != nil {
		return e
	}
	if v > canvasSizeMax {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be at most %d.", field, canvasSizeMax), field)
	}
	return nil
}

// checkCanvasZIndex mirrors canvasZIndexSchema's bounds. z-order is exposed only
// through update_canvas_element; there is no separate bring-to-front tool.
func checkCanvasZIndex(z int) *mcperr.McpError {
	if z < canvasZIndexMin || z > canvasZIndexMax {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("zIndex must be between %d and %d.", canvasZIndexMin, canvasZIndexMax), "zIndex")
	}
	return nil
}

// checkCanvasStyleNumber validates one bounded numeric style field.
func checkCanvasStyleNumber(field string, v float64, min, max float64) *mcperr.McpError {
	if e := checkFinite(field, v); e != nil {
		return e
	}
	if v < min || v > max {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be between %g and %g.", field, min, max), field)
	}
	return nil
}

// checkCanvasEnum validates one fixed-set style field.
func checkCanvasEnum(field, value string, allowed ...string) *mcperr.McpError {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return mcperr.NewField(mcperr.ValidationError,
		fmt.Sprintf("%s must be one of: %v.", field, allowed), field)
}

// buildCanvasStyle validates a style input and renders it as the payload object.
// Returns nil (not an empty map) when no style was supplied, so the key is left
// out of the emit entirely and the server keeps the element's stored style.
func buildCanvasStyle(in *canvasStyleInput) (map[string]any, *mcperr.McpError) {
	if in == nil {
		return nil, nil
	}
	style := map[string]any{}

	// cssColorSchema is z.string().min(1).max(64).
	colors := []struct {
		field string
		value *string
	}{
		{"fill", in.Fill},
		{"stroke", in.Stroke},
		{"color", in.Color},
	}
	for _, c := range colors {
		if c.value == nil {
			continue
		}
		if e := checkLen(c.field, *c.value, 1, 64); e != nil {
			return nil, e
		}
		style[c.field] = *c.value
	}

	numbers := []struct {
		field    string
		value    *float64
		min, max float64
	}{
		{"strokeWidth", in.StrokeWidth, 0, 64},
		{"fontSize", in.FontSize, 1, 512},
		{"cornerRadius", in.CornerRadius, 0, 512},
	}
	for _, n := range numbers {
		if n.value == nil {
			continue
		}
		if e := checkCanvasStyleNumber(n.field, *n.value, n.min, n.max); e != nil {
			return nil, e
		}
		style[n.field] = *n.value
	}

	if in.TextAlign != nil {
		if e := checkCanvasEnum("textAlign", *in.TextAlign, "left", "center", "right"); e != nil {
			return nil, e
		}
		style["textAlign"] = *in.TextAlign
	}
	if in.VerticalAlign != nil {
		if e := checkCanvasEnum("verticalAlign", *in.VerticalAlign, "top", "middle", "bottom"); e != nil {
			return nil, e
		}
		style["verticalAlign"] = *in.VerticalAlign
	}

	return style, nil
}

// buildCanvasElementCreatePayload validates a create request and renders the
// element:create payload.
//
// props is built HERE from kind and is never accepted from the caller:
// createCanvasElementSchema refines that kind === props.kind, and every kind this
// tool supports has an empty props arm, so props is exactly {"kind": <kind>}.
// boardId is omitted because the server spreads its own last — a payload naming
// another board cannot redirect the write.
func buildCanvasElementCreatePayload(in createCanvasElementInput) (map[string]any, *mcperr.McpError) {
	if !schema.IsValidCanvasElementKind(in.Kind) {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("kind must be one of: %v. Use create_canvas_connector for connectors; groups are not supported.",
				schema.CanvasElementKinds), "kind")
	}
	if e := checkCanvasCoord("positionX", in.PositionX); e != nil {
		return nil, e
	}
	if e := checkCanvasCoord("positionY", in.PositionY); e != nil {
		return nil, e
	}
	if e := checkCanvasSize("width", in.Width); e != nil {
		return nil, e
	}
	if e := checkCanvasSize("height", in.Height); e != nil {
		return nil, e
	}

	payload := map[string]any{
		"kind":      in.Kind,
		"positionX": in.PositionX,
		"positionY": in.PositionY,
		"width":     in.Width,
		"height":    in.Height,
		"props":     map[string]any{"kind": in.Kind},
	}

	if in.Text != nil {
		if e := checkMaxLen("text", *in.Text, canvasTextMaxLength); e != nil {
			return nil, e
		}
		payload["text"] = *in.Text
	}
	style, e := buildCanvasStyle(in.Style)
	if e != nil {
		return nil, e
	}
	if style != nil {
		payload["style"] = style
	}
	return payload, nil
}

// buildCanvasElementUpdatePayload validates an update request and renders the
// element:update payload: elementId plus exactly the fields the caller supplied.
//
// expectedRevision is never sent. It is undo's conditional-write guard; an MCP
// edit is an ordinary forward edit under last-write-wins, and this tool holds no
// revision it could legitimately expect.
func buildCanvasElementUpdatePayload(in updateCanvasElementInput) (map[string]any, *mcperr.McpError) {
	if e := validateUUID("elementId", in.ElementID); e != nil {
		return nil, e
	}

	payload := map[string]any{"elementId": in.ElementID}

	coords := []struct {
		field string
		value *float64
	}{
		{"positionX", in.PositionX},
		{"positionY", in.PositionY},
	}
	for _, c := range coords {
		if c.value == nil {
			continue
		}
		if e := checkCanvasCoord(c.field, *c.value); e != nil {
			return nil, e
		}
		payload[c.field] = *c.value
	}

	sizes := []struct {
		field string
		value *float64
	}{
		{"width", in.Width},
		{"height", in.Height},
	}
	for _, s := range sizes {
		if s.value == nil {
			continue
		}
		if e := checkCanvasSize(s.field, *s.value); e != nil {
			return nil, e
		}
		payload[s.field] = *s.value
	}

	if in.ZIndex != nil {
		if e := checkCanvasZIndex(*in.ZIndex); e != nil {
			return nil, e
		}
		payload["zIndex"] = *in.ZIndex
	}
	if in.Text != nil {
		if e := checkMaxLen("text", *in.Text, canvasTextMaxLength); e != nil {
			return nil, e
		}
		payload["text"] = *in.Text
	}
	style, e := buildCanvasStyle(in.Style)
	if e != nil {
		return nil, e
	}
	if style != nil {
		payload["style"] = style
	}

	// Only elementId present: the caller named no change. Refuse locally rather
	// than emit, which would bump the row's revision for nothing.
	if len(payload) == 1 {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"Provide at least one field to change: positionX, positionY, width, height, zIndex, text, or style.",
			"elementId")
	}
	return payload, nil
}

// canvasAckMessage shapes the message of a failed canvas ack. The CODE mapping
// stays with the shared mcperr.AckCodeToMcpCode — this only adds context for
// REVISION_MISMATCH, a code the ER tools never see and whose raw server message
// ("is at revision 4, expected 2") reads as nonsense from a tool that sends no
// expectedRevision. Reaching it means the element changed under a concurrent
// write, or the server's contract changed.
func canvasAckMessage(code, message string) string {
	if code == "REVISION_MISMATCH" {
		return "The canvas element changed on the server while this request was in flight; " +
			"re-read the board and retry. Server said: " + message
	}
	return message
}

// canvasAckFailure converts a failed ack into an MCP error result.
func canvasAckFailure(ack socket.AckResult, fallback string) (*mcp.CallToolResult, any, error) {
	code := ack.Code()
	return mcpError(mcperr.AckCodeToMcpCode(code), canvasAckMessage(code, msgOr(ack.Message(), fallback)))
}

// emitCanvasElementEvent runs the write gate and emits one element event on the
// canvas namespace. Returns the ack's entity on success.
func emitCanvasElementEvent(
	ctx context.Context,
	canvasBoardID, event string,
	payload map[string]any,
	fallbackMessage string,
) (*mcp.CallToolResult, any, error) {
	userID, err := authorizeCanvasEdit(ctx, canvasBoardID)
	if err != nil {
		return fail(err)
	}
	ack, err := socket.SocketEmitWithAckNS(ctx, socket.NamespaceCanvas, canvasBoardID, userID, event, payload)
	if err != nil {
		return fail(err)
	}
	if !ack.OK() {
		return canvasAckFailure(ack, fallbackMessage)
	}
	return success(ack.Entity())
}

// RegisterCanvasWriteTools registers create_canvas_element,
// update_canvas_element, and delete_canvas_element.
func RegisterCanvasWriteTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_canvas_element",
		Description: "Create a shape or text element on a canvas board (freeform FigJam-style board, " +
			"NOT an ER diagram whiteboard). kind is rectangle, ellipse, diamond, triangle, or text. " +
			"The new element is placed on top of the board automatically. " +
			"Use create_canvas_connector to draw a line between elements. " +
			"For ER diagram tables use create_table instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCanvasElementInput) (*mcp.CallToolResult, any, error) {
		payload, e := buildCanvasElementCreatePayload(in)
		if e != nil {
			return fail(e)
		}
		return emitCanvasElementEvent(ctx, in.CanvasBoardID, "element:create", payload,
			"Server rejected canvas element creation.")
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_canvas_element",
		Description: "Move, resize, restyle, re-label, or restack one canvas element (freeform " +
			"FigJam-style board, NOT an ER diagram whiteboard). Supply canvasBoardId, elementId, " +
			"and at least one field to change. zIndex sets paint order: a higher value draws on top. " +
			"style is written whole — omitted style keys reset to the engine defaults.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCanvasElementInput) (*mcp.CallToolResult, any, error) {
		payload, e := buildCanvasElementUpdatePayload(in)
		if e != nil {
			return fail(e)
		}
		return emitCanvasElementEvent(ctx, in.CanvasBoardID, "element:update", payload,
			"Server rejected canvas element update.")
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_canvas_element",
		Description: "Delete one element from a canvas board (freeform FigJam-style board, NOT an " +
			"ER diagram whiteboard). Deletion is permanent from this tool's side. " +
			"An element id that does not exist, or that belongs to a different board, " +
			"both report NOT_FOUND.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteCanvasElementInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("elementId", in.ElementID); e != nil {
			return fail(e)
		}
		return emitCanvasElementEvent(ctx, in.CanvasBoardID, "element:delete",
			map[string]any{"elementId": in.ElementID},
			"Server rejected canvas element deletion.")
	})
}

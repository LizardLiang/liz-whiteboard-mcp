// Canvas connector tools (Wave 3 of the canvas-whiteboard-mcp-support tactical
// plan): create_canvas_connector and update_canvas_connector.
//
// A connector is a canvas element like any other, but it gets its OWN pair of
// tools instead of riding create_canvas_element / update_canvas_element, for two
// reasons that both come from the app's schema:
//
//  1. Its props arm is the only one carrying real content, and it carries three
//     cross-field invariants (exactly one endpoint form per end, twice, and no
//     self connector). Folding that into the generic create would produce a
//     union schema an LLM cannot fill reliably.
//  2. updateCanvasElementSchema takes props as a REPLACEMENT, not a patch. A
//     connector's props may also hold sourceAttach, targetAttach, sourceAnchor,
//     targetAnchor and curvature — all optional, all written by the UI, none of
//     them things this tool's caller knows about. So update_canvas_connector
//     READS the stored props first and merges into them. A write assembled from
//     a fixed field list would silently un-anchor and un-bow a connector the
//     caller only meant to re-route.
//
// A connector has no geometry of its own: connector-geometry.ts derives the
// drawn path from the two endpoints' live bounds on every frame. The four NOT
// NULL, .positive() geometry columns therefore take a degenerate 1x1 placeholder
// at the source's centre, exactly as the app's own makeConnector does, and
// nothing ever reads it back.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
)

// canvasCurvatureLimit mirrors CURVATURE_LIMIT in connector-geometry.ts. The
// app's Zod schema deliberately does NOT range-check curvature, so that a row
// hand-edited out of range stays editable; the geometry clamps on render
// instead. This tool checks the range anyway, because it only ever writes NEW
// values: accepting 99 would store a number that draws as 2, which is a lie the
// caller cannot see.
const canvasCurvatureLimit = 2.0

// canvasPointInput is a free connector end's own board coordinate. Mirrors
// canvasConnectorPointSchema: both components are ordinary board coordinates.
type canvasPointInput struct {
	X float64 `json:"x" jsonschema:"X coordinate in board coordinates"`
	Y float64 `json:"y" jsonschema:"Y coordinate in board coordinates"`
}

type createCanvasConnectorInput struct {
	CanvasBoardID   string            `json:"canvasBoardId" jsonschema:"The canvas board UUID"`
	SourceElementID *string           `json:"sourceElementId,omitempty" jsonschema:"UUID of the element the line starts at; supply this OR sourcePoint, never both"`
	SourcePoint     *canvasPointInput `json:"sourcePoint,omitempty" jsonschema:"Free board coordinate the line starts at; supply this OR sourceElementId, never both"`
	TargetElementID *string           `json:"targetElementId,omitempty" jsonschema:"UUID of the element the line ends at; supply this OR targetPoint, never both"`
	TargetPoint     *canvasPointInput `json:"targetPoint,omitempty" jsonschema:"Free board coordinate the line ends at; supply this OR targetElementId, never both"`
	Routing         string            `json:"routing" jsonschema:"How the line is drawn: straight, elbow, or curved"`
	Curvature       *float64          `json:"curvature,omitempty" jsonschema:"Optional hand-applied bow, -2 to 2, as a signed fraction of the straight chord; omit for an unbowed line"`
}

type updateCanvasConnectorInput struct {
	CanvasBoardID   string            `json:"canvasBoardId" jsonschema:"The canvas board UUID the connector belongs to"`
	ElementID       string            `json:"elementId" jsonschema:"The connector element UUID"`
	Routing         *string           `json:"routing,omitempty" jsonschema:"New line style: straight, elbow, or curved"`
	SourceElementID *string           `json:"sourceElementId,omitempty" jsonschema:"Re-attach the start of the line to this element; supply this OR sourcePoint, never both"`
	SourcePoint     *canvasPointInput `json:"sourcePoint,omitempty" jsonschema:"Detach the start of the line to this free board coordinate; supply this OR sourceElementId, never both"`
	TargetElementID *string           `json:"targetElementId,omitempty" jsonschema:"Re-attach the end of the line to this element; supply this OR targetPoint, never both"`
	TargetPoint     *canvasPointInput `json:"targetPoint,omitempty" jsonschema:"Detach the end of the line to this free board coordinate; supply this OR targetElementId, never both"`
	Curvature       *float64          `json:"curvature,omitempty" jsonschema:"New hand-applied bow, -2 to 2; 0 straightens the line"`
}

// canvasConnectorFns is the injectable dependency set, mirroring canvasEditFns
// in canvas_write.go so both the gate and the element read are testable without
// a database or a socket.
type canvasConnectorFns struct {
	authorize   func(ctx context.Context, canvasBoardID string) (string, error)
	loadElement func(ctx context.Context, elementID string) (*data.CanvasElement, error)
}

// prodCanvasConnectorFns wires the production gate and reader.
//
// authorize MUST stay authorizeCanvasEdit: connector writes are element writes
// and take the same EDITOR+ gate as every other canvas mutation.
// TestProdCanvasConnectorFns_UseTheEditorGateAndTheCanvasElementReader pins both.
func prodCanvasConnectorFns() canvasConnectorFns {
	return canvasConnectorFns{
		authorize:   authorizeCanvasEdit,
		loadElement: data.FindCanvasElementByID,
	}
}

// ---------------------------------------------------------------------------
// Endpoint invariants, checked BEFORE the emit
// ---------------------------------------------------------------------------

// checkCanvasConnectorEnd enforces the app's hasExactlyOneEnd refine for one end
// ("source" or "target") and validates whichever form was supplied.
//
// Checked here rather than left to the server so the failure is a named MCP
// validation error instead of an opaque Zod string arriving over a socket ack.
func checkCanvasConnectorEnd(end string, elementID *string, point *canvasPointInput) *mcperr.McpError {
	idField := end + "ElementId"
	switch {
	case elementID != nil && point != nil:
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("A connector end is either attached to an element or a free point, never both: "+
				"supply %s or %sPoint, not both.", idField, end), idField)
	case elementID == nil && point == nil:
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("A connector end must be either attached to an element or a free point: "+
				"supply %s or %sPoint.", idField, end), idField)
	case elementID != nil:
		return validateUUID(idField, *elementID)
	}
	if e := checkCanvasCoord(end+"Point.x", point.X); e != nil {
		return e
	}
	return checkCanvasCoord(end+"Point.y", point.Y)
}

// checkCanvasConnectorNotSelf refuses a connector joining an element to itself.
// Such a connector has no drawable path — its two endpoint rects are the same
// rect — so it would persist as permanently invisible and unselectable.
//
// Guarded on BOTH ids being present: two free ends are both absent, and that is
// an ordinary floating line, not a self connector.
func checkCanvasConnectorNotSelf(sourceElementID, targetElementID *string) *mcperr.McpError {
	if sourceElementID == nil || targetElementID == nil {
		return nil
	}
	if *sourceElementID != *targetElementID {
		return nil
	}
	return mcperr.NewField(mcperr.ValidationError,
		"A connector cannot join an element to itself; it would have no drawable path.",
		"targetElementId")
}

// checkCanvasCurvature holds the hand-applied bow to the range the renderer
// clamps to. See canvasCurvatureLimit for why this is stricter than the app's
// own schema.
func checkCanvasCurvature(v float64) *mcperr.McpError {
	if e := checkFinite("curvature", v); e != nil {
		return e
	}
	if v < -canvasCurvatureLimit || v > canvasCurvatureLimit {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("curvature must be between %g and %g.", -canvasCurvatureLimit, canvasCurvatureLimit),
			"curvature")
	}
	return nil
}

// checkCanvasRouting validates the line style against the app's enum.
func checkCanvasRouting(routing string) *mcperr.McpError {
	if schema.IsValidCanvasConnectorRouting(routing) {
		return nil
	}
	return mcperr.NewField(mcperr.ValidationError,
		fmt.Sprintf("routing must be one of: %v.", schema.CanvasConnectorRoutings), "routing")
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// buildCanvasConnectorProps renders the connector props arm.
//
// The two id keys are written even when the end is free, because the app's
// schema declares them .nullable() and NOT .optional(): an absent key fails
// validation, while an explicit null is what a free end means. sourceAttach,
// targetAttach and the legacy *Anchor keys are omitted on create, so the
// geometry falls back to centre-derived border points per end.
func buildCanvasConnectorProps(in createCanvasConnectorInput) (map[string]any, *mcperr.McpError) {
	if e := checkCanvasRouting(in.Routing); e != nil {
		return nil, e
	}
	if e := checkCanvasConnectorEnd("source", in.SourceElementID, in.SourcePoint); e != nil {
		return nil, e
	}
	if e := checkCanvasConnectorEnd("target", in.TargetElementID, in.TargetPoint); e != nil {
		return nil, e
	}
	if e := checkCanvasConnectorNotSelf(in.SourceElementID, in.TargetElementID); e != nil {
		return nil, e
	}

	props := map[string]any{
		"kind":            "connector",
		"sourceElementId": nil,
		"targetElementId": nil,
		"routing":         in.Routing,
	}
	applyCanvasConnectorEnd(props, "source", in.SourceElementID, in.SourcePoint)
	applyCanvasConnectorEnd(props, "target", in.TargetElementID, in.TargetPoint)

	if in.Curvature != nil {
		if e := checkCanvasCurvature(*in.Curvature); e != nil {
			return nil, e
		}
		// Written only when supplied: a connector that was never bowed carries
		// no curvature key at all, and a deliberate 0 is a value, not an absence.
		props["curvature"] = *in.Curvature
	}
	return props, nil
}

// applyCanvasConnectorEnd writes one end into props, in the form the caller
// chose. Setting one form always removes the other, so the merged props can
// never carry both an id and a point for the same end.
//
// Exactly one of elementID / point must be non-nil; checkCanvasConnectorEnd is
// what guarantees that.
func applyCanvasConnectorEnd(props map[string]any, end string, elementID *string, point *canvasPointInput) {
	idKey, pointKey := end+"ElementId", end+"Point"
	if elementID != nil {
		props[idKey] = *elementID
		delete(props, pointKey)
		return
	}
	props[idKey] = nil
	props[pointKey] = map[string]any{"x": point.X, "y": point.Y}
}

// clampCanvasCoord holds a derived coordinate inside boardCoordSchema's range.
func clampCanvasCoord(v float64) float64 {
	if v > canvasCoordMax {
		return canvasCoordMax
	}
	if v < -canvasCoordMax {
		return -canvasCoordMax
	}
	return v
}

// canvasConnectorPlaceholder returns where the degenerate 1x1 element sits: the
// source element's centre, or the free source point.
//
// Nothing reads this back — connector-geometry.ts derives the path from live
// endpoint bounds — but the columns are NOT NULL and boardCoordSchema bounds
// them, so an element parked at the coordinate ceiling would otherwise produce a
// centre outside the range and fail the whole write.
func canvasConnectorPlaceholder(sourceEl *data.CanvasElement, sourcePoint *canvasPointInput) (float64, float64) {
	if sourceEl != nil {
		return clampCanvasCoord(sourceEl.PositionX + sourceEl.Width/2),
			clampCanvasCoord(sourceEl.PositionY + sourceEl.Height/2)
	}
	return clampCanvasCoord(sourcePoint.X), clampCanvasCoord(sourcePoint.Y)
}

// buildCanvasConnectorCreatePayload renders the element:create payload.
//
// sourceEl is the loaded source element, or nil when the source end is a free
// point. id, zIndex, rotation, minRevision and boardId are absent for the same
// reasons they are absent from create_canvas_element: the server owns them.
func buildCanvasConnectorCreatePayload(
	in createCanvasConnectorInput,
	sourceEl *data.CanvasElement,
) (map[string]any, *mcperr.McpError) {
	props, e := buildCanvasConnectorProps(in)
	if e != nil {
		return nil, e
	}
	x, y := canvasConnectorPlaceholder(sourceEl, in.SourcePoint)
	return map[string]any{
		"kind":      "connector",
		"positionX": x,
		"positionY": y,
		// .positive(), so 0 is rejected — the placeholder is 1x1, never 0x0.
		"width":  1.0,
		"height": 1.0,
		"props":  props,
	}, nil
}

// ---------------------------------------------------------------------------
// Element resolution
// ---------------------------------------------------------------------------

// canvasElementNotFound is the single miss message for a canvas element. An id
// that exists on ANOTHER board reports the same thing as an id that exists
// nowhere, matching the collaboration server's own loadOwnedElement, which
// deliberately does not distinguish the two.
func canvasElementNotFound(field, elementID string) *mcperr.McpError {
	return mcperr.NewField(mcperr.NotFound,
		fmt.Sprintf("Canvas element %s not found on this board.", elementID), field)
}

// loadCanvasElementOnBoard reads one element and proves it belongs to the board
// the caller was authorized for. Call it only AFTER the gate: it reveals whether
// an element exists.
func loadCanvasElementOnBoard(
	ctx context.Context,
	fns canvasConnectorFns,
	canvasBoardID, field, elementID string,
) (*data.CanvasElement, error) {
	el, err := fns.loadElement(ctx, elementID)
	if err != nil {
		return nil, err
	}
	if el == nil || el.BoardID != canvasBoardID {
		return nil, canvasElementNotFound(field, elementID)
	}
	return el, nil
}

// loadCanvasConnectorForUpdate resolves the element being updated and refuses
// anything that is not a connector — a shape sent here would get connector props
// written over its empty props arm and fail the schema's kind cross-check.
func loadCanvasConnectorForUpdate(
	ctx context.Context,
	fns canvasConnectorFns,
	canvasBoardID, elementID string,
) (*data.CanvasElement, error) {
	el, err := loadCanvasElementOnBoard(ctx, fns, canvasBoardID, "elementId", elementID)
	if err != nil {
		return nil, err
	}
	if el.Kind != "connector" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("Element %s is a %s, not a connector. Use update_canvas_element for shapes and text.",
				elementID, el.Kind), "elementId")
	}
	return el, nil
}

// ---------------------------------------------------------------------------
// Update — read, merge, write
// ---------------------------------------------------------------------------

// mergeCanvasConnectorProps folds the caller's changes into the connector's
// STORED props and returns the whole replacement object.
//
// Decoding into a generic map, rather than a typed struct, is deliberate: every
// key the tool does not know about — sourceAttach, targetAttach, the legacy
// sourceAnchor / targetAnchor, curvature, and anything the app adds later —
// survives untouched. A typed round trip would delete each one it had no field
// for, un-anchoring and un-bowing a connector the caller only meant to re-route.
//
// Re-pointing an end REPLACES that end, so the opposing form is removed with it;
// otherwise the merged props would carry both an id and a point and fail the
// schema's exactly-one refine.
func mergeCanvasConnectorProps(
	stored data.JSONText,
	in updateCanvasConnectorInput,
) (map[string]any, *mcperr.McpError) {
	props := map[string]any{}
	if err := json.Unmarshal(stored, &props); err != nil {
		return nil, mcperr.New(mcperr.InternalError,
			"The connector's stored properties could not be read; the board may need repair in the app.")
	}
	if kind, _ := props["kind"].(string); kind != "connector" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"That element's stored properties are not a connector's. Use update_canvas_element for shapes and text.",
			"elementId")
	}

	named := false

	if in.Routing != nil {
		if e := checkCanvasRouting(*in.Routing); e != nil {
			return nil, e
		}
		props["routing"] = *in.Routing
		named = true
	}
	if in.Curvature != nil {
		if e := checkCanvasCurvature(*in.Curvature); e != nil {
			return nil, e
		}
		props["curvature"] = *in.Curvature
		named = true
	}

	ends := []struct {
		name      string
		elementID *string
		point     *canvasPointInput
	}{
		{"source", in.SourceElementID, in.SourcePoint},
		{"target", in.TargetElementID, in.TargetPoint},
	}
	for _, end := range ends {
		if end.elementID == nil && end.point == nil {
			continue // this end was not named; the stored one stands
		}
		if e := checkCanvasConnectorEnd(end.name, end.elementID, end.point); e != nil {
			return nil, e
		}
		applyCanvasConnectorEnd(props, end.name, end.elementID, end.point)
		named = true
	}

	if !named {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"Provide at least one field to change: routing, curvature, sourceElementId, sourcePoint, "+
				"targetElementId, or targetPoint.", "elementId")
	}

	// The two id keys are nullable but never optional, so a stored row missing
	// one would fail validation on the way back in.
	for _, key := range []string{"sourceElementId", "targetElementId"} {
		if _, ok := props[key]; !ok {
			props[key] = nil
		}
	}
	// A self connector can be formed by the MERGE — one end from the stored row,
	// the other from this request — without the caller ever naming both.
	if e := checkCanvasConnectorNotSelf(mergedConnectorID(props, "sourceElementId"),
		mergedConnectorID(props, "targetElementId")); e != nil {
		return nil, e
	}
	if routing, _ := props["routing"].(string); !schema.IsValidCanvasConnectorRouting(routing) {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("The connector's stored routing is unusable; supply routing as one of: %v.",
				schema.CanvasConnectorRoutings), "routing")
	}
	return props, nil
}

// mergedConnectorID reads one endpoint id out of the merged props: a string, or
// nil for a free end.
func mergedConnectorID(props map[string]any, key string) *string {
	s, ok := props[key].(string)
	if !ok {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// createCanvasConnector validates, gates, resolves both attached ends, then
// emits. The endpoint reads run AFTER the gate and exist because connector
// endpoint ids live in a JSON blob with no foreign key: a typo would persist as
// a connector that renders nowhere and can only be found by reading the board.
func createCanvasConnector(
	ctx context.Context,
	fns canvasConnectorFns,
	in createCanvasConnectorInput,
) (*mcp.CallToolResult, any, error) {
	// Validated before the gate and before any read, so a malformed request
	// never costs a database round trip. Matches create_canvas_element's order.
	if _, e := buildCanvasConnectorCreatePayload(in, nil); e != nil {
		return fail(e)
	}
	userID, err := fns.authorize(ctx, in.CanvasBoardID)
	if err != nil {
		return fail(err)
	}

	var sourceEl *data.CanvasElement
	if in.SourceElementID != nil {
		if sourceEl, err = loadCanvasElementOnBoard(ctx, fns, in.CanvasBoardID,
			"sourceElementId", *in.SourceElementID); err != nil {
			return fail(err)
		}
	}
	if in.TargetElementID != nil {
		if _, err = loadCanvasElementOnBoard(ctx, fns, in.CanvasBoardID,
			"targetElementId", *in.TargetElementID); err != nil {
			return fail(err)
		}
	}
	// Rebuilt with the resolved source so the placeholder sits at its centre.
	payload, e := buildCanvasConnectorCreatePayload(in, sourceEl)
	if e != nil {
		return fail(e)
	}

	return emitCanvasElementEventAs(ctx, userID, in.CanvasBoardID, "element:create", payload,
		"Server rejected canvas connector creation.")
}

// updateCanvasConnector reads the connector, merges the caller's changes into
// its stored props, and emits the whole props object as the replacement.
func updateCanvasConnector(
	ctx context.Context,
	fns canvasConnectorFns,
	in updateCanvasConnectorInput,
) (*mcp.CallToolResult, any, error) {
	if e := validateUUID("elementId", in.ElementID); e != nil {
		return fail(e)
	}
	userID, err := fns.authorize(ctx, in.CanvasBoardID)
	if err != nil {
		return fail(err)
	}
	el, err := loadCanvasConnectorForUpdate(ctx, fns, in.CanvasBoardID, in.ElementID)
	if err != nil {
		return fail(err)
	}
	props, e := mergeCanvasConnectorProps(el.Props, in)
	if e != nil {
		return fail(e)
	}
	// Geometry is NOT re-sent. The stored 1x1 placeholder is never read back, so
	// moving an endpoint changes nothing about it.
	payload := map[string]any{"elementId": in.ElementID, "props": props}

	return emitCanvasElementEventAs(ctx, userID, in.CanvasBoardID, "element:update", payload,
		"Server rejected canvas connector update.")
}

// RegisterCanvasConnectorTools registers create_canvas_connector and
// update_canvas_connector.
func RegisterCanvasConnectorTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_canvas_connector",
		Description: "Draw a connector line on a canvas board (freeform FigJam-style board, NOT an " +
			"ER diagram whiteboard). Each end is EITHER attached to an element (sourceElementId / " +
			"targetElementId) OR pinned to a free board coordinate (sourcePoint / targetPoint) — " +
			"supply exactly one form per end. A connector cannot join an element to itself. " +
			"routing is straight, elbow, or curved. The line's path is derived from its endpoints " +
			"live, so it follows the elements when they move. " +
			"For ER diagram foreign keys use create_relationship instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCanvasConnectorInput) (*mcp.CallToolResult, any, error) {
		return createCanvasConnector(ctx, prodCanvasConnectorFns(), in)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_canvas_connector",
		Description: "Re-route, re-bow, or move either end of an existing canvas connector (freeform " +
			"FigJam-style board, NOT an ER diagram whiteboard). Supply canvasBoardId, elementId, and " +
			"at least one of routing, curvature, sourceElementId, sourcePoint, targetElementId, or " +
			"targetPoint. Naming one end replaces that end and leaves the other alone; every " +
			"property you do not name — including the border attachment points set in the app — is " +
			"preserved. Use update_canvas_element to move, resize or restyle a shape.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCanvasConnectorInput) (*mcp.CallToolResult, any, error) {
		return updateCanvasConnector(ctx, prodCanvasConnectorFns(), in)
	})
}

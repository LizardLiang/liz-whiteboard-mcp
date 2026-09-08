// Package tools — canvas element write tool tests (Wave 2).
// Strategy matches canvas_read_test.go: exercise the DB-free and socket-free
// halves — the injectable EDITOR gate (canvasEditFns) and the pure payload
// builders. No DB and no Socket.IO server, so a VIEWER refusal is provably
// checked BEFORE any socket dial.
package tools

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
)

const canvasTestElementID = "aaaabbbb-aaaa-4aaa-8aaa-aaaabbbbcccc"

// canvasEditFnsOK builds a gate whose two stages both succeed, so each test can
// override exactly the stage it is about.
func canvasEditFnsOK() canvasEditFns {
	return canvasEditFns{
		resolveProject: func(context.Context, string) (string, error) {
			return canvasTestProjectID, nil
		},
		assertEdit: func(context.Context, string, string) error { return nil },
	}
}

func f64Ptr(f float64) *float64 { return &f }
func intPtr(i int) *int         { return &i }

func validCreateInput() createCanvasElementInput {
	return createCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		Kind:          "rectangle",
		PositionX:     10,
		PositionY:     20,
		Width:         100,
		Height:        50,
	}
}

// ---------------------------------------------------------------------------
// authorizeCanvasEdit — the EDITOR+ gate
// ---------------------------------------------------------------------------

// Writes must NOT reuse the read gate: a canvas mutation requires EDITOR+
// (a9b9e95), not mere project membership.
func TestProdCanvasEditFns_UseEditorGateAndCanvasResolver(t *testing.T) {
	fns := prodCanvasEditFns()

	assert.Equal(t,
		reflect.ValueOf(data.GetCanvasBoardProjectID).Pointer(),
		reflect.ValueOf(fns.resolveProject).Pointer(),
		"canvas writes must resolve the project through the CanvasBoard table")
	assert.Equal(t,
		reflect.ValueOf(auth.AssertSchemaEditAccess).Pointer(),
		reflect.ValueOf(fns.assertEdit).Pointer(),
		"canvas writes must gate on AssertSchemaEditAccess (EDITOR+), not AssertProjectAccess")
}

func TestAuthorizeCanvasEdit_RejectsNonUUID(t *testing.T) {
	fns := canvasEditFnsOK()
	fns.resolveProject = func(context.Context, string) (string, error) {
		t.Fatal("resolveProject must not run for an invalid UUID")
		return "", nil
	}

	err := authorizeCanvasEditWithFns(context.Background(), fns, canvasTestUserID, "not-a-uuid")

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "canvasBoardId", mcpErr.Field)
}

// An unknown board reads NOT_FOUND with the SAME message the read tools use,
// so a caller cannot tell the read and write paths apart by their errors.
func TestAuthorizeCanvasEdit_UnknownBoardIsNotFound(t *testing.T) {
	fns := canvasEditFnsOK()
	fns.resolveProject = func(context.Context, string) (string, error) { return "", nil }
	fns.assertEdit = func(context.Context, string, string) error {
		t.Fatal("the EDITOR gate must not run when the board does not exist")
		return nil
	}

	err := authorizeCanvasEditWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	mcpErr := requireMcpCode(t, err, mcperr.NotFound)
	assert.Equal(t, canvasBoardNotFound(canvasTestBoardID).Message, mcpErr.Message)
}

// The spec's "Viewer is refused before any socket dial": the refusal comes from
// the gate, and no socket package call exists on this path at all.
func TestAuthorizeCanvasEdit_ViewerIsForbidden(t *testing.T) {
	fns := canvasEditFnsOK()
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "Editor access required.")
	}

	err := authorizeCanvasEditWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	requireMcpCode(t, err, mcperr.Forbidden)
}

func TestAuthorizeCanvasEdit_ResolverErrorPropagates(t *testing.T) {
	sentinel := errors.New("db down")
	fns := canvasEditFnsOK()
	fns.resolveProject = func(context.Context, string) (string, error) { return "", sentinel }

	err := authorizeCanvasEditWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	assert.ErrorIs(t, err, sentinel)
}

func TestAuthorizeCanvasEdit_EditorPasses(t *testing.T) {
	gated := false
	fns := canvasEditFnsOK()
	fns.assertEdit = func(_ context.Context, userID, projectID string) error {
		gated = true
		assert.Equal(t, canvasTestUserID, userID)
		assert.Equal(t, canvasTestProjectID, projectID)
		return nil
	}

	err := authorizeCanvasEditWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	require.NoError(t, err)
	assert.True(t, gated, "the EDITOR gate must actually run")
}

// ---------------------------------------------------------------------------
// create payload — server-owned fields and the props/kind invariant
// ---------------------------------------------------------------------------

// The collaboration server computes zIndex (MAX+1), generates the id, forces
// rotation to 0, and treats minRevision as undo-restore-only. Sending any of
// them buys nothing and risks tripping the restore-gated branch.
func TestBuildCanvasElementCreatePayload_OmitsServerOwnedFields(t *testing.T) {
	payload, e := buildCanvasElementCreatePayload(validCreateInput())

	require.Nil(t, e)
	for _, key := range []string{"id", "zIndex", "rotation", "minRevision", "boardId"} {
		assert.NotContains(t, payload, key, key+" is server-owned and must not be sent")
	}
}

// createCanvasElementSchema's .refine requires kind === props.kind, and every
// supported kind's props arm is a strictObject holding nothing else.
func TestBuildCanvasElementCreatePayload_PropsKindMatchesKind(t *testing.T) {
	for _, kind := range schema.CanvasElementKinds {
		in := validCreateInput()
		in.Kind = kind

		payload, e := buildCanvasElementCreatePayload(in)

		require.Nil(t, e, kind)
		assert.Equal(t, kind, payload["kind"])
		assert.Equal(t, map[string]any{"kind": kind}, payload["props"],
			"props for %s must be exactly {kind}", kind)
	}
}

func TestBuildCanvasElementCreatePayload_CarriesGeometry(t *testing.T) {
	payload, e := buildCanvasElementCreatePayload(validCreateInput())

	require.Nil(t, e)
	assert.Equal(t, 10.0, payload["positionX"])
	assert.Equal(t, 20.0, payload["positionY"])
	assert.Equal(t, 100.0, payload["width"])
	assert.Equal(t, 50.0, payload["height"])
}

func TestBuildCanvasElementCreatePayload_RejectsUnsupportedKinds(t *testing.T) {
	// connector has its own tool; group is out of scope. Both are valid kinds
	// server-side, so only this check keeps them off the generic create.
	for _, kind := range []string{"connector", "group", "", "Rectangle", "circle"} {
		in := validCreateInput()
		in.Kind = kind

		_, e := buildCanvasElementCreatePayload(in)

		require.NotNil(t, e, "kind %q must be refused", kind)
		assert.Equal(t, mcperr.ValidationError, e.Code)
		assert.Equal(t, "kind", e.Field)
	}
}

func TestBuildCanvasElementCreatePayload_RejectsNonFinitePositions(t *testing.T) {
	cases := []struct {
		field string
		set   func(*createCanvasElementInput, float64)
	}{
		{"positionX", func(in *createCanvasElementInput, v float64) { in.PositionX = v }},
		{"positionY", func(in *createCanvasElementInput, v float64) { in.PositionY = v }},
	}
	for _, c := range cases {
		for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
			in := validCreateInput()
			c.set(&in, v)

			_, e := buildCanvasElementCreatePayload(in)

			require.NotNil(t, e, c.field)
			assert.Equal(t, c.field, e.Field)
		}
	}
}

// boardCoordSchema bounds every coordinate at ±10,000,000.
func TestBuildCanvasElementCreatePayload_RejectsOutOfBoundsPositions(t *testing.T) {
	for _, v := range []float64{canvasCoordMax + 1, -canvasCoordMax - 1} {
		in := validCreateInput()
		in.PositionX = v

		_, e := buildCanvasElementCreatePayload(in)

		require.NotNil(t, e)
		assert.Equal(t, "positionX", e.Field)
	}

	in := validCreateInput()
	in.PositionY = canvasCoordMax
	_, e := buildCanvasElementCreatePayload(in)
	assert.Nil(t, e, "the bound itself is accepted")
}

func TestBuildCanvasElementCreatePayload_RejectsNonPositiveOrOversizeDimensions(t *testing.T) {
	cases := []struct {
		field string
		set   func(*createCanvasElementInput, float64)
	}{
		{"width", func(in *createCanvasElementInput, v float64) { in.Width = v }},
		{"height", func(in *createCanvasElementInput, v float64) { in.Height = v }},
	}
	for _, c := range cases {
		for _, v := range []float64{0, -1, canvasSizeMax + 1, math.NaN()} {
			in := validCreateInput()
			c.set(&in, v)

			_, e := buildCanvasElementCreatePayload(in)

			require.NotNil(t, e, "%s = %v must be refused", c.field, v)
			assert.Equal(t, c.field, e.Field)
		}

		in := validCreateInput()
		c.set(&in, canvasSizeMax)
		_, e := buildCanvasElementCreatePayload(in)
		assert.Nil(t, e, "%s at the ceiling is accepted", c.field)
	}
}

// text is .default(null) server-side, so an absent text must stay absent rather
// than be sent as an empty string.
func TestBuildCanvasElementCreatePayload_OmitsAbsentText(t *testing.T) {
	payload, e := buildCanvasElementCreatePayload(validCreateInput())

	require.Nil(t, e)
	assert.NotContains(t, payload, "text")
}

func TestBuildCanvasElementCreatePayload_CarriesText(t *testing.T) {
	in := validCreateInput()
	in.Kind = "text"
	in.Text = strPtr("hello")

	payload, e := buildCanvasElementCreatePayload(in)

	require.Nil(t, e)
	assert.Equal(t, "hello", payload["text"])
}

func TestBuildCanvasElementCreatePayload_RejectsOverLongText(t *testing.T) {
	in := validCreateInput()
	in.Text = strPtr(strings.Repeat("x", canvasTextMaxLength+1))

	_, e := buildCanvasElementCreatePayload(in)

	require.NotNil(t, e)
	assert.Equal(t, "text", e.Field)

	in.Text = strPtr(strings.Repeat("x", canvasTextMaxLength))
	_, e = buildCanvasElementCreatePayload(in)
	assert.Nil(t, e, "text at the limit is accepted")
}

// ---------------------------------------------------------------------------
// update payload
// ---------------------------------------------------------------------------

func TestBuildCanvasElementUpdatePayload_CarriesElementIDAndOnlySuppliedFields(t *testing.T) {
	payload, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     canvasTestElementID,
		PositionX:     f64Ptr(42),
	})

	require.Nil(t, e)
	assert.Equal(t, canvasTestElementID, payload["elementId"])
	assert.Equal(t, 42.0, payload["positionX"])
	for _, key := range []string{"positionY", "width", "height", "zIndex", "text", "style"} {
		assert.NotContains(t, payload, key, "an unsupplied field must not be written")
	}
}

// MCP edits are ordinary forward edits under last-write-wins. expectedRevision
// is undo's guard and must never be sent, or a REVISION_MISMATCH becomes
// reachable from a tool that has no revision to expect.
func TestBuildCanvasElementUpdatePayload_NeverSendsExpectedRevision(t *testing.T) {
	payload, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     canvasTestElementID,
		ZIndex:        intPtr(3),
	})

	require.Nil(t, e)
	assert.NotContains(t, payload, "expectedRevision")
	assert.NotContains(t, payload, "boardId")
}

func TestBuildCanvasElementUpdatePayload_RejectsNonUUIDElementID(t *testing.T) {
	_, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     "not-a-uuid",
		PositionX:     f64Ptr(1),
	})

	require.NotNil(t, e)
	assert.Equal(t, "elementId", e.Field)
}

// An update naming no field is a wasted round trip that would still bump the
// row's revision server-side; refuse it locally instead.
func TestBuildCanvasElementUpdatePayload_RejectsEmptyUpdate(t *testing.T) {
	_, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     canvasTestElementID,
	})

	require.NotNil(t, e)
	assert.Equal(t, mcperr.ValidationError, e.Code)
}

func TestBuildCanvasElementUpdatePayload_EnforcesZIndexBounds(t *testing.T) {
	for _, z := range []int{canvasZIndexMin - 1, canvasZIndexMax + 1} {
		_, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
			CanvasBoardID: canvasTestBoardID,
			ElementID:     canvasTestElementID,
			ZIndex:        intPtr(z),
		})

		require.NotNil(t, e, "zIndex %d must be refused", z)
		assert.Equal(t, "zIndex", e.Field)
	}

	for _, z := range []int{canvasZIndexMin, 0, canvasZIndexMax} {
		payload, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
			CanvasBoardID: canvasTestBoardID,
			ElementID:     canvasTestElementID,
			ZIndex:        intPtr(z),
		})

		require.Nil(t, e, "zIndex %d is in bounds", z)
		assert.Equal(t, z, payload["zIndex"])
	}
}

func TestBuildCanvasElementUpdatePayload_ValidatesGeometryLikeCreate(t *testing.T) {
	_, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     canvasTestElementID,
		Width:         f64Ptr(0),
	})
	require.NotNil(t, e)
	assert.Equal(t, "width", e.Field)

	_, e = buildCanvasElementUpdatePayload(updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     canvasTestElementID,
		PositionY:     f64Ptr(canvasCoordMax + 1),
	})
	require.NotNil(t, e)
	assert.Equal(t, "positionY", e.Field)
}

// An empty string is how a caller clears text: the tool takes no null, because a
// JSON null and an omitted key are indistinguishable once decoded into a pointer.
func TestBuildCanvasElementUpdatePayload_EmptyTextIsAClear(t *testing.T) {
	payload, e := buildCanvasElementUpdatePayload(updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     canvasTestElementID,
		Text:          strPtr(""),
	})

	require.Nil(t, e)
	assert.Equal(t, "", payload["text"])
}

// ---------------------------------------------------------------------------
// style — the app's canvasElementStyleSchema is a strictObject
// ---------------------------------------------------------------------------

// Unknown keys would be rejected by the strictObject, so the tool exposes the
// exact key set and nothing else. Only supplied keys are emitted.
func TestBuildCanvasStyle_EmitsOnlySuppliedKeys(t *testing.T) {
	style, e := buildCanvasStyle(&canvasStyleInput{
		Fill:        strPtr("#ffffff"),
		StrokeWidth: f64Ptr(4),
	})

	require.Nil(t, e)
	assert.Equal(t, map[string]any{"fill": "#ffffff", "strokeWidth": 4.0}, style)
}

func TestBuildCanvasStyle_NilStyleEmitsNothing(t *testing.T) {
	style, e := buildCanvasStyle(nil)

	require.Nil(t, e)
	assert.Nil(t, style)
}

func TestBuildCanvasStyle_RejectsOutOfRangeNumbers(t *testing.T) {
	cases := []struct {
		field string
		style canvasStyleInput
	}{
		{"strokeWidth", canvasStyleInput{StrokeWidth: f64Ptr(-1)}},
		{"strokeWidth", canvasStyleInput{StrokeWidth: f64Ptr(65)}},
		{"fontSize", canvasStyleInput{FontSize: f64Ptr(0)}},
		{"fontSize", canvasStyleInput{FontSize: f64Ptr(513)}},
		{"cornerRadius", canvasStyleInput{CornerRadius: f64Ptr(-1)}},
		{"cornerRadius", canvasStyleInput{CornerRadius: f64Ptr(513)}},
	}
	for _, c := range cases {
		style := c.style
		_, e := buildCanvasStyle(&style)

		require.NotNil(t, e, c.field)
		assert.Equal(t, c.field, e.Field)
	}
}

func TestBuildCanvasStyle_RejectsBadColorsAndAlignments(t *testing.T) {
	_, e := buildCanvasStyle(&canvasStyleInput{Fill: strPtr("")})
	require.NotNil(t, e)
	assert.Equal(t, "fill", e.Field)

	_, e = buildCanvasStyle(&canvasStyleInput{Stroke: strPtr(strings.Repeat("x", 65))})
	require.NotNil(t, e)
	assert.Equal(t, "stroke", e.Field)

	_, e = buildCanvasStyle(&canvasStyleInput{TextAlign: strPtr("justify")})
	require.NotNil(t, e)
	assert.Equal(t, "textAlign", e.Field)

	_, e = buildCanvasStyle(&canvasStyleInput{VerticalAlign: strPtr("centre")})
	require.NotNil(t, e)
	assert.Equal(t, "verticalAlign", e.Field)

	style, e := buildCanvasStyle(&canvasStyleInput{
		TextAlign:     strPtr("center"),
		VerticalAlign: strPtr("middle"),
		Color:         strPtr("#0f172a"),
	})
	require.Nil(t, e)
	assert.Equal(t, map[string]any{"textAlign": "center", "verticalAlign": "middle", "color": "#0f172a"}, style)
}

func TestBuildCanvasElementCreatePayload_CarriesValidatedStyle(t *testing.T) {
	in := validCreateInput()
	in.Style = &canvasStyleInput{Fill: strPtr("#eeeeee")}

	payload, e := buildCanvasElementCreatePayload(in)

	require.Nil(t, e)
	assert.Equal(t, map[string]any{"fill": "#eeeeee"}, payload["style"])
}

func TestBuildCanvasElementCreatePayload_PropagatesStyleErrors(t *testing.T) {
	in := validCreateInput()
	in.Style = &canvasStyleInput{FontSize: f64Ptr(0)}

	_, e := buildCanvasElementCreatePayload(in)

	require.NotNil(t, e)
	assert.Equal(t, "fontSize", e.Field)
}

// ---------------------------------------------------------------------------
// ack mapping
// ---------------------------------------------------------------------------

// The canvas ack union adds REVISION_MISMATCH to the codes the ER tools already
// handle. It maps to VALIDATION_ERROR through the SHARED mapper (no second
// mapper), and gets a message that says what actually happened.
func TestCanvasAckCodeMapping(t *testing.T) {
	assert.Equal(t, mcperr.Forbidden, mcperr.AckCodeToMcpCode("FORBIDDEN"))
	assert.Equal(t, mcperr.NotFound, mcperr.AckCodeToMcpCode("NOT_FOUND"))
	assert.Equal(t, mcperr.SessionExpired, mcperr.AckCodeToMcpCode("SESSION_EXPIRED"))
	assert.Equal(t, mcperr.ValidationError, mcperr.AckCodeToMcpCode("VALIDATION_ERROR"))
	assert.Equal(t, mcperr.ValidationError, mcperr.AckCodeToMcpCode("INTERNAL_ERROR"))
	assert.Equal(t, mcperr.ValidationError, mcperr.AckCodeToMcpCode("REVISION_MISMATCH"))
}

func TestCanvasAckMessage_ExplainsRevisionMismatch(t *testing.T) {
	msg := canvasAckMessage("REVISION_MISMATCH", "Canvas element X is at revision 4, expected 2")

	assert.Contains(t, msg, "changed on the server")
	assert.Contains(t, msg, "Canvas element X is at revision 4, expected 2",
		"the server's own message must survive")
}

func TestCanvasAckMessage_PassesOtherCodesThrough(t *testing.T) {
	assert.Equal(t, "Insufficient permission", canvasAckMessage("FORBIDDEN", "Insufficient permission"))
	assert.Equal(t, "boom", canvasAckMessage("", "boom"))
}

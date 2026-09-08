// Package tools — canvas connector tool tests (Wave 3).
//
// Strategy matches canvas_write_test.go: exercise the DB-free, socket-free
// halves — the injectable gate, the pure endpoint validators, the props builder
// and, above all, the props MERGE. The merge is the one place in this feature
// where a correct-looking write silently destroys data: the app's
// updateCanvasElementSchema takes props as a REPLACEMENT, so a merge that drops
// sourceAttach / targetAttach / sourceAnchor / targetAnchor / curvature
// un-anchors and un-bows a connector that was perfectly fine before.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const (
	cnSourceID  = "11111111-1111-4111-8111-111111111111"
	cnTargetID  = "22222222-2222-4222-8222-222222222222"
	cnConnectID = "33333333-3333-4333-8333-333333333333"
)

func pt(x, y float64) *canvasPointInput { return &canvasPointInput{X: x, Y: y} }

// attachedCreateInput joins two elements with an elbow line — the ordinary case.
func attachedCreateInput() createCanvasConnectorInput {
	return createCanvasConnectorInput{
		CanvasBoardID:   canvasTestBoardID,
		SourceElementID: strPtr(cnSourceID),
		TargetElementID: strPtr(cnTargetID),
		Routing:         "elbow",
	}
}

func sourceElement() *data.CanvasElement {
	return &data.CanvasElement{
		ID: cnSourceID, BoardID: canvasTestBoardID, Kind: "rectangle",
		PositionX: 100, PositionY: 200, Width: 180, Height: 80,
	}
}

// ---------------------------------------------------------------------------
// Endpoint invariants — the reason connectors get their own tool
// ---------------------------------------------------------------------------

func TestCheckCanvasConnectorEnd_RejectsBothForms(t *testing.T) {
	e := checkCanvasConnectorEnd("source", strPtr(cnSourceID), pt(10, 10))
	require.NotNil(t, e, "an end may not be attached AND free")
	assert.Equal(t, mcperr.ValidationError, e.Code)
	assert.Equal(t, "sourceElementId", e.Field)
}

func TestCheckCanvasConnectorEnd_RejectsNeitherForm(t *testing.T) {
	e := checkCanvasConnectorEnd("target", nil, nil)
	require.NotNil(t, e, "an end must be attached OR free")
	assert.Equal(t, mcperr.ValidationError, e.Code)
	assert.Equal(t, "targetElementId", e.Field)
}

func TestCheckCanvasConnectorEnd_AcceptsEitherFormAlone(t *testing.T) {
	assert.Nil(t, checkCanvasConnectorEnd("source", strPtr(cnSourceID), nil))
	assert.Nil(t, checkCanvasConnectorEnd("target", nil, pt(-5, 12.5)))
}

func TestCheckCanvasConnectorEnd_RejectsNonUUIDElementID(t *testing.T) {
	e := checkCanvasConnectorEnd("source", strPtr("not-a-uuid"), nil)
	require.NotNil(t, e)
	assert.Equal(t, "sourceElementId", e.Field)
}

func TestCheckCanvasConnectorEnd_RejectsOutOfRangePoint(t *testing.T) {
	cases := []struct {
		name  string
		point *canvasPointInput
		field string
	}{
		{"x too large", pt(canvasCoordMax+1, 0), "targetPoint.x"},
		{"y too small", pt(0, -canvasCoordMax-1), "targetPoint.y"},
		{"x not finite", pt(math.Inf(1), 0), "targetPoint.x"},
		{"y NaN", pt(0, math.NaN()), "targetPoint.y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := checkCanvasConnectorEnd("target", nil, tc.point)
			require.NotNil(t, e)
			assert.Equal(t, tc.field, e.Field)
		})
	}
}

func TestCheckCanvasConnectorNotSelf(t *testing.T) {
	e := checkCanvasConnectorNotSelf(strPtr(cnSourceID), strPtr(cnSourceID))
	require.NotNil(t, e, "a self connector has no drawable path")
	assert.Equal(t, "targetElementId", e.Field)

	assert.Nil(t, checkCanvasConnectorNotSelf(strPtr(cnSourceID), strPtr(cnTargetID)))
	// Two free ends are both absent; that is an ordinary floating line, not a
	// self connector.
	assert.Nil(t, checkCanvasConnectorNotSelf(nil, nil))
}

func TestCheckCanvasCurvature(t *testing.T) {
	assert.Nil(t, checkCanvasCurvature(0))
	assert.Nil(t, checkCanvasCurvature(canvasCurvatureLimit))
	assert.Nil(t, checkCanvasCurvature(-canvasCurvatureLimit))

	for _, v := range []float64{canvasCurvatureLimit + 0.01, -canvasCurvatureLimit - 0.01, math.NaN(), math.Inf(-1)} {
		e := checkCanvasCurvature(v)
		require.NotNil(t, e, "%v must be refused", v)
		assert.Equal(t, "curvature", e.Field)
	}
}

// ---------------------------------------------------------------------------
// Create — props construction
// ---------------------------------------------------------------------------

func TestBuildCanvasConnectorProps_AttachedEnds(t *testing.T) {
	props, e := buildCanvasConnectorProps(attachedCreateInput())
	require.Nil(t, e)

	assert.Equal(t, "connector", props["kind"], "props.kind must equal the element kind")
	assert.Equal(t, cnSourceID, props["sourceElementId"])
	assert.Equal(t, cnTargetID, props["targetElementId"])
	assert.Equal(t, "elbow", props["routing"])

	for _, absent := range []string{"sourcePoint", "targetPoint", "curvature",
		"sourceAttach", "targetAttach", "sourceAnchor", "targetAnchor"} {
		_, ok := props[absent]
		assert.False(t, ok, "%s must be absent on a freshly created connector", absent)
	}
}

// The two id keys are NULLABLE but not optional in the app's schema, so a free
// end must send an explicit null rather than omit the key.
func TestBuildCanvasConnectorProps_FreeEndSendsExplicitNullID(t *testing.T) {
	in := createCanvasConnectorInput{
		CanvasBoardID: canvasTestBoardID,
		SourcePoint:   pt(10, 20),
		TargetPoint:   pt(30, 40.5),
		Routing:       "straight",
	}
	props, e := buildCanvasConnectorProps(in)
	require.Nil(t, e)

	raw, err := json.Marshal(props)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"kind":"connector","sourceElementId":null,"targetElementId":null,`+
			`"sourcePoint":{"x":10,"y":20},"targetPoint":{"x":30,"y":40.5},"routing":"straight"}`,
		string(raw))
}

func TestBuildCanvasConnectorProps_CarriesCurvatureOnlyWhenSupplied(t *testing.T) {
	in := attachedCreateInput()
	props, e := buildCanvasConnectorProps(in)
	require.Nil(t, e)
	_, ok := props["curvature"]
	assert.False(t, ok, "an un-bowed connector writes NO curvature key")

	in.Curvature = f64Ptr(0.25)
	props, e = buildCanvasConnectorProps(in)
	require.Nil(t, e)
	assert.Equal(t, 0.25, props["curvature"])

	// A deliberate 0 is a value, not an absence.
	in.Curvature = f64Ptr(0)
	props, e = buildCanvasConnectorProps(in)
	require.Nil(t, e)
	assert.Equal(t, 0.0, props["curvature"])
}

func TestBuildCanvasConnectorProps_RejectsInvalidRouting(t *testing.T) {
	in := attachedCreateInput()
	in.Routing = "orthogonal"
	_, e := buildCanvasConnectorProps(in)
	require.NotNil(t, e)
	assert.Equal(t, "routing", e.Field)
}

func TestBuildCanvasConnectorProps_PropagatesEndpointErrors(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*createCanvasConnectorInput)
		field string
	}{
		{"both source forms", func(in *createCanvasConnectorInput) { in.SourcePoint = pt(1, 1) }, "sourceElementId"},
		{"no target form", func(in *createCanvasConnectorInput) { in.TargetElementID = nil }, "targetElementId"},
		{"self connector", func(in *createCanvasConnectorInput) { in.TargetElementID = strPtr(cnSourceID) }, "targetElementId"},
		{"bad curvature", func(in *createCanvasConnectorInput) { in.Curvature = f64Ptr(99) }, "curvature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := attachedCreateInput()
			tc.mut(&in)
			_, e := buildCanvasConnectorProps(in)
			require.NotNil(t, e)
			assert.Equal(t, mcperr.ValidationError, e.Code)
			assert.Equal(t, tc.field, e.Field)
		})
	}
}

// ---------------------------------------------------------------------------
// Create — the degenerate placeholder geometry
// ---------------------------------------------------------------------------

func TestCanvasConnectorPlaceholder_AtSourceElementCentre(t *testing.T) {
	x, y := canvasConnectorPlaceholder(sourceElement(), nil)
	assert.Equal(t, 190.0, x, "100 + 180/2")
	assert.Equal(t, 240.0, y, "200 + 80/2")
}

func TestCanvasConnectorPlaceholder_AtFreeSourcePoint(t *testing.T) {
	x, y := canvasConnectorPlaceholder(nil, pt(-12.5, 7))
	assert.Equal(t, -12.5, x)
	assert.Equal(t, 7.0, y)
}

// An element sitting at the coordinate ceiling has a centre BEYOND it, and
// boardCoordSchema would reject the placeholder — for a value nothing ever reads.
func TestCanvasConnectorPlaceholder_ClampsToBoardRange(t *testing.T) {
	el := sourceElement()
	el.PositionX = canvasCoordMax
	el.PositionY = -canvasCoordMax
	x, y := canvasConnectorPlaceholder(el, nil)
	assert.Equal(t, float64(canvasCoordMax), x, "the centre would be 1e7+90, past the ceiling")
	assert.Equal(t, float64(-canvasCoordMax)+40, y, "this centre is inside the range, so it stands")

	assert.Equal(t, float64(-canvasCoordMax), clampCanvasCoord(-canvasCoordMax-1))
	assert.Equal(t, 12.5, clampCanvasCoord(12.5))
}

func TestBuildCanvasConnectorCreatePayload_ShapeAndServerOwnedFields(t *testing.T) {
	payload, e := buildCanvasConnectorCreatePayload(attachedCreateInput(), sourceElement())
	require.Nil(t, e)

	assert.Equal(t, "connector", payload["kind"])
	assert.Equal(t, 190.0, payload["positionX"])
	assert.Equal(t, 240.0, payload["positionY"])
	assert.Equal(t, 1.0, payload["width"], "width is .positive(), so the placeholder is 1 and never 0")
	assert.Equal(t, 1.0, payload["height"])

	props, ok := payload["props"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, payload["kind"], props["kind"])

	for _, serverOwned := range []string{"id", "zIndex", "rotation", "minRevision", "boardId", "text", "style"} {
		_, present := payload[serverOwned]
		assert.False(t, present, "%s is the server's to decide", serverOwned)
	}
}

// ---------------------------------------------------------------------------
// Update — the props merge. The whole point of Wave 3's read-before-write.
// ---------------------------------------------------------------------------

// storedConnectorProps is a connector that has been anchored and hand-bowed —
// exactly the row a careless replacement write would ruin.
const storedConnectorProps = `{
  "kind": "connector",
  "sourceElementId": "11111111-1111-4111-8111-111111111111",
  "targetElementId": "22222222-2222-4222-8222-222222222222",
  "sourceAttach": {"x": 1, "y": 0.5},
  "targetAttach": {"x": 0, "y": 0.25},
  "sourceAnchor": "right",
  "targetAnchor": "left",
  "routing": "elbow",
  "curvature": 0.3
}`

func updateRoutingOnly() updateCanvasConnectorInput {
	return updateCanvasConnectorInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     cnConnectID,
		Routing:       strPtr("curved"),
	}
}

func TestMergeCanvasConnectorProps_PreservesEveryUntouchedKey(t *testing.T) {
	merged, e := mergeCanvasConnectorProps(data.JSONText(storedConnectorProps), updateRoutingOnly())
	require.Nil(t, e)

	assert.Equal(t, "curved", merged["routing"], "the one field the caller named")

	// Everything else must survive byte for byte. Dropping any of these
	// un-anchors or un-bows a connector the caller only meant to re-route.
	assert.Equal(t, "connector", merged["kind"])
	assert.Equal(t, cnSourceID, merged["sourceElementId"])
	assert.Equal(t, cnTargetID, merged["targetElementId"])
	assert.Equal(t, map[string]any{"x": 1.0, "y": 0.5}, merged["sourceAttach"])
	assert.Equal(t, map[string]any{"x": 0.0, "y": 0.25}, merged["targetAttach"])
	assert.Equal(t, "right", merged["sourceAnchor"])
	assert.Equal(t, "left", merged["targetAnchor"])
	assert.Equal(t, 0.3, merged["curvature"])
}

// A key this tool has never heard of must survive too: the app may add one, and
// a replacement write assembled from a fixed field list would delete it.
func TestMergeCanvasConnectorProps_PreservesUnknownKeys(t *testing.T) {
	stored := `{"kind":"connector","sourceElementId":null,"targetElementId":null,` +
		`"sourcePoint":{"x":1,"y":2},"targetPoint":{"x":3,"y":4},"routing":"straight",` +
		`"someFutureKey":{"nested":true}}`
	merged, e := mergeCanvasConnectorProps(data.JSONText(stored), updateRoutingOnly())
	require.Nil(t, e)
	assert.Equal(t, map[string]any{"nested": true}, merged["someFutureKey"])
}

// Re-pointing an end REPLACES that end: the opposing form must go, or the merged
// props carry both an id and a point and fail the schema's exactly-one refine.
func TestMergeCanvasConnectorProps_AttachingAFreeEndDropsItsPoint(t *testing.T) {
	stored := `{"kind":"connector","sourceElementId":null,"targetElementId":"` + cnTargetID + `",` +
		`"sourcePoint":{"x":1,"y":2},"routing":"straight","curvature":0.5}`
	in := updateCanvasConnectorInput{
		CanvasBoardID:   canvasTestBoardID,
		ElementID:       cnConnectID,
		SourceElementID: strPtr(cnSourceID),
	}
	merged, e := mergeCanvasConnectorProps(data.JSONText(stored), in)
	require.Nil(t, e)

	assert.Equal(t, cnSourceID, merged["sourceElementId"])
	_, hasPoint := merged["sourcePoint"]
	assert.False(t, hasPoint, "the end is attached now; a leftover sourcePoint fails the schema")
	assert.Equal(t, cnTargetID, merged["targetElementId"], "the other end is untouched")
	assert.Equal(t, 0.5, merged["curvature"])
}

func TestMergeCanvasConnectorProps_DetachingAnEndNullsItsID(t *testing.T) {
	in := updateCanvasConnectorInput{
		CanvasBoardID: canvasTestBoardID,
		ElementID:     cnConnectID,
		TargetPoint:   pt(900, 950),
	}
	merged, e := mergeCanvasConnectorProps(data.JSONText(storedConnectorProps), in)
	require.Nil(t, e)

	assert.Nil(t, merged["targetElementId"], "a free end stores an explicit null id")
	assert.Equal(t, map[string]any{"x": 900.0, "y": 950.0}, merged["targetPoint"])
	assert.Equal(t, cnSourceID, merged["sourceElementId"], "the other end is untouched")
	assert.Equal(t, 0.3, merged["curvature"])
}

func TestMergeCanvasConnectorProps_UpdatesCurvature(t *testing.T) {
	in := updateRoutingOnly()
	in.Routing = nil
	in.Curvature = f64Ptr(-1.5)
	merged, e := mergeCanvasConnectorProps(data.JSONText(storedConnectorProps), in)
	require.Nil(t, e)
	assert.Equal(t, -1.5, merged["curvature"])
	assert.Equal(t, "elbow", merged["routing"], "routing was not named, so it stands")
}

func TestMergeCanvasConnectorProps_RejectsEmptyUpdate(t *testing.T) {
	in := updateCanvasConnectorInput{CanvasBoardID: canvasTestBoardID, ElementID: cnConnectID}
	_, e := mergeCanvasConnectorProps(data.JSONText(storedConnectorProps), in)
	require.NotNil(t, e, "an update naming no field would bump the revision and broadcast a no-op")
	assert.Equal(t, mcperr.ValidationError, e.Code)
}

func TestMergeCanvasConnectorProps_RejectsBothFormsForOneEnd(t *testing.T) {
	in := updateCanvasConnectorInput{
		CanvasBoardID:   canvasTestBoardID,
		ElementID:       cnConnectID,
		SourceElementID: strPtr(cnSourceID),
		SourcePoint:     pt(1, 2),
	}
	_, e := mergeCanvasConnectorProps(data.JSONText(storedConnectorProps), in)
	require.NotNil(t, e)
	assert.Equal(t, "sourceElementId", e.Field)
}

// The merge can create a self connector the caller never spelled out: one end
// comes from the stored row, the other from this request.
func TestMergeCanvasConnectorProps_RejectsSelfConnectorFormedByTheMerge(t *testing.T) {
	in := updateCanvasConnectorInput{
		CanvasBoardID:   canvasTestBoardID,
		ElementID:       cnConnectID,
		SourceElementID: strPtr(cnTargetID), // stored target is already cnTargetID
	}
	_, e := mergeCanvasConnectorProps(data.JSONText(storedConnectorProps), in)
	require.NotNil(t, e, "the merged props would join an element to itself")
	assert.Equal(t, "targetElementId", e.Field)
}

func TestMergeCanvasConnectorProps_RejectsInvalidRoutingAndPoints(t *testing.T) {
	cases := []struct {
		name  string
		in    updateCanvasConnectorInput
		field string
	}{
		{"routing", updateCanvasConnectorInput{Routing: strPtr("zigzag")}, "routing"},
		{"point range", updateCanvasConnectorInput{TargetPoint: pt(canvasCoordMax+1, 0)}, "targetPoint.x"},
		{"element id", updateCanvasConnectorInput{SourceElementID: strPtr("nope")}, "sourceElementId"},
		{"curvature", updateCanvasConnectorInput{Curvature: f64Ptr(7)}, "curvature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			in.CanvasBoardID = canvasTestBoardID
			in.ElementID = cnConnectID
			_, e := mergeCanvasConnectorProps(data.JSONText(storedConnectorProps), in)
			require.NotNil(t, e)
			assert.Equal(t, tc.field, e.Field)
		})
	}
}

func TestMergeCanvasConnectorProps_RejectsUnreadableStoredProps(t *testing.T) {
	_, e := mergeCanvasConnectorProps(data.JSONText(`not json`), updateRoutingOnly())
	require.NotNil(t, e)
	assert.Equal(t, mcperr.InternalError, e.Code)
}

// A row whose props carry another kind is not this tool's element.
func TestMergeCanvasConnectorProps_RejectsNonConnectorProps(t *testing.T) {
	_, e := mergeCanvasConnectorProps(data.JSONText(`{"kind":"rectangle"}`), updateRoutingOnly())
	require.NotNil(t, e)
	assert.Equal(t, mcperr.ValidationError, e.Code)
	assert.Contains(t, e.Message, "update_canvas_element")
}

// ---------------------------------------------------------------------------
// The gate and the element loader
// ---------------------------------------------------------------------------

func TestProdCanvasConnectorFns_UseTheEditorGateAndTheCanvasElementReader(t *testing.T) {
	fns := prodCanvasConnectorFns()
	assert.Equal(t,
		reflect.ValueOf(authorizeCanvasEdit).Pointer(),
		reflect.ValueOf(fns.authorize).Pointer(),
		"connector writes must run the same EDITOR+ gate as every other canvas write")
	assert.Equal(t,
		reflect.ValueOf(data.FindCanvasElementByID).Pointer(),
		reflect.ValueOf(fns.loadElement).Pointer())
}

func canvasConnectorFnsOK(el *data.CanvasElement) canvasConnectorFns {
	return canvasConnectorFns{
		authorize: func(context.Context, string) (string, error) {
			return canvasTestUserID, nil
		},
		loadElement: func(context.Context, string) (*data.CanvasElement, error) {
			return el, nil
		},
	}
}

func TestLoadCanvasElementOnBoard_ReturnsTheElement(t *testing.T) {
	fns := canvasConnectorFnsOK(sourceElement())
	el, err := loadCanvasElementOnBoard(context.Background(), fns, canvasTestBoardID, "sourceElementId", cnSourceID)
	require.NoError(t, err)
	require.NotNil(t, el)
	assert.Equal(t, cnSourceID, el.ID)
}

// A missing element and an element on ANOTHER board report the same NOT_FOUND —
// the collaboration server's own loadOwnedElement makes no distinction either.
func TestLoadCanvasElementOnBoard_MissingAndForeignBothNotFound(t *testing.T) {
	foreign := sourceElement()
	foreign.BoardID = "99999999-9999-4999-8999-999999999999"

	cases := map[string]*data.CanvasElement{"missing": nil, "another board": foreign}
	for name, el := range cases {
		t.Run(name, func(t *testing.T) {
			fns := canvasConnectorFnsOK(el)
			_, err := loadCanvasElementOnBoard(context.Background(), fns, canvasTestBoardID, "sourceElementId", cnSourceID)
			require.Error(t, err)
			var me *mcperr.McpError
			require.True(t, errors.As(err, &me))
			assert.Equal(t, mcperr.NotFound, me.Code)
			assert.Equal(t, "sourceElementId", me.Field)
			assert.True(t, strings.Contains(me.Message, cnSourceID), "the message names the id")
		})
	}
}

func TestLoadCanvasElementOnBoard_PropagatesReadErrors(t *testing.T) {
	boom := errors.New("db down")
	fns := canvasConnectorFns{
		authorize:   func(context.Context, string) (string, error) { return canvasTestUserID, nil },
		loadElement: func(context.Context, string) (*data.CanvasElement, error) { return nil, boom },
	}
	_, err := loadCanvasElementOnBoard(context.Background(), fns, canvasTestBoardID, "sourceElementId", cnSourceID)
	assert.ErrorIs(t, err, boom)
}

func TestLoadCanvasConnectorForUpdate_RefusesANonConnectorElement(t *testing.T) {
	fns := canvasConnectorFnsOK(sourceElement()) // a rectangle
	_, err := loadCanvasConnectorForUpdate(context.Background(), fns, canvasTestBoardID, cnConnectID)
	require.Error(t, err)
	var me *mcperr.McpError
	require.True(t, errors.As(err, &me))
	assert.Equal(t, mcperr.ValidationError, me.Code)
	assert.Contains(t, me.Message, "update_canvas_element")
}

func TestLoadCanvasConnectorForUpdate_AcceptsAConnector(t *testing.T) {
	conn := &data.CanvasElement{
		ID: cnConnectID, BoardID: canvasTestBoardID, Kind: "connector",
		Props: data.JSONText(storedConnectorProps),
	}
	fns := canvasConnectorFnsOK(conn)
	got, err := loadCanvasConnectorForUpdate(context.Background(), fns, canvasTestBoardID, cnConnectID)
	require.NoError(t, err)
	assert.Equal(t, cnConnectID, got.ID)
}

// REGRESSION (found by the canvas e2e gate, 2026-09-08): createCanvasConnector
// calls buildCanvasConnectorCreatePayload(in, nil) BEFORE the authorization
// gate, purely to validate the request without paying for a database round
// trip. For an element-attached source that call arrives here with sourceEl nil
// AND in.SourcePoint nil, and the old code dereferenced sourcePoint — a
// SIGSEGV that killed the whole MCP server process on every ordinary
// create_canvas_connector call. The earlier tests never hit it because each one
// supplied either an element or a point.
func TestCanvasConnectorPlaceholder_SurvivesPreGateValidationCall(t *testing.T) {
	x, y := canvasConnectorPlaceholder(nil, nil)
	assert.Equal(t, 0.0, x, "the pre-gate call discards these coordinates")
	assert.Equal(t, 0.0, y)
}

func TestBuildCanvasConnectorCreatePayload_PreGateValidationWithoutSourceElement(t *testing.T) {
	// Exactly the call createCanvasConnector makes at its validation step.
	payload, e := buildCanvasConnectorCreatePayload(attachedCreateInput(), nil)
	require.Nil(t, e)
	assert.Equal(t, "connector", payload["kind"])
	assert.Equal(t, 0.0, payload["positionX"],
		"placeholder is provisional here; the real payload is rebuilt with the resolved element")
	assert.Equal(t, 0.0, payload["positionY"])
}

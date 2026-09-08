// Package tools — canvas read tool tests (Wave 1).
// Strategy: the DB-free halves of the read path — the injectable authz/load
// pipeline (canvasBoardLoaderFns, mirroring internal/auth's ...WithFn pattern)
// and the pure element-bounding helpers. No DB and no Socket.IO server.
package tools

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const (
	canvasTestUserID    = "11112222-1111-4111-8111-111122223333"
	canvasTestProjectID = "44445555-4444-4444-8444-444455556666"
	canvasTestBoardID   = "77778888-7777-4777-8777-777788889999"
)

// canvasFnsFor builds a loader whose three stages are all successful, so each
// test can override exactly the stage it is about.
func canvasFnsFor(board *data.CanvasBoardWithElements) canvasBoardLoaderFns {
	return canvasBoardLoaderFns{
		resolveProject: func(context.Context, string) (string, error) {
			return canvasTestProjectID, nil
		},
		assertAccess: func(context.Context, string, string) error { return nil },
		loadBoard: func(context.Context, string) (*data.CanvasBoardWithElements, error) {
			return board, nil
		},
	}
}

func canvasBoardFixture() *data.CanvasBoardWithElements {
	return &data.CanvasBoardWithElements{
		CanvasBoard: data.CanvasBoard{ID: canvasTestBoardID, Name: "Flow", ProjectID: canvasTestProjectID},
		Elements:    []data.CanvasElement{},
	}
}

func requireMcpCode(t *testing.T, err error, code mcperr.Code) *mcperr.McpError {
	t.Helper()
	require.Error(t, err)
	mcpErr, ok := err.(*mcperr.McpError)
	require.True(t, ok, "expected *McpError, got %T", err)
	assert.Equal(t, code, mcpErr.Code)
	return mcpErr
}

// ---------------------------------------------------------------------------
// loadAuthorizedCanvasBoard — the shared read gate
// ---------------------------------------------------------------------------

// A non-UUID board id is rejected before any resolver or DB call.
func TestLoadAuthorizedCanvasBoard_RejectsNonUUID(t *testing.T) {
	fns := canvasFnsFor(canvasBoardFixture())
	fns.resolveProject = func(context.Context, string) (string, error) {
		t.Fatal("resolveProject must not run for an invalid UUID")
		return "", nil
	}

	_, err := loadAuthorizedCanvasBoardWithFns(context.Background(), fns, canvasTestUserID, "not-a-uuid")

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "canvasBoardId", mcpErr.Field)
}

// An unknown board id resolves to an empty project and must read NOT_FOUND —
// never FORBIDDEN, which would leak whether the board exists.
func TestLoadAuthorizedCanvasBoard_UnknownBoardIsNotFound(t *testing.T) {
	fns := canvasFnsFor(canvasBoardFixture())
	fns.resolveProject = func(context.Context, string) (string, error) { return "", nil }
	fns.assertAccess = func(context.Context, string, string) error {
		t.Fatal("assertAccess must not run when the board does not exist")
		return nil
	}

	_, err := loadAuthorizedCanvasBoardWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	mcpErr := requireMcpCode(t, err, mcperr.NotFound)
	assert.Contains(t, mcpErr.Message, canvasTestBoardID)
}

// A board deleted between the resolve and the load also reads NOT_FOUND.
func TestLoadAuthorizedCanvasBoard_MissingRowAfterResolveIsNotFound(t *testing.T) {
	fns := canvasFnsFor(nil)

	_, err := loadAuthorizedCanvasBoardWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	requireMcpCode(t, err, mcperr.NotFound)
}

// A non-member is refused, and the board is never loaded.
func TestLoadAuthorizedCanvasBoard_NonMemberIsForbidden(t *testing.T) {
	fns := canvasFnsFor(canvasBoardFixture())
	fns.assertAccess = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "no access")
	}
	fns.loadBoard = func(context.Context, string) (*data.CanvasBoardWithElements, error) {
		t.Fatal("loadBoard must not run for a refused caller")
		return nil, nil
	}

	_, err := loadAuthorizedCanvasBoardWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	requireMcpCode(t, err, mcperr.Forbidden)
}

// A resolver failure propagates raw; it must not be flattened into NOT_FOUND.
func TestLoadAuthorizedCanvasBoard_ResolverErrorPropagates(t *testing.T) {
	fns := canvasFnsFor(canvasBoardFixture())
	fns.resolveProject = func(context.Context, string) (string, error) { return "", assert.AnError }

	_, err := loadAuthorizedCanvasBoardWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	require.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

// The happy path returns the board and scopes access to the resolved project.
func TestLoadAuthorizedCanvasBoard_ReturnsBoardForMember(t *testing.T) {
	var gotUserID, gotProjectID string
	fns := canvasFnsFor(canvasBoardFixture())
	fns.assertAccess = func(_ context.Context, userID, projectID string) error {
		gotUserID, gotProjectID = userID, projectID
		return nil
	}

	board, err := loadAuthorizedCanvasBoardWithFns(context.Background(), fns, canvasTestUserID, canvasTestBoardID)

	require.NoError(t, err)
	require.NotNil(t, board)
	assert.Equal(t, canvasTestBoardID, board.ID)
	assert.Equal(t, canvasTestUserID, gotUserID)
	assert.Equal(t, canvasTestProjectID, gotProjectID)
}

// The production loader must use the CANVAS project resolver. Passing a canvas
// board id to GetWhiteboardProjectID returns "", which would turn every canvas
// tool into a NOT_FOUND — the single most likely silent failure in this feature.
func TestProdCanvasBoardLoaderFns_UseCanvasProjectResolver(t *testing.T) {
	got := reflect.ValueOf(prodCanvasBoardLoaderFns().resolveProject).Pointer()
	assert.Equal(t, reflect.ValueOf(data.GetCanvasBoardProjectID).Pointer(), got,
		"canvas reads must resolve the project through GetCanvasBoardProjectID")
	assert.NotEqual(t, reflect.ValueOf(data.GetWhiteboardProjectID).Pointer(), got)
}

// ---------------------------------------------------------------------------
// resolveMaxElements — the read bound (A5)
// ---------------------------------------------------------------------------

func TestResolveMaxElements_DefaultsWhenOmitted(t *testing.T) {
	n, err := resolveMaxElements(nil)
	require.Nil(t, err)
	assert.Equal(t, canvasMaxElementsDefault, n)
}

func TestResolveMaxElements_AcceptsInRangeValue(t *testing.T) {
	requested := 25
	n, err := resolveMaxElements(&requested)
	require.Nil(t, err)
	assert.Equal(t, 25, n)
}

func TestResolveMaxElements_AcceptsTheCeiling(t *testing.T) {
	requested := canvasMaxElementsDefault
	n, err := resolveMaxElements(&requested)
	require.Nil(t, err)
	assert.Equal(t, canvasMaxElementsDefault, n)
}

func TestResolveMaxElements_RejectsOutOfRangeValues(t *testing.T) {
	for name, requested := range map[string]int{
		"zero":           0,
		"negative":       -1,
		"above ceiling":  canvasMaxElementsDefault + 1,
		"absurdly large": 1_000_000,
	} {
		t.Run(name, func(t *testing.T) {
			v := requested
			_, err := resolveMaxElements(&v)
			require.NotNil(t, err)
			assert.Equal(t, mcperr.ValidationError, err.Code)
			assert.Equal(t, "maxElements", err.Field)
		})
	}
}

// ---------------------------------------------------------------------------
// truncateCanvasElements — bounded reads keep paint order from the bottom
// ---------------------------------------------------------------------------

func canvasElements(n int) []data.CanvasElement {
	out := make([]data.CanvasElement, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, data.CanvasElement{ID: "el", Kind: "rectangle", ZIndex: i})
	}
	return out
}

func TestTruncateCanvasElements_UnderCapReturnsEverything(t *testing.T) {
	shown, truncated := truncateCanvasElements(canvasElements(3), 500)
	assert.Len(t, shown, 3)
	assert.False(t, truncated)
}

func TestTruncateCanvasElements_AtCapIsNotTruncated(t *testing.T) {
	shown, truncated := truncateCanvasElements(canvasElements(500), 500)
	assert.Len(t, shown, 500)
	assert.False(t, truncated, "exactly the cap is a complete read")
}

func TestTruncateCanvasElements_OverCapKeepsTheFirstNInPaintOrder(t *testing.T) {
	shown, truncated := truncateCanvasElements(canvasElements(501), 500)
	require.Len(t, shown, 500)
	assert.True(t, truncated)
	assert.Equal(t, 0, shown[0].ZIndex, "must keep the bottom of the paint order")
	assert.Equal(t, 499, shown[499].ZIndex)
}

func TestTruncateCanvasElements_EmptyBoardReturnsNonNilSlice(t *testing.T) {
	shown, truncated := truncateCanvasElements(nil, 500)
	assert.NotNil(t, shown, "an empty board must serialise as [] rather than null")
	assert.Empty(t, shown)
	assert.False(t, truncated)
}

// ---------------------------------------------------------------------------
// newCanvasBoardOut — truncation is stated in the response, never silent
// ---------------------------------------------------------------------------

func TestNewCanvasBoardOut_StatesTruncationExplicitly(t *testing.T) {
	board := canvasBoardFixture()
	board.Elements = canvasElements(650)

	out := newCanvasBoardOut(board, canvasMaxElementsDefault)

	assert.True(t, out.Truncated)
	assert.Equal(t, 650, out.TotalElements)
	assert.Equal(t, 500, out.ReturnedElements)
	assert.Len(t, out.Elements, 500)
	assert.Equal(t, canvasMaxElementsDefault, out.MaxElements)
	assert.Equal(t, canvasTestBoardID, out.ID)
}

func TestNewCanvasBoardOut_CompleteReadReportsNoTruncation(t *testing.T) {
	board := canvasBoardFixture()
	board.Elements = canvasElements(2)

	out := newCanvasBoardOut(board, canvasMaxElementsDefault)

	assert.False(t, out.Truncated)
	assert.Equal(t, 2, out.TotalElements)
	assert.Equal(t, 2, out.ReturnedElements)
}

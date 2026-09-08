// Package tools — canvas board lifecycle tool tests (Wave 5).
// Strategy matches canvas_write_test.go: exercise the DB-free and HTTP-free
// halves through the injectable canvasBoardLifecycleFns, so every guard is
// provably checked BEFORE the app route is called.
package tools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const canvasTestFolderID = "aaaabbbb-cccc-4ddd-8eee-ffff00001111"

// canvasBoardLifecycleFnsOK builds a pipeline whose every stage succeeds, so a
// test can override exactly the stage it is about. Each route call fails the
// test by default: a test that expects a call must opt in.
func canvasBoardLifecycleFnsOK(t *testing.T) canvasBoardLifecycleFns {
	t.Helper()
	return canvasBoardLifecycleFns{
		resolveProject: func(context.Context, string) (string, error) {
			return canvasTestProjectID, nil
		},
		assertEdit: func(context.Context, string, string) error { return nil },
		findBoard: func(context.Context, string) (*data.CanvasBoard, error) {
			return &data.CanvasBoard{
				ID:        canvasTestBoardID,
				Name:      "Sprint board",
				ProjectID: canvasTestProjectID,
			}, nil
		},
		createBoard: func(context.Context, string, appapi.CreateCanvasBoardRequest) (map[string]any, error) {
			t.Fatal("the app route must not be called in this test")
			return nil, nil
		},
		updateBoard: func(context.Context, string, string, appapi.UpdateCanvasBoardRequest) (map[string]any, error) {
			t.Fatal("the app route must not be called in this test")
			return nil, nil
		},
		deleteBoard: func(context.Context, string, string) (map[string]any, error) {
			t.Fatal("the app route must not be called in this test")
			return nil, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Production wiring
// ---------------------------------------------------------------------------

// Board lifecycle is a mutation, so it gates on EDITOR+ exactly like element
// writes, and it must resolve the project through the CanvasBoard table —
// GetWhiteboardProjectID would answer "" for every canvas board and turn every
// call into a NOT_FOUND.
func TestProdCanvasBoardLifecycleFns_UseEditorGateAndCanvasResolver(t *testing.T) {
	fns := prodCanvasBoardLifecycleFns()

	assert.Equal(t,
		reflect.ValueOf(data.GetCanvasBoardProjectID).Pointer(),
		reflect.ValueOf(fns.resolveProject).Pointer(),
		"board lifecycle must resolve the project through the CanvasBoard table")
	assert.Equal(t,
		reflect.ValueOf(auth.AssertSchemaEditAccess).Pointer(),
		reflect.ValueOf(fns.assertEdit).Pointer(),
		"board lifecycle must gate on AssertSchemaEditAccess (EDITOR+)")
	assert.Equal(t,
		reflect.ValueOf(data.FindCanvasBoardByID).Pointer(),
		reflect.ValueOf(fns.findBoard).Pointer(),
		"the confirmName check must read the board's real stored name")
}

// ---------------------------------------------------------------------------
// create_canvas_board
// ---------------------------------------------------------------------------

func TestCreateCanvasBoard_RejectsNonUUIDProjectID(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		t.Fatal("the EDITOR gate must not run for an invalid UUID")
		return nil
	}

	_, err := createCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		createCanvasBoardInput{ProjectID: "not-a-uuid", Name: "Sprint board"})

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "projectId", mcpErr.Field)
}

func TestCreateCanvasBoard_RejectsBadNames(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"too long":  strings.Repeat("x", 256),
		"only name": "",
	}
	for label, name := range cases {
		t.Run(label, func(t *testing.T) {
			fns := canvasBoardLifecycleFnsOK(t)
			_, err := createCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
				createCanvasBoardInput{ProjectID: canvasTestProjectID, Name: name})

			mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
			assert.Equal(t, "name", mcpErr.Field)
		})
	}
}

func TestCreateCanvasBoard_RejectsNonUUIDFolderID(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	bad := "not-a-folder"

	_, err := createCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		createCanvasBoardInput{ProjectID: canvasTestProjectID, Name: "Sprint board", FolderID: &bad})

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "folderId", mcpErr.Field)
}

// A VIEWER is refused by the MCP's own gate, before the route is called. The
// route re-checks independently; this test pins the near-side half.
func TestCreateCanvasBoard_ViewerIsForbiddenBeforeTheRouteCall(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "Editor access required.")
	}

	_, err := createCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		createCanvasBoardInput{ProjectID: canvasTestProjectID, Name: "Sprint board"})

	requireMcpCode(t, err, mcperr.Forbidden)
}

func TestCreateCanvasBoard_PassesFieldsToTheRoute(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	folder := canvasTestFolderID
	var got appapi.CreateCanvasBoardRequest
	var gotUser string
	fns.createBoard = func(_ context.Context, userID string, req appapi.CreateCanvasBoardRequest) (map[string]any, error) {
		gotUser, got = userID, req
		return map[string]any{"id": canvasTestBoardID}, nil
	}

	board, err := createCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		createCanvasBoardInput{ProjectID: canvasTestProjectID, Name: "Sprint board", FolderID: &folder})

	require.NoError(t, err)
	assert.Equal(t, canvasTestUserID, gotUser)
	assert.Equal(t, canvasTestProjectID, got.ProjectID)
	assert.Equal(t, "Sprint board", got.Name)
	require.NotNil(t, got.FolderID)
	assert.Equal(t, canvasTestFolderID, *got.FolderID)
	assert.Equal(t, canvasTestBoardID, board["id"])
}

func TestCreateCanvasBoard_PropagatesRouteErrors(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.createBoard = func(context.Context, string, appapi.CreateCanvasBoardRequest) (map[string]any, error) {
		return nil, mcperr.New(mcperr.Forbidden, "route says no")
	}

	_, err := createCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		createCanvasBoardInput{ProjectID: canvasTestProjectID, Name: "Sprint board"})

	requireMcpCode(t, err, mcperr.Forbidden)
}

// ---------------------------------------------------------------------------
// update_canvas_board
// ---------------------------------------------------------------------------

func TestUpdateCanvasBoard_RejectsNonUUIDBoardID(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	name := "Renamed"

	_, err := updateCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		updateCanvasBoardInput{CanvasBoardID: "nope", Name: &name})

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "canvasBoardId", mcpErr.Field)
}

// An update naming no field would be a no-op round trip that still bumps
// updatedAt, so it is refused here rather than sent.
func TestUpdateCanvasBoard_RejectsEmptyUpdate(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)

	_, err := updateCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		updateCanvasBoardInput{CanvasBoardID: canvasTestBoardID})

	requireMcpCode(t, err, mcperr.ValidationError)
}

func TestUpdateCanvasBoard_UnknownBoardIsNotFound(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.resolveProject = func(context.Context, string) (string, error) { return "", nil }
	fns.assertEdit = func(context.Context, string, string) error {
		t.Fatal("the EDITOR gate must not run when the board does not exist")
		return nil
	}
	name := "Renamed"

	_, err := updateCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		updateCanvasBoardInput{CanvasBoardID: canvasTestBoardID, Name: &name})

	mcpErr := requireMcpCode(t, err, mcperr.NotFound)
	assert.Equal(t, canvasBoardNotFound(canvasTestBoardID).Message, mcpErr.Message)
}

func TestUpdateCanvasBoard_ViewerIsForbiddenBeforeTheRouteCall(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "Editor access required.")
	}
	name := "Renamed"

	_, err := updateCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		updateCanvasBoardInput{CanvasBoardID: canvasTestBoardID, Name: &name})

	requireMcpCode(t, err, mcperr.Forbidden)
}

func TestUpdateCanvasBoard_PassesOnlySuppliedFields(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	var got appapi.UpdateCanvasBoardRequest
	var gotID string
	fns.updateBoard = func(_ context.Context, _ string, id string, req appapi.UpdateCanvasBoardRequest) (map[string]any, error) {
		gotID, got = id, req
		return map[string]any{"id": canvasTestBoardID}, nil
	}
	name := "Renamed"

	_, err := updateCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		updateCanvasBoardInput{CanvasBoardID: canvasTestBoardID, Name: &name})

	require.NoError(t, err)
	assert.Equal(t, canvasTestBoardID, gotID)
	require.NotNil(t, got.Name)
	assert.Equal(t, "Renamed", *got.Name)
	assert.Nil(t, got.FolderID, "an absent folderId must stay absent, not become null")
}

func TestUpdateCanvasBoard_RejectsBadName(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	tooLong := strings.Repeat("x", 256)

	_, err := updateCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		updateCanvasBoardInput{CanvasBoardID: canvasTestBoardID, Name: &tooLong})

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "name", mcpErr.Field)
}

// ---------------------------------------------------------------------------
// delete_canvas_board — the confirmName guard
// ---------------------------------------------------------------------------

// A9. The delete cascades every CanvasElement and CanvasBoardShareLink on the
// board through ON DELETE CASCADE. confirmName exists so an LLM cannot destroy
// a board by accident, and the mismatch must be caught BEFORE the route is
// called — canvasBoardLifecycleFnsOK fails the test if deleteBoard runs.
func TestDeleteCanvasBoard_MismatchedConfirmNameNeverReachesTheRoute(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: "Wrong name"})

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "confirmName", mcpErr.Field)
}

// The mismatch error must NOT print the board's real name. Echoing it would let
// a caller blind-retry with the revealed value, which defeats the guard's real
// purpose: catching a wrong board id before it destroys the wrong board.
func TestDeleteCanvasBoard_MismatchErrorDoesNotRevealTheRealName(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: "Wrong name"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "Sprint board",
		"the mismatch error must not disclose the board's stored name")
}

func TestDeleteCanvasBoard_RejectsEmptyConfirmName(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: ""})

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "confirmName", mcpErr.Field)
}

func TestDeleteCanvasBoard_MatchingConfirmNameDeletes(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	var gotID, gotUser string
	fns.deleteBoard = func(_ context.Context, userID, id string) (map[string]any, error) {
		gotUser, gotID = userID, id
		return map[string]any{"id": canvasTestBoardID}, nil
	}

	board, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: "Sprint board"})

	require.NoError(t, err)
	assert.Equal(t, canvasTestUserID, gotUser)
	assert.Equal(t, canvasTestBoardID, gotID)
	assert.Equal(t, canvasTestBoardID, board["id"])
}

// The comparison is exact: no trimming, no case folding. A name that differs
// only in whitespace is a different name, because whitespace is exactly what a
// careless paste gets wrong.
func TestDeleteCanvasBoard_ConfirmNameIsCompared(t *testing.T) {
	for _, confirm := range []string{"sprint board", " Sprint board", "Sprint board "} {
		t.Run(confirm, func(t *testing.T) {
			fns := canvasBoardLifecycleFnsOK(t)
			_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
				deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: confirm})

			mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
			assert.Equal(t, "confirmName", mcpErr.Field)
		})
	}
}

func TestDeleteCanvasBoard_RejectsNonUUIDBoardID(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: "nope", ConfirmName: "Sprint board"})

	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.Equal(t, "canvasBoardId", mcpErr.Field)
}

func TestDeleteCanvasBoard_UnknownBoardIsNotFound(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.resolveProject = func(context.Context, string) (string, error) { return "", nil }

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: "Sprint board"})

	requireMcpCode(t, err, mcperr.NotFound)
}

// A board that resolves a project but has vanished between the two reads is
// NOT_FOUND, not a nil-pointer panic.
func TestDeleteCanvasBoard_VanishedBoardIsNotFound(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.findBoard = func(context.Context, string) (*data.CanvasBoard, error) { return nil, nil }

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: "Sprint board"})

	requireMcpCode(t, err, mcperr.NotFound)
}

// The EDITOR gate runs before the board is read, so a VIEWER cannot use
// confirmName mismatches as a name oracle.
func TestDeleteCanvasBoard_ViewerIsForbiddenBeforeTheBoardIsRead(t *testing.T) {
	fns := canvasBoardLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "Editor access required.")
	}
	fns.findBoard = func(context.Context, string) (*data.CanvasBoard, error) {
		t.Fatal("the board must not be read before the EDITOR gate passes")
		return nil, nil
	}

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: "Sprint board"})

	requireMcpCode(t, err, mcperr.Forbidden)
}

func TestDeleteCanvasBoard_FindBoardErrorPropagates(t *testing.T) {
	sentinel := errors.New("db down")
	fns := canvasBoardLifecycleFnsOK(t)
	fns.findBoard = func(context.Context, string) (*data.CanvasBoard, error) { return nil, sentinel }

	_, err := deleteCanvasBoardWithFns(context.Background(), fns, canvasTestUserID,
		deleteCanvasBoardInput{CanvasBoardID: canvasTestBoardID, ConfirmName: "Sprint board"})

	assert.ErrorIs(t, err, sentinel)
}

// Package tools — folder lifecycle tool tests.
// Same strategy as project_test.go: every guard is exercised through the
// injectable folderLifecycleFns, so it is provably checked BEFORE the app route
// is called.
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const lifecycleFolderID = "ffff0000-1111-4222-8333-444455556666"
const lifecycleFolderName = "Billing docs"

func folderLifecycleFnsOK(t *testing.T) folderLifecycleFns {
	t.Helper()
	return folderLifecycleFns{
		resolveProject: func(context.Context, string) (string, error) {
			return lifecycleProjectID, nil
		},
		assertEdit: func(context.Context, string, string) error { return nil },
		findFolder: func(context.Context, string) (*data.Folder, error) {
			return &data.Folder{
				ID:        lifecycleFolderID,
				Name:      lifecycleFolderName,
				ProjectID: lifecycleProjectID,
			}, nil
		},
		listFolders: func(context.Context, string) ([]data.FolderSummary, error) {
			return []data.FolderSummary{{
				ID:               lifecycleFolderID,
				Name:             lifecycleFolderName,
				WhiteboardCount:  3,
				CanvasBoardCount: 1,
				ChildFolderCount: 2,
			}}, nil
		},
		createFolder: func(context.Context, string, appapi.CreateFolderRequest) (map[string]any, error) {
			t.Error("createFolder called; this test did not expect a route call")
			return nil, nil
		},
		updateFolder: func(context.Context, string, string, string) (map[string]any, error) {
			t.Error("updateFolder called; this test did not expect a route call")
			return nil, nil
		},
		deleteFolder: func(context.Context, string, string) (map[string]any, error) {
			t.Error("deleteFolder called; this test did not expect a route call")
			return nil, nil
		},
	}
}

// ---------------------------------------------------------------------------
// create_folder
// ---------------------------------------------------------------------------

func TestCreateFolder_SendsParentWhenSupplied(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	var got appapi.CreateFolderRequest
	fns.createFolder = func(_ context.Context, _ string, req appapi.CreateFolderRequest) (map[string]any, error) {
		got = req
		return map[string]any{"id": lifecycleFolderID}, nil
	}

	parent := lifecycleFolderID
	_, err := createFolderWithFns(context.Background(), fns, "user-1",
		createFolderInput{ProjectID: lifecycleProjectID, Name: "Docs", ParentFolderID: &parent})
	require.NoError(t, err)
	assert.Equal(t, lifecycleProjectID, got.ProjectID)
	require.NotNil(t, got.ParentFolderID)
	assert.Equal(t, parent, *got.ParentFolderID)
}

func TestCreateFolder_RejectsBadIDs(t *testing.T) {
	bad := "not-a-uuid"
	cases := []createFolderInput{
		{ProjectID: "nope", Name: "Docs"},
		{ProjectID: lifecycleProjectID, Name: "Docs", ParentFolderID: &bad},
	}
	for _, in := range cases {
		fns := folderLifecycleFnsOK(t)
		_, err := createFolderWithFns(context.Background(), fns, "user-1", in)
		requireMcpCode(t, err, mcperr.ValidationError)
	}
}

func TestCreateFolder_RejectsOutOfRangeName(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("x", folderNameMaxLength+1)} {
		fns := folderLifecycleFnsOK(t)
		_, err := createFolderWithFns(context.Background(), fns, "user-1",
			createFolderInput{ProjectID: lifecycleProjectID, Name: name})
		requireMcpCode(t, err, mcperr.ValidationError)
	}
}

func TestCreateFolder_StopsOnEditorDenial(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "viewer")
	}
	_, err := createFolderWithFns(context.Background(), fns, "user-1",
		createFolderInput{ProjectID: lifecycleProjectID, Name: "Docs"})
	requireMcpCode(t, err, mcperr.Forbidden)
}

// ---------------------------------------------------------------------------
// update_folder
// ---------------------------------------------------------------------------

func TestUpdateFolder_RenamesThroughTheRoute(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	var gotName string
	fns.updateFolder = func(_ context.Context, _, _, name string) (map[string]any, error) {
		gotName = name
		return map[string]any{"id": lifecycleFolderID}, nil
	}

	_, err := updateFolderWithFns(context.Background(), fns, "user-1",
		updateFolderInput{FolderID: lifecycleFolderID, Name: "Renamed"})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", gotName)
}

func TestUpdateFolder_UnknownFolderIsNotFound(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	fns.resolveProject = func(context.Context, string) (string, error) { return "", nil }

	_, err := updateFolderWithFns(context.Background(), fns, "user-1",
		updateFolderInput{FolderID: lifecycleFolderID, Name: "Renamed"})
	requireMcpCode(t, err, mcperr.NotFound)
}

func TestUpdateFolder_RejectsEmptyName(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	_, err := updateFolderWithFns(context.Background(), fns, "user-1",
		updateFolderInput{FolderID: lifecycleFolderID, Name: ""})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// ---------------------------------------------------------------------------
// delete_folder
// ---------------------------------------------------------------------------

func TestDeleteFolder_DeletesWhenConfirmNameMatches(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	deleted := ""
	fns.deleteFolder = func(_ context.Context, _, folderID string) (map[string]any, error) {
		deleted = folderID
		return map[string]any{"id": folderID}, nil
	}

	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: lifecycleFolderName})
	require.NoError(t, err)
	assert.Equal(t, lifecycleFolderID, deleted)
}

func TestDeleteFolder_MismatchDeletesNothing(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: "Wrong"})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// The cascade is the whole reason this delete is dangerous, so the refusal must
// say what it would have destroyed.
func TestDeleteFolder_MismatchErrorReportsTheCascadeSize(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: "Wrong"})
	require.Error(t, err)
	// 3 whiteboards + 1 canvas board = 4 boards, plus 2 child folders.
	assert.Contains(t, err.Error(), "4 board(s)")
	assert.Contains(t, err.Error(), "2 child folder(s)")
}

func TestDeleteFolder_MismatchErrorDoesNotRevealTheName(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: "Wrong"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), lifecycleFolderName)
}

// A counting failure must not turn a refused delete into an error about
// counting: the refusal is the point, the warning is a courtesy.
func TestDeleteFolder_CascadeCountFailureStillRefusesCleanly(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	fns.listFolders = func(context.Context, string) ([]data.FolderSummary, error) {
		return nil, errors.New("count query exploded")
	}

	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: "Wrong"})
	mcpErr := requireMcpCode(t, err, mcperr.ValidationError)
	assert.NotContains(t, mcpErr.Message, "count query exploded")
}

// An empty folder draws no cascade sentence — a warning about zero boards is
// noise that trains an agent to ignore the real one.
func TestDeleteFolder_EmptyFolderMismatchHasNoCascadeSentence(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	fns.listFolders = func(context.Context, string) ([]data.FolderSummary, error) {
		return []data.FolderSummary{{ID: lifecycleFolderID, Name: lifecycleFolderName}}, nil
	}

	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: "Wrong"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "would have destroyed")
}

func TestDeleteFolder_EmptyConfirmNameIsRefused(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: ""})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// Order is load-bearing: the EDITOR gate runs BEFORE the folder is read, so a
// VIEWER cannot use confirmName mismatches as a folder-name oracle.
func TestDeleteFolder_EditorGateRunsBeforeTheNameIsRead(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "viewer")
	}
	fns.findFolder = func(context.Context, string) (*data.Folder, error) {
		t.Error("the folder was read before the EDITOR gate passed")
		return nil, nil
	}

	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: "anything"})
	requireMcpCode(t, err, mcperr.Forbidden)
}

func TestDeleteFolder_UnknownFolderIsNotFound(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	fns.resolveProject = func(context.Context, string) (string, error) { return "", nil }

	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: lifecycleFolderID, ConfirmName: lifecycleFolderName})
	requireMcpCode(t, err, mcperr.NotFound)
}

func TestDeleteFolder_RejectsNonUUIDFolderID(t *testing.T) {
	fns := folderLifecycleFnsOK(t)
	_, err := deleteFolderWithFns(context.Background(), fns, "user-1",
		deleteFolderInput{FolderID: "nope", ConfirmName: lifecycleFolderName})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// Package tools — ER whiteboard lifecycle tool tests.
// Same strategy as project_test.go and folder_test.go: every guard is exercised
// through the injectable whiteboardLifecycleFns, so it is provably checked
// BEFORE the app route is called.
package tools

import (
	"context"
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

const lifecycleWhiteboardID = "aaaa1111-2222-4333-8444-555566667777"
const lifecycleWhiteboardName = "Orders schema"

func whiteboardLifecycleFnsOK(t *testing.T) whiteboardLifecycleFns {
	t.Helper()
	return whiteboardLifecycleFns{
		resolveProject: func(context.Context, string) (string, error) {
			return lifecycleProjectID, nil
		},
		assertEdit: func(context.Context, string, string) error { return nil },
		findWhiteboard: func(context.Context, string) (*data.Whiteboard, error) {
			return &data.Whiteboard{
				ID:        lifecycleWhiteboardID,
				Name:      lifecycleWhiteboardName,
				ProjectID: lifecycleProjectID,
			}, nil
		},
		createWhiteboard: func(context.Context, string, appapi.CreateWhiteboardRequest) (map[string]any, error) {
			t.Error("createWhiteboard called; this test did not expect a route call")
			return nil, nil
		},
		updateWhiteboard: func(context.Context, string, string, appapi.UpdateWhiteboardRequest) (map[string]any, error) {
			t.Error("updateWhiteboard called; this test did not expect a route call")
			return nil, nil
		},
		deleteWhiteboard: func(context.Context, string, string) (map[string]any, error) {
			t.Error("deleteWhiteboard called; this test did not expect a route call")
			return nil, nil
		},
	}
}

// ---------------------------------------------------------------------------
// create_whiteboard
// ---------------------------------------------------------------------------

func TestCreateWhiteboard_SendsProjectNameAndFolder(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	var got appapi.CreateWhiteboardRequest
	fns.createWhiteboard = func(_ context.Context, _ string, req appapi.CreateWhiteboardRequest) (map[string]any, error) {
		got = req
		return map[string]any{"id": lifecycleWhiteboardID}, nil
	}

	folder := lifecycleFolderID
	out, err := createWhiteboardWithFns(context.Background(), fns, "user-1",
		createWhiteboardInput{ProjectID: lifecycleProjectID, Name: lifecycleWhiteboardName, FolderID: &folder})
	require.NoError(t, err)
	assert.Equal(t, lifecycleWhiteboardID, out["id"])
	assert.Equal(t, lifecycleProjectID, got.ProjectID)
	assert.Equal(t, lifecycleWhiteboardName, got.Name)
	require.NotNil(t, got.FolderID)
	assert.Equal(t, folder, *got.FolderID)
}

func TestCreateWhiteboard_RejectsBadIDsAndNames(t *testing.T) {
	bad := "not-a-uuid"
	cases := []createWhiteboardInput{
		{ProjectID: "nope", Name: "S"},
		{ProjectID: lifecycleProjectID, Name: ""},
		{ProjectID: lifecycleProjectID, Name: strings.Repeat("x", whiteboardNameMaxLength+1)},
		{ProjectID: lifecycleProjectID, Name: "S", FolderID: &bad},
	}
	for _, in := range cases {
		fns := whiteboardLifecycleFnsOK(t)
		_, err := createWhiteboardWithFns(context.Background(), fns, "user-1", in)
		requireMcpCode(t, err, mcperr.ValidationError)
	}
}

func TestCreateWhiteboard_StopsOnEditorDenial(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "viewer")
	}
	_, err := createWhiteboardWithFns(context.Background(), fns, "user-1",
		createWhiteboardInput{ProjectID: lifecycleProjectID, Name: "S"})
	requireMcpCode(t, err, mcperr.Forbidden)
}

// ---------------------------------------------------------------------------
// update_whiteboard
// ---------------------------------------------------------------------------

func TestUpdateWhiteboard_SendsOnlySuppliedFields(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	var got appapi.UpdateWhiteboardRequest
	fns.updateWhiteboard = func(_ context.Context, _, _ string, req appapi.UpdateWhiteboardRequest) (map[string]any, error) {
		got = req
		return map[string]any{"id": lifecycleWhiteboardID}, nil
	}

	name := "Renamed"
	_, err := updateWhiteboardWithFns(context.Background(), fns, "user-1",
		updateWhiteboardInput{WhiteboardID: lifecycleWhiteboardID, Name: &name})
	require.NoError(t, err)
	require.NotNil(t, got.Name)
	assert.Equal(t, "Renamed", *got.Name)
	assert.Nil(t, got.FolderID)
}

func TestUpdateWhiteboard_RefusesEmptyUpdate(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	_, err := updateWhiteboardWithFns(context.Background(), fns, "user-1",
		updateWhiteboardInput{WhiteboardID: lifecycleWhiteboardID})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// A canvas board id resolves to "" through the whiteboard resolver, and the
// message must say so rather than leaving the agent to guess.
func TestUpdateWhiteboard_UnknownIDNamesTheBoardKind(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	fns.resolveProject = func(context.Context, string) (string, error) { return "", nil }

	name := "Renamed"
	_, err := updateWhiteboardWithFns(context.Background(), fns, "user-1",
		updateWhiteboardInput{WhiteboardID: lifecycleWhiteboardID, Name: &name})
	requireMcpCode(t, err, mcperr.NotFound)
	assert.Contains(t, err.Error(), "canvas board")
}

// ---------------------------------------------------------------------------
// delete_whiteboard
// ---------------------------------------------------------------------------

func TestDeleteWhiteboard_DeletesWhenConfirmNameMatches(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	deleted := ""
	fns.deleteWhiteboard = func(_ context.Context, _, id string) (map[string]any, error) {
		deleted = id
		return map[string]any{"id": id}, nil
	}

	_, err := deleteWhiteboardWithFns(context.Background(), fns, "user-1",
		deleteWhiteboardInput{WhiteboardID: lifecycleWhiteboardID, ConfirmName: lifecycleWhiteboardName})
	require.NoError(t, err)
	assert.Equal(t, lifecycleWhiteboardID, deleted)
}

func TestDeleteWhiteboard_MismatchDeletesNothing(t *testing.T) {
	for _, confirm := range []string{
		"Wrong name",
		" " + lifecycleWhiteboardName,
		lifecycleWhiteboardName + " ",
		strings.ToUpper(lifecycleWhiteboardName),
	} {
		fns := whiteboardLifecycleFnsOK(t)
		_, err := deleteWhiteboardWithFns(context.Background(), fns, "user-1",
			deleteWhiteboardInput{WhiteboardID: lifecycleWhiteboardID, ConfirmName: confirm})
		requireMcpCode(t, err, mcperr.ValidationError)
	}
}

func TestDeleteWhiteboard_MismatchErrorDoesNotRevealTheName(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	_, err := deleteWhiteboardWithFns(context.Background(), fns, "user-1",
		deleteWhiteboardInput{WhiteboardID: lifecycleWhiteboardID, ConfirmName: "Wrong"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), lifecycleWhiteboardName)
}

func TestDeleteWhiteboard_EmptyConfirmNameIsRefused(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	_, err := deleteWhiteboardWithFns(context.Background(), fns, "user-1",
		deleteWhiteboardInput{WhiteboardID: lifecycleWhiteboardID, ConfirmName: ""})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// Order is load-bearing: the EDITOR gate runs BEFORE the board is read.
func TestDeleteWhiteboard_EditorGateRunsBeforeTheNameIsRead(t *testing.T) {
	fns := whiteboardLifecycleFnsOK(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "viewer")
	}
	fns.findWhiteboard = func(context.Context, string) (*data.Whiteboard, error) {
		t.Error("the board was read before the EDITOR gate passed")
		return nil, nil
	}

	_, err := deleteWhiteboardWithFns(context.Background(), fns, "user-1",
		deleteWhiteboardInput{WhiteboardID: lifecycleWhiteboardID, ConfirmName: "anything"})
	requireMcpCode(t, err, mcperr.Forbidden)
}

// ---------------------------------------------------------------------------
// Production wiring
// ---------------------------------------------------------------------------

// The whiteboard and canvas board resolvers have the same signature and target
// different tables. Wiring the canvas one here would make every ER board look
// missing, which is the bug requireCanvasBoardRole exists to prevent on the
// socket path.
func TestProdWhiteboardLifecycleFns_UseTheWhiteboardResolver(t *testing.T) {
	fns := prodWhiteboardLifecycleFns()

	assert.Equal(t, reflect.ValueOf(data.GetWhiteboardProjectID).Pointer(),
		reflect.ValueOf(fns.resolveProject).Pointer())
	assert.NotEqual(t, reflect.ValueOf(data.GetCanvasBoardProjectID).Pointer(),
		reflect.ValueOf(fns.resolveProject).Pointer())
	assert.Equal(t, reflect.ValueOf(auth.AssertSchemaEditAccess).Pointer(),
		reflect.ValueOf(fns.assertEdit).Pointer())
	assert.Equal(t, reflect.ValueOf(data.FindWhiteboardByID).Pointer(),
		reflect.ValueOf(fns.findWhiteboard).Pointer())
}

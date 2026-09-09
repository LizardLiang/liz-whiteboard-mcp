// Package tools — cross-file table reference tool tests (LizMeter #83).
// Same strategy as folder_test.go: every guard is exercised through the
// injectable tableReferenceFns, so it is provably checked BEFORE the app route
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
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const (
	refLocalBoardID  = "aaaa0000-1111-4222-8333-444455556666"
	refSourceBoardID = "bbbb0000-1111-4222-8333-444455556666"
	refSourceTableID = "cccc0000-1111-4222-8333-444455556666"
	refColumnID      = "dddd0000-1111-4222-8333-444455556666"
	refNodeID        = "eeee0000-1111-4222-8333-444455556666"
)

// tableReferenceFnsRefusing fails the test if any route call happens. Guard
// tests inject this: a guard that lets the call through has not guarded.
func tableReferenceFnsRefusing(t *testing.T) tableReferenceFns {
	t.Helper()
	return tableReferenceFns{
		createReference: func(context.Context, string, appapi.CreateTableReferenceRequest) (map[string]any, error) {
			t.Error("createReference called; this test did not expect a route call")
			return nil, nil
		},
		updateReference: func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
			t.Error("updateReference called; this test did not expect a route call")
			return nil, nil
		},
		deleteReference: func(context.Context, string, string) (map[string]any, error) {
			t.Error("deleteReference called; this test did not expect a route call")
			return nil, nil
		},
		listReferences: func(context.Context, string, string) ([]map[string]any, error) {
			t.Error("listReferences called; this test did not expect a route call")
			return nil, nil
		},
	}
}

func validReferenceInput() createTableReferenceInput {
	return createTableReferenceInput{
		WhiteboardID:       refLocalBoardID,
		SourceWhiteboardID: refSourceBoardID,
		SourceTableID:      refSourceTableID,
		SourceColumnIDs:    []string{refColumnID},
	}
}

func requireValidationField(t *testing.T, err error, field string) {
	t.Helper()
	require.Error(t, err)
	var mcpErr *mcperr.McpError
	require.True(t, errors.As(err, &mcpErr), "expected an McpError, got %T", err)
	assert.Equal(t, mcperr.ValidationError, mcpErr.Code)
	if field != "" {
		assert.Equal(t, field, mcpErr.Field)
	}
}

// ── create ───────────────────────────────────────────────────────────────────

func TestCreateTableReferenceRejectsBadUUIDs(t *testing.T) {
	cases := map[string]struct {
		mutate func(*createTableReferenceInput)
		field  string
	}{
		"whiteboardId":       {func(in *createTableReferenceInput) { in.WhiteboardID = "nope" }, "whiteboardId"},
		"sourceWhiteboardId": {func(in *createTableReferenceInput) { in.SourceWhiteboardID = "nope" }, "sourceWhiteboardId"},
		"sourceTableId":      {func(in *createTableReferenceInput) { in.SourceTableID = "nope" }, "sourceTableId"},
		"sourceColumnIds":    {func(in *createTableReferenceInput) { in.SourceColumnIDs = []string{"nope"} }, "sourceColumnIds"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := validReferenceInput()
			tc.mutate(&in)
			_, err := createTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1", in)
			requireValidationField(t, err, tc.field)
		})
	}
}

func TestCreateTableReferenceRequiresAtLeastOneColumn(t *testing.T) {
	in := validReferenceInput()
	in.SourceColumnIDs = nil

	_, err := createTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1", in)

	requireValidationField(t, err, "sourceColumnIds")
	assert.Contains(t, err.Error(), "at least one column")
}

func TestCreateTableReferenceRejectsTooManyColumns(t *testing.T) {
	in := validReferenceInput()
	in.SourceColumnIDs = make([]string, referenceColumnsMax+1)
	for i := range in.SourceColumnIDs {
		in.SourceColumnIDs[i] = refColumnID
	}

	_, err := createTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1", in)

	requireValidationField(t, err, "sourceColumnIds")
}

// A reference to a table on the SAME board is a relationship. Catching it here
// rather than at the app lets the message name the right tool.
func TestCreateTableReferenceRejectsSameBoardAndNamesTheRightTool(t *testing.T) {
	in := validReferenceInput()
	in.SourceWhiteboardID = in.WhiteboardID

	_, err := createTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1", in)

	requireValidationField(t, err, "sourceWhiteboardId")
	assert.Contains(t, err.Error(), "create_relationship")
}

func TestCreateTableReferencePassesEverythingThrough(t *testing.T) {
	var got appapi.CreateTableReferenceRequest
	x, y := 12.5, -4.0
	fns := tableReferenceFnsRefusing(t)
	fns.createReference = func(_ context.Context, userID string, req appapi.CreateTableReferenceRequest) (map[string]any, error) {
		assert.Equal(t, "user-1", userID)
		got = req
		return map[string]any{"table": map[string]any{"id": refNodeID}}, nil
	}
	in := validReferenceInput()
	in.PositionX, in.PositionY = &x, &y

	out, err := createTableReferenceWithFns(context.Background(), fns, "user-1", in)

	require.NoError(t, err)
	assert.Equal(t, refLocalBoardID, got.WhiteboardID)
	assert.Equal(t, refSourceBoardID, got.SourceWhiteboardID)
	assert.Equal(t, refSourceTableID, got.SourceTableID)
	assert.Equal(t, []string{refColumnID}, got.SourceColumnIDs)
	require.NotNil(t, got.PositionX)
	assert.InDelta(t, 12.5, *got.PositionX, 0.0001)
	assert.NotNil(t, out["table"])
}

// ── update ───────────────────────────────────────────────────────────────────

func TestUpdateTableReferenceRejectsAnEmptyChange(t *testing.T) {
	_, err := updateTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1",
		updateTableReferenceInput{TableID: refNodeID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Nothing was changed")
}

func TestUpdateTableReferenceRejectsSelfReference(t *testing.T) {
	self := refNodeID
	_, err := updateTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1",
		updateTableReferenceInput{TableID: refNodeID, SourceTableID: &self})

	requireValidationField(t, err, "sourceTableId")
}

func TestUpdateTableReferenceRejectsBadTableID(t *testing.T) {
	next := refSourceTableID
	_, err := updateTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1",
		updateTableReferenceInput{TableID: "nope", SourceTableID: &next})

	requireValidationField(t, err, "tableId")
}

// The deleted count must ride in the payload, not only in prose: an agent
// re-targeting in a loop needs to total it.
func TestUpdateTableReferenceReportsDeletedRelationships(t *testing.T) {
	next := refSourceTableID
	fns := tableReferenceFnsRefusing(t)
	fns.updateReference = func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
		return &appapi.UpdateTableReferenceResult{
			Reference:            map[string]any{"table": map[string]any{"id": refNodeID}},
			DeletedRelationships: 3,
		}, nil
	}

	out, err := updateTableReferenceWithFns(context.Background(), fns, "user-1",
		updateTableReferenceInput{TableID: refNodeID, SourceTableID: &next})

	require.NoError(t, err)
	assert.Equal(t, 3, out["deletedRelationships"])
	assert.NotNil(t, out["reference"])
}

// A nil result must not be dereferenced: this server has no panic recovery, so
// one nil deref would take every other tool down with it.
func TestUpdateTableReferenceSurvivesANilResult(t *testing.T) {
	next := refSourceTableID
	fns := tableReferenceFnsRefusing(t)
	fns.updateReference = func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
		return nil, nil
	}

	out, err := updateTableReferenceWithFns(context.Background(), fns, "user-1",
		updateTableReferenceInput{TableID: refNodeID, SourceTableID: &next})

	require.Error(t, err)
	assert.Nil(t, out)
	var mcpErr *mcperr.McpError
	require.True(t, errors.As(err, &mcpErr))
	assert.Equal(t, mcperr.InternalError, mcpErr.Code)
}

// ── delete ───────────────────────────────────────────────────────────────────

func TestDeleteTableReferenceRequiresConfirmName(t *testing.T) {
	_, err := deleteTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1",
		deleteTableReferenceInput{TableID: refNodeID})

	requireValidationField(t, err, "confirmName")
	assert.Contains(t, err.Error(), "list_table_references")
}

func TestDeleteTableReferenceRejectsBadTableID(t *testing.T) {
	_, err := deleteTableReferenceWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1",
		deleteTableReferenceInput{TableID: "nope", ConfirmName: "orders"})

	requireValidationField(t, err, "tableId")
}

func TestDeleteTableReferenceDeletesOnceConfirmed(t *testing.T) {
	var deletedID string
	fns := tableReferenceFnsRefusing(t)
	fns.deleteReference = func(_ context.Context, _ string, tableID string) (map[string]any, error) {
		deletedID = tableID
		return map[string]any{"table": map[string]any{"id": tableID}}, nil
	}

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	require.NoError(t, err)
	assert.Equal(t, refNodeID, deletedID)
}

// ── list ─────────────────────────────────────────────────────────────────────

func TestListTableReferencesRejectsBadWhiteboardID(t *testing.T) {
	_, err := listTableReferencesWithFns(context.Background(), tableReferenceFnsRefusing(t), "user-1",
		listTableReferencesInput{WhiteboardID: "nope"})

	requireValidationField(t, err, "whiteboardId")
}

func TestListTableReferencesReportsCountAndBoard(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.listReferences = func(context.Context, string, string) ([]map[string]any, error) {
		return []map[string]any{
			{"sourceTableName": "orders", "missing": false},
			{"sourceTableName": "payments", "missing": true},
		}, nil
	}

	out, err := listTableReferencesWithFns(context.Background(), fns, "user-1",
		listTableReferencesInput{WhiteboardID: refLocalBoardID})

	require.NoError(t, err)
	assert.Equal(t, refLocalBoardID, out["whiteboardId"])
	assert.Equal(t, 2, out["count"])
}

func TestListTableReferencesPropagatesRouteErrors(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.listReferences = func(context.Context, string, string) ([]map[string]any, error) {
		return nil, mcperr.New(mcperr.Forbidden, "no access")
	}

	_, err := listTableReferencesWithFns(context.Background(), fns, "user-1",
		listTableReferencesInput{WhiteboardID: refLocalBoardID})

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "no access"))
}

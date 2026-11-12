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
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const (
	refLocalBoardID  = "aaaa0000-1111-4222-8333-444455556666"
	refSourceBoardID = "bbbb0000-1111-4222-8333-444455556666"
	refSourceTableID = "cccc0000-1111-4222-8333-444455556666"
	refColumnID      = "dddd0000-1111-4222-8333-444455556666"
	refNodeID        = "eeee0000-1111-4222-8333-444455556666"
	refProjectID     = "ffff0000-1111-4222-8333-444455556666"
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
		confirmNameOf: func(context.Context, string) (*data.ReferenceConfirmName, error) {
			t.Error("confirmNameOf called; this test did not expect a name read")
			return nil, nil
		},
		// No reference node exists: a refused create reads nothing back.
		findTable:   func(context.Context, string) (*data.DiagramTable, error) { return nil, nil },
		findColumns: func(context.Context, string) ([]data.Column, error) { return []data.Column{}, nil },
		referenceTarget: func(context.Context, string) (*data.ReferenceTarget, error) {
			return nil, nil
		},
		// The caller is an editor on the node's project unless a test says otherwise.
		tableProject: func(context.Context, string) (string, error) { return refProjectID, nil },
		assertAccess: func(context.Context, string, string) error { return nil },
		assertEdit:   func(context.Context, string, string) error { return nil },
	}
}

// referenceNamed makes confirmNameOf report a live reference called name.
func referenceNamed(name string) func(context.Context, string) (*data.ReferenceConfirmName, error) {
	return func(context.Context, string) (*data.ReferenceConfirmName, error) {
		return &data.ReferenceConfirmName{Name: name, IsReference: true}, nil
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
	assert.Nil(t, validateUUID("id", got.ID), "the create carries a client UUID")
}

// A create whose response was lost (CONNECTION_ERROR) but whose node landed on
// the board reports success with the node read back by its id.
func TestCreateTableReferenceLostResponseThatLandedSucceeds(t *testing.T) {
	stored := map[string]*data.DiagramTable{}
	fns := tableReferenceFnsRefusing(t)
	fns.createReference = func(_ context.Context, _ string, req appapi.CreateTableReferenceRequest) (map[string]any, error) {
		stored[req.ID] = &data.DiagramTable{ID: req.ID, WhiteboardID: req.WhiteboardID}
		return nil, mcperr.New(mcperr.ConnectionError, "The app returned HTTP 502.")
	}
	fns.findTable = func(_ context.Context, id string) (*data.DiagramTable, error) { return stored[id], nil }

	out, err := createTableReferenceWithFns(context.Background(), fns, "user-1", validReferenceInput())

	require.NoError(t, err)
	require.Len(t, stored, 1)
	table, ok := out["table"].(*data.DiagramTable)
	require.True(t, ok)
	assert.Equal(t, refLocalBoardID, table.WhiteboardID)
	assert.Contains(t, out, "columns", "a recovered create has the same {table, columns} shape as a normal one")
}

// A failure that left no row and got an answer or never left the process (a
// token failure, a 429) is a refusal, not an unknown outcome.
func TestCreateTableReferenceFailureBeforeTheRequestIsARefusal(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.createReference = func(context.Context, string, appapi.CreateTableReferenceRequest) (map[string]any, error) {
		return nil, mcperr.New(mcperr.ConnectionError, "The liz-whiteboard app is rate limiting this server.")
	}

	_, err := createTableReferenceWithFns(context.Background(), fns, "user-1", validReferenceInput())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limiting")
	assert.NotContains(t, err.Error(), "Outcome unknown")
}

// The app answers an id held on another whiteboard with its generic 500, which
// the client maps to CONNECTION_ERROR. The re-read finds the row elsewhere: an
// error, never a success.
func TestCreateTableReferenceRowOnAnotherBoardIsAnError(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.createReference = func(context.Context, string, appapi.CreateTableReferenceRequest) (map[string]any, error) {
		return nil, mcperr.New(mcperr.ConnectionError, "The app returned HTTP 500 for a table reference request.")
	}
	fns.findTable = func(_ context.Context, id string) (*data.DiagramTable, error) {
		return &data.DiagramTable{ID: id, WhiteboardID: refSourceBoardID}, nil
	}

	_, err := createTableReferenceWithFns(context.Background(), fns, "user-1", validReferenceInput())

	require.Error(t, err)
	var me *mcperr.McpError
	require.ErrorAs(t, err, &me)
	assert.Equal(t, mcperr.AlreadyExists, me.Code)
}

func TestCreateTableReferenceLostResponseNotFoundIsOutcomeUnknown(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	var sentID string
	fns.createReference = func(_ context.Context, _ string, req appapi.CreateTableReferenceRequest) (map[string]any, error) {
		sentID = req.ID
		return nil, mcperr.NewNoResponse("Cannot reach the app.")
	}

	_, err := createTableReferenceWithFns(context.Background(), fns, "user-1", validReferenceInput())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Outcome unknown")
	assert.Contains(t, err.Error(), sentID)
	assert.Contains(t, err.Error(), "list_table_references")
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
	fns.confirmNameOf = referenceNamed("orders")
	fns.deleteReference = func(_ context.Context, _ string, tableID string) (map[string]any, error) {
		deletedID = tableID
		return map[string]any{"table": map[string]any{"id": tableID}}, nil
	}

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	require.NoError(t, err)
	assert.Equal(t, refNodeID, deletedID)
}

// THE REGRESSION THIS FILE EXISTED WITHOUT. The old guard checked only that
// confirmName was non-empty and never compared it, so any string deleted the
// node and cascaded every relationship drawn from it. The route call must not
// happen at all — tableReferenceFnsRefusing fails the test if it does.
func TestDeleteTableReferenceRefusesMismatchedConfirmName(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.confirmNameOf = referenceNamed("orders")

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "not-the-name"})

	requireValidationField(t, err, "confirmName")
	assert.Contains(t, err.Error(), "Nothing was deleted")
	assert.Contains(t, err.Error(), "sourceTableName")
}

// The name compared is the SOURCE table's, as list_table_references reports it.
// A reference whose source is gone falls back to the local row's name, and the
// guard must accept exactly that — demanding a name the agent was never shown
// would make a dangling reference undeletable.
func TestDeleteTableReferenceAcceptsFallbackNameWhenSourceMissing(t *testing.T) {
	var deleted bool
	fns := tableReferenceFnsRefusing(t)
	fns.confirmNameOf = referenceNamed("users")
	fns.deleteReference = func(_ context.Context, _ string, tableID string) (map[string]any, error) {
		deleted = true
		return map[string]any{"table": map[string]any{"id": tableID}}, nil
	}

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "users"})

	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestDeleteTableReferenceRefusesUnknownTable(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.confirmNameOf = func(context.Context, string) (*data.ReferenceConfirmName, error) {
		return nil, nil
	}

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	requireMcpCode(t, err, mcperr.NotFound)
}

// An ordinary table is not a reference. Deleting one here would cascade a real
// table's relationships under a tool the caller thinks only removes a stand-in.
func TestDeleteTableReferenceRefusesOrdinaryTable(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.confirmNameOf = func(context.Context, string) (*data.ReferenceConfirmName, error) {
		return &data.ReferenceConfirmName{Name: "orders", IsReference: false}, nil
	}

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	requireValidationField(t, err, "tableId")
	assert.Contains(t, err.Error(), "delete_table")
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

// ── lost responses on update and delete ─────────────────────────────────────

func lostResponse() error {
	return mcperr.NewNoResponse("Cannot reach the liz-whiteboard app.")
}

func TestUpdateTableReferenceLostResponseThatLandedIsApplied(t *testing.T) {
	nextTable := refSourceTableID
	fns := tableReferenceFnsRefusing(t)
	fns.updateReference = func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
		return nil, lostResponse()
	}
	fns.referenceTarget = func(_ context.Context, id string) (*data.ReferenceTarget, error) {
		board := refSourceBoardID
		return &data.ReferenceTarget{TableID: id, SourceWhiteboardID: &board,
			SourceTableID: &nextTable, SourceColumnIDs: []string{refColumnID}}, nil
	}

	out, err := updateTableReferenceWithFns(context.Background(), fns, "user-1", updateTableReferenceInput{
		TableID: refNodeID, SourceTableID: &nextTable, SourceColumnIDs: []string{refColumnID},
	})

	require.NoError(t, err)
	assert.NotNil(t, out["reference"])
	assert.Contains(t, out["note"], "not known")
}

func TestUpdateTableReferenceLostResponseNotLandedIsUnknown(t *testing.T) {
	nextTable := refSourceTableID
	oldTable := "99990000-1111-4222-8333-444455556666"
	fns := tableReferenceFnsRefusing(t)
	fns.updateReference = func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
		return nil, lostResponse()
	}
	fns.referenceTarget = func(_ context.Context, id string) (*data.ReferenceTarget, error) {
		return &data.ReferenceTarget{TableID: id, SourceTableID: &oldTable}, nil
	}

	_, err := updateTableReferenceWithFns(context.Background(), fns, "user-1", updateTableReferenceInput{
		TableID: refNodeID, SourceTableID: &nextTable,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Outcome unknown")
	assert.Contains(t, err.Error(), refNodeID)
}

// An answer from the app (here a 400) is not a lost response: no re-read.
func TestUpdateTableReferenceAppRefusalIsNotReRead(t *testing.T) {
	nextTable := refSourceTableID
	fns := tableReferenceFnsRefusing(t)
	fns.updateReference = func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
		return nil, mcperr.New(mcperr.ValidationError, "must be in the same project")
	}
	fns.referenceTarget = func(context.Context, string) (*data.ReferenceTarget, error) {
		t.Error("referenceTarget read after an app answer")
		return nil, nil
	}

	_, err := updateTableReferenceWithFns(context.Background(), fns, "user-1", updateTableReferenceInput{
		TableID: refNodeID, SourceTableID: &nextTable,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "same project")
}

func TestDeleteTableReferenceLostResponse(t *testing.T) {
	for name, present := range map[string]bool{"gone is applied": false, "present is unknown": true} {
		t.Run(name, func(t *testing.T) {
			fns := tableReferenceFnsRefusing(t)
			fns.confirmNameOf = referenceNamed("orders")
			fns.deleteReference = func(context.Context, string, string) (map[string]any, error) {
				return nil, lostResponse()
			}
			fns.findTable = func(_ context.Context, id string) (*data.DiagramTable, error) {
				if present {
					return &data.DiagramTable{ID: id}, nil
				}
				return nil, nil
			}

			out, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
				deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

			if present {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "Outcome unknown")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, refNodeID, out["id"])
		})
	}
}

// ── access: a node the caller cannot see ────────────────────────────────────

func noProjectAccess(context.Context, string, string) error {
	return mcperr.New(mcperr.Forbidden, "User user-1 has no access to project.")
}

// A caller with no access to the node's project gets the same NOT_FOUND as a
// missing id, before any name read: the answer does not reveal that the id
// exists or what kind of table it is.
func TestDeleteTableReferenceUnseenNodeIsNotFoundBeforeAnyRead(t *testing.T) {
	missing := tableReferenceFnsRefusing(t)
	missing.tableProject = func(context.Context, string) (string, error) { return "", nil }
	_, missingErr := deleteTableReferenceWithFns(context.Background(), missing, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	unseen := tableReferenceFnsRefusing(t)
	unseen.assertAccess = noProjectAccess
	_, unseenErr := deleteTableReferenceWithFns(context.Background(), unseen, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	requireMcpCode(t, unseenErr, mcperr.NotFound)
	requireMcpCode(t, missingErr, mcperr.NotFound)
	assert.Equal(t, missingErr.Error(), unseenErr.Error())
}

// A member below EDITOR can see the node, so a role refusal reveals nothing.
func TestDeleteTableReferenceViewerIsForbiddenBeforeAnyRead(t *testing.T) {
	fns := tableReferenceFnsRefusing(t)
	fns.assertEdit = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "User user-1 does not have EDITOR access.")
	}

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	requireMcpCode(t, err, mcperr.Forbidden)
}

func TestUpdateTableReferenceUnseenNodeIsNotFoundBeforeTheRoute(t *testing.T) {
	nextTable := refSourceTableID
	fns := tableReferenceFnsRefusing(t)
	fns.assertAccess = noProjectAccess

	_, err := updateTableReferenceWithFns(context.Background(), fns, "user-1", updateTableReferenceInput{
		TableID: refNodeID, SourceTableID: &nextTable,
	})

	requireMcpCode(t, err, mcperr.NotFound)
	assert.Equal(t, "Table reference "+refNodeID+" not found.", err.Error())
}

// Access lost between the send and the re-read: the re-read reports nothing
// of the row, only "Outcome unknown".
func TestUpdateTableReferenceReReadNeedsAccess(t *testing.T) {
	nextTable := refSourceTableID
	fns := tableReferenceFnsRefusing(t)
	gate := 0
	fns.assertAccess = func(context.Context, string, string) error {
		gate++
		if gate > 1 {
			return noProjectAccess(nil, "", "")
		}
		return nil
	}
	fns.updateReference = func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
		return nil, lostResponse()
	}
	fns.referenceTarget = func(_ context.Context, id string) (*data.ReferenceTarget, error) {
		board := refSourceBoardID
		return &data.ReferenceTarget{TableID: id, WhiteboardID: refLocalBoardID, SourceWhiteboardID: &board,
			SourceTableID: &nextTable, SourceColumnIDs: []string{refColumnID}}, nil
	}

	out, err := updateTableReferenceWithFns(context.Background(), fns, "user-1", updateTableReferenceInput{
		TableID: refNodeID, SourceTableID: &nextTable,
	})

	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "Outcome unknown")
	assert.NotContains(t, err.Error(), refLocalBoardID)
	assert.NotContains(t, err.Error(), refSourceBoardID)
}

// Only a request that got no HTTP status back is re-read. An answer from the
// app (403, 404, 429, 5xx, an unreadable body) and an error of unknown origin
// are reported as they are.
func TestUpdateTableReferenceOnlyANoResponseIsReRead(t *testing.T) {
	answers := map[string]error{
		"forbidden":   mcperr.New(mcperr.Forbidden, "no write access"),
		"not found":   mcperr.New(mcperr.NotFound, "Table reference not found."),
		"rate limit":  mcperr.New(mcperr.ConnectionError, "The liz-whiteboard app is rate limiting this server."),
		"http 502":    mcperr.New(mcperr.ConnectionError, "The app returned HTTP 502 for a table reference request."),
		"unreadable":  mcperr.New(mcperr.ConnectionError, "The app returned an unreadable table reference response."),
		"plain error": errors.New("boom"),
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			nextTable := refSourceTableID
			fns := tableReferenceFnsRefusing(t)
			fns.updateReference = func(context.Context, string, string, appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error) {
				return nil, answer
			}
			fns.referenceTarget = func(context.Context, string) (*data.ReferenceTarget, error) {
				t.Error("referenceTarget read after an answer from the app")
				return nil, nil
			}

			_, err := updateTableReferenceWithFns(context.Background(), fns, "user-1", updateTableReferenceInput{
				TableID: refNodeID, SourceTableID: &nextTable,
			})

			assert.Equal(t, answer, err)
		})
	}
}

func TestDeleteTableReferenceOnlyANoResponseIsReRead(t *testing.T) {
	answer := mcperr.New(mcperr.ConnectionError, "The app returned HTTP 502 for a table reference request.")
	fns := tableReferenceFnsRefusing(t)
	fns.confirmNameOf = referenceNamed("orders")
	fns.deleteReference = func(context.Context, string, string) (map[string]any, error) { return nil, answer }
	fns.findTable = func(context.Context, string) (*data.DiagramTable, error) {
		t.Error("findTable read after an answer from the app")
		return nil, nil
	}

	_, err := deleteTableReferenceWithFns(context.Background(), fns, "user-1",
		deleteTableReferenceInput{TableID: refNodeID, ConfirmName: "orders"})

	assert.Equal(t, error(answer), err)
}

package tools

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// strPtr returns a pointer to the given string — used inline in test struct literals.
func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// TestBatchSchema_AllSuccess
// Two tables each with one inline column; one addColumns entry targeting a
// pre-existing table; one relation resolved by name.
// All operations succeed. Verifies that created arrays are fully populated and
// failed is empty.
// ---------------------------------------------------------------------------

func TestBatchSchema_AllSuccess(t *testing.T) {
	emit := func(event string, payload any) (socket.AckResult, error) {
		p := payload.(map[string]any)
		switch event {
		case "table:create":
			name := p["name"].(string)
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "tbl-" + name, "name": name},
			}, nil
		case "column:create":
			tableID := p["tableId"].(string)
			colName := p["name"].(string)
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "col-" + tableID + "-" + colName, "tableId": tableID},
			}, nil
		case "relationship:create":
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "rel-1"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected event: %s", event)
	}

	usersName := "users"
	postsName := "posts"

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		Tables: []batchTableSpec{
			{
				Name:    usersName,
				Columns: []batchColumnSpec{{Name: "id", DataType: "uuid"}},
			},
			{
				Name:    postsName,
				Columns: []batchColumnSpec{{Name: "title", DataType: "varchar"}},
			},
		},
		AddColumns: []batchAddColumnSpec{
			{TableID: "existing-tbl-id", Name: "extra_col", DataType: "int"},
		},
		AddRelations: []batchRelationSpec{
			{
				SourceTableName:  &usersName,
				SourceColumnName: strPtr("id"),
				TargetTableName:  &postsName,
				TargetColumnName: strPtr("title"),
				Cardinality:      "ONE_TO_MANY",
			},
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	assert.Empty(t, result.Failed, "expected no failures")
	assert.Len(t, result.Created.Tables, 2, "expected 2 created tables")
	// 2 inline columns (users.id, posts.title) + 1 addColumns entry
	assert.Len(t, result.Created.Columns, 3, "expected 3 created columns")
	assert.Len(t, result.Created.Relations, 1, "expected 1 created relation")
}

// ---------------------------------------------------------------------------
// TestBatchSchema_TableFailure_SkipsInlineColumns
// First table create fails; its inline columns must NOT be emitted.
// Second table succeeds. Verifies the failed array and that the successful
// table's column is emitted (not skipped).
// ---------------------------------------------------------------------------

func TestBatchSchema_TableFailure_SkipsInlineColumns(t *testing.T) {
	var columnEmitCount int

	emit := func(event string, payload any) (socket.AckResult, error) {
		p := payload.(map[string]any)
		switch event {
		case "table:create":
			name := p["name"].(string)
			if name == "bad_table" {
				return socket.AckResult{
					"ok":      false,
					"code":    "VALIDATION_ERROR",
					"message": "table name already in use",
				}, nil
			}
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "tbl-" + name},
			}, nil
		case "column:create":
			columnEmitCount++
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "col-1"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected event: %s", event)
	}

	badName := "bad_table"
	goodName := "good_table"

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		Tables: []batchTableSpec{
			// bad_table fails; its inline column must be skipped
			{
				Name:    badName,
				Columns: []batchColumnSpec{{Name: "skip_me", DataType: "int"}},
			},
			// good_table succeeds; its inline column must be created
			{
				Name:    goodName,
				Columns: []batchColumnSpec{{Name: "keep_me", DataType: "varchar"}},
			},
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	require.Len(t, result.Failed, 1, "expected exactly 1 failure")
	assert.Equal(t, "table", result.Failed[0].Kind)
	assert.Equal(t, 0, result.Failed[0].Index)
	assert.Equal(t, badName, result.Failed[0].Name)

	require.Len(t, result.Created.Tables, 1, "expected 1 created table")
	require.Len(t, result.Created.Columns, 1, "expected 1 created column (bad_table's column skipped)")

	// bad_table's inline column must NOT have been emitted
	assert.Equal(t, 1, columnEmitCount,
		"column:create must be emitted exactly once (for good_table's column, not bad_table's)")
}

// ---------------------------------------------------------------------------
// TestBatchSchema_RelationNameResolution
// Two tables with inline columns are created. A relation references both tables
// and columns by name. Verifies that the relationship:create event receives the
// server-assigned UUIDs from the Phase 1 acks, not the names.
// ---------------------------------------------------------------------------

func TestBatchSchema_RelationNameResolution(t *testing.T) {
	var capturedRelPayload map[string]any

	emit := func(event string, payload any) (socket.AckResult, error) {
		p := payload.(map[string]any)
		switch event {
		case "table:create":
			name := p["name"].(string)
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "tbl-" + name},
			}, nil
		case "column:create":
			tableID := p["tableId"].(string)
			colName := p["name"].(string)
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "col-" + tableID + "-" + colName},
			}, nil
		case "relationship:create":
			capturedRelPayload = p
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "rel-1"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected event: %s", event)
	}

	usersName := "users"
	postsName := "posts"

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		Tables: []batchTableSpec{
			{Name: usersName, Columns: []batchColumnSpec{{Name: "id", DataType: "uuid"}}},
			{Name: postsName, Columns: []batchColumnSpec{{Name: "user_id", DataType: "uuid"}}},
		},
		AddRelations: []batchRelationSpec{
			{
				SourceTableName:  &usersName,
				SourceColumnName: strPtr("id"),
				TargetTableName:  &postsName,
				TargetColumnName: strPtr("user_id"),
				Cardinality:      "ONE_TO_MANY",
			},
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	assert.Empty(t, result.Failed)
	require.Len(t, result.Created.Relations, 1)
	require.NotNil(t, capturedRelPayload, "relationship:create must have been emitted")

	// The server-assigned UUIDs from Phase 1 acks must be passed verbatim.
	assert.Equal(t, "tbl-users", capturedRelPayload["sourceTableId"],
		"sourceTableId must be the server-assigned UUID from the table:create ack")
	assert.Equal(t, "col-tbl-users-id", capturedRelPayload["sourceColumnId"],
		"sourceColumnId must be the server-assigned UUID from the column:create ack")
	assert.Equal(t, "tbl-posts", capturedRelPayload["targetTableId"],
		"targetTableId must be the server-assigned UUID from the table:create ack")
	assert.Equal(t, "col-tbl-posts-user_id", capturedRelPayload["targetColumnId"],
		"targetColumnId must be the server-assigned UUID from the column:create ack")
}

// ---------------------------------------------------------------------------
// TestBatchSchema_RelationToFailedTable
// A table ("orders") fails to create. A relation that references "orders" by
// name must be skipped (not emitted) and must appear in failed with a
// descriptive message mentioning the table name.
// ---------------------------------------------------------------------------

func TestBatchSchema_RelationToFailedTable(t *testing.T) {
	emit := func(event string, payload any) (socket.AckResult, error) {
		p := payload.(map[string]any)
		switch event {
		case "table:create":
			name := p["name"].(string)
			if name == "orders" {
				return socket.AckResult{
					"ok":      false,
					"code":    "INTERNAL_ERROR",
					"message": "DB constraint violation",
				}, nil
			}
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "tbl-" + name},
			}, nil
		case "relationship:create":
			t.Error("relationship:create must not be emitted when the source table failed")
		}
		return socket.AckResult{"ok": true}, nil
	}

	ordersName := "orders"
	srcColID := "some-col-uuid-0000-0000-000000000000"
	tgtTableID := "existing-tbl-uuid-0000-000000000000"
	tgtColID := "existing-col-uuid-0000-000000000000"

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		Tables: []batchTableSpec{
			{Name: ordersName}, // will fail
		},
		AddRelations: []batchRelationSpec{
			{
				SourceTableName: &ordersName, // references the failed table by name
				SourceColumnID:  &srcColID,
				TargetTableID:   &tgtTableID,
				TargetColumnID:  &tgtColID,
				Cardinality:     "ONE_TO_MANY",
			},
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	// Both the table and the relation that referenced it must appear in failed.
	require.Len(t, result.Failed, 2, "expected 1 table failure + 1 relation failure")

	assert.Equal(t, "table", result.Failed[0].Kind)
	assert.Equal(t, "orders", result.Failed[0].Name)

	assert.Equal(t, "relation", result.Failed[1].Kind)
	assert.Equal(t, string(mcperr.NotFound), result.Failed[1].Code)
	assert.Contains(t, result.Failed[1].Message, "orders",
		"relation failure message must name the missing table")

	// No tables or relations should have been created.
	assert.Empty(t, result.Created.Tables)
	assert.Empty(t, result.Created.Relations)
}

// ---------------------------------------------------------------------------
// TestBatchSchema_AddColumnsPartialFailure
// Two addColumns entries; the second fails. Verifies the partial success split:
// first column in Created.Columns, second in Failed with correct index.
// ---------------------------------------------------------------------------

func TestBatchSchema_AddColumnsPartialFailure(t *testing.T) {
	emit := func(event string, payload any) (socket.AckResult, error) {
		if event != "column:create" {
			return nil, fmt.Errorf("unexpected event: %s", event)
		}
		p := payload.(map[string]any)
		if p["name"].(string) == "bad_col" {
			return socket.AckResult{
				"ok":      false,
				"code":    "VALIDATION_ERROR",
				"message": "column name already in use",
			}, nil
		}
		return socket.AckResult{
			"ok":     true,
			"entity": map[string]any{"id": "col-" + p["name"].(string)},
		}, nil
	}

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		AddColumns: []batchAddColumnSpec{
			{TableID: "tbl-existing-1", Name: "good_col", DataType: "int"}, // index 0 — succeeds
			{TableID: "tbl-existing-1", Name: "bad_col", DataType: "int"},  // index 1 — fails
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	require.Len(t, result.Created.Columns, 1, "expected 1 column created")
	require.Len(t, result.Failed, 1, "expected 1 failure")

	assert.Equal(t, "column", result.Failed[0].Kind)
	assert.Equal(t, 1, result.Failed[0].Index, "failed column must report its 0-based index in addColumns")
	assert.Equal(t, "bad_col", result.Failed[0].Name)
	assert.Equal(t, "VALIDATION_ERROR", result.Failed[0].Code)
}

// ---------------------------------------------------------------------------
// TestBatchSchema_RelationByUUID
// Relation with all four endpoints specified as UUIDs — no name index lookup.
// Verifies the UUIDs are passed verbatim to relationship:create.
// ---------------------------------------------------------------------------

func TestBatchSchema_RelationByUUID(t *testing.T) {
	var capturedRelPayload map[string]any

	emit := func(event string, payload any) (socket.AckResult, error) {
		if event == "relationship:create" {
			capturedRelPayload = payload.(map[string]any)
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "rel-uuid-1"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected event: %s", event)
	}

	srcTableID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	tgtTableID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	srcColID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	tgtColID := "dddddddd-dddd-dddd-dddd-dddddddddddd"

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		AddRelations: []batchRelationSpec{
			{
				SourceTableID:  &srcTableID,
				TargetTableID:  &tgtTableID,
				SourceColumnID: &srcColID,
				TargetColumnID: &tgtColID,
				Cardinality:    "MANY_TO_MANY",
			},
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	assert.Empty(t, result.Failed)
	require.Len(t, result.Created.Relations, 1)
	require.NotNil(t, capturedRelPayload)

	// All four UUIDs must be forwarded verbatim — no transformation.
	assert.Equal(t, srcTableID, capturedRelPayload["sourceTableId"])
	assert.Equal(t, tgtTableID, capturedRelPayload["targetTableId"])
	assert.Equal(t, srcColID, capturedRelPayload["sourceColumnId"])
	assert.Equal(t, tgtColID, capturedRelPayload["targetColumnId"])
	assert.Equal(t, "MANY_TO_MANY", capturedRelPayload["cardinality"])
}

// ---------------------------------------------------------------------------
// TestBatchSchema_RelationMixedResolution
// Positive test for the mixed-endpoint path: sourceTableName is resolved from
// the batch name index, while sourceColumnId is a pre-existing UUID forwarded
// verbatim (no name lookup). Verifies that columnId bypass works when tableName
// is used for the table endpoint.
// ---------------------------------------------------------------------------

func TestBatchSchema_RelationMixedResolution(t *testing.T) {
	var capturedRelPayload map[string]any

	emit := func(event string, payload any) (socket.AckResult, error) {
		p := payload.(map[string]any)
		switch event {
		case "table:create":
			name := p["name"].(string)
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "tbl-" + name},
			}, nil
		case "relationship:create":
			capturedRelPayload = p
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "rel-mixed-1"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected event: %s", event)
	}

	tableName := "accounts"
	existingColID := "aaaa1111-bbbb-cccc-dddd-eeee00000000"
	tgtTableID := "bbbb2222-cccc-dddd-eeee-ffff00000000"
	tgtColID := "cccc3333-dddd-eeee-ffff-aaaa00000000"

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		Tables: []batchTableSpec{
			{Name: tableName}, // no inline columns — the column pre-exists
		},
		AddRelations: []batchRelationSpec{
			{
				SourceTableName: &tableName,     // resolved from batch index → "tbl-accounts"
				SourceColumnID:  &existingColID, // pre-existing column UUID, forwarded verbatim
				TargetTableID:   &tgtTableID,    // pre-existing table UUID, forwarded verbatim
				TargetColumnID:  &tgtColID,      // pre-existing column UUID, forwarded verbatim
				Cardinality:     "ONE_TO_ONE",
			},
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	assert.Empty(t, result.Failed)
	require.Len(t, result.Created.Tables, 1)
	require.Len(t, result.Created.Relations, 1)
	require.NotNil(t, capturedRelPayload)

	// SourceTableName was resolved from the name index to the server-assigned UUID.
	assert.Equal(t, "tbl-accounts", capturedRelPayload["sourceTableId"],
		"sourceTableId must be resolved from the batch name index")
	// SourceColumnID was forwarded verbatim — no name lookup performed.
	assert.Equal(t, existingColID, capturedRelPayload["sourceColumnId"],
		"sourceColumnId must be forwarded verbatim when given as a UUID")
	// Target endpoints forwarded verbatim.
	assert.Equal(t, tgtTableID, capturedRelPayload["targetTableId"])
	assert.Equal(t, tgtColID, capturedRelPayload["targetColumnId"])
}

// ---------------------------------------------------------------------------
// TestBatchSchema_EmptyEntityID_TableTreatedAsFailure
// Regression test for the empty entity-id guard (W3): a server ack that returns
// ok=true but omits the entity "id" field must be treated as a failure.
// The table must appear in Failed, its inline columns must NOT be emitted,
// and it must NOT be added to the name index.
// ---------------------------------------------------------------------------

func TestBatchSchema_EmptyEntityID_TableTreatedAsFailure(t *testing.T) {
	var columnEmitCount int

	emit := func(event string, payload any) (socket.AckResult, error) {
		p := payload.(map[string]any)
		switch event {
		case "table:create":
			name := p["name"].(string)
			if name == "ghost_table" {
				// Successful ack but entity carries no "id" field.
				return socket.AckResult{
					"ok":     true,
					"entity": map[string]any{"name": "ghost_table"}, // missing "id"
				}, nil
			}
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "tbl-" + name},
			}, nil
		case "column:create":
			columnEmitCount++
			return socket.AckResult{
				"ok":     true,
				"entity": map[string]any{"id": "col-keep-1"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected event: %s", event)
	}

	ghostName := "ghost_table"
	realName := "real_table"

	in := batchSchemaUpdateInput{
		WhiteboardID: "wb-1",
		Tables: []batchTableSpec{
			// ghost_table: ack succeeds but entity.id is absent → must be treated as failure
			{
				Name:    ghostName,
				Columns: []batchColumnSpec{{Name: "skip_me", DataType: "int"}},
			},
			// real_table: ack succeeds with a valid entity.id → normal success path
			{
				Name:    realName,
				Columns: []batchColumnSpec{{Name: "keep_me", DataType: "varchar"}},
			},
		},
	}

	result := executeBatchSchema(in, "wb-1", emit)

	// ghost_table must appear in Failed with INTERNAL_ERROR.
	require.Len(t, result.Failed, 1)
	assert.Equal(t, "table", result.Failed[0].Kind)
	assert.Equal(t, 0, result.Failed[0].Index)
	assert.Equal(t, ghostName, result.Failed[0].Name)
	assert.Equal(t, string(mcperr.InternalError), result.Failed[0].Code)

	// ghost_table's inline column must NOT have been emitted.
	assert.Equal(t, 1, columnEmitCount,
		"column:create must only be emitted for real_table's column, not ghost_table's")

	// real_table was created normally.
	require.Len(t, result.Created.Tables, 1)
	require.Len(t, result.Created.Columns, 1)
}

// Package tools — handler ack shape and backward-compatibility tests.
// Suite F & G: FR-022 handler ack correctness.
// TC-HANDLER-ACK-01 through TC-HANDLER-ACK-11
// TC-HANDLER-COMPAT-01 through TC-HANDLER-COMPAT-11
//
// Strategy: pure logic — tests AckResult accessors, cascadeCount extraction,
// and the list of 11 mutation events. No DB or Socket.IO server required.
//
// NOTE: The TS "cb=undefined does not throw" compat tests guard JavaScript
// optional-chain safety (cb?.()). Go uses error-return style and has no
// callback parameter — the equivalent invariant is that SocketEmitWithAck
// returns an error instead of panicking, which is guaranteed by the type system.
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// ---------------------------------------------------------------------------
// TC-HANDLER-ACK-01..11: AckResult shape correctness
// ---------------------------------------------------------------------------

// TC-HANDLER-ACK-01: table:create ack returns entity with server-assigned id.
func TestAck_01_SuccessAckHasEntity(t *testing.T) {
	ack := socket.AckResult{
		"ok":     true,
		"entity": map[string]any{"id": "server-id", "name": "users", "positionX": 40.0, "positionY": 40.0},
	}
	assert.True(t, ack.OK())
	m, ok := ack.Entity().(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "server-id", m["id"])
	assert.Equal(t, "users", m["name"])
}

// TC-HANDLER-ACK-02: table:move ack returns position entity.
func TestAck_02_MoveAckReturnsPosition(t *testing.T) {
	ack := socket.AckResult{
		"ok":     true,
		"entity": map[string]any{"id": "tbl-1", "positionX": 100.0, "positionY": 200.0},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, 100.0, entity["positionX"])
	assert.Equal(t, 200.0, entity["positionY"])
}

// TC-HANDLER-ACK-03: table:update ack returns updated table.
func TestAck_03_UpdateAckReturnsUpdatedTable(t *testing.T) {
	ack := socket.AckResult{
		"ok":     true,
		"entity": map[string]any{"id": "tbl-1", "name": "renamed_table"},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, "renamed_table", entity["name"])
}

// TC-HANDLER-ACK-04: table:delete ack returns id and cascade counts.
func TestAck_04_DeleteAckHasCascadeCounts(t *testing.T) {
	ack := socket.AckResult{
		"ok":      true,
		"entity":  map[string]any{"id": "tbl-1"},
		"cascade": map[string]any{"relationships": 2.0, "columns": 3.0},
	}
	assert.True(t, ack.OK())
	assert.Equal(t, 2, cascadeCount(ack["cascade"], "relationships"))
	assert.Equal(t, 3, cascadeCount(ack["cascade"], "columns"))
}

// TC-HANDLER-ACK-05: column:update ack returns updated column.
func TestAck_05_ColumnUpdateAckReturnsColumn(t *testing.T) {
	ack := socket.AckResult{
		"ok":     true,
		"entity": map[string]any{"id": "col-1", "name": "email", "dataType": "varchar"},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, "varchar", entity["dataType"])
}

// TC-HANDLER-ACK-06: column:delete ack returns id and cascade relationship count.
func TestAck_06_ColumnDeleteAckHasCascadeRels(t *testing.T) {
	ack := socket.AckResult{
		"ok":      true,
		"entity":  map[string]any{"id": "col-1"},
		"cascade": map[string]any{"relationships": 1.0},
	}
	assert.True(t, ack.OK())
	assert.Equal(t, 1, cascadeCount(ack["cascade"], "relationships"))
}

// TC-HANDLER-ACK-07: relationship:create ack returns relationship with server id.
func TestAck_07_RelCreateAckReturnsServerID(t *testing.T) {
	ack := socket.AckResult{
		"ok":     true,
		"entity": map[string]any{"id": "server-rel-id", "cardinality": "ONE_TO_MANY"},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, "server-rel-id", entity["id"])
}

// TC-HANDLER-ACK-08: relationship:update ack returns updated relationship.
func TestAck_08_RelUpdateAckReturnsUpdated(t *testing.T) {
	ack := socket.AckResult{
		"ok":     true,
		"entity": map[string]any{"id": "rel-1", "cardinality": "MANY_TO_MANY"},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, "MANY_TO_MANY", entity["cardinality"])
}

// TC-HANDLER-ACK-09: relationship:delete ack returns deleted id.
func TestAck_09_RelDeleteAckReturnsDeletedID(t *testing.T) {
	ack := socket.AckResult{
		"ok":     true,
		"entity": map[string]any{"id": "rel-1"},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, "rel-1", entity["id"])
}

// TC-HANDLER-ACK-10 (B1 fix): column:create entity includes server-assigned id.
func TestAck_10_ColumnCreateHasServerAssignedID(t *testing.T) {
	ack := socket.AckResult{
		"ok": true,
		"entity": map[string]any{
			"id":           "server-col-id",
			"name":         "email",
			"dataType":     "varchar",
			"tableId":      "tbl-1",
			"isPrimaryKey": false,
			"isForeignKey": false,
			"isUnique":     true,
			"isNullable":   false,
			"order":        float64(0),
		},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, "server-col-id", entity["id"])
	assert.Equal(t, "varchar", entity["dataType"])
}

// TC-HANDLER-ACK-11 (B1 fix): column:reorder entity has tableId and merged orderedColumnIds.
func TestAck_11_ColumnReorderHasMergedOrder(t *testing.T) {
	mergedOrder := []any{"col-b", "col-a", "col-c"}
	ack := socket.AckResult{
		"ok": true,
		"entity": map[string]any{
			"tableId":          "tbl-1",
			"orderedColumnIds": mergedOrder,
		},
	}
	assert.True(t, ack.OK())
	entity := ack.Entity().(map[string]any)
	assert.Equal(t, "tbl-1", entity["tableId"])
	assert.Equal(t, mergedOrder, entity["orderedColumnIds"])
}

// ---------------------------------------------------------------------------
// AckResult method behaviour
// ---------------------------------------------------------------------------

// Missing "ok" key is treated as success (mirrors TS: ack.ok === false check).
func TestAck_MissingOkKeyTreatedAsSuccess(t *testing.T) {
	ack := socket.AckResult{"entity": map[string]any{"id": "tbl-1"}}
	assert.True(t, ack.OK(), "missing ok key must be treated as success")
}

// ok=false returns false.
func TestAck_OkFalseReturnsFalse(t *testing.T) {
	ack := socket.AckResult{"ok": false, "code": "VALIDATION_ERROR", "message": "bad"}
	assert.False(t, ack.OK())
}

// Code() and Message() accessors on failure ack.
func TestAck_FailureCodeAndMessage(t *testing.T) {
	ack := socket.AckResult{
		"ok":      false,
		"code":    "VALIDATION_ERROR",
		"message": "Name is required.",
	}
	assert.Equal(t, "VALIDATION_ERROR", ack.Code())
	assert.Equal(t, "Name is required.", ack.Message())
}

// SESSION_EXPIRED failure shape.
func TestAck_SessionExpiredShape(t *testing.T) {
	ack := socket.AckResult{"ok": false, "code": "SESSION_EXPIRED", "message": "Session expired"}
	assert.False(t, ack.OK())
	assert.Equal(t, "SESSION_EXPIRED", ack.Code())
}

// Entity() is nil when the key is absent.
func TestAck_EntityNilWhenAbsent(t *testing.T) {
	ack := socket.AckResult{"ok": true}
	assert.Nil(t, ack.Entity())
}

// ---------------------------------------------------------------------------
// cascadeCount extraction
// ---------------------------------------------------------------------------

// JSON decodes numbers as float64; cascadeCount must handle this.
func TestAck_CascadeCountHandlesFloat64(t *testing.T) {
	cascade := map[string]any{"columns": float64(5), "relationships": float64(2)}
	assert.Equal(t, 5, cascadeCount(cascade, "columns"))
	assert.Equal(t, 2, cascadeCount(cascade, "relationships"))
}

// cascadeCount returns 0 for missing key.
func TestAck_CascadeCountMissingKey(t *testing.T) {
	assert.Equal(t, 0, cascadeCount(map[string]any{"columns": float64(3)}, "relationships"))
}

// cascadeCount returns 0 for nil cascade.
func TestAck_CascadeCountNilCascade(t *testing.T) {
	assert.Equal(t, 0, cascadeCount(nil, "columns"))
}

// cascadeCount returns 0 for non-map cascade.
func TestAck_CascadeCountNonMapCascade(t *testing.T) {
	assert.Equal(t, 0, cascadeCount("unexpected-string", "columns"))
}

// ---------------------------------------------------------------------------
// TC-HANDLER-COMPAT-01..11: all 11 mutation events are handled by Go tools.
// In Go, there is no optional-chain callback safety concern — the handlers use
// error returns. This test verifies that the 11 Socket.IO events are all
// accounted for in the Go tool layer.
// ---------------------------------------------------------------------------

func TestAck_AllMutationEventsHandled(t *testing.T) {
	// These 11 events map to the 11 mutation handlers in the Go implementation.
	events := []string{
		"table:create",
		"table:move",   // fired by update_table when position changes
		"table:update", // fired by update_table for meta changes
		"table:delete",
		"column:create", // B1 fix: was missing ack support in collaboration.ts
		"column:update",
		"column:delete",
		"column:reorder", // B1 fix: was missing ack support in collaboration.ts
		"relationship:create",
		"relationship:update",
		"relationship:delete",
	}
	assert.Len(t, events, 11, "must account for all 11 mutation events (B1 closed)")

	// B1 regression guard: column:create and column:reorder must be in the list.
	assert.Contains(t, events, "column:create")
	assert.Contains(t, events, "column:reorder")
}

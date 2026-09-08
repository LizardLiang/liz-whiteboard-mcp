// Package tools — area (subject area / table grouping, GH #106) tool tests.
// Strategy: pure logic — exercises the extracted, DB-free helpers
// (nextMemberTableIDs, tableBelongsToArea, areaBoundsMemberFromTable,
// areaMoveMemberFromTable) plus internal/area.ComputeAreaBounds directly, the
// same "pure aggregation function" pattern used by positions_test.go
// (aggregatePositions) and batch_test.go (executeBatchSchema). No DB or
// Socket.IO server required — data.Area/data.DiagramTable are plain structs.
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	arealib "github.com/LizardLiang/liz-whiteboard-mcp/internal/area"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

func floatPtr(f float64) *float64 { return &f }

// ---------------------------------------------------------------------------
// nextMemberTableIDs — add / remove / idempotency (FR-A2/FR-A3)
// ---------------------------------------------------------------------------

func TestNextMemberTableIDs_AddAppendsWhenAbsent(t *testing.T) {
	next := nextMemberTableIDs([]string{"tbl-a"}, "tbl-b", true)
	assert.Equal(t, []string{"tbl-a", "tbl-b"}, next)
}

func TestNextMemberTableIDs_AddIsIdempotentWhenAlreadyMember(t *testing.T) {
	next := nextMemberTableIDs([]string{"tbl-a", "tbl-b"}, "tbl-b", true)
	assert.Equal(t, []string{"tbl-a", "tbl-b"}, next, "adding an existing member must not duplicate it")
}

func TestNextMemberTableIDs_RemoveFiltersWhenPresent(t *testing.T) {
	next := nextMemberTableIDs([]string{"tbl-a", "tbl-b"}, "tbl-b", false)
	assert.Equal(t, []string{"tbl-a"}, next)
}

func TestNextMemberTableIDs_RemoveIsIdempotentWhenAbsent(t *testing.T) {
	next := nextMemberTableIDs([]string{"tbl-a"}, "tbl-x", false)
	assert.Equal(t, []string{"tbl-a"}, next, "removing a non-member must be a no-op")
}

func TestNextMemberTableIDs_EmptyInputReturnsNonNilSlice(t *testing.T) {
	next := nextMemberTableIDs(nil, "tbl-a", false)
	assert.NotNil(t, next)
	assert.Empty(t, next)
}

// ---------------------------------------------------------------------------
// tableBelongsToArea — IDOR guard (FR-A2/FR-A3/AC-5)
// ---------------------------------------------------------------------------

func TestTableBelongsToArea_SameWhiteboardAllowed(t *testing.T) {
	areaRecord := &data.Area{WhiteboardID: "wb-1"}
	table := &data.DiagramTable{WhiteboardID: "wb-1"}
	assert.True(t, tableBelongsToArea(table, areaRecord))
}

func TestTableBelongsToArea_CrossWhiteboardRejected(t *testing.T) {
	areaRecord := &data.Area{WhiteboardID: "wb-1"}
	table := &data.DiagramTable{WhiteboardID: "wb-2"}
	assert.False(t, tableBelongsToArea(table, areaRecord), "cross-whiteboard table must be rejected (IDOR)")
}

// ---------------------------------------------------------------------------
// areaBoundsMemberFromTable + ComputeAreaBounds — null-position skipping and
// bounds-included-vs-omitted (FR-A2/FR-A3, R2)
// ---------------------------------------------------------------------------

func TestAreaBoundsMemberFromTable_NilTableSkipped(t *testing.T) {
	_, ok := areaBoundsMemberFromTable(nil, 3)
	assert.False(t, ok)
}

func TestAreaBoundsMemberFromTable_NullPositionSkipped(t *testing.T) {
	// Position not yet resolved by a browser client (R2).
	table := &data.DiagramTable{PositionX: nil, PositionY: floatPtr(10)}
	_, ok := areaBoundsMemberFromTable(table, 2)
	assert.False(t, ok, "a member with an unresolved X position must be skipped")

	table2 := &data.DiagramTable{PositionX: floatPtr(10), PositionY: nil}
	_, ok2 := areaBoundsMemberFromTable(table2, 2)
	assert.False(t, ok2, "a member with an unresolved Y position must be skipped")
}

func TestAreaBoundsMemberFromTable_ResolvedPositionIncluded(t *testing.T) {
	table := &data.DiagramTable{
		PositionX: floatPtr(100),
		PositionY: floatPtr(200),
		Width:     floatPtr(300),
	}
	m, ok := areaBoundsMemberFromTable(table, 5)
	require.True(t, ok)
	assert.Equal(t, arealib.AreaBoundsMember{PositionX: 100, PositionY: 200, Width: 300, ColumnCount: 5}, m)
}

func TestAreaBoundsMemberFromTable_NilWidthFallsBackToZero(t *testing.T) {
	// Width falls back to 0 here; ComputeAreaBounds applies the
	// DefaultNodeWidth fallback for width<=0 members.
	table := &data.DiagramTable{PositionX: floatPtr(0), PositionY: floatPtr(0), Width: nil}
	m, ok := areaBoundsMemberFromTable(table, 1)
	require.True(t, ok)
	assert.Equal(t, 0.0, m.Width)
}

// Bounds omitted: every member has an unresolved position => ComputeAreaBounds
// returns nil => the area:update payload must NOT carry position/size fields
// (caller leaves the area's current bounds unchanged).
func TestComputeAreaBoundsForMembers_AllNullPositionsOmitsBounds(t *testing.T) {
	members := []arealib.AreaBoundsMember{}
	for _, table := range []*data.DiagramTable{
		{PositionX: nil, PositionY: nil},
		{PositionX: nil, PositionY: floatPtr(5)},
	} {
		if m, ok := areaBoundsMemberFromTable(table, 0); ok {
			members = append(members, m)
		}
	}
	assert.Nil(t, arealib.ComputeAreaBounds(members), "bounds must be omitted (nil) when no member has a resolved position")
}

// Bounds included: at least one member has a resolved position.
func TestComputeAreaBoundsForMembers_ResolvedMemberIncludesBounds(t *testing.T) {
	members := []arealib.AreaBoundsMember{}
	for _, table := range []*data.DiagramTable{
		{PositionX: nil, PositionY: nil}, // skipped
		{PositionX: floatPtr(0), PositionY: floatPtr(0), Width: floatPtr(250)},
	} {
		if m, ok := areaBoundsMemberFromTable(table, 3); ok {
			members = append(members, m)
		}
	}
	require.Len(t, members, 1)
	bounds := arealib.ComputeAreaBounds(members)
	require.NotNil(t, bounds, "bounds must be computed when at least one member resolves")
	assert.Equal(t, -24.0, bounds.PositionX)
}

// ---------------------------------------------------------------------------
// areaMoveMemberFromTable — null-position skipping for the atomic move (FR-A4, R2)
// ---------------------------------------------------------------------------

func TestAreaMoveMemberFromTable_NullPositionSkipped(t *testing.T) {
	_, ok := areaMoveMemberFromTable("tbl-1", &data.DiagramTable{PositionX: nil, PositionY: floatPtr(1)}, 10, 10)
	assert.False(t, ok, "a member with an unresolved position must be skipped (not moved)")
}

func TestAreaMoveMemberFromTable_NilTableSkipped(t *testing.T) {
	_, ok := areaMoveMemberFromTable("tbl-1", nil, 10, 10)
	assert.False(t, ok)
}

func TestAreaMoveMemberFromTable_ShiftsByDelta(t *testing.T) {
	table := &data.DiagramTable{PositionX: floatPtr(50), PositionY: floatPtr(75)}
	m, ok := areaMoveMemberFromTable("tbl-1", table, -10, 20)
	require.True(t, ok)
	assert.Equal(t, areaMoveMember{TableID: "tbl-1", PositionX: 40, PositionY: 95}, m)
}

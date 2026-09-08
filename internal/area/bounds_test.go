// Unit tests for ComputeAreaBounds — Go port of computeAreaBounds
// (src/lib/react-flow/area-bounds.ts, GH #106 grouping bugfix). Numeric
// fixtures are cross-checked against src/lib/react-flow/area-bounds.test.ts
// for parity between the TS and Go implementations.
package area

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeAreaBounds_EmptyReturnsNil(t *testing.T) {
	assert.Nil(t, ComputeAreaBounds(nil))
	assert.Nil(t, ComputeAreaBounds([]AreaBoundsMember{}))
}

// Mirrors area-bounds.test.ts: "wraps a single member with default padding + top inset".
func TestComputeAreaBounds_SingleMember(t *testing.T) {
	bounds := ComputeAreaBounds([]AreaBoundsMember{
		{PositionX: 100, PositionY: 200, Width: 250, ColumnCount: 5},
	})
	height := CalculateTableHeight(5) + addColumnRowHeight
	assert.Equal(t, &AreaBounds{
		PositionX: 100 - 24,
		PositionY: 200 - 24 - 32,
		Width:     250 + 24*2,
		Height:    height + 24*2 + 32,
	}, bounds)
}

// Mirrors area-bounds.test.ts: "computes the union bounding box across multiple members".
func TestComputeAreaBounds_MultipleMembers(t *testing.T) {
	bounds := ComputeAreaBounds([]AreaBoundsMember{
		{PositionX: 0, PositionY: 0, Width: 100, ColumnCount: 3},
		{PositionX: 300, PositionY: 200, Width: 100, ColumnCount: 3},
	})
	height := CalculateTableHeight(3) + addColumnRowHeight
	assert.Equal(t, &AreaBounds{
		PositionX: 0 - 24,
		PositionY: 0 - 24 - 32,
		Width:     400 + 24*2,
		Height:    200 + height + 24*2 + 32,
	}, bounds)
}

// Mirrors area-bounds.test.ts: "enforces MIN_AREA_WIDTH for a narrow member".
func TestComputeAreaBounds_MinWidthFloor(t *testing.T) {
	bounds := ComputeAreaBounds([]AreaBoundsMember{
		{PositionX: 0, PositionY: 0, Width: 10, ColumnCount: 0},
	})
	assert.Equal(t, float64(MinWidth), bounds.Width)
	assert.Equal(t, CalculateTableHeight(0)+addColumnRowHeight+24*2+32, bounds.Height)
}

// Mirrors area-bounds.test.ts: "falls back to LAYOUT_CONSTRAINTS default width when unmeasured".
func TestComputeAreaBounds_DefaultWidthFallback(t *testing.T) {
	bounds := ComputeAreaBounds([]AreaBoundsMember{
		{PositionX: 0, PositionY: 0, Width: 0, ColumnCount: 4},
	})
	height := CalculateTableHeight(4) + addColumnRowHeight
	assert.Equal(t, &AreaBounds{
		PositionX: -24,
		PositionY: -24 - 32,
		Width:     250 + 24*2,
		Height:    height + 24*2 + 32,
	}, bounds)
}

// area-fit-member-content parity: more columns => taller computed area.
func TestComputeAreaBounds_MoreColumnsTallerArea(t *testing.T) {
	fewColumns := ComputeAreaBounds([]AreaBoundsMember{{PositionX: 0, PositionY: 0, Width: 250, ColumnCount: 2}})
	manyColumns := ComputeAreaBounds([]AreaBoundsMember{{PositionX: 0, PositionY: 0, Width: 250, ColumnCount: 12}})
	assert.Greater(t, manyColumns.Height, fewColumns.Height)
	assert.Equal(t, CalculateTableHeight(12)+addColumnRowHeight+24*2+32, manyColumns.Height)
}

func TestCalculateTableHeight(t *testing.T) {
	// Mirrors calculateTableHeight in layout-adapter.ts: 40 + cols*28 + 12.
	assert.Equal(t, float64(40+0*28+12), CalculateTableHeight(0))
	assert.Equal(t, float64(40+5*28+12), CalculateTableHeight(5))
}

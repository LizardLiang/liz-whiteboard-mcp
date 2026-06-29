package positioning

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	testColW = 280.0
	testRowH = 220.0
)

// TC-UNIT-POS: grid formula for representative counts.
func TestComputeFromCount(t *testing.T) {
	cases := []struct {
		count int
		wantX float64
		wantY float64
	}{
		{0, 40, 40},  // first table at grid origin
		{1, 320, 40}, // second table, first row
		{3, 880, 40}, // fourth table, last in first row
		{4, 40, 260}, // fifth table wraps to second row
		{8, 40, 480}, // ninth table, third row
	}
	for _, c := range cases {
		x, y := ComputeFromCount(c.count)
		assert.Equal(t, c.wantX, x, "x for count %d", c.count)
		assert.Equal(t, c.wantY, y, "y for count %d", c.count)
	}
}

// No two auto-placed tables overlap for counts 0-15.
func TestComputeFromCount_NoOverlap(t *testing.T) {
	type pt struct{ x, y float64 }
	var positions []pt
	for count := 0; count < 16; count++ {
		x, y := ComputeFromCount(count)
		positions = append(positions, pt{x, y})
	}
	for i := 0; i < len(positions); i++ {
		for j := i + 1; j < len(positions); j++ {
			a, b := positions[i], positions[j]
			xOverlap := a.x < b.x+testColW && b.x < a.x+testColW
			yOverlap := a.y < b.y+testRowH && b.y < a.y+testRowH
			assert.False(t, xOverlap && yOverlap, "tables %d and %d overlap", i, j)
		}
	}
}

// ComputeDefaultPosition uses the injected RectsFetcher.
// Fetcher returns 4 rects occupying slots 0-3; expected result is slot 4 = (40, 260).
func TestComputeDefaultPosition(t *testing.T) {
	fetcher := func(_ context.Context, _ string) ([]Rect, error) {
		return []Rect{
			{X: 40, Y: 40, W: 280, H: 220},   // slot 0
			{X: 320, Y: 40, W: 280, H: 220},  // slot 1
			{X: 600, Y: 40, W: 280, H: 220},  // slot 2
			{X: 880, Y: 40, W: 280, H: 220},  // slot 3
		}, nil
	}
	x, y, err := ComputeDefaultPosition(context.Background(), "wb", fetcher)
	assert.NoError(t, err)
	assert.Equal(t, 40.0, x)
	assert.Equal(t, 260.0, y)
}

// TestComputeFromRects_DeletedTableCollision verifies that when a table is
// deleted (leaving a gap in the slot sequence), the algorithm fills the gap
// rather than colliding with the surviving table that occupies the count-based slot.
//
// Scenario: 5 tables were created at slots 0-4, then slot 2 (600, 40) was deleted.
// Surviving rects: slots 0, 1, 3, 4. Count-based logic would place the next
// table at count=4 → slot 4 = (40, 260), which collides with the table at slot 4.
// The AABB algorithm must skip slots 0, 1 (occupied), find slot 2 free, and
// return (600, 40).
func TestComputeFromRects_DeletedTableCollision(t *testing.T) {
	existing := []Rect{
		{X: 40, Y: 40, W: 280, H: 220},   // slot 0
		{X: 320, Y: 40, W: 280, H: 220},  // slot 1
		// slot 2 deleted — gap here
		{X: 880, Y: 40, W: 280, H: 220},  // slot 3
		{X: 40, Y: 260, W: 280, H: 220},  // slot 4
	}
	x, y := ComputeFromRects(existing)
	assert.Equal(t, 600.0, x, "should fill the gap at slot 2 (x)")
	assert.Equal(t, 40.0, y, "should fill the gap at slot 2 (y)")
}

// TestComputeFromRects_ManuallyPlacedCollision verifies that a table manually
// placed at slot 0's coordinates forces the algorithm to skip to slot 1.
func TestComputeFromRects_ManuallyPlacedCollision(t *testing.T) {
	existing := []Rect{
		{X: 40, Y: 40, W: 280, H: 220}, // manually placed at slot 0 position
	}
	x, y := ComputeFromRects(existing)
	assert.Equal(t, 320.0, x, "should skip slot 0 and return slot 1 (x)")
	assert.Equal(t, 40.0, y, "should skip slot 0 and return slot 1 (y)")
}

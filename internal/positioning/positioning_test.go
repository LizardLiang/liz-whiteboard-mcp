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

// ComputeDefaultPosition uses the injected counter.
func TestComputeDefaultPosition(t *testing.T) {
	counter := func(_ context.Context, _ string) (int, error) { return 4, nil }
	x, y, err := ComputeDefaultPosition(context.Background(), "wb", counter)
	assert.NoError(t, err)
	assert.Equal(t, 40.0, x)
	assert.Equal(t, 260.0, y)
}

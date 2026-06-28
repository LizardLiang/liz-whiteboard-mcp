// Package positioning computes default non-overlapping table positions (FR-006)
// using a deterministic slot-by-count 4-column grid.
//
// Ported from src/mcp/positioning.ts.
package positioning

import "context"

// Grid constants (must match src/mcp/positioning.ts).
const (
	colW    = 280.0
	rowH    = 220.0
	cols    = 4
	originX = 40.0
	originY = 40.0
)

// TableCounter abstracts the table-count lookup so positioning can be unit
// tested without a live database.
type TableCounter func(ctx context.Context, whiteboardID string) (int, error)

// ComputeFromCount maps an existing-table count to a grid slot position.
// Exposed separately so the pure grid formula can be tested directly.
func ComputeFromCount(count int) (posX, posY float64) {
	colIndex := count % cols
	rowIndex := count / cols
	return originX + float64(colIndex)*colW, originY + float64(rowIndex)*rowH
}

// ComputeDefaultPosition counts existing tables in the whiteboard and returns a
// non-overlapping grid position for a new table.
func ComputeDefaultPosition(ctx context.Context, whiteboardID string, count TableCounter) (posX, posY float64, err error) {
	n, err := count(ctx, whiteboardID)
	if err != nil {
		return 0, 0, err
	}
	x, y := ComputeFromCount(n)
	return x, y, nil
}

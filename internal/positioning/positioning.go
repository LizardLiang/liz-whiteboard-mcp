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

// Rect is the axis-aligned bounding box of a positioned table.
// Width and Height should default to colW/rowH when the DB value is NULL.
type Rect struct {
	X, Y, W, H float64
}

// RectsFetcher abstracts the DB lookup so positioning can be unit-tested
// without a live database.
type RectsFetcher func(ctx context.Context, whiteboardID string) ([]Rect, error)

// ComputeFromCount maps an existing-table count to a grid slot position.
// Exposed separately so the pure grid formula can be tested directly.
func ComputeFromCount(count int) (posX, posY float64) {
	colIndex := count % cols
	rowIndex := count / cols
	return originX + float64(colIndex)*colW, originY + float64(rowIndex)*rowH
}

// ComputeFromRects finds the lowest-indexed grid slot whose bounding box
// (colW × rowH) does not overlap any rect in existing.
// Iterates up to maxSlots (1000) before falling back to count-based placement.
func ComputeFromRects(existing []Rect) (posX, posY float64) {
	const maxSlots = 1000
	for slot := 0; slot < maxSlots; slot++ {
		cx, cy := ComputeFromCount(slot)
		if !overlapsAny(cx, cy, colW, rowH, existing) {
			return cx, cy
		}
	}
	// Fallback: place beyond the last slot (should not happen in normal usage).
	return ComputeFromCount(maxSlots)
}

// overlapsAny returns true if the candidate rect (cx,cy,cw,ch) overlaps
// any rect in existing using standard AABB intersection.
func overlapsAny(cx, cy, cw, ch float64, existing []Rect) bool {
	for _, r := range existing {
		xOverlap := cx < r.X+r.W && r.X < cx+cw
		yOverlap := cy < r.Y+r.H && r.Y < cy+ch
		if xOverlap && yOverlap {
			return true
		}
	}
	return false
}

// ComputeDefaultPosition fetches all existing table rects for the whiteboard
// and returns a non-overlapping grid position for the new table.
func ComputeDefaultPosition(ctx context.Context, whiteboardID string, fetch RectsFetcher) (posX, posY float64, err error) {
	rects, err := fetch(ctx, whiteboardID)
	if err != nil {
		return 0, 0, err
	}
	x, y := ComputeFromRects(rects)
	return x, y, nil
}

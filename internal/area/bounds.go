// Package area implements pure geometry helpers for MCP-side subject-area
// (GH #106) bounds computation. ComputeAreaBounds is a Go port of
// computeAreaBounds in src/lib/react-flow/area-bounds.ts, used by the
// add_table_to_area/remove_table_from_area MCP tools to recompute an area's
// enclosing box after a headless (no browser peer) membership change.
//
// R1 (tactical plan, mcp-area-tools): the MCP server lacks the browser's
// measured content width, so it uses the stored `width` column or the
// DefaultNodeWidth fallback. This makes MCP-computed bounds an approximation
// — a connected browser peer refits precisely on its next render. Documented,
// acceptable; do not attempt to reproduce max-content sizing here.
package area

import "math"

const (
	// headerHeight, rowHeight, tablePadding compose CalculateTableHeight —
	// mirrors calculateTableHeight in src/lib/react-flow/layout-adapter.ts.
	headerHeight = 40
	rowHeight    = 28
	tablePadding = 12

	// addColumnRowHeight is the always-rendered "+" add-column affordance row
	// at the bottom of an editable table node, added on top of
	// CalculateTableHeight so a member is fully enclosed — mirrors
	// ADD_COLUMN_ROW_HEIGHT in area-bounds.ts.
	addColumnRowHeight = 28

	// DefaultPadding/DefaultTopInset mirror DEFAULT_PADDING/DEFAULT_TOP_INSET
	// in area-bounds.ts.
	DefaultPadding  = 24
	DefaultTopInset = 32

	// MinWidth/MinHeight mirror MIN_AREA_WIDTH/MIN_AREA_HEIGHT in
	// src/lib/react-flow/types.ts.
	MinWidth  = 160
	MinHeight = 120

	// DefaultNodeWidth mirrors LAYOUT_CONSTRAINTS.DEFAULT_NODE_WIDTH in
	// src/lib/react-flow/types.ts — the fallback used when a member's width
	// is unresolved (0 or unset).
	DefaultNodeWidth = 250
)

// CalculateTableHeight mirrors calculateTableHeight in layout-adapter.ts:
// header + one row per column + padding. Deliberately independent of any
// client's display mode (Compact/Keys/All) — a full-content, mode-independent
// height so an area's fit is identical across every peer.
func CalculateTableHeight(columnCount int) float64 {
	return float64(headerHeight) + float64(columnCount)*float64(rowHeight) + float64(tablePadding)
}

// AreaBoundsMember is the minimal per-member shape ComputeAreaBounds needs —
// mirrors AreaBoundsMemberNode in area-bounds.ts. Height is always derived
// from ColumnCount (never a measured/display value); Width falls back to
// DefaultNodeWidth when <= 0 (unresolved).
type AreaBoundsMember struct {
	PositionX   float64
	PositionY   float64
	Width       float64 // <= 0 => DefaultNodeWidth fallback
	ColumnCount int
}

// AreaBounds is the computed enclosing box (top-left position + size).
type AreaBounds struct {
	PositionX float64
	PositionY float64
	Width     float64
	Height    float64
}

// ComputeAreaBounds returns the bounding box that encloses every member, plus
// padding and a label-header inset, floored at MinWidth/MinHeight. Returns nil
// for an empty member slice — callers must leave the area's current bounds
// unchanged (mirrors "empty area keeps manual bounds" in area-bounds.ts).
func ComputeAreaBounds(members []AreaBoundsMember) *AreaBounds {
	if len(members) == 0 {
		return nil
	}

	minX := math.Inf(1)
	minY := math.Inf(1)
	maxX := math.Inf(-1)
	maxY := math.Inf(-1)

	for _, m := range members {
		w := m.Width
		if w <= 0 {
			w = DefaultNodeWidth
		}
		h := CalculateTableHeight(m.ColumnCount) + addColumnRowHeight

		minX = math.Min(minX, m.PositionX)
		minY = math.Min(minY, m.PositionY)
		maxX = math.Max(maxX, m.PositionX+w)
		maxY = math.Max(maxY, m.PositionY+h)
	}

	rawWidth := maxX - minX + DefaultPadding*2
	rawHeight := maxY - minY + DefaultPadding*2 + DefaultTopInset

	return &AreaBounds{
		PositionX: minX - DefaultPadding,
		PositionY: minY - DefaultPadding - DefaultTopInset,
		Width:     math.Max(rawWidth, MinWidth),
		Height:    math.Max(rawHeight, MinHeight),
	}
}

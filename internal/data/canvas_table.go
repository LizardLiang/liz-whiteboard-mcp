package data

import (
	"encoding/json"
	"errors"
)

// CanvasTable mirrors the app's CanvasTable (liz-whiteboard
// src/lib/canvas-engine/table.ts): rows and columns addressed by id, cells keyed
// "rowId:colId". It is decoded READ-ONLY. A cell reads only the fields the MCP
// uses, and the MCP never writes a stored cell back whole, so fields it does not
// read (textRuns) are never touched.
type CanvasTable struct {
	Rows      []TableRow           `json:"rows"`
	Cols      []TableCol           `json:"cols"`
	Cells     map[string]TableCell `json:"cells"`
	HeaderRow bool                 `json:"headerRow"`
}

// TableRow is one row: its id and height in world px.
type TableRow struct {
	ID     string  `json:"id"`
	Height float64 `json:"height"`
}

// TableCol is one column: its id and width in world px.
type TableCol struct {
	ID    string  `json:"id"`
	Width float64 `json:"width"`
}

// TableCell is one stored cell. A missing cell is an empty cell.
type TableCell struct {
	Text      string `json:"text"`
	TextAlign string `json:"textAlign,omitempty"`
	Fill      string `json:"fill,omitempty"`
}

// ErrNotATable reports props that do not hold a table.
var ErrNotATable = errors.New("props are not a table")

// ParseCanvasTable decodes an element's props blob. It never returns a nil
// table with a nil error, and its Cells map is never nil.
func ParseCanvasTable(props JSONText) (*CanvasTable, error) {
	var p struct {
		Kind  string       `json:"kind"`
		Table *CanvasTable `json:"table"`
	}
	if err := json.Unmarshal(props, &p); err != nil || p.Kind != "table" || p.Table == nil {
		return nil, ErrNotATable
	}
	if p.Table.Cells == nil {
		p.Table.Cells = map[string]TableCell{}
	}
	return p.Table, nil
}

// CellText returns the text of the cell at (rowID, colID), or "".
func (t *CanvasTable) CellText(rowID, colID string) string {
	return t.Cells[rowID+":"+colID].Text
}

// edit_canvas_table: an ordered list of changes to one canvas table.
//
// Rows, columns and cells are named by 0-based number. The MCP resolves a number
// to an id against the STORED table before EACH change, because an earlier
// change in the same list (insertRow, removeColumn) moves the numbers.
//
// The list stops at the first refused change. Continuing is unsafe: later row
// numbers may point at different rows. This differs on purpose from
// batch_schema_update, which attempts every item.
//
// After a transport error (a lost ack) the table is read back and the change
// counted as applied only if the read shows it landed; otherwise the outcome is
// reported as unknown, because an op may still be in flight.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// The change types of edit_canvas_table.
const (
	changeSetText      = "setText"
	changeInsertRow    = "insertRow"
	changeRemoveRow    = "removeRow"
	changeInsertColumn = "insertColumn"
	changeRemoveColumn = "removeColumn"
	changeResizeRow    = "resizeRow"
	changeResizeColumn = "resizeColumn"
	changeSetHeaderRow = "setHeaderRow"
	changeSetFill      = "setFill"
	changeSetAlign     = "setAlign"
)

var tableChangeTypes = []string{
	changeSetText, changeInsertRow, changeRemoveRow, changeInsertColumn, changeRemoveColumn,
	changeResizeRow, changeResizeColumn, changeSetHeaderRow, changeSetFill, changeSetAlign,
}

var rangeFields = []string{"fromRow", "fromColumn", "toRow", "toColumn"}

// changeFields lists the fields each change type reads. A field outside the list
// is refused, so a misplaced field is an error and not a silent no-op.
var changeFields = map[string][]string{
	changeSetText:      {"row", "column", "text"},
	changeInsertRow:    {"at"},
	changeRemoveRow:    {"row"},
	changeInsertColumn: {"at"},
	changeRemoveColumn: {"column"},
	changeResizeRow:    {"row", "height"},
	changeResizeColumn: {"column", "width"},
	changeSetHeaderRow: {"on"},
	changeSetFill:      append([]string{"fill"}, rangeFields...),
	changeSetAlign:     append([]string{"align"}, rangeFields...),
}

// allChangeFields is every optional field, in declaration order.
var allChangeFields = []string{
	"row", "column", "text", "at", "height", "width", "on",
	"fromRow", "fromColumn", "toRow", "toColumn", "fill", "align",
}

var editCanvasTableDescription = fmt.Sprintf("Change one table on a canvas board (see create_canvas_table). "+
	"changes is an ordered list, 1 to %d items, applied one after another; each item has a type and only the fields its type uses: "+
	"setText {row, column, text}; insertRow {at} (0 to the row count; the row count appends); removeRow {row}; "+
	"insertColumn {at}; removeColumn {column}; resizeRow {row, height >= %g}; resizeColumn {column, width >= %g}; "+
	"setHeaderRow {on}; setFill {fromRow, fromColumn, toRow, toColumn, fill} (fill is required: any CSS colour; null or an empty "+
	"string clears); setAlign {fromRow, fromColumn, toRow, toColumn, align: left|center|right}. "+
	"Text is at most %d characters. Rows and columns are numbered from 0 and renumber after each insert or remove. "+
	"setText grows the row to fit the text, header text counted bold; it never shrinks a row (use resizeRow). "+
	"The list stops at the first refused change and reports applied, failed and skipped; "+
	"earlier changes stay applied. If the connection drops mid-change, the table is re-read: a change that landed counts "+
	"as applied, otherwise failed.code is CONNECTION_ERROR and the outcome is unknown, so re-read the table before retrying.",
	tableMaxChanges, tableMinRowHeight, tableMinColWidth, tableMaxTextLength)

// tableChangeInput is one change. The fields each type reads are listed in
// changeFields; any other field is refused.
type tableChangeInput struct {
	Type       string   `json:"type" jsonschema:"setText, insertRow, removeRow, insertColumn, removeColumn, resizeRow, resizeColumn, setHeaderRow, setFill, or setAlign"`
	Row        *int     `json:"row,omitempty" jsonschema:"0-based row number (setText, removeRow, resizeRow)"`
	Column     *int     `json:"column,omitempty" jsonschema:"0-based column number (setText, removeColumn, resizeColumn)"`
	Text       *string  `json:"text,omitempty" jsonschema:"Cell text, at most 5000 characters (setText)"`
	At         *int     `json:"at,omitempty" jsonschema:"Insert position, 0 to the current count (insertRow, insertColumn)"`
	Height     *float64 `json:"height,omitempty" jsonschema:"Row height, at least 24 (resizeRow)"`
	Width      *float64 `json:"width,omitempty" jsonschema:"Column width, at least 40 (resizeColumn)"`
	On         *bool    `json:"on,omitempty" jsonschema:"Whether the first row is a header (setHeaderRow)"`
	FromRow    *int     `json:"fromRow,omitempty" jsonschema:"First row of the cell range (setFill, setAlign)"`
	FromColumn *int     `json:"fromColumn,omitempty" jsonschema:"First column of the cell range (setFill, setAlign)"`
	ToRow      *int     `json:"toRow,omitempty" jsonschema:"Last row of the cell range, inclusive (setFill, setAlign)"`
	ToColumn   *int     `json:"toColumn,omitempty" jsonschema:"Last column of the cell range, inclusive (setFill, setAlign)"`
	Fill       *string  `json:"fill,omitempty" jsonschema:"CSS colour; null or empty clears the fill. Required for setFill"`
	Align      *string  `json:"align,omitempty" jsonschema:"left, center, or right (setAlign)"`

	// present records which keys the JSON carried, so a missing fill differs
	// from a null one. Nil for a change built in Go; has() then reads the pointers.
	present map[string]bool
}

// UnmarshalJSON decodes the change and records which keys it carried.
func (c *tableChangeInput) UnmarshalJSON(b []byte) error {
	type plain tableChangeInput
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return err
	}
	*c = tableChangeInput(p)
	c.present = make(map[string]bool, len(keys))
	for k := range keys {
		c.present[k] = true
	}
	return nil
}

// has reports whether the change carries the named field, null included.
func (c tableChangeInput) has(field string) bool {
	if c.present != nil {
		return c.present[field]
	}
	switch field {
	case "row":
		return c.Row != nil
	case "column":
		return c.Column != nil
	case "text":
		return c.Text != nil
	case "at":
		return c.At != nil
	case "height":
		return c.Height != nil
	case "width":
		return c.Width != nil
	case "on":
		return c.On != nil
	case "fromRow":
		return c.FromRow != nil
	case "fromColumn":
		return c.FromColumn != nil
	case "toRow":
		return c.ToRow != nil
	case "toColumn":
		return c.ToColumn != nil
	case "fill":
		return c.Fill != nil
	case "align":
		return c.Align != nil
	}
	return false
}

type editCanvasTableInput struct {
	CanvasBoardID string             `json:"canvasBoardId" jsonschema:"The canvas board UUID"`
	ElementID     string             `json:"elementId" jsonschema:"The table element UUID"`
	Changes       []tableChangeInput `json:"changes" jsonschema:"Ordered list of 1 to 200 changes"`
}

// tableChangeFailure is the refused change in the result.
type tableChangeFailure struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// editCanvasTableResult is the success result. Rows and Columns are the counts
// after the last applied change. RowHeightSkipped lists the setText changes whose
// grown row height was not stored because a concurrent edit had moved the row.
type editCanvasTableResult struct {
	Applied          int                 `json:"applied"`
	Failed           *tableChangeFailure `json:"failed,omitempty"`
	Skipped          int                 `json:"skipped"`
	Rows             int                 `json:"rows"`
	Columns          int                 `json:"columns"`
	RowHeightSkipped []int               `json:"rowHeightSkipped,omitempty"`
}

// ---------------------------------------------------------------------------
// Shape validation, before the gate
// ---------------------------------------------------------------------------

func changeField(i int, field string) string { return fmt.Sprintf("changes[%d].%s", i, field) }

func changeError(i int, field, format string, args ...any) *mcperr.McpError {
	return mcperr.NewField(mcperr.ValidationError,
		fmt.Sprintf("%s %s", changeField(i, field), fmt.Sprintf(format, args...)), changeField(i, field))
}

func needField(i int, ch tableChangeInput, field string) *mcperr.McpError {
	if !ch.has(field) {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("changes[%d] (%s) needs %s.", i, ch.Type, field), changeField(i, field))
	}
	return nil
}

func needAll(i int, ch tableChangeInput, fields ...string) *mcperr.McpError {
	for _, f := range fields {
		if e := needField(i, ch, f); e != nil {
			return e
		}
	}
	return nil
}

// checkForeignFields refuses a field the change's type does not read.
func checkForeignFields(i int, ch tableChangeInput) *mcperr.McpError {
	allowed := changeFields[ch.Type]
	for _, f := range allChangeFields {
		if ch.has(f) && !slices.Contains(allowed, f) {
			return changeError(i, f, "is not used by %s (it reads: %v).", ch.Type, allowed)
		}
	}
	return nil
}

func checkNonNegative(i int, ch tableChangeInput) *mcperr.McpError {
	for _, n := range []struct {
		field string
		v     *int
	}{
		{"row", ch.Row}, {"column", ch.Column}, {"at", ch.At},
		{"fromRow", ch.FromRow}, {"fromColumn", ch.FromColumn}, {"toRow", ch.ToRow}, {"toColumn", ch.ToColumn},
	} {
		if n.v != nil && *n.v < 0 {
			return changeError(i, n.field, "must be 0 or more.")
		}
	}
	return nil
}

func shapeSetText(i int, ch tableChangeInput) *mcperr.McpError {
	if e := needAll(i, ch, "row", "column", "text"); e != nil {
		return e
	}
	if utf8.RuneCountInString(*ch.Text) > tableMaxTextLength {
		return changeError(i, "text", "must be at most %d characters.", tableMaxTextLength)
	}
	return nil
}

func checkSizeBounds(i int, field string, v, min float64) *mcperr.McpError {
	if e := checkFinite(changeField(i, field), v); e != nil {
		return e
	}
	if v < min || v > canvasSizeMax {
		return changeError(i, field, "must be between %g and %d.", min, canvasSizeMax)
	}
	return nil
}

func shapeResizeRow(i int, ch tableChangeInput) *mcperr.McpError {
	if e := needAll(i, ch, "row", "height"); e != nil {
		return e
	}
	return checkSizeBounds(i, "height", *ch.Height, tableMinRowHeight)
}

func shapeResizeColumn(i int, ch tableChangeInput) *mcperr.McpError {
	if e := needAll(i, ch, "column", "width"); e != nil {
		return e
	}
	return checkSizeBounds(i, "width", *ch.Width, tableMinColWidth)
}

// shapeRange checks the cell range setFill and setAlign share.
func shapeRange(i int, ch tableChangeInput) *mcperr.McpError {
	if e := needAll(i, ch, rangeFields...); e != nil {
		return e
	}
	if *ch.FromRow > *ch.ToRow {
		return changeError(i, "fromRow", "must not exceed %s.", changeField(i, "toRow"))
	}
	if *ch.FromColumn > *ch.ToColumn {
		return changeError(i, "fromColumn", "must not exceed %s.", changeField(i, "toColumn"))
	}
	return nil
}

func shapeSetFill(i int, ch tableChangeInput) *mcperr.McpError {
	if e := shapeRange(i, ch); e != nil {
		return e
	}
	// A missing fill is refused: clearing must be explicit (null or "").
	if e := needField(i, ch, "fill"); e != nil {
		return e
	}
	if ch.Fill != nil && *ch.Fill != "" {
		// cssColorSchema is z.string().min(1).max(64).
		return checkLen(changeField(i, "fill"), *ch.Fill, 1, 64)
	}
	return nil
}

func shapeSetAlign(i int, ch tableChangeInput) *mcperr.McpError {
	if e := shapeRange(i, ch); e != nil {
		return e
	}
	if e := needField(i, ch, "align"); e != nil {
		return e
	}
	return checkCanvasEnum(changeField(i, "align"), *ch.Align, "left", "center", "right")
}

var changeShapers = map[string]func(int, tableChangeInput) *mcperr.McpError{
	changeSetText:      shapeSetText,
	changeInsertRow:    func(i int, ch tableChangeInput) *mcperr.McpError { return needField(i, ch, "at") },
	changeInsertColumn: func(i int, ch tableChangeInput) *mcperr.McpError { return needField(i, ch, "at") },
	changeRemoveRow:    func(i int, ch tableChangeInput) *mcperr.McpError { return needField(i, ch, "row") },
	changeRemoveColumn: func(i int, ch tableChangeInput) *mcperr.McpError { return needField(i, ch, "column") },
	changeResizeRow:    shapeResizeRow,
	changeResizeColumn: shapeResizeColumn,
	changeSetHeaderRow: func(i int, ch tableChangeInput) *mcperr.McpError { return needField(i, ch, "on") },
	changeSetFill:      shapeSetFill,
	changeSetAlign:     shapeSetAlign,
}

// checkTableChangeShape validates one change without any stored data: the type,
// the fields the type reads, enums and bounds. Index ranges against the table
// are checked later, when the table is loaded.
func checkTableChangeShape(i int, ch tableChangeInput) *mcperr.McpError {
	shape, known := changeShapers[ch.Type]
	if !known {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("changes[%d].type must be one of: %v.", i, tableChangeTypes), changeField(i, "type"))
	}
	if e := checkForeignFields(i, ch); e != nil {
		return e
	}
	if e := checkNonNegative(i, ch); e != nil {
		return e
	}
	return shape(i, ch)
}

// checkEditCanvasTableShape validates the whole request before the gate.
func checkEditCanvasTableShape(in editCanvasTableInput) *mcperr.McpError {
	if e := validateUUID("elementId", in.ElementID); e != nil {
		return e
	}
	if len(in.Changes) == 0 {
		return mcperr.NewField(mcperr.ValidationError, "changes must hold at least 1 change.", "changes")
	}
	if len(in.Changes) > tableMaxChanges {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("changes must hold at most %d changes; got %d.", tableMaxChanges, len(in.Changes)), "changes")
	}
	for i, ch := range in.Changes {
		if e := checkTableChangeShape(i, ch); e != nil {
			return e
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Change -> op
// ---------------------------------------------------------------------------

// builtOp is one change resolved against the stored table.
type builtOp struct {
	// wire is the app's TableOp. Its type names (insertCol, removeCol, resizeCol,
	// setCellText, setCellFill, setCellAlign) are the app's wire names
	// (table.ts:67-141), which differ from this tool's change names.
	wire map[string]any
	// noop is set when the change would change nothing: no emit, counted applied.
	noop bool
	// landed reports whether a table read shows the change was applied.
	landed func(t *data.CanvasTable) bool
	// growRow is the row whose height the op sets (-1: none) and wantHeight the
	// height it asked for, to report a skipped write.
	growRow    int
	wantHeight float64
}

func rangeError(i int, field string, v, count int, noun string) *mcperr.McpError {
	return mcperr.NewField(mcperr.ValidationError,
		fmt.Sprintf("%s: %d is out of range, the table has %d %s.", changeField(i, field), v, count, noun), changeField(i, field))
}

func rowAt(i int, t *data.CanvasTable, field string, n int) (string, *mcperr.McpError) {
	if n >= len(t.Rows) {
		return "", rangeError(i, field, n, len(t.Rows), "rows")
	}
	return t.Rows[n].ID, nil
}

func colAt(i int, t *data.CanvasTable, field string, n int) (string, *mcperr.McpError) {
	if n >= len(t.Cols) {
		return "", rangeError(i, field, n, len(t.Cols), "columns")
	}
	return t.Cols[n].ID, nil
}

func hasRow(t *data.CanvasTable, id string) bool {
	return slices.ContainsFunc(t.Rows, func(r data.TableRow) bool { return r.ID == id })
}

func hasCol(t *data.CanvasTable, id string) bool {
	return slices.ContainsFunc(t.Cols, func(c data.TableCol) bool { return c.ID == id })
}

func rowHeightIs(t *data.CanvasTable, id string, h float64) bool {
	for _, r := range t.Rows {
		if r.ID == id {
			return math.Abs(r.Height-h) < 0.5
		}
	}
	return false
}

func colWidthIs(t *data.CanvasTable, id string, w float64) bool {
	for _, c := range t.Cols {
		if c.ID == id {
			return math.Abs(c.Width-w) < 0.5
		}
	}
	return false
}

func opSetText(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	t := st.Table
	rowID, e := rowAt(i, t, "row", *ch.Row)
	if e != nil {
		return nil, e
	}
	colID, e := colAt(i, t, "column", *ch.Column)
	if e != nil {
		return nil, e
	}
	text := *ch.Text
	stored := t.Rows[*ch.Row].Height
	// Grow only: a height goes out when the estimate beats the stored row, and
	// ifRowHeight guards it so a concurrent browser edit wins.
	want := estimateRowHeight(text, t.Cols[*ch.Column].Width, st.Style.FontSize, cellIsBold(t, st.Style, *ch.Row))
	grows := want > stored
	op := &builtOp{growRow: -1, landed: func(after *data.CanvasTable) bool { return after.CellText(rowID, colID) == text }}
	if t.CellText(rowID, colID) == text && !grows {
		op.noop = true
		return op, nil
	}
	op.wire = map[string]any{"type": "setCellText", "rowId": rowID, "colId": colID, "text": text}
	if grows {
		op.wantHeight = math.Min(want, canvasSizeMax)
		op.wire["rowHeight"] = op.wantHeight
		op.wire["ifRowHeight"] = stored
		op.growRow = *ch.Row
	}
	return op, nil
}

func opInsertRow(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	t := st.Table
	if *ch.At > len(t.Rows) {
		return nil, rangeError(i, "at", *ch.At, len(t.Rows), "rows (at may equal the row count to append)")
	}
	var after any
	if *ch.At > 0 {
		after = t.Rows[*ch.At-1].ID
	}
	id := newTableID()
	return &builtOp{
		growRow: -1,
		wire: map[string]any{
			"type": "insertRow", "afterRowId": after,
			"row": map[string]any{"id": id, "height": tableCellHeight},
		},
		landed: func(a *data.CanvasTable) bool { return hasRow(a, id) },
	}, nil
}

func opInsertColumn(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	t := st.Table
	if *ch.At > len(t.Cols) {
		return nil, rangeError(i, "at", *ch.At, len(t.Cols), "columns (at may equal the column count to append)")
	}
	var after any
	if *ch.At > 0 {
		after = t.Cols[*ch.At-1].ID
	}
	id := newTableID()
	return &builtOp{
		growRow: -1,
		wire: map[string]any{
			"type": "insertCol", "afterColId": after,
			"col": map[string]any{"id": id, "width": tableCellWidth},
		},
		landed: func(a *data.CanvasTable) bool { return hasCol(a, id) },
	}, nil
}

func opRemoveRow(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	id, e := rowAt(i, st.Table, "row", *ch.Row)
	if e != nil {
		return nil, e
	}
	return &builtOp{growRow: -1, wire: map[string]any{"type": "removeRow", "rowId": id},
		landed: func(a *data.CanvasTable) bool { return !hasRow(a, id) }}, nil
}

func opRemoveColumn(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	id, e := colAt(i, st.Table, "column", *ch.Column)
	if e != nil {
		return nil, e
	}
	return &builtOp{growRow: -1, wire: map[string]any{"type": "removeCol", "colId": id},
		landed: func(a *data.CanvasTable) bool { return !hasCol(a, id) }}, nil
}

func opResizeRow(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	id, e := rowAt(i, st.Table, "row", *ch.Row)
	if e != nil {
		return nil, e
	}
	h := *ch.Height
	return &builtOp{growRow: -1, wire: map[string]any{"type": "resizeRow", "rowId": id, "height": h},
		landed: func(a *data.CanvasTable) bool { return rowHeightIs(a, id, h) }}, nil
}

func opResizeColumn(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	id, e := colAt(i, st.Table, "column", *ch.Column)
	if e != nil {
		return nil, e
	}
	w := *ch.Width
	return &builtOp{growRow: -1, wire: map[string]any{"type": "resizeCol", "colId": id, "width": w},
		landed: func(a *data.CanvasTable) bool { return colWidthIs(a, id, w) }}, nil
}

func opSetHeaderRow(_ int, ch tableChangeInput, _ *storedTable) (*builtOp, *mcperr.McpError) {
	on := *ch.On
	return &builtOp{growRow: -1, wire: map[string]any{"type": "setHeaderRow", "on": on},
		landed: func(a *data.CanvasTable) bool { return a.HeaderRow == on }}, nil
}

// cellRefs resolves the change's cell range to ids, or a range error.
func cellRefs(i int, ch tableChangeInput, t *data.CanvasTable) ([]map[string]any, [][2]string, *mcperr.McpError) {
	for _, r := range []struct {
		field string
		v     int
	}{{"fromRow", *ch.FromRow}, {"toRow", *ch.ToRow}} {
		if _, e := rowAt(i, t, r.field, r.v); e != nil {
			return nil, nil, e
		}
	}
	for _, c := range []struct {
		field string
		v     int
	}{{"fromColumn", *ch.FromColumn}, {"toColumn", *ch.ToColumn}} {
		if _, e := colAt(i, t, c.field, c.v); e != nil {
			return nil, nil, e
		}
	}
	count := (*ch.ToRow - *ch.FromRow + 1) * (*ch.ToColumn - *ch.FromColumn + 1)
	if count > tableMaxCells {
		return nil, nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("changes[%d]: the range holds %d cells, at most %d.", i, count, tableMaxCells), changeField(i, "toRow"))
	}
	refs := make([]map[string]any, 0, count)
	ids := make([][2]string, 0, count)
	for r := *ch.FromRow; r <= *ch.ToRow; r++ {
		for c := *ch.FromColumn; c <= *ch.ToColumn; c++ {
			refs = append(refs, map[string]any{"rowId": t.Rows[r].ID, "colId": t.Cols[c].ID})
			ids = append(ids, [2]string{t.Rows[r].ID, t.Cols[c].ID})
		}
	}
	return refs, ids, nil
}

func opSetFill(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	refs, ids, e := cellRefs(i, ch, st.Table)
	if e != nil {
		return nil, e
	}
	var fill any // null clears
	want := ""
	if ch.Fill != nil && *ch.Fill != "" {
		fill, want = *ch.Fill, *ch.Fill
	}
	return &builtOp{growRow: -1, wire: map[string]any{"type": "setCellFill", "cells": refs, "fill": fill},
		landed: func(a *data.CanvasTable) bool {
			return !slices.ContainsFunc(ids, func(id [2]string) bool { return a.Cells[id[0]+":"+id[1]].Fill != want })
		}}, nil
}

func opSetAlign(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	refs, ids, e := cellRefs(i, ch, st.Table)
	if e != nil {
		return nil, e
	}
	align := *ch.Align
	return &builtOp{growRow: -1, wire: map[string]any{"type": "setCellAlign", "cells": refs, "align": align},
		landed: func(a *data.CanvasTable) bool {
			return !slices.ContainsFunc(ids, func(id [2]string) bool {
				got := a.Cells[id[0]+":"+id[1]].TextAlign
				return got != align && !(align == "left" && got == "")
			})
		}}, nil
}

// buildTableOp resolves one change's numbers against the stored table and
// returns the app's TableOp.
func buildTableOp(i int, ch tableChangeInput, st *storedTable) (*builtOp, *mcperr.McpError) {
	switch ch.Type {
	case changeSetText:
		return opSetText(i, ch, st)
	case changeInsertRow:
		return opInsertRow(i, ch, st)
	case changeInsertColumn:
		return opInsertColumn(i, ch, st)
	case changeRemoveRow:
		return opRemoveRow(i, ch, st)
	case changeRemoveColumn:
		return opRemoveColumn(i, ch, st)
	case changeResizeRow:
		return opResizeRow(i, ch, st)
	case changeResizeColumn:
		return opResizeColumn(i, ch, st)
	case changeSetHeaderRow:
		return opSetHeaderRow(i, ch, st)
	case changeSetFill:
		return opSetFill(i, ch, st)
	case changeSetAlign:
		return opSetAlign(i, ch, st)
	}
	return nil, mcperr.New(mcperr.InternalError, fmt.Sprintf("Unhandled table change type %q.", ch.Type))
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// errField returns the Field of an McpError in err's chain, or "".
func errField(err error) string {
	var me *mcperr.McpError
	if errors.As(err, &me) {
		return me.Field
	}
	return ""
}

// editRun holds the state of one edit_canvas_table call.
type editRun struct {
	ctx    context.Context
	fns    canvasTableFns
	in     editCanvasTableInput
	userID string
	result editCanvasTableResult
}

// refuse ends the run at change i. A refusal before any change applied is a tool
// error; after progress it is a success result that carries failed.
func (r *editRun) refuse(i int, code mcperr.Code, message, field string) (*mcp.CallToolResult, any, error) {
	if r.result.Applied == 0 {
		return toCallResult(mcperr.MakeMcpError(code, message, field)), nil, nil
	}
	r.result.Failed = &tableChangeFailure{Index: i, Type: r.in.Changes[i].Type, Code: string(code), Message: message}
	r.result.Skipped = len(r.in.Changes) - i - 1
	return success(r.result)
}

// setCounts records the row and column counts of a table.
func (r *editRun) setCounts(t *data.CanvasTable) {
	r.result.Rows, r.result.Columns = len(t.Rows), len(t.Cols)
}

// reconcile runs after an emit error: it reads the table back and reports
// whether the change landed. A failed read, or a change that did not land, is an
// unknown outcome, because an op may still be in flight.
func (r *editRun) reconcile(op *builtOp) bool {
	after, err := loadStoredTable(r.ctx, r.fns, r.in.CanvasBoardID, r.in.ElementID)
	if err != nil {
		return false
	}
	r.setCounts(after.Table)
	return op.landed(after.Table)
}

// noteSkippedHeight records a setText whose grown row height the app did not
// store (ifRowHeight no longer matched).
func (r *editRun) noteSkippedHeight(i int, op *builtOp) {
	if op.growRow < 0 {
		return
	}
	after, err := loadStoredTable(r.ctx, r.fns, r.in.CanvasBoardID, r.in.ElementID)
	if err != nil || op.growRow >= len(after.Table.Rows) {
		return
	}
	if after.Table.Rows[op.growRow].Height < op.wantHeight-0.5 {
		r.result.RowHeightSkipped = append(r.result.RowHeightSkipped, i)
	}
}

// editCanvasTable validates the whole list, gates, then applies the changes one
// by one, re-reading the stored table before each.
func editCanvasTable(
	ctx context.Context,
	fns canvasTableFns,
	in editCanvasTableInput,
) (*mcp.CallToolResult, any, error) {
	if e := checkEditCanvasTableShape(in); e != nil {
		return fail(e)
	}
	userID, err := fns.authorize(ctx, in.CanvasBoardID)
	if err != nil {
		return fail(err)
	}
	run := &editRun{ctx: ctx, fns: fns, in: in, userID: userID}

	for i, ch := range in.Changes {
		st, err := loadStoredTable(ctx, fns, in.CanvasBoardID, in.ElementID)
		if err != nil {
			code, message := codeMsg(err, mcperr.InternalError)
			return run.refuse(i, code, message, errField(err))
		}
		run.setCounts(st.Table)

		op, e := buildTableOp(i, ch, st)
		if e != nil {
			return run.refuse(i, e.Code, e.Message, e.Field)
		}
		if op.noop {
			run.result.Applied++
			continue
		}
		ack, err := fns.emit(ctx, userID, in.CanvasBoardID, "element:table-op",
			map[string]any{"elementId": in.ElementID, "op": op.wire})
		if err != nil {
			if !run.reconcile(op) {
				return run.refuse(i, mcperr.ConnectionError, "Outcome unknown: the connection failed while this change was sent, "+
					"and a re-read does not show it applied. Re-read the table (get_canvas_board) before retrying.", "")
			}
			run.result.Applied++
			continue
		}
		if !ack.OK() {
			message := canvasAckMessage(ack.Code(), msgOr(ack.Message(), "Server rejected the table change."))
			return run.refuse(i, mcperr.AckCodeToMcpCode(ack.Code()), message, "")
		}
		run.result.Applied++
		applyCountDelta(ch, &run.result.Rows, &run.result.Columns)
		run.noteSkippedHeight(i, op)
	}
	return success(run.result)
}

// applyCountDelta adjusts the running row and column counts after an applied change.
func applyCountDelta(ch tableChangeInput, rows, cols *int) {
	switch ch.Type {
	case changeInsertRow:
		*rows++
	case changeRemoveRow:
		*rows--
	case changeInsertColumn:
		*cols++
	case changeRemoveColumn:
		*cols--
	}
}

// Package tools — canvas table tool tests. DB-free and socket-free: the
// canvasTableFns seam swaps the gate, the element read and the emit.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// ---------------------------------------------------------------------------
// estimateRowHeight
// ---------------------------------------------------------------------------

// browserHeight is what the app's exact fit gives for n lines at 14 px.
func browserHeight(lines int) float64 { return math.Ceil(float64(lines)*14*1.4 + 16) }

// Each row was measured in the browser (120 px column, 14 px font). The estimate
// must never be below it, or the text spills out of its row.
func TestEstimateRowHeight_NeverBelowTheBrowser(t *testing.T) {
	cases := []struct {
		name         string
		text         string
		browserLines int
		maxLines     int
	}{
		{"e2e long string", strings.Repeat("a long sentence that has to wrap over several lines inside one cell ", 2), 12, 13},
		{"long word text", "Revenue growth by quarter for the internationalization programme", 7, 9},
		{"wide Latin glyphs", "WWWW MMMM WWWW MMMM WWWW MMMM WWWW MMMM", 8, 10},
		{"CJK", "這是一段很長的中文文字，需要在儲存格裡換行好幾次才能完整顯示出來。", 5, 5},
		{"short sentence", "a long sentence that has to wrap", 3, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateRowHeight(tc.text, 120, 14, false)
			assert.GreaterOrEqual(t, got, browserHeight(tc.browserLines), "must not under-estimate")
			assert.LessOrEqual(t, got, browserHeight(tc.maxLines), "must not be absurd")
		})
	}
}

func TestEstimateRowHeight(t *testing.T) {
	// One short line: ceil(1 * 14 * 1.4 + 16) = 36.
	assert.Equal(t, 36.0, estimateRowHeight("hi", 120, 14, false))
	// Empty text asks for no change.
	assert.Equal(t, 0.0, estimateRowHeight("", 120, 14, false))
	// Two paragraphs are two lines: ceil(2*19.6+16) = 56.
	assert.Equal(t, 56.0, estimateRowHeight("a\nb", 120, 14, false))
	// A trailing newline adds an empty paragraph, one more line.
	assert.Equal(t, 56.0, estimateRowHeight("a\n", 120, 14, false))
	// A word wider than the line breaks mid-word.
	assert.Equal(t, 56.0, estimateRowHeight(strings.Repeat("W", 12), 120, 14, false))
	// CJK has no spaces, so it breaks per character: 7 fit in 104 px, 8 wrap.
	assert.Equal(t, 36.0, estimateRowHeight(strings.Repeat("字", 7), 120, 14, false))
	assert.Equal(t, 56.0, estimateRowHeight(strings.Repeat("字", 8), 120, 14, false))
	// A column narrower than its padding never divides by zero or below.
	assert.Greater(t, estimateRowHeight("abc", 10, 14, false), 36.0)
	// Every printable ASCII glyph has a width.
	for _, w := range regularAdvanceEm {
		assert.Greater(t, w, 0.0)
	}
}

// ---------------------------------------------------------------------------
// create payload
// ---------------------------------------------------------------------------

func validCreateTable() createCanvasTableInput {
	return createCanvasTableInput{CanvasBoardID: canvasTestBoardID, PositionX: 10, PositionY: 20, Rows: 3, Columns: 3}
}

func TestBuildCanvasTablePayload_Defaults(t *testing.T) {
	payload, rowIDs, colIDs, e := buildCanvasTablePayload(validCreateTable())
	require.Nil(t, e)
	assert.Len(t, rowIDs, 3)
	assert.Len(t, colIDs, 3)
	assert.Equal(t, "table", payload["kind"])
	assert.Equal(t, 360.0, payload["width"])
	assert.Equal(t, 120.0, payload["height"])
	props := payload["props"].(map[string]any)
	assert.Equal(t, "table", props["kind"])
	table := props["table"].(map[string]any)
	assert.Equal(t, true, table["headerRow"], "the header row is on by default")
	assert.Empty(t, table["cells"])
	for _, id := range append(rowIDs, colIDs...) {
		assert.NotContains(t, id, ":")
	}
}

func TestBuildCanvasTablePayload_RaggedCellsAndHeaderOff(t *testing.T) {
	in := validCreateTable()
	off := false
	in.HeaderRow = &off
	in.Cells = [][]string{{"a", "", "c"}, {"d"}}
	payload, rowIDs, colIDs, e := buildCanvasTablePayload(in)
	require.Nil(t, e)
	table := payload["props"].(map[string]any)["table"].(map[string]any)
	assert.Equal(t, false, table["headerRow"])
	cells := table["cells"].(map[string]any)
	assert.Len(t, cells, 3, "empty text stores no cell")
	assert.Equal(t, map[string]any{"text": "a"}, cells[rowIDs[0]+":"+colIDs[0]])
	assert.Equal(t, map[string]any{"text": "d"}, cells[rowIDs[1]+":"+colIDs[0]])
}

func TestBuildCanvasTablePayload_RowGrowsToFitStartingText(t *testing.T) {
	in := validCreateTable()
	in.Cells = [][]string{{strings.Repeat("word ", 20)}}
	payload, _, _, e := buildCanvasTablePayload(in)
	require.Nil(t, e)
	rows := payload["props"].(map[string]any)["table"].(map[string]any)["rows"].([]map[string]any)
	assert.Greater(t, rows[0]["height"].(float64), 40.0)
	assert.Equal(t, 40.0, rows[1]["height"])
}

func TestBuildCanvasTablePayload_Refusals(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*createCanvasTableInput)
		field string
	}{
		{"zero rows", func(i *createCanvasTableInput) { i.Rows = 0 }, "rows"},
		{"zero columns", func(i *createCanvasTableInput) { i.Columns = 0 }, "rows"},
		{"over 500 cells", func(i *createCanvasTableInput) { i.Rows, i.Columns = 21, 24 }, "rows"},
		{"exactly 501 cells", func(i *createCanvasTableInput) { i.Rows, i.Columns = 501, 1 }, "rows"},
		{"too many cell rows", func(i *createCanvasTableInput) { i.Cells = make([][]string, 4) }, "cells"},
		{"too many cells in a row", func(i *createCanvasTableInput) { i.Cells = [][]string{{"a", "b", "c", "d"}} }, "cells[0]"},
		{"text too long", func(i *createCanvasTableInput) { i.Cells = [][]string{{strings.Repeat("x", 5001)}} }, "cells[0][0]"},
		{"widths length", func(i *createCanvasTableInput) { i.ColumnWidths = []float64{100, 100} }, "columnWidths"},
		{"width below min", func(i *createCanvasTableInput) { i.ColumnWidths = []float64{100, 39, 100} }, "columnWidths[1]"},
		{"x out of range", func(i *createCanvasTableInput) { i.PositionX = 1e9 }, "positionX"},
		{"too wide", func(i *createCanvasTableInput) { i.ColumnWidths = []float64{60000, 60000, 40} }, "columnWidths"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validCreateTable()
			tc.edit(&in)
			_, _, _, e := buildCanvasTablePayload(in)
			require.NotNil(t, e)
			assert.Equal(t, mcperr.ValidationError, e.Code)
			assert.Equal(t, tc.field, e.Field)
		})
	}
	// 500 cells is allowed.
	in := validCreateTable()
	in.Rows, in.Columns = 25, 20
	_, _, _, e := buildCanvasTablePayload(in)
	assert.Nil(t, e)
}

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type fakeTableEnv struct {
	table    data.CanvasTable
	kind     string
	boardID  string
	emitted  []map[string]any
	events   []string
	ackFor   func(n int, payload map[string]any) socket.AckResult
	authed   int
	authFail error
	// errFor simulates a transport error on the nth emit; applied says whether
	// the server applied the op anyway (a lost ack).
	errFor func(n int) (err error, applied bool)
	// loadFail simulates a database error on the nth element read.
	loadFail func(call int) error
	loads    int
	// style is the stored element style JSON; missing makes the element read return nil.
	style   string
	missing bool
	// skipHeight makes the fake ignore a rowHeight write, as the app does when ifRowHeight no longer matches.
	skipHeight bool
}

func (f *fakeTableEnv) styleJSON() string {
	if f.style != "" {
		return f.style
	}
	return `{"fontSize":14}`
}

func (f *fakeTableEnv) element() *data.CanvasElement {
	props, _ := json.Marshal(map[string]any{"kind": "table", "table": f.table})
	return &data.CanvasElement{
		ID: canvasTestElementID, BoardID: f.boardID, Kind: f.kind,
		Props: props, Style: data.JSONText(f.styleJSON()),
	}
}

func (f *fakeTableEnv) fns() canvasTableFns {
	return canvasTableFns{
		authorize: func(context.Context, string) (string, error) {
			f.authed++
			return "user-1", f.authFail
		},
		loadElement: func(context.Context, string) (*data.CanvasElement, error) {
			f.loads++
			if f.loadFail != nil {
				if err := f.loadFail(f.loads); err != nil {
					return nil, err
				}
			}
			if f.missing {
				return nil, nil
			}
			return f.element(), nil
		},
		emit: func(_ context.Context, _, _, event string, payload map[string]any) (socket.AckResult, error) {
			f.emitted = append(f.emitted, payload)
			f.events = append(f.events, event)
			if f.errFor != nil {
				if err, applied := f.errFor(len(f.emitted)); err != nil {
					if applied {
						f.apply(payload)
					}
					return nil, err
				}
			}
			ack := socket.AckResult{"ok": true, "entity": map[string]any{"id": "x"}}
			if f.ackFor != nil {
				ack = f.ackFor(len(f.emitted), payload)
			}
			if ack.OK() {
				f.apply(payload)
			}
			return ack, nil
		},
	}
}

// apply mimics just enough of the server to keep the stored table in step.
func (f *fakeTableEnv) apply(payload map[string]any) {
	op, ok := payload["op"].(map[string]any)
	if !ok {
		return
	}
	switch op["type"] {
	case "setCellText":
		key := op["rowId"].(string) + ":" + op["colId"].(string)
		cell := f.table.Cells[key]
		cell.Text = op["text"].(string)
		f.table.Cells[key] = cell
		if h, ok := op["rowHeight"].(float64); ok {
			for i, r := range f.table.Rows {
				if r.ID == op["rowId"] && r.Height == op["ifRowHeight"] && !f.skipHeight {
					f.table.Rows[i].Height = h
				}
			}
		}
	case "setHeaderRow":
		f.table.HeaderRow = op["on"].(bool)
	case "insertCol":
		col := op["col"].(map[string]any)
		f.table.Cols = append(f.table.Cols, data.TableCol{ID: col["id"].(string), Width: 120})
	case "removeCol":
		for i, c := range f.table.Cols {
			if c.ID == op["colId"] {
				f.table.Cols = append(f.table.Cols[:i], f.table.Cols[i+1:]...)
				break
			}
		}
	case "setCellFill":
		for _, ref := range op["cells"].([]map[string]any) {
			key := ref["rowId"].(string) + ":" + ref["colId"].(string)
			cell := f.table.Cells[key]
			cell.Fill, _ = op["fill"].(string)
			f.table.Cells[key] = cell
		}
	case "insertRow":
		row := op["row"].(map[string]any)
		at := 0
		if after, ok := op["afterRowId"].(string); ok {
			for i, r := range f.table.Rows {
				if r.ID == after {
					at = i + 1
				}
			}
		}
		f.table.Rows = append(f.table.Rows[:at], append([]data.TableRow{{ID: row["id"].(string), Height: 40}}, f.table.Rows[at:]...)...)
	case "removeRow":
		for i, r := range f.table.Rows {
			if r.ID == op["rowId"] {
				f.table.Rows = append(f.table.Rows[:i], f.table.Rows[i+1:]...)
				break
			}
		}
	}
}

func newFakeTable(rows, cols int) *fakeTableEnv {
	f := &fakeTableEnv{kind: "table", boardID: canvasTestBoardID}
	f.table = data.CanvasTable{Cells: map[string]data.TableCell{}, HeaderRow: true}
	for i := 0; i < rows; i++ {
		f.table.Rows = append(f.table.Rows, data.TableRow{ID: "r" + string(rune('0'+i)), Height: 40})
	}
	for i := 0; i < cols; i++ {
		f.table.Cols = append(f.table.Cols, data.TableCol{ID: "c" + string(rune('0'+i)), Width: 120})
	}
	return f
}

// chg decodes a change from JSON, as the SDK does, so key presence is recorded.
func chg(t *testing.T, raw string) tableChangeInput {
	t.Helper()
	var c tableChangeInput
	require.NoError(t, json.Unmarshal([]byte(raw), &c))
	return c
}

func ip(i int) *int { return &i }

func bp(b bool) *bool { return &b }
func editOf(chs ...tableChangeInput) editCanvasTableInput {
	return editCanvasTableInput{CanvasBoardID: canvasTestBoardID, ElementID: canvasTestElementID, Changes: chs}
}

func textOf(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func decodeResult(t *testing.T, res any, text string) editCanvasTableResult {
	t.Helper()
	var out editCanvasTableResult
	require.NoError(t, json.Unmarshal([]byte(text), &out), text)
	return out
}

func runEdit(t *testing.T, f *fakeTableEnv, in editCanvasTableInput) (isErr bool, text string) {
	t.Helper()
	res, _, err := editCanvasTable(context.Background(), f.fns(), in)
	require.NoError(t, err)
	require.NotNil(t, res)
	return res.IsError, textOf(res)
}

// ---------------------------------------------------------------------------
// edit: shape validation, before the gate
// ---------------------------------------------------------------------------

func TestEditCanvasTable_ShapeRefusalsNeverReachTheGate(t *testing.T) {
	long := strings.Repeat("x", 5001)
	cases := []struct {
		name string
		in   editCanvasTableInput
	}{
		{"empty changes", editOf()},
		{"too many changes", editOf(make([]tableChangeInput, 201)...)},
		{"unknown type", editOf(tableChangeInput{Type: "explode"})},
		{"setText no text", editOf(tableChangeInput{Type: "setText", Row: ip(0), Column: ip(0)})},
		{"setText too long", editOf(tableChangeInput{Type: "setText", Row: ip(0), Column: ip(0), Text: &long})},
		{"negative row", editOf(tableChangeInput{Type: "removeRow", Row: ip(-1)})},
		{"insertRow no at", editOf(tableChangeInput{Type: "insertRow"})},
		{"resizeRow below min", editOf(tableChangeInput{Type: "resizeRow", Row: ip(0), Height: f64Ptr(23)})},
		{"resizeColumn below min", editOf(tableChangeInput{Type: "resizeColumn", Column: ip(0), Width: f64Ptr(39)})},
		{"header no on", editOf(tableChangeInput{Type: "setHeaderRow"})},
		{"fill reversed range", editOf(tableChangeInput{Type: "setFill", FromRow: ip(2), ToRow: ip(1), FromColumn: ip(0), ToColumn: ip(0)})},
		{"align bad enum", editOf(tableChangeInput{Type: "setAlign", FromRow: ip(0), ToRow: ip(0), FromColumn: ip(0), ToColumn: ip(0), Align: sp("justify")})},
		{"bad element id", editCanvasTableInput{ElementID: "nope", Changes: []tableChangeInput{{Type: "setHeaderRow", On: bp(true)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTable(2, 2)
			isErr, _ := runEdit(t, f, tc.in)
			assert.True(t, isErr)
			assert.Zero(t, f.authed, "shape errors must be raised before the gate")
			assert.Empty(t, f.emitted)
		})
	}
}

func TestEditCanvasTable_NamesTheChangeInTheError(t *testing.T) {
	e := checkEditCanvasTableShape(editOf(
		tableChangeInput{Type: "setHeaderRow", On: bp(true)},
		tableChangeInput{Type: "removeRow"},
	))
	require.NotNil(t, e)
	assert.Equal(t, "changes[1].row", e.Field)
}

func TestEditCanvasTable_ViewerIsRefusedBeforeAnyRead(t *testing.T) {
	f := newFakeTable(2, 2)
	f.authFail = mcperr.New(mcperr.Forbidden, "no")
	isErr, _ := runEdit(t, f, editOf(tableChangeInput{Type: "setHeaderRow", On: bp(true)}))
	assert.True(t, isErr)
	assert.Empty(t, f.emitted)
}

func TestProdCanvasTableFns_UseTheEditorGate(t *testing.T) {
	fns := prodCanvasTableFns()
	require.NotNil(t, fns.authorize)
	require.NotNil(t, fns.loadElement)
	require.NotNil(t, fns.emit)
}

// ---------------------------------------------------------------------------
// edit: change -> op mapping
// ---------------------------------------------------------------------------

func opOf(t *testing.T, f *fakeTableEnv, i int) map[string]any {
	t.Helper()
	require.Greater(t, len(f.emitted), i)
	assert.Equal(t, "element:table-op", f.events[i])
	assert.Equal(t, canvasTestElementID, f.emitted[i]["elementId"])
	return f.emitted[i]["op"].(map[string]any)
}

func TestEditCanvasTable_OpMapping(t *testing.T) {
	f := newFakeTable(3, 3)
	isErr, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "setText", Row: ip(1), Column: ip(2), Text: sp("hi")},
		tableChangeInput{Type: "removeColumn", Column: ip(0)},
		tableChangeInput{Type: "resizeRow", Row: ip(2), Height: f64Ptr(80)},
		tableChangeInput{Type: "resizeColumn", Column: ip(1), Width: f64Ptr(200)},
		tableChangeInput{Type: "setHeaderRow", On: bp(false)},
		tableChangeInput{Type: "setFill", FromRow: ip(0), ToRow: ip(1), FromColumn: ip(0), ToColumn: ip(1), Fill: sp("#ff0000")},
		chg(t, `{"type":"setFill","fromRow":0,"toRow":0,"fromColumn":0,"toColumn":0,"fill":null}`),
		tableChangeInput{Type: "setAlign", FromRow: ip(0), ToRow: ip(0), FromColumn: ip(0), ToColumn: ip(0), Align: sp("center")},
	))
	require.False(t, isErr, text)
	res := decodeResult(t, nil, text)
	assert.Equal(t, 8, res.Applied)
	assert.Nil(t, res.Failed)

	op := opOf(t, f, 0)
	assert.Equal(t, "setCellText", op["type"])
	assert.Equal(t, "r1", op["rowId"])
	assert.Equal(t, "c2", op["colId"])
	assert.Equal(t, "removeCol", opOf(t, f, 1)["type"])
	assert.Equal(t, map[string]any{"type": "resizeRow", "rowId": "r2", "height": 80.0}, opOf(t, f, 2))
	assert.Equal(t, map[string]any{"type": "resizeCol", "colId": "c2", "width": 200.0}, opOf(t, f, 3)) // c0 was removed: column 1 is now c2
	assert.Equal(t, map[string]any{"type": "setHeaderRow", "on": false}, opOf(t, f, 4))
	fill := opOf(t, f, 5)
	assert.Equal(t, "setCellFill", fill["type"])
	assert.Equal(t, "#ff0000", fill["fill"])
	assert.Len(t, fill["cells"], 4)
	cleared := opOf(t, f, 6)
	assert.Nil(t, cleared["fill"], "an explicit null fill clears")
	_, hasFill := cleared["fill"]
	assert.True(t, hasFill, "the key is sent as null")
	assert.Equal(t, "center", opOf(t, f, 7)["align"])
}

func TestEditCanvasTable_InsertAtAndAppend(t *testing.T) {
	f := newFakeTable(2, 2)
	isErr, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "insertRow", At: ip(0)},
		tableChangeInput{Type: "insertRow", At: ip(3)}, // 3 rows now; 3 appends
		tableChangeInput{Type: "insertColumn", At: ip(1)},
	))
	require.False(t, isErr, text)
	first := opOf(t, f, 0)
	assert.Nil(t, first["afterRowId"], "at 0 inserts at the top")
	assert.Equal(t, 40.0, first["row"].(map[string]any)["height"])
	assert.Equal(t, f.table.Rows[2].ID, opOf(t, f, 1)["afterRowId"], "at = count appends after the last row")
	col := opOf(t, f, 2)
	assert.Equal(t, "insertCol", col["type"])
	assert.Equal(t, "c0", col["afterColId"])
	assert.Equal(t, 120.0, col["col"].(map[string]any)["width"])
	res := decodeResult(t, nil, text)
	assert.Equal(t, 4, res.Rows)
	assert.Equal(t, 3, res.Columns)
}

// Grow only: a height goes out when the estimate beats the stored row, guarded
// by ifRowHeight; a short text sends no height at all.
func TestEditCanvasTable_SetTextGrowsRowsOnly(t *testing.T) {
	f := newFakeTable(2, 2)
	long := strings.Repeat("word ", 30)
	_, _ = runEdit(t, f, editOf(
		tableChangeInput{Type: "setText", Row: ip(0), Column: ip(0), Text: &long},
		tableChangeInput{Type: "setText", Row: ip(1), Column: ip(0), Text: sp("ok")},
		tableChangeInput{Type: "setText", Row: ip(1), Column: ip(1), Text: sp("")},
	))
	grow := opOf(t, f, 0)
	assert.Greater(t, grow["rowHeight"].(float64), 40.0)
	assert.Equal(t, 40.0, grow["ifRowHeight"])
	short := opOf(t, f, 1)
	_, hasH := short["rowHeight"]
	_, hasIf := short["ifRowHeight"]
	assert.False(t, hasH || hasIf, "a text that fits sends no height")
	assert.Len(t, f.emitted, 2, "clearing an already empty cell is a no-op and emits nothing")
}

// ---------------------------------------------------------------------------
// edit: range errors, stop at first failure
// ---------------------------------------------------------------------------

func TestEditCanvasTable_RangeErrorNamesTheField(t *testing.T) {
	f := newFakeTable(4, 2)
	isErr, text := runEdit(t, f, editOf(tableChangeInput{Type: "removeRow", Row: ip(5)}))
	assert.True(t, isErr, "nothing applied: a tool error")
	assert.Contains(t, text, "changes[0].row: 5 is out of range, the table has 4 rows")
	assert.Empty(t, f.emitted)
}

func TestEditCanvasTable_StopsAtTheFirstRefusedChange(t *testing.T) {
	f := newFakeTable(2, 1)
	f.ackFor = func(n int, _ map[string]any) socket.AckResult {
		if n == 2 {
			return socket.AckResult{"ok": false, "code": "VALIDATION_ERROR", "message": "Cannot remove the last row or column"}
		}
		return socket.AckResult{"ok": true}
	}
	isErr, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "setHeaderRow", On: bp(true)},
		tableChangeInput{Type: "removeColumn", Column: ip(0)},
		tableChangeInput{Type: "setHeaderRow", On: bp(false)},
		tableChangeInput{Type: "setHeaderRow", On: bp(true)},
	))
	require.False(t, isErr, "a refusal after progress is a success result carrying failed")
	res := decodeResult(t, nil, text)
	assert.Equal(t, 1, res.Applied)
	require.NotNil(t, res.Failed)
	assert.Equal(t, 1, res.Failed.Index)
	assert.Equal(t, "removeColumn", res.Failed.Type)
	assert.Equal(t, "VALIDATION_ERROR", res.Failed.Code)
	assert.Contains(t, res.Failed.Message, "last row or column")
	assert.Equal(t, 2, res.Skipped)
	assert.Len(t, f.emitted, 2, "no change after the refusal is emitted")
	assert.Equal(t, 2, res.Rows)
	assert.Equal(t, 1, res.Columns)
}

func TestEditCanvasTable_FirstChangeRefusedIsAToolError(t *testing.T) {
	f := newFakeTable(1, 1)
	f.ackFor = func(int, map[string]any) socket.AckResult {
		return socket.AckResult{"ok": false, "code": "FORBIDDEN", "message": "viewer"}
	}
	isErr, text := runEdit(t, f, editOf(tableChangeInput{Type: "removeRow", Row: ip(0)}))
	assert.True(t, isErr)
	assert.Contains(t, text, "FORBIDDEN")
}

func TestEditCanvasTable_RenumbersAfterEachChange(t *testing.T) {
	f := newFakeTable(3, 1)
	// After removing row 0, old row 2 is row 1: the stored table is re-read.
	_, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "removeRow", Row: ip(0)},
		tableChangeInput{Type: "removeRow", Row: ip(1)},
	))
	res := decodeResult(t, nil, text)
	assert.Equal(t, 2, res.Applied)
	assert.Equal(t, "r2", opOf(t, f, 1)["rowId"], "the second change saw the table after the first")
}

func TestLoadStoredTable_RefusesNonTablesAndOtherBoards(t *testing.T) {
	f := newFakeTable(1, 1)
	f.kind = "rectangle"
	_, err := loadStoredTable(context.Background(), f.fns(), canvasTestBoardID, canvasTestElementID)
	require.Error(t, err)
	assert.Equal(t, mcperr.ValidationError, err.(*mcperr.McpError).Code)

	f = newFakeTable(1, 1)
	f.boardID = "someone-elses-board"
	_, err = loadStoredTable(context.Background(), f.fns(), canvasTestBoardID, canvasTestElementID)
	require.Error(t, err)
	assert.Equal(t, mcperr.NotFound, err.(*mcperr.McpError).Code)
}

// ---------------------------------------------------------------------------
// create handler
// ---------------------------------------------------------------------------

func TestCreateCanvasTable_ValidatesBeforeTheGate(t *testing.T) {
	f := newFakeTable(1, 1)
	in := validCreateTable()
	in.Rows = 0
	res, _, err := createCanvasTable(context.Background(), f.fns(), in)
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Zero(t, f.authed)
}

func TestCreateCanvasTable_EmitsElementCreateAndReturnsIDs(t *testing.T) {
	f := newFakeTable(1, 1)
	res, _, err := createCanvasTable(context.Background(), f.fns(), validCreateTable())
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Equal(t, []string{"element:create"}, f.events)
	var out struct {
		Element   map[string]any `json:"element"`
		RowIDs    []string       `json:"rowIds"`
		ColumnIDs []string       `json:"columnIds"`
	}
	require.NoError(t, json.Unmarshal([]byte(textOf(res)), &out))
	assert.Len(t, out.RowIDs, 3)
	assert.Len(t, out.ColumnIDs, 3)
}

// ---------------------------------------------------------------------------
// estimate: one test per branch of the app's layout
// ---------------------------------------------------------------------------

// "Total revenue" is 97.15 px regular and 109.43 px bold in a 104 px text area:
// the header row is drawn bold, so it wraps where a body cell does not.
func TestEstimateRowHeight_BoldHeaderWraps(t *testing.T) {
	assert.Equal(t, browserHeight(1), estimateRowHeight("Total revenue", 120, 14, false))
	assert.GreaterOrEqual(t, estimateRowHeight("Total revenue", 120, 14, true), browserHeight(2))
}

func TestEstimateRowHeight_TodoMarker(t *testing.T) {
	// Measured in the browser: 4 lines in a 120 px column.
	got := estimateRowHeight("☐ Review the quarterly figures", 120, 14, false)
	assert.GreaterOrEqual(t, got, browserHeight(4))
	assert.LessOrEqual(t, got, browserHeight(6))
	assert.Greater(t, got, estimateRowHeight("x Review the quarterly figures", 120, 14, false)-1,
		"the hanging indent never makes a todo shorter than plain text")
}

func TestEstimateRowHeight_NonASCIIPunctuationAndCyrillic(t *testing.T) {
	// An em dash and an ellipsis reach 1.34 em, so this wraps in 104 px.
	assert.GreaterOrEqual(t, estimateRowHeight("Q1 — Q2 — Q3", 120, 14, false), browserHeight(2))
	assert.GreaterOrEqual(t, estimateRowHeight("ЖЖЖЖЖЖЖЖ", 120, 14, true), browserHeight(2))
	assert.GreaterOrEqual(t, estimateRowHeight("12‰ 12‰ 12‰ 12‰", 120, 14, false), browserHeight(2))
	// Bopomofo and symbols count as wide: 8 per 104 px line is too many.
	assert.GreaterOrEqual(t, estimateRowHeight("ㄅㄆㄇㄈㄉㄊㄋㄌ", 120, 14, false), browserHeight(2))
}

func TestEstimateRowHeight_BoldIsNeverNarrower(t *testing.T) {
	for i, reg := range regularAdvanceEm {
		assert.GreaterOrEqual(t, boldAdvanceEm[i], reg, "glyph %q", rune(0x20+i))
	}
}

// ---------------------------------------------------------------------------
// bold in create and edit
// ---------------------------------------------------------------------------

func TestBuildCanvasTablePayload_HeaderCellIsMeasuredBold(t *testing.T) {
	in := validCreateTable()
	in.Cells = [][]string{{"Total revenue"}, {"Total revenue"}}
	payload, _, _, e := buildCanvasTablePayload(in)
	require.Nil(t, e)
	rows := payload["props"].(map[string]any)["table"].(map[string]any)["rows"].([]map[string]any)
	assert.GreaterOrEqual(t, rows[0]["height"].(float64), browserHeight(2), "header row: bold wraps")
	assert.Equal(t, 40.0, rows[1]["height"], "body row: regular fits one line")

	off := false
	in.HeaderRow = &off
	payload, _, _, _ = buildCanvasTablePayload(in)
	rows = payload["props"].(map[string]any)["table"].(map[string]any)["rows"].([]map[string]any)
	assert.Equal(t, 40.0, rows[0]["height"], "no header row: nothing is bold")
}

func TestEditCanvasTable_HeaderRowAndBoldStyleDecideTheWeight(t *testing.T) {
	f := newFakeTable(2, 1) // headerRow true
	_, _ = runEdit(t, f, editOf(
		tableChangeInput{Type: "setText", Row: ip(0), Column: ip(0), Text: sp("Total revenue")},
		tableChangeInput{Type: "setText", Row: ip(1), Column: ip(0), Text: sp("Total revenue")},
	))
	assert.Contains(t, opOf(t, f, 0), "rowHeight")
	assert.NotContains(t, opOf(t, f, 1), "rowHeight")

	f = newFakeTable(2, 1)
	f.style = `{"fontSize":14,"fontWeight":"bold"}`
	_, _ = runEdit(t, f, editOf(tableChangeInput{Type: "setText", Row: ip(1), Column: ip(0), Text: sp("Total revenue")}))
	assert.Contains(t, opOf(t, f, 0), "rowHeight", "a table styled bold is bold throughout")
}

// ---------------------------------------------------------------------------
// edit: change validation
// ---------------------------------------------------------------------------

func TestEditCanvasTable_SetFillNeedsAnExplicitFill(t *testing.T) {
	rng := `"fromRow":0,"toRow":0,"fromColumn":0,"toColumn":0`
	e := checkTableChangeShape(0, chg(t, `{"type":"setFill",`+rng+`}`))
	require.NotNil(t, e, "a missing fill must not silently clear")
	assert.Equal(t, "changes[0].fill", e.Field)
	assert.Nil(t, checkTableChangeShape(0, chg(t, `{"type":"setFill",`+rng+`,"fill":null}`)))
	assert.Nil(t, checkTableChangeShape(0, chg(t, `{"type":"setFill",`+rng+`,"fill":""}`)))

	f := newFakeTable(1, 1)
	_, _ = runEdit(t, f, editOf(chg(t, `{"type":"setFill",`+rng+`,"fill":""}`)))
	assert.Nil(t, opOf(t, f, 0)["fill"], "an empty string clears")
}

func TestEditCanvasTable_RefusesAFieldForeignToItsType(t *testing.T) {
	cases := map[string]string{
		`{"type":"resizeRow","row":0,"height":30,"width":50}`: "changes[1].width",
		`{"type":"removeRow","row":0,"text":"x"}`:             "changes[1].text",
		`{"type":"setHeaderRow","on":true,"row":0}`:           "changes[1].row",
	}
	for raw, field := range cases {
		e := checkEditCanvasTableShape(editOf(
			tableChangeInput{Type: "setHeaderRow", On: bp(true)}, chg(t, raw)))
		require.NotNil(t, e, raw)
		assert.Equal(t, field, e.Field)
	}
}

func TestEditCanvasTable_UnhandledTypeIsInternal(t *testing.T) {
	_, e := buildTableOp(0, tableChangeInput{Type: "bogus"}, &storedTable{Table: &data.CanvasTable{}})
	require.NotNil(t, e)
	assert.Equal(t, mcperr.InternalError, e.Code)
}

func TestEditCanvasTable_SetTextEqualToStoredIsANoOp(t *testing.T) {
	f := newFakeTable(1, 1)
	f.table.Cells["r0:c0"] = data.TableCell{Text: "same"}
	_, text := runEdit(t, f, editOf(tableChangeInput{Type: "setText", Row: ip(0), Column: ip(0), Text: sp("same")}))
	assert.Empty(t, f.emitted)
	assert.Equal(t, 1, decodeResult(t, nil, text).Applied)
}

func TestEditCanvasTable_ReportsASkippedRowHeight(t *testing.T) {
	f := newFakeTable(1, 1)
	f.skipHeight = true // the app ignores the height: ifRowHeight no longer matches
	long := strings.Repeat("word ", 30)
	_, text := runEdit(t, f, editOf(tableChangeInput{Type: "setText", Row: ip(0), Column: ip(0), Text: &long}))
	res := decodeResult(t, nil, text)
	assert.Equal(t, 1, res.Applied)
	assert.Equal(t, []int{0}, res.RowHeightSkipped)

	f = newFakeTable(1, 1)
	_, text = runEdit(t, f, editOf(tableChangeInput{Type: "setText", Row: ip(0), Column: ip(0), Text: &long}))
	assert.Empty(t, decodeResult(t, nil, text).RowHeightSkipped)
}

// ---------------------------------------------------------------------------
// edit: transport and database errors mid-list
// ---------------------------------------------------------------------------

func TestEditCanvasTable_LostAckThatLanded_CountsAsApplied(t *testing.T) {
	f := newFakeTable(2, 1)
	f.errFor = func(n int) (error, bool) {
		if n == 2 {
			return mcperr.New(mcperr.ConnectionError, "ack timeout"), true // applied anyway
		}
		return nil, false
	}
	isErr, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "setHeaderRow", On: bp(false)},
		tableChangeInput{Type: "insertRow", At: ip(2)},
		tableChangeInput{Type: "setHeaderRow", On: bp(true)},
	))
	require.False(t, isErr, text)
	res := decodeResult(t, nil, text)
	assert.Equal(t, 3, res.Applied, "the insert is found on re-read, so it is not reported as refused")
	assert.Nil(t, res.Failed)
	assert.Equal(t, 3, res.Rows)
}

func TestEditCanvasTable_LostAckThatDidNotLand_IsAnUnknownOutcome(t *testing.T) {
	f := newFakeTable(2, 2)
	f.errFor = func(n int) (error, bool) {
		if n == 2 {
			return mcperr.New(mcperr.ConnectionError, "ack timeout"), false
		}
		return nil, false
	}
	_, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "setHeaderRow", On: bp(false)},
		tableChangeInput{Type: "insertColumn", At: ip(0)},
		tableChangeInput{Type: "setHeaderRow", On: bp(true)},
	))
	res := decodeResult(t, nil, text)
	assert.Equal(t, 1, res.Applied)
	require.NotNil(t, res.Failed)
	assert.Equal(t, "CONNECTION_ERROR", res.Failed.Code)
	assert.Contains(t, res.Failed.Message, "Outcome unknown")
	assert.Contains(t, res.Failed.Message, "before retrying")
	assert.Equal(t, 1, res.Skipped)
	assert.Equal(t, 2, res.Columns, "the counts come from the re-read")
}

func TestEditCanvasTable_AnEmitErrorIsMaskedWhenItIsNotAnMcpError(t *testing.T) {
	f := newFakeTable(1, 1)
	f.errFor = func(n int) (error, bool) {
		if n == 2 {
			return errors.New("dial tcp 10.0.0.7:3000: secret-host refused"), false
		}
		return nil, false
	}
	_, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "setHeaderRow", On: bp(false)},
		tableChangeInput{Type: "setHeaderRow", On: bp(true)},
	))
	assert.NotContains(t, text, "secret-host")
	assert.Contains(t, text, "Outcome unknown")
}

func TestEditCanvasTable_DatabaseErrorMidListIsMasked(t *testing.T) {
	f := newFakeTable(1, 1)
	f.loadFail = func(call int) error {
		if call == 2 {
			return errors.New("pq: password authentication failed for user liz")
		}
		return nil
	}
	isErr, text := runEdit(t, f, editOf(
		tableChangeInput{Type: "setHeaderRow", On: bp(false)},
		tableChangeInput{Type: "setHeaderRow", On: bp(true)},
	))
	require.False(t, isErr)
	res := decodeResult(t, nil, text)
	require.NotNil(t, res.Failed)
	assert.Equal(t, "INTERNAL_ERROR", res.Failed.Code)
	assert.Equal(t, "An internal error occurred.", res.Failed.Message)
	assert.NotContains(t, text, "password")

	f = newFakeTable(1, 1)
	f.loadFail = func(int) error { return errors.New("pq: boom") }
	isErr, text = runEdit(t, f, editOf(tableChangeInput{Type: "setHeaderRow", On: bp(false)}))
	assert.True(t, isErr)
	assert.NotContains(t, text, "boom")
}

func TestCodeMsg(t *testing.T) {
	code, msg := codeMsg(mcperr.New(mcperr.NotFound, "gone"), mcperr.InternalError)
	assert.Equal(t, mcperr.NotFound, code)
	assert.Equal(t, "gone", msg)
	code, msg = codeMsg(fmt.Errorf("wrapped: %w", mcperr.New(mcperr.Forbidden, "no")), mcperr.InternalError)
	assert.Equal(t, mcperr.Forbidden, code, "a wrapped McpError keeps its code")
	assert.Equal(t, "no", msg)
	code, msg = codeMsg(errors.New("secret detail"), mcperr.ConnectionError)
	assert.Equal(t, mcperr.ConnectionError, code)
	assert.Equal(t, "An internal error occurred.", msg)
}

// ---------------------------------------------------------------------------
// create: client id and lost ack
// ---------------------------------------------------------------------------

func TestCreateCanvasTable_SendsAClientElementID(t *testing.T) {
	f := newFakeTable(1, 1)
	_, _, err := createCanvasTable(context.Background(), f.fns(), validCreateTable())
	require.NoError(t, err)
	id, _ := f.emitted[0]["id"].(string)
	assert.Nil(t, validateUUID("id", id))
}

func TestCreateCanvasTable_LostAckThatLanded_Succeeds(t *testing.T) {
	f := newFakeTable(1, 1)
	f.errFor = func(int) (error, bool) { return mcperr.New(mcperr.ConnectionError, "ack timeout"), true }
	res, _, err := createCanvasTable(context.Background(), f.fns(), validCreateTable())
	require.NoError(t, err)
	require.False(t, res.IsError, textOf(res))
	assert.Contains(t, textOf(res), f.emitted[0]["id"].(string))
}

func TestCreateCanvasTable_LostAckNotFound_ReportsTheIDAndUnknownOutcome(t *testing.T) {
	f := newFakeTable(1, 1)
	f.missing = true
	f.errFor = func(int) (error, bool) { return mcperr.New(mcperr.ConnectionError, "ack timeout"), false }
	res, _, err := createCanvasTable(context.Background(), f.fns(), validCreateTable())
	require.NoError(t, err)
	require.True(t, res.IsError)
	text := textOf(res)
	assert.Contains(t, text, f.emitted[0]["id"].(string), "the id is returned so the caller can check it")
	assert.Contains(t, text, "Outcome unknown")
	assert.Contains(t, text, "before retrying")
}

// ---------------------------------------------------------------------------
// documented limits match the constants
// ---------------------------------------------------------------------------

func TestEditCanvasTable_DocumentedLimitsMatchTheConstants(t *testing.T) {
	for _, want := range []string{
		fmt.Sprintf("1 to %d items", tableMaxChanges),
		fmt.Sprintf("height >= %g", tableMinRowHeight),
		fmt.Sprintf("width >= %g", tableMinColWidth),
		fmt.Sprintf("at most %d characters", tableMaxTextLength),
	} {
		assert.Contains(t, editCanvasTableDescription, want)
	}
	tag := func(typ any, field string) string {
		f, ok := reflect.TypeOf(typ).FieldByName(field)
		require.True(t, ok)
		return f.Tag.Get("jsonschema")
	}
	assert.Contains(t, tag(tableChangeInput{}, "Height"), fmt.Sprintf("at least %g", tableMinRowHeight))
	assert.Contains(t, tag(tableChangeInput{}, "Width"), fmt.Sprintf("at least %g", tableMinColWidth))
	assert.Contains(t, tag(tableChangeInput{}, "Text"), fmt.Sprintf("%d characters", tableMaxTextLength))
	assert.Contains(t, tag(editCanvasTableInput{}, "Changes"), fmt.Sprintf("1 to %d", tableMaxChanges))
	assert.Contains(t, tag(createCanvasTableInput{}, "Rows"), fmt.Sprintf("%d", tableMaxCells))
	assert.Contains(t, tag(createCanvasTableInput{}, "Cells"), fmt.Sprintf("%d characters", tableMaxTextLength))
	assert.Contains(t, tag(createCanvasTableInput{}, "ColumnWidths"), fmt.Sprintf("at least %g", tableMinColWidth))
}

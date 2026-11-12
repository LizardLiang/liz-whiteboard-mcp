// Canvas table support: the stored table model, the row-height estimate, and
// create_canvas_table. edit_canvas_table lives in canvas_table_edit.go.
//
// The table element is the app's own grid (liz-whiteboard
// src/lib/canvas-engine/table.ts). This file mirrors only what the MCP needs,
// and every constant names its TypeScript source.
//
// Writes go over Socket.IO like every other canvas write: element:create for a
// new table, element:table-op for a change. The app applies an op to its STORED
// table, so two writers who touch different cells both keep their text.
//
// Only the browser can measure text. The MCP is a second writer, and it
// ESTIMATES the height a row needs (estimateRowHeight) and sends it in the op.
// The estimate rounds up. A wide-glyph Latin string can still spill by a line;
// resizeRow (edit_canvas_table) fixes that by hand.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// Constants mirrored from the app. Source in each comment.
const (
	// tableCellWidth / tableCellHeight mirror TABLE_CELL_WIDTH / TABLE_CELL_HEIGHT
	// (table.ts:51-52).
	tableCellWidth  = 120.0
	tableCellHeight = 40.0
	// tableMinColWidth / tableMinRowHeight mirror TABLE_MIN_COL_WIDTH /
	// TABLE_MIN_ROW_HEIGHT (table.ts:53-54).
	tableMinColWidth  = 40.0
	tableMinRowHeight = 24.0
	// tableMaxCells mirrors TABLE_MAX_CELLS (table.ts:56).
	tableMaxCells = 500
	// tableMaxTextLength mirrors TABLE_MAX_TEXT_LENGTH (table.ts:57).
	tableMaxTextLength = 5000
	// tableTextPadding mirrors TEXT_PADDING (render.ts:276): per side.
	tableTextPadding = 8.0
	// tableLineHeight mirrors the line-height factor in text-layout.ts:42.
	tableLineHeight = 1.4
	// tableFontSize mirrors TABLE_DEFAULT_STYLE.fontSize (table.ts:60-65).
	tableFontSize = 14.0
	// tableLatinEm / tableWideEm are the estimated glyph widths in em. Latin text
	// averages below 0.6 em; CJK, Hangul, kana and full-width forms are 1.0 em.
	tableLatinEm = 0.6
	tableWideEm  = 1.0
	// tableMaxChanges bounds one edit_canvas_table call.
	tableMaxChanges = 200
)

// tableRow, tableCol, tableCell and canvasTable mirror CanvasTable. A cell reads
// only its text: the MCP never writes a stored cell back whole, so unknown cell
// fields (textRuns) are never touched.
type tableRow struct {
	ID     string  `json:"id"`
	Height float64 `json:"height"`
}

type tableCol struct {
	ID    string  `json:"id"`
	Width float64 `json:"width"`
}

type tableCell struct {
	Text string `json:"text"`
}

type canvasTable struct {
	Rows      []tableRow           `json:"rows"`
	Cols      []tableCol           `json:"cols"`
	Cells     map[string]tableCell `json:"cells"`
	HeaderRow bool                 `json:"headerRow"`
}

// storedTable is a table element read from the database.
type storedTable struct {
	Table    canvasTable
	FontSize float64
}

// parseStoredTable decodes an element's props into a canvasTable. It never
// returns a nil table with a nil error.
func parseStoredTable(el *data.CanvasElement) (*storedTable, error) {
	var props struct {
		Kind  string       `json:"kind"`
		Table *canvasTable `json:"table"`
	}
	if err := json.Unmarshal(el.Props, &props); err != nil || props.Kind != "table" || props.Table == nil {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"That element's stored properties are not a table's.", "elementId")
	}
	if props.Table.Cells == nil {
		props.Table.Cells = map[string]tableCell{}
	}
	fontSize := tableFontSize
	var style struct {
		FontSize float64 `json:"fontSize"`
	}
	if json.Unmarshal(el.Style, &style) == nil && style.FontSize > 0 {
		fontSize = style.FontSize
	}
	return &storedTable{Table: *props.Table, FontSize: fontSize}, nil
}

// canvasTableFns is the injectable dependency set of both table tools: the
// write gate, the element read, and the socket emit. A test swaps all three.
type canvasTableFns struct {
	authorize   func(ctx context.Context, canvasBoardID string) (string, error)
	loadElement func(ctx context.Context, elementID string) (*data.CanvasElement, error)
	emit        func(ctx context.Context, userID, canvasBoardID, event string, payload map[string]any) (socket.AckResult, error)
}

// prodCanvasTableFns wires the production gate, reader and emitter. authorize
// MUST stay authorizeCanvasEdit (EDITOR+).
func prodCanvasTableFns() canvasTableFns {
	return canvasTableFns{
		authorize:   authorizeCanvasEdit,
		loadElement: data.FindCanvasElementByID,
		emit: func(ctx context.Context, userID, boardID, event string, payload map[string]any) (socket.AckResult, error) {
			return socket.SocketEmitWithAckNS(ctx, socket.NamespaceCanvas, boardID, userID, event, payload)
		},
	}
}

// loadStoredTable reads one table element and proves it is a table on the board
// the caller was authorized for. Call it only AFTER the gate. An id on another
// board reports the same NOT_FOUND as an id that exists nowhere.
func loadStoredTable(ctx context.Context, fns canvasTableFns, canvasBoardID, elementID string) (*storedTable, error) {
	el, err := fns.loadElement(ctx, elementID)
	if err != nil {
		return nil, err
	}
	if el == nil || el.BoardID != canvasBoardID {
		return nil, canvasElementNotFound("elementId", elementID)
	}
	if el.Kind != "table" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("Element %s is a %s, not a table. Use update_canvas_element for other kinds.",
				elementID, el.Kind), "elementId")
	}
	return parseStoredTable(el)
}

// ---------------------------------------------------------------------------
// Row-height estimate
// ---------------------------------------------------------------------------

// isWideRune reports a glyph drawn about one em wide: CJK, kana, Hangul,
// full-width forms and emoji.
func isWideRune(r rune) bool {
	switch {
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r),
		unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
		return true
	case r >= 0x3000 && r <= 0x303F, // CJK symbols and punctuation
		r >= 0xFF00 && r <= 0xFFEF, // full-width and half-width forms
		r >= 0x1F300:               // emoji and pictographs
		return true
	}
	return false
}

// estimateRowHeight estimates the row height a cell's text needs in a column
// colWidth wide, at fontSize. It splits on newlines, sums the glyph widths of
// each paragraph, wraps at the cell's inner width, and rounds up.
// Empty text returns 0: it asks for no change.
func estimateRowHeight(text string, colWidth, fontSize float64) float64 {
	if text == "" {
		return 0
	}
	inner := colWidth - 2*tableTextPadding
	if inner < 1 {
		inner = 1
	}
	lines := 0
	start := 0
	paragraphs := make([]string, 0, 4)
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			paragraphs = append(paragraphs, text[start:i])
			start = i + 1
		}
	}
	for _, p := range paragraphs {
		width := 0.0
		for _, r := range p {
			if isWideRune(r) {
				width += tableWideEm * fontSize
			} else {
				width += tableLatinEm * fontSize
			}
		}
		n := int(math.Ceil(width / inner))
		if n < 1 {
			n = 1
		}
		lines += n
	}
	return math.Ceil(float64(lines)*fontSize*tableLineHeight + 2*tableTextPadding)
}

// ---------------------------------------------------------------------------
// create_canvas_table
// ---------------------------------------------------------------------------

type createCanvasTableInput struct {
	CanvasBoardID string     `json:"canvasBoardId" jsonschema:"The canvas board UUID"`
	PositionX     float64    `json:"positionX" jsonschema:"X position of the table's top-left corner in board coordinates"`
	PositionY     float64    `json:"positionY" jsonschema:"Y position of the table's top-left corner in board coordinates"`
	Rows          int        `json:"rows" jsonschema:"Number of rows, at least 1; rows times columns is at most 500"`
	Columns       int        `json:"columns" jsonschema:"Number of columns, at least 1; rows times columns is at most 500"`
	Cells         [][]string `json:"cells,omitempty" jsonschema:"Starting cell text, row-major: cells[row][column]. May be shorter than the grid, never longer. Each text is at most 5000 characters"`
	HeaderRow     *bool      `json:"headerRow,omitempty" jsonschema:"Draw the first row as a bold header; defaults to true"`
	ColumnWidths  []float64  `json:"columnWidths,omitempty" jsonschema:"Width of each column, at least 40; one entry per column. Defaults to 120 each"`
}

// tableStyleForCreate is the style the browser sends for a new table
// (TABLE_DEFAULT_STYLE over DEFAULT_ELEMENT_STYLE, use-canvas-input.ts:842). The
// schema supplies the other style keys' defaults.
func tableStyleForCreate() map[string]any {
	return map[string]any{
		"fill":        "none",
		"stroke":      "#94a3b8",
		"strokeWidth": 1,
		"fontSize":    tableFontSize,
	}
}

// newTableID returns a fresh row or column id. A UUID holds no ":".
func newTableID() string { return uuid.NewString() }

// buildCanvasTablePayload validates a create request and renders the
// element:create payload plus the new row and column ids in order.
//
// It reads nothing that exists only after load: it runs BEFORE the write gate
// (commit 3640069: a pre-load path that dereferenced post-load data killed the
// server).
func buildCanvasTablePayload(in createCanvasTableInput) (map[string]any, []string, []string, *mcperr.McpError) {
	if e := checkCanvasCoord("positionX", in.PositionX); e != nil {
		return nil, nil, nil, e
	}
	if e := checkCanvasCoord("positionY", in.PositionY); e != nil {
		return nil, nil, nil, e
	}
	if in.Rows < 1 || in.Columns < 1 {
		return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
			"rows and columns must each be at least 1.", "rows")
	}
	if in.Rows > tableMaxCells || in.Columns > tableMaxCells || in.Rows*in.Columns > tableMaxCells {
		return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("A table holds at most %d cells; %d rows x %d columns is too many.",
				tableMaxCells, in.Rows, in.Columns), "rows")
	}
	if len(in.Cells) > in.Rows {
		return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("cells has %d rows but the table has %d.", len(in.Cells), in.Rows), "cells")
	}
	for r, line := range in.Cells {
		if len(line) > in.Columns {
			return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
				fmt.Sprintf("cells[%d] has %d entries but the table has %d columns.", r, len(line), in.Columns),
				fmt.Sprintf("cells[%d]", r))
		}
		for c, text := range line {
			if utf8.RuneCountInString(text) > tableMaxTextLength {
				return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
					fmt.Sprintf("cells[%d][%d] must be at most %d characters.", r, c, tableMaxTextLength),
					fmt.Sprintf("cells[%d][%d]", r, c))
			}
		}
	}
	if in.ColumnWidths != nil && len(in.ColumnWidths) != in.Columns {
		return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("columnWidths has %d entries but the table has %d columns.", len(in.ColumnWidths), in.Columns),
			"columnWidths")
	}
	widths := make([]float64, in.Columns)
	for c := range widths {
		widths[c] = tableCellWidth
		if in.ColumnWidths != nil {
			w := in.ColumnWidths[c]
			if e := checkFinite(fmt.Sprintf("columnWidths[%d]", c), w); e != nil {
				return nil, nil, nil, e
			}
			if w < tableMinColWidth {
				return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
					fmt.Sprintf("columnWidths[%d] must be at least %g.", c, tableMinColWidth),
					fmt.Sprintf("columnWidths[%d]", c))
			}
			widths[c] = w
		}
	}

	rowIDs := make([]string, in.Rows)
	colIDs := make([]string, in.Columns)
	for i := range rowIDs {
		rowIDs[i] = newTableID()
	}
	for i := range colIDs {
		colIDs[i] = newTableID()
	}

	rows := make([]map[string]any, in.Rows)
	cells := map[string]any{}
	totalHeight := 0.0
	for r := 0; r < in.Rows; r++ {
		height := tableCellHeight
		if r < len(in.Cells) {
			for c, text := range in.Cells[r] {
				if text == "" {
					continue
				}
				cells[rowIDs[r]+":"+colIDs[c]] = map[string]any{"text": text}
				height = math.Max(height, estimateRowHeight(text, widths[c], tableFontSize))
			}
		}
		rows[r] = map[string]any{"id": rowIDs[r], "height": height}
		totalHeight += height
	}
	cols := make([]map[string]any, in.Columns)
	totalWidth := 0.0
	for c := range cols {
		cols[c] = map[string]any{"id": colIDs[c], "width": widths[c]}
		totalWidth += widths[c]
	}
	if totalWidth > canvasSizeMax || totalHeight > canvasSizeMax {
		return nil, nil, nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("The table would be %gx%g; its width and height are each at most %d.",
				totalWidth, totalHeight, canvasSizeMax), "columnWidths")
	}

	headerRow := true
	if in.HeaderRow != nil {
		headerRow = *in.HeaderRow
	}
	return map[string]any{
		"kind":      "table",
		"positionX": in.PositionX,
		"positionY": in.PositionY,
		// The app recomputes both from the grid; they are sent because the
		// create schema requires them.
		"width":  totalWidth,
		"height": totalHeight,
		"style":  tableStyleForCreate(),
		"props": map[string]any{
			"kind": "table",
			"table": map[string]any{
				"rows":      rows,
				"cols":      cols,
				"cells":     cells,
				"headerRow": headerRow,
			},
		},
	}, rowIDs, colIDs, nil
}

// createCanvasTable validates, gates, then emits element:create.
func createCanvasTable(
	ctx context.Context,
	fns canvasTableFns,
	in createCanvasTableInput,
) (*mcp.CallToolResult, any, error) {
	payload, rowIDs, colIDs, e := buildCanvasTablePayload(in)
	if e != nil {
		return fail(e)
	}
	userID, err := fns.authorize(ctx, in.CanvasBoardID)
	if err != nil {
		return fail(err)
	}
	ack, err := fns.emit(ctx, userID, in.CanvasBoardID, "element:create", payload)
	if err != nil {
		return fail(err)
	}
	if !ack.OK() {
		return canvasAckFailure(ack, "Server rejected canvas table creation.")
	}
	return success(map[string]any{
		"element":   ack.Entity(),
		"rowIds":    rowIDs,
		"columnIds": colIDs,
	})
}

// RegisterCanvasTableTools registers create_canvas_table and edit_canvas_table.
func RegisterCanvasTableTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_canvas_table",
		Description: "Create a grid table on a canvas board (freeform FigJam-style board, NOT an ER " +
			"diagram whiteboard). Give rows and columns (at most 500 cells in all) and optionally " +
			"cells, the starting text as a row-major list of lists. The first row is a header unless " +
			"headerRow is false. Rows grow to fit their text. Returns the element plus rowIds and " +
			"columnIds in order. Change a table afterwards with edit_canvas_table. " +
			"For ER diagram tables use create_table instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCanvasTableInput) (*mcp.CallToolResult, any, error) {
		return createCanvasTable(ctx, prodCanvasTableFns(), in)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "edit_canvas_table",
		Description: editCanvasTableDescription,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in editCanvasTableInput) (*mcp.CallToolResult, any, error) {
		return editCanvasTable(ctx, prodCanvasTableFns(), in)
	})
}

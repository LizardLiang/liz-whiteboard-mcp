// Canvas table support: the stored table read, the idempotent create helper, and
// create_canvas_table. edit_canvas_table lives in canvas_table_edit.go and the
// row-height estimate in canvas_table_measure.go.
//
// The table element is the app's own grid (liz-whiteboard
// src/lib/canvas-engine/table.ts). This file mirrors only what the MCP needs,
// and every constant names its TypeScript source.
//
// Writes go over Socket.IO like every other canvas write: element:create for a
// new table, element:table-op for a change. The app applies an op to its STORED
// table, so two writers who touch different cells both keep their text.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	// tableMaxChanges bounds one edit_canvas_table call.
	tableMaxChanges = 200
)

// storedTable is a table element read from the database.
type storedTable struct {
	Table *data.CanvasTable
	Style tableStyle
}

// parseStoredTable decodes a table element's props and style.
func parseStoredTable(el *data.CanvasElement) (*storedTable, error) {
	table, err := data.ParseCanvasTable(el.Props)
	if err != nil {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"That element's stored properties are not a table's.", "elementId")
	}
	st := tableStyle{FontSize: tableFontSize}
	var style struct {
		FontSize   float64 `json:"fontSize"`
		FontWeight string  `json:"fontWeight"`
	}
	if json.Unmarshal(el.Style, &style) == nil {
		if style.FontSize > 0 {
			st.FontSize = style.FontSize
		}
		st.Bold = style.FontWeight == "bold"
	}
	return &storedTable{Table: table, Style: st}, nil
}

// canvasTableFns is the injectable dependency set of the table tools: the write
// gate, the element read, and the socket emit. A test swaps all three.
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
// Idempotent create
// ---------------------------------------------------------------------------

// emitCreateWithID emits element:create with a client-generated element id, so a
// lost ack cannot double-create: after an emit error it reads the element back by
// that id. It returns the entity to report, or a ready tool result when the call
// failed or its outcome is unknown.
//
// The app honours data.id on element:create (handlers.ts:262-266). It also skips
// the kind colour preset for such a create (handlers.ts:287), so callers use this
// only for kinds without a preset: table, text, connector.
func emitCreateWithID(
	ctx context.Context,
	fns canvasTableFns,
	userID, canvasBoardID string,
	payload map[string]any,
	fallbackMessage string,
) (any, *mcp.CallToolResult) {
	id := uuid.NewString()
	payload["id"] = id
	ack, err := fns.emit(ctx, userID, canvasBoardID, "element:create", payload)
	if err != nil {
		el, readErr := fns.loadElement(ctx, id)
		if readErr == nil && el != nil && el.BoardID == canvasBoardID {
			return map[string]any{"id": id, "note": "created; the ack was lost, the element was found on re-read"}, nil
		}
		return nil, toCallResult(mcperr.MakeMcpError(mcperr.ConnectionError, fmt.Sprintf(
			"Outcome unknown: the create may or may not have landed. The element id is %s; "+
				"re-read the board (get_canvas_board) for that id before retrying, and do not create again blindly.", id), ""))
	}
	if !ack.OK() {
		res, _, _ := canvasAckFailure(ack, fallbackMessage)
		return nil, res
	}
	return ack.Entity(), nil
}

// ---------------------------------------------------------------------------
// create_canvas_table
// ---------------------------------------------------------------------------

type createCanvasTableInput struct {
	CanvasBoardID string     `json:"canvasBoardId"`
	PositionX     float64    `json:"positionX"`
	PositionY     float64    `json:"positionY"`
	Rows          int        `json:"rows" jsonschema:"At least 1; rows x columns at most 500"`
	Columns       int        `json:"columns" jsonschema:"At least 1; rows x columns at most 500"`
	Cells         [][]string `json:"cells,omitempty" jsonschema:"Row-major cells[row][column]; may be shorter than the grid; each at most 5000 characters"`
	HeaderRow     *bool      `json:"headerRow,omitempty" jsonschema:"Default true"`
	ColumnWidths  []float64  `json:"columnWidths,omitempty" jsonschema:"One per column, at least 40; default 120"`
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

func createFieldError(field, format string, args ...any) *mcperr.McpError {
	return mcperr.NewField(mcperr.ValidationError, fmt.Sprintf(format, args...), field)
}

// validateCreateInput checks everything about a create request that needs no
// derived data: position, grid size and the shape of the starting cells.
func validateCreateInput(in createCanvasTableInput) *mcperr.McpError {
	if e := checkCanvasCoord("positionX", in.PositionX); e != nil {
		return e
	}
	if e := checkCanvasCoord("positionY", in.PositionY); e != nil {
		return e
	}
	if in.Rows < 1 || in.Columns < 1 {
		return createFieldError("rows", "rows and columns must each be at least 1.")
	}
	if in.Rows > tableMaxCells || in.Columns > tableMaxCells || in.Rows*in.Columns > tableMaxCells {
		return createFieldError("rows", "A table holds at most %d cells; %d rows x %d columns is too many.",
			tableMaxCells, in.Rows, in.Columns)
	}
	if len(in.Cells) > in.Rows {
		return createFieldError("cells", "cells has %d rows but the table has %d.", len(in.Cells), in.Rows)
	}
	for r, line := range in.Cells {
		if len(line) > in.Columns {
			return createFieldError(fmt.Sprintf("cells[%d]", r),
				"cells[%d] has %d entries but the table has %d columns.", r, len(line), in.Columns)
		}
		for c, text := range line {
			if utf8.RuneCountInString(text) > tableMaxTextLength {
				return createFieldError(fmt.Sprintf("cells[%d][%d]", r, c),
					"cells[%d][%d] must be at most %d characters.", r, c, tableMaxTextLength)
			}
		}
	}
	return nil
}

// columnWidthsOf returns the validated width of every column: the given
// columnWidths, or the default width for each.
func columnWidthsOf(in createCanvasTableInput) ([]float64, *mcperr.McpError) {
	if in.ColumnWidths != nil && len(in.ColumnWidths) != in.Columns {
		return nil, createFieldError("columnWidths", "columnWidths has %d entries but the table has %d columns.",
			len(in.ColumnWidths), in.Columns)
	}
	widths := make([]float64, in.Columns)
	for c := range widths {
		widths[c] = tableCellWidth
		if in.ColumnWidths == nil {
			continue
		}
		field := fmt.Sprintf("columnWidths[%d]", c)
		w := in.ColumnWidths[c]
		if e := checkFinite(field, w); e != nil {
			return nil, e
		}
		if w < tableMinColWidth {
			return nil, createFieldError(field, "%s must be at least %g.", field, tableMinColWidth)
		}
		widths[c] = w
	}
	return widths, nil
}

// buildRowsAndCells renders the rows, the cells map and the total height. A
// row is as tall as its tallest starting cell needs, and never below the default.
func buildRowsAndCells(
	in createCanvasTableInput, widths []float64, rowIDs, colIDs []string, headerRow bool,
) ([]map[string]any, map[string]any, float64) {
	style := tableStyle{FontSize: tableFontSize}
	rows := make([]map[string]any, in.Rows)
	cells := map[string]any{}
	total := 0.0
	for r := 0; r < in.Rows; r++ {
		height := tableCellHeight
		if r < len(in.Cells) {
			bold := style.Bold || (headerRow && r == 0)
			for c, text := range in.Cells[r] {
				if text == "" {
					continue
				}
				cells[rowIDs[r]+":"+colIDs[c]] = map[string]any{"text": text}
				height = math.Max(height, estimateRowHeight(text, widths[c], style.FontSize, bold))
			}
		}
		rows[r] = map[string]any{"id": rowIDs[r], "height": height}
		total += height
	}
	return rows, cells, total
}

// buildCanvasTablePayload validates a create request and renders the
// element:create payload plus the new row and column ids in order.
//
// It reads nothing that exists only after load: it runs BEFORE the write gate
// (commit 3640069: a pre-load path that dereferenced post-load data killed the
// server).
func buildCanvasTablePayload(in createCanvasTableInput) (map[string]any, []string, []string, *mcperr.McpError) {
	if e := validateCreateInput(in); e != nil {
		return nil, nil, nil, e
	}
	widths, e := columnWidthsOf(in)
	if e != nil {
		return nil, nil, nil, e
	}
	headerRow := in.HeaderRow == nil || *in.HeaderRow

	rowIDs := make([]string, in.Rows)
	colIDs := make([]string, in.Columns)
	for i := range rowIDs {
		rowIDs[i] = newTableID()
	}
	for i := range colIDs {
		colIDs[i] = newTableID()
	}
	rows, cells, totalHeight := buildRowsAndCells(in, widths, rowIDs, colIDs, headerRow)

	cols := make([]map[string]any, in.Columns)
	totalWidth := 0.0
	for c := range cols {
		cols[c] = map[string]any{"id": colIDs[c], "width": widths[c]}
		totalWidth += widths[c]
	}
	if totalWidth > canvasSizeMax || totalHeight > canvasSizeMax {
		return nil, nil, nil, createFieldError("columnWidths",
			"The table would be %gx%g; its width and height are each at most %d.", totalWidth, totalHeight, canvasSizeMax)
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

// createCanvasTable validates, gates, then emits element:create with a
// client-generated element id.
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
	entity, res := emitCreateWithID(ctx, fns, userID, in.CanvasBoardID, payload, "Server rejected canvas table creation.")
	if res != nil {
		return res, nil, nil
	}
	return success(map[string]any{
		"element":   entity,
		"rowIds":    rowIDs,
		"columnIds": colIDs,
	})
}

// RegisterCanvasTableTools registers create_canvas_table and edit_canvas_table.
func RegisterCanvasTableTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_canvas_table",
		Description: "Create a grid table on a canvas board: rows, columns (rows x columns at most 500), optional row-major cells and a header row. Returns rowIds and columnIds. Change it with edit_canvas_table. Guide: get_tool_guide.",
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

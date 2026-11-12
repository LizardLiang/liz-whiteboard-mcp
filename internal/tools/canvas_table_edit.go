// edit_canvas_table: an ordered list of changes to one canvas table.
//
// Rows, columns and cells are named by 0-based number. The MCP resolves a number
// to an id against the STORED table before EACH change, because an earlier
// change in the same list (insertRow, removeColumn) moves the numbers.
//
// The list stops at the first refused change. Continuing is unsafe: later row
// numbers may point at different rows. This differs on purpose from
// batch_schema_update, which attempts every item.
package tools

import (
	"context"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const editCanvasTableDescription = "Change one table on a canvas board (see create_canvas_table). " +
	"changes is an ordered list, 1 to 200 items, applied one after another; each item has a type: " +
	"setText {row, column, text}; insertRow {at} (0 to the row count; the row count appends); removeRow {row}; " +
	"insertColumn {at}; removeColumn {column}; resizeRow {row, height >= 24}; resizeColumn {column, width >= 40}; " +
	"setHeaderRow {on}; setFill {fromRow, fromColumn, toRow, toColumn, fill} (any CSS colour; null or an empty " +
	"string clears); setAlign {fromRow, fromColumn, toRow, toColumn, align: left|center|right}. " +
	"Rows and columns are numbered from 0 and renumber after each insert or remove. " +
	"setText grows the row to fit the text; it never shrinks one (use resizeRow). " +
	"The list stops at the first refused change and reports applied, failed and skipped; " +
	"earlier changes stay applied."

// tableChangeInput is one change. The fields each type reads are listed in
// editCanvasTableDescription; the rest are ignored.
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
	Fill       *string  `json:"fill,omitempty" jsonschema:"CSS colour; null or empty clears the fill (setFill)"`
	Align      *string  `json:"align,omitempty" jsonschema:"left, center, or right (setAlign)"`
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
// after the last applied change.
type editCanvasTableResult struct {
	Applied int                 `json:"applied"`
	Failed  *tableChangeFailure `json:"failed,omitempty"`
	Skipped int                 `json:"skipped"`
	Rows    int                 `json:"rows"`
	Columns int                 `json:"columns"`
}

var tableChangeTypes = []string{
	"setText", "insertRow", "removeRow", "insertColumn", "removeColumn",
	"resizeRow", "resizeColumn", "setHeaderRow", "setFill", "setAlign",
}

func changeField(i int, field string) string { return fmt.Sprintf("changes[%d].%s", i, field) }

func missingChangeField(i int, ch tableChangeInput, field string) *mcperr.McpError {
	return mcperr.NewField(mcperr.ValidationError,
		fmt.Sprintf("changes[%d] (%s) needs %s.", i, ch.Type, field), changeField(i, field))
}

// checkTableChangeShape validates one change without any stored data: the type,
// the fields the type needs, enums and bounds. Index ranges against the table
// are checked later, when the table is loaded.
func checkTableChangeShape(i int, ch tableChangeInput) *mcperr.McpError {
	known := false
	for _, t := range tableChangeTypes {
		if ch.Type == t {
			known = true
		}
	}
	if !known {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("changes[%d].type must be one of: %v.", i, tableChangeTypes), changeField(i, "type"))
	}
	for _, n := range []struct {
		field string
		v     *int
	}{
		{"row", ch.Row}, {"column", ch.Column}, {"at", ch.At},
		{"fromRow", ch.FromRow}, {"fromColumn", ch.FromColumn}, {"toRow", ch.ToRow}, {"toColumn", ch.ToColumn},
	} {
		if n.v != nil && *n.v < 0 {
			return mcperr.NewField(mcperr.ValidationError,
				fmt.Sprintf("%s must be 0 or more.", changeField(i, n.field)), changeField(i, n.field))
		}
	}
	needInt := func(field string, v *int) *mcperr.McpError {
		if v == nil {
			return missingChangeField(i, ch, field)
		}
		return nil
	}
	switch ch.Type {
	case "setText":
		if e := needInt("row", ch.Row); e != nil {
			return e
		}
		if e := needInt("column", ch.Column); e != nil {
			return e
		}
		if ch.Text == nil {
			return missingChangeField(i, ch, "text")
		}
		if utf8.RuneCountInString(*ch.Text) > tableMaxTextLength {
			return mcperr.NewField(mcperr.ValidationError,
				fmt.Sprintf("%s must be at most %d characters.", changeField(i, "text"), tableMaxTextLength),
				changeField(i, "text"))
		}
	case "insertRow", "insertColumn":
		return needInt("at", ch.At)
	case "removeRow":
		return needInt("row", ch.Row)
	case "removeColumn":
		return needInt("column", ch.Column)
	case "resizeRow":
		if e := needInt("row", ch.Row); e != nil {
			return e
		}
		if ch.Height == nil {
			return missingChangeField(i, ch, "height")
		}
		if e := checkFinite(changeField(i, "height"), *ch.Height); e != nil {
			return e
		}
		if *ch.Height < tableMinRowHeight || *ch.Height > canvasSizeMax {
			return mcperr.NewField(mcperr.ValidationError,
				fmt.Sprintf("%s must be between %g and %d.", changeField(i, "height"), tableMinRowHeight, canvasSizeMax),
				changeField(i, "height"))
		}
	case "resizeColumn":
		if e := needInt("column", ch.Column); e != nil {
			return e
		}
		if ch.Width == nil {
			return missingChangeField(i, ch, "width")
		}
		if e := checkFinite(changeField(i, "width"), *ch.Width); e != nil {
			return e
		}
		if *ch.Width < tableMinColWidth || *ch.Width > canvasSizeMax {
			return mcperr.NewField(mcperr.ValidationError,
				fmt.Sprintf("%s must be between %g and %d.", changeField(i, "width"), tableMinColWidth, canvasSizeMax),
				changeField(i, "width"))
		}
	case "setHeaderRow":
		if ch.On == nil {
			return missingChangeField(i, ch, "on")
		}
	case "setFill", "setAlign":
		for _, n := range []struct {
			field string
			v     *int
		}{{"fromRow", ch.FromRow}, {"fromColumn", ch.FromColumn}, {"toRow", ch.ToRow}, {"toColumn", ch.ToColumn}} {
			if e := needInt(n.field, n.v); e != nil {
				return e
			}
		}
		if *ch.FromRow > *ch.ToRow {
			return mcperr.NewField(mcperr.ValidationError,
				fmt.Sprintf("%s must not exceed %s.", changeField(i, "fromRow"), changeField(i, "toRow")),
				changeField(i, "fromRow"))
		}
		if *ch.FromColumn > *ch.ToColumn {
			return mcperr.NewField(mcperr.ValidationError,
				fmt.Sprintf("%s must not exceed %s.", changeField(i, "fromColumn"), changeField(i, "toColumn")),
				changeField(i, "fromColumn"))
		}
		if ch.Type == "setFill" {
			if ch.Fill != nil && *ch.Fill != "" {
				// cssColorSchema is z.string().min(1).max(64).
				return checkLen(changeField(i, "fill"), *ch.Fill, 1, 64)
			}
		} else {
			if ch.Align == nil {
				return missingChangeField(i, ch, "align")
			}
			return checkCanvasEnum(changeField(i, "align"), *ch.Align, "left", "center", "right")
		}
	}
	return nil
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

func rangeError(i int, field string, v, count int, noun string) *mcperr.McpError {
	return mcperr.NewField(mcperr.ValidationError,
		fmt.Sprintf("%s: %d is out of range, the table has %d %s.", changeField(i, field), v, count, noun),
		changeField(i, field))
}

// buildTableOp resolves one change's numbers against the stored table and
// returns the app's TableOp.
func buildTableOp(i int, ch tableChangeInput, st *storedTable) (map[string]any, *mcperr.McpError) {
	t := st.Table
	rowAt := func(field string, n int) (string, *mcperr.McpError) {
		if n >= len(t.Rows) {
			return "", rangeError(i, field, n, len(t.Rows), "rows")
		}
		return t.Rows[n].ID, nil
	}
	colAt := func(field string, n int) (string, *mcperr.McpError) {
		if n >= len(t.Cols) {
			return "", rangeError(i, field, n, len(t.Cols), "columns")
		}
		return t.Cols[n].ID, nil
	}

	switch ch.Type {
	case "setText":
		rowID, e := rowAt("row", *ch.Row)
		if e != nil {
			return nil, e
		}
		colID, e := colAt("column", *ch.Column)
		if e != nil {
			return nil, e
		}
		op := map[string]any{"type": "setCellText", "rowId": rowID, "colId": colID, "text": *ch.Text}
		// Grow only: send a height when the estimate beats the stored one, and
		// guard it with ifRowHeight so a concurrent browser edit wins.
		stored := t.Rows[*ch.Row].Height
		if want := estimateRowHeight(*ch.Text, t.Cols[*ch.Column].Width, st.FontSize); want > stored {
			op["rowHeight"] = math.Min(want, canvasSizeMax)
			op["ifRowHeight"] = stored
		}
		return op, nil
	case "insertRow":
		if *ch.At > len(t.Rows) {
			return nil, rangeError(i, "at", *ch.At, len(t.Rows), "rows (at may equal the row count to append)")
		}
		var after any
		if *ch.At > 0 {
			after = t.Rows[*ch.At-1].ID
		}
		return map[string]any{
			"type": "insertRow", "afterRowId": after,
			"row": map[string]any{"id": newTableID(), "height": tableCellHeight},
		}, nil
	case "insertColumn":
		if *ch.At > len(t.Cols) {
			return nil, rangeError(i, "at", *ch.At, len(t.Cols), "columns (at may equal the column count to append)")
		}
		var after any
		if *ch.At > 0 {
			after = t.Cols[*ch.At-1].ID
		}
		return map[string]any{
			"type": "insertCol", "afterColId": after,
			"col": map[string]any{"id": newTableID(), "width": tableCellWidth},
		}, nil
	case "removeRow":
		rowID, e := rowAt("row", *ch.Row)
		if e != nil {
			return nil, e
		}
		return map[string]any{"type": "removeRow", "rowId": rowID}, nil
	case "removeColumn":
		colID, e := colAt("column", *ch.Column)
		if e != nil {
			return nil, e
		}
		return map[string]any{"type": "removeCol", "colId": colID}, nil
	case "resizeRow":
		rowID, e := rowAt("row", *ch.Row)
		if e != nil {
			return nil, e
		}
		return map[string]any{"type": "resizeRow", "rowId": rowID, "height": *ch.Height}, nil
	case "resizeColumn":
		colID, e := colAt("column", *ch.Column)
		if e != nil {
			return nil, e
		}
		return map[string]any{"type": "resizeCol", "colId": colID, "width": *ch.Width}, nil
	case "setHeaderRow":
		return map[string]any{"type": "setHeaderRow", "on": *ch.On}, nil
	}

	// setFill and setAlign share the cell range.
	if _, e := rowAt("fromRow", *ch.FromRow); e != nil {
		return nil, e
	}
	if _, e := rowAt("toRow", *ch.ToRow); e != nil {
		return nil, e
	}
	if _, e := colAt("fromColumn", *ch.FromColumn); e != nil {
		return nil, e
	}
	if _, e := colAt("toColumn", *ch.ToColumn); e != nil {
		return nil, e
	}
	count := (*ch.ToRow - *ch.FromRow + 1) * (*ch.ToColumn - *ch.FromColumn + 1)
	if count > tableMaxCells {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("changes[%d]: the range holds %d cells, at most %d.", i, count, tableMaxCells),
			changeField(i, "toRow"))
	}
	refs := make([]map[string]any, 0, count)
	for r := *ch.FromRow; r <= *ch.ToRow; r++ {
		for c := *ch.FromColumn; c <= *ch.ToColumn; c++ {
			refs = append(refs, map[string]any{"rowId": t.Rows[r].ID, "colId": t.Cols[c].ID})
		}
	}
	if ch.Type == "setAlign" {
		return map[string]any{"type": "setCellAlign", "cells": refs, "align": *ch.Align}, nil
	}
	var fill any
	if ch.Fill != nil && *ch.Fill != "" {
		fill = *ch.Fill
	}
	return map[string]any{"type": "setCellFill", "cells": refs, "fill": fill}, nil
}

// applyCountDelta adjusts the running row and column counts after an applied change.
func applyCountDelta(ch tableChangeInput, rows, cols *int) {
	switch ch.Type {
	case "insertRow":
		*rows++
	case "removeRow":
		*rows--
	case "insertColumn":
		*cols++
	case "removeColumn":
		*cols--
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

	result := editCanvasTableResult{}
	refuse := func(i int, code, message string, cause error) (*mcp.CallToolResult, any, error) {
		if result.Applied == 0 {
			if cause != nil {
				return fail(cause)
			}
			return mcpError(mcperr.Code(code), message)
		}
		result.Failed = &tableChangeFailure{Index: i, Type: in.Changes[i].Type, Code: code, Message: message}
		result.Skipped = len(in.Changes) - i - 1
		return success(result)
	}

	for i, ch := range in.Changes {
		st, err := loadStoredTable(ctx, fns, in.CanvasBoardID, in.ElementID)
		if err != nil {
			code, message := mcperr.Code(mcperr.InternalError), err.Error()
			if me, ok := err.(*mcperr.McpError); ok {
				code = me.Code
			}
			return refuse(i, string(code), message, err)
		}
		result.Rows, result.Columns = len(st.Table.Rows), len(st.Table.Cols)

		op, e := buildTableOp(i, ch, st)
		if e != nil {
			return refuse(i, string(e.Code), e.Message, e)
		}
		ack, err := fns.emit(ctx, userID, in.CanvasBoardID, "element:table-op",
			map[string]any{"elementId": in.ElementID, "op": op})
		if err != nil {
			code, message := mcperr.Code(mcperr.ConnectionError), err.Error()
			if me, ok := err.(*mcperr.McpError); ok {
				code = me.Code
			}
			return refuse(i, string(code), message, err)
		}
		if !ack.OK() {
			code := mcperr.AckCodeToMcpCode(ack.Code())
			message := canvasAckMessage(ack.Code(), msgOr(ack.Message(), "Server rejected the table change."))
			return refuse(i, string(code), message, nil)
		}
		result.Applied++
		applyCountDelta(ch, &result.Rows, &result.Columns)
	}
	return success(result)
}

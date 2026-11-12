package summary

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

// canvasElem builds a CanvasElement with the fields the summary reads, plus the
// fields it must NOT print (id, style, props) filled with recognisable values.
func canvasElem(id, kind string, x, y, w, h float64, text *string) data.CanvasElement {
	return data.CanvasElement{
		ID:        id,
		BoardID:   "board-uuid-should-not-appear",
		Kind:      kind,
		PositionX: x,
		PositionY: y,
		Width:     w,
		Height:    h,
		Text:      text,
		Style:     data.JSONText(`{"fill":"#ff0000"}`),
		Props:     data.JSONText(`{"kind":"` + kind + `"}`),
	}
}

func canvasBoard(name string, elements ...data.CanvasElement) *data.CanvasBoardWithElements {
	return &data.CanvasBoardWithElements{
		CanvasBoard: data.CanvasBoard{
			ID:        "board-uuid-should-not-appear",
			Name:      name,
			ProjectID: "project-uuid-should-not-appear",
		},
		Elements: elements,
	}
}

// TestFormatCanvasSummary_CompactFormat pins the whole rendering: header, kind
// counts, and one line per element in the order given (paint order from the DB).
func TestFormatCanvasSummary_CompactFormat(t *testing.T) {
	board := canvasBoard("Product Flow",
		canvasElem("el-1", "rectangle", 100, 200, 180, 80, strptr("Users")),
		canvasElem("el-2", "rectangle", 400, 200, 180, 80, nil),
		canvasElem("el-3", "text", 10, 10.5, 200, 24, strptr("hello")),
	)

	got := FormatCanvasSummary(board, 3)

	want := strings.Join([]string{
		"CANVAS Product Flow",
		"ELEMENTS 3 (rectangle=2, text=1)",
		"",
		`rectangle @(100,200) 180x80 "Users"`,
		"rectangle @(400,200) 180x80",
		`text @(10,10.5) 200x24 "hello"`,
	}, "\n")
	assert.Equal(t, want, got)
}

// TestFormatCanvasSummary_EmptyBoard: an element-free board renders the header
// only, never a dangling blank line or an element section.
func TestFormatCanvasSummary_EmptyBoard(t *testing.T) {
	got := FormatCanvasSummary(canvasBoard("Empty"), 0)
	assert.Equal(t, "CANVAS Empty\nELEMENTS 0", got)
}

// TestFormatCanvasSummary_OmitsIdentifiersStyleAndProps: the summary is the
// compact read, so UUIDs, style, and props must never reach the output.
func TestFormatCanvasSummary_OmitsIdentifiersStyleAndProps(t *testing.T) {
	got := FormatCanvasSummary(canvasBoard("Board",
		canvasElem("el-1", "ellipse", 0, 0, 10, 10, strptr("x"))), 1)

	assert.NotContains(t, got, "el-1")
	assert.NotContains(t, got, "board-uuid-should-not-appear")
	assert.NotContains(t, got, "project-uuid-should-not-appear")
	assert.NotContains(t, got, "#ff0000")
	assert.NotContains(t, got, `"kind"`)
}

// TestFormatCanvasSummary_TruncatesLongText: a long text element must not blow
// the line budget the summary exists to protect.
func TestFormatCanvasSummary_TruncatesLongText(t *testing.T) {
	long := strings.Repeat("a", canvasSummaryTextLimit+50)
	got := FormatCanvasSummary(canvasBoard("Board",
		canvasElem("el-1", "text", 0, 0, 10, 10, &long)), 1)

	assert.Contains(t, got, strings.Repeat("a", canvasSummaryTextLimit)+"…")
	assert.NotContains(t, got, strings.Repeat("a", canvasSummaryTextLimit+1))
}

// TestFormatCanvasSummary_NormalizesNewlinesInText: one element is one line, so
// embedded newlines and tabs collapse to single spaces.
func TestFormatCanvasSummary_NormalizesNewlinesInText(t *testing.T) {
	multiline := "first\nsecond\r\nthird\tfourth"
	got := FormatCanvasSummary(canvasBoard("Board",
		canvasElem("el-1", "text", 0, 0, 10, 10, &multiline)), 1)

	lines := strings.Split(got, "\n")
	require.Len(t, lines, 4, "header, counts, blank, one element line")
	assert.Equal(t, `text @(0,0) 10x10 "first second third fourth"`, lines[3])
}

// TestFormatCanvasSummary_ReportsTruncation: when the caller passes a total
// larger than the slice, the truncation is stated twice — in the header and in a
// trailing marker — so it can never be read as a complete board.
func TestFormatCanvasSummary_ReportsTruncation(t *testing.T) {
	got := FormatCanvasSummary(canvasBoard("Big",
		canvasElem("el-1", "rectangle", 0, 0, 10, 10, nil),
		canvasElem("el-2", "rectangle", 0, 0, 10, 10, nil)), 10000)

	assert.Contains(t, got, "ELEMENTS 2 of 10000 (rectangle=2)")
	assert.Contains(t, got, "TRUNCATED 2 of 10000 elements shown, in paint order from the bottom.")
}

// TestFormatCanvasSummary_KindCountsAreSorted: counts render in ascending kind
// order so the same board always produces the same text.
func TestFormatCanvasSummary_KindCountsAreSorted(t *testing.T) {
	got := FormatCanvasSummary(canvasBoard("Board",
		canvasElem("el-1", "triangle", 0, 0, 1, 1, nil),
		canvasElem("el-2", "ellipse", 0, 0, 1, 1, nil),
		canvasElem("el-3", "connector", 0, 0, 1, 1, nil),
		canvasElem("el-4", "ellipse", 0, 0, 1, 1, nil)), 4)

	assert.Contains(t, got, "ELEMENTS 4 (connector=1, ellipse=2, triangle=1)")
}

// TestFormatCanvasSummary_FormatsNumbersWithoutTrailingZeros: coordinates are
// float64 but whole numbers must print as integers, not "100.000000".
func TestFormatCanvasSummary_FormatsNumbersWithoutTrailingZeros(t *testing.T) {
	got := FormatCanvasSummary(canvasBoard("Board",
		canvasElem("el-1", "rectangle", -12.25, 0, 1024, 7.5, nil)), 1)

	assert.Contains(t, got, "rectangle @(-12.25,0) 1024x7.5")
}

func tableElem(rows, cols int, header bool, cells map[string]string) data.CanvasElement {
	type r struct {
		ID string `json:"id"`
	}
	props := map[string]any{"kind": "table"}
	rs := make([]r, rows)
	cs := make([]r, cols)
	for i := range rs {
		rs[i].ID = "r" + strconv.Itoa(i)
	}
	for i := range cs {
		cs[i].ID = "c" + strconv.Itoa(i)
	}
	cellMap := map[string]any{}
	for k, v := range cells {
		cellMap[k] = map[string]any{"text": v}
	}
	props["table"] = map[string]any{"rows": rs, "cols": cs, "cells": cellMap, "headerRow": header}
	raw, _ := json.Marshal(props)
	return data.CanvasElement{Kind: "table", PositionX: 5, PositionY: 6, Width: float64(120 * cols), Height: float64(40 * rows), Props: data.JSONText(raw)}
}

func TestFormatCanvasSummary_TablePrintsCellText(t *testing.T) {
	el := tableElem(2, 2, true, map[string]string{
		"r0:c0": "Name", "r0:c1": "Role", "r1:c0": "Ada\nLovelace", "r1:c1": strings.Repeat("x", 40),
	})
	got := FormatCanvasSummary(canvasBoard("B", el), 1)
	assert.Contains(t, got, "table @(5,6) 240x80 2×2 header")
	assert.Contains(t, got, "\n  Name | Role\n")
	assert.Contains(t, got, "  Ada Lovelace | "+strings.Repeat("x", 30)+"…")
}

func TestFormatCanvasSummary_TableCapsRows(t *testing.T) {
	got := FormatCanvasSummary(canvasBoard("B", tableElem(25, 1, false, nil)), 1)
	assert.NotContains(t, got, "header")
	assert.Contains(t, got, "… 15 more rows")
}

func TestFormatCanvasSummary_UnreadableTableFallsBackToOneLine(t *testing.T) {
	el := data.CanvasElement{Kind: "table", Width: 1, Height: 1, Props: data.JSONText(`not json`)}
	got := FormatCanvasSummary(canvasBoard("B", el), 1)
	assert.Contains(t, got, "table @(0,0) 1x1")
}

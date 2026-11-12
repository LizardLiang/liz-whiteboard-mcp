// The row-height estimate for canvas table cells.
//
// Only the browser can measure text. The MCP is a second writer, so it
// ESTIMATES the height a cell needs and sends it in the op. The estimate mirrors
// these branches of the app's layout (text-layout.ts layoutText / wrapParagraph,
// table.ts cellTextElement), and each has a test in canvas_table_test.go:
//
//   - greedy wrap: break at the last space on the line, else mid-word; a space
//     broken on is consumed; a script without spaces breaks per character
//   - bold: row 0 of a table with headerRow is drawn bold, and a table whose
//     style.fontWeight is bold is bold throughout
//   - todo marker: a paragraph starting with a checkbox takes a fixed 1.25 em
//     marker advance, and its wrapped lines hang-indent by the same amount
//   - padding: TEXT_PADDING on each side, line height 1.4
//
// NOT mirrored: per-run fonts (textRuns; the MCP writes none), italic, and a
// resolved font wider than DejaVu Sans. The widths below are DejaVu Sans, the
// widest of the fonts the app's FONT_FAMILY (render-target.ts:102) usually
// resolves to (system-ui, Segoe UI, Roboto, Noto Sans), so on most machines the
// estimate is at or above the browser's. A machine that resolves to a wider
// font can still spill a line; resizeRow (edit_canvas_table) fixes that by hand.
package tools

import (
	"math"
	"strings"
	"unicode"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

const (
	// tableTextPadding mirrors TEXT_PADDING (render.ts:276): per side.
	tableTextPadding = 8.0
	// tableLineHeight mirrors the line-height factor in text-layout.ts:42.
	tableLineHeight = 1.4
	// tableFontSize mirrors TABLE_DEFAULT_STYLE.fontSize (table.ts:60-65).
	tableFontSize = 14.0
	// tableWideEm is the width in em of a CJK, Hangul, kana, full-width or symbol glyph.
	tableWideEm = 1.0
	// otherAdvanceEm is the width in em of any other non-ASCII glyph. It sits
	// above the widest advances measured in the font stack: Cyrillic Ж/Ш/Щ/Ю
	// reach 1.33 em bold, and an em dash, an ellipsis and a per-mille sign
	// reach 1.34 em. Accented Latin is over-estimated, which only makes a row
	// taller than it needs to be.
	otherAdvanceEm = 1.35
	// tableTodoAdvanceEm mirrors todoAdvance: TODO_BOX_RATIO (0.9) +
	// TODO_GAP_RATIO (0.35), text-layout.ts:141-148.
	tableTodoAdvanceEm = 1.25
	// tableTodoOpen and tableTodoDone mirror TODO_OPEN and TODO_DONE (text-todo.ts).
	tableTodoOpen = '☐'
	tableTodoDone = '☑'
)

// regularAdvanceEm and boldAdvanceEm hold the advance of each printable ASCII
// glyph (0x20 to 0x7E) in em, from DejaVu Sans and DejaVu Sans Bold.
var regularAdvanceEm = [95]float64{
	0.318, 0.401, 0.460, 0.838, 0.636, 0.950, 0.780, 0.275, 0.390, 0.390, // space ! " # $ % & ' ( )
	0.500, 0.838, 0.318, 0.361, 0.318, 0.337, // * + , - . /
	0.636, 0.636, 0.636, 0.636, 0.636, 0.636, 0.636, 0.636, 0.636, 0.636, // 0-9
	0.337, 0.337, 0.838, 0.838, 0.838, 0.531, 1.000, // : ; < = > ? @
	0.684, 0.686, 0.698, 0.770, 0.632, 0.575, 0.775, 0.752, 0.295, 0.295, // A-J
	0.656, 0.557, 0.863, 0.748, 0.787, 0.603, 0.787, 0.695, 0.635, 0.611, // K-T
	0.732, 0.684, 0.989, 0.685, 0.611, 0.685, // U-Z
	0.390, 0.337, 0.390, 0.838, 0.500, 0.500, // [ \ ] ^ _ `
	0.613, 0.635, 0.550, 0.635, 0.615, 0.352, 0.635, 0.634, 0.278, 0.278, // a-j
	0.579, 0.278, 0.974, 0.634, 0.612, 0.635, 0.635, 0.411, 0.521, 0.392, // k-t
	0.634, 0.592, 0.818, 0.592, 0.592, 0.525, // u-z
	0.636, 0.337, 0.636, 0.838, // { | } ~
}

var boldAdvanceEm = [95]float64{
	0.348, 0.456, 0.521, 0.838, 0.696, 1.002, 0.872, 0.313, 0.457, 0.457, // space ! " # $ % & ' ( )
	0.523, 0.838, 0.380, 0.415, 0.380, 0.365, // * + , - . /
	0.696, 0.696, 0.696, 0.696, 0.696, 0.696, 0.696, 0.696, 0.696, 0.696, // 0-9
	0.400, 0.400, 0.838, 0.838, 0.838, 0.580, 1.000, // : ; < = > ? @ (@ raised to the regular advance so bold is never narrower)
	0.774, 0.762, 0.734, 0.830, 0.683, 0.683, 0.821, 0.837, 0.372, 0.372, // A-J
	0.775, 0.637, 0.995, 0.837, 0.850, 0.733, 0.850, 0.770, 0.720, 0.682, // K-T
	0.812, 0.774, 1.103, 0.771, 0.724, 0.725, // U-Z
	0.457, 0.365, 0.457, 0.838, 0.500, 0.500, // [ \ ] ^ _ `
	0.675, 0.716, 0.593, 0.716, 0.678, 0.435, 0.716, 0.712, 0.343, 0.343, // a-j
	0.665, 0.343, 1.042, 0.712, 0.687, 0.716, 0.716, 0.493, 0.595, 0.478, // k-t
	0.712, 0.652, 0.924, 0.645, 0.652, 0.582, // u-z
	0.712, 0.365, 0.712, 0.838, // { | } ~
}

// tableStyle is the part of a table element's style that decides text width.
type tableStyle struct {
	FontSize float64
	Bold     bool
}

// isWideRune reports a glyph drawn about one em wide: CJK, kana, Hangul,
// Bopomofo, full-width and compatibility forms, symbols, dingbats and emoji.
func isWideRune(r rune) bool {
	switch {
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r),
		unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
		return true
	case r >= 0x2600 && r <= 0x27BF, // symbols and dingbats
		r >= 0x2FF0 && r <= 0x2FFF, // ideographic description characters
		r >= 0x3000 && r <= 0x303F, // CJK symbols and punctuation
		r >= 0x3100 && r <= 0x312F, // Bopomofo
		r >= 0xFE30 && r <= 0xFE4F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFFEF, // full-width and half-width forms
		r >= 0x1F300:               // emoji and pictographs
		return true
	}
	return false
}

// glyphWidth is the estimated advance of one rune at fontSize.
func glyphWidth(r rune, fontSize float64, bold bool) float64 {
	switch {
	case r >= 0x20 && r <= 0x7E:
		if bold {
			return boldAdvanceEm[r-0x20] * fontSize
		}
		return regularAdvanceEm[r-0x20] * fontSize
	case isWideRune(r):
		return tableWideEm * fontSize
	}
	return otherAdvanceEm * fontSize
}

// wrapLineCount counts the lines one newline-free paragraph wraps to, following
// wrapParagraph (text-layout.ts:397) and the todo-marker width rule of layoutText
// (text-layout.ts:191-198): a leading checkbox is measured as a fixed advance,
// and every later stretch gains that advance for the hanging indent.
func wrapLineCount(paragraph []rune, maxWidth, fontSize float64, bold bool) int {
	if len(paragraph) == 0 {
		return 1
	}
	// prefix[i] is the width of paragraph[:i], so a stretch's width is O(1).
	prefix := make([]float64, len(paragraph)+1)
	for i, r := range paragraph {
		prefix[i+1] = prefix[i] + glyphWidth(r, fontSize, bold)
	}
	todo := paragraph[0] == tableTodoOpen || paragraph[0] == tableTodoDone
	advance := tableTodoAdvanceEm * fontSize
	markerWidth := prefix[1]
	widthOf := func(from, to int) float64 {
		raw := prefix[to] - prefix[from]
		switch {
		case !todo:
			return raw
		case from == 0:
			return raw - markerWidth + advance
		}
		return raw + advance
	}

	if maxWidth <= 0 || widthOf(0, len(paragraph)) <= maxWidth {
		return 1
	}
	lines := 0
	lineStart, lastBreak := 0, -1 // lastBreak: index of the last space on the line
	for i := 0; i < len(paragraph); {
		if widthOf(lineStart, i+1) > maxWidth && i > lineStart {
			lines++
			if lastBreak > lineStart {
				lineStart = lastBreak + 1 // a space broken on is consumed
			} else {
				lineStart = i // a mid-word break is not
			}
			lastBreak = -1
			i = lineStart
			continue
		}
		if paragraph[i] == ' ' {
			lastBreak = i
		}
		i++
	}
	if lineStart < len(paragraph) {
		lines++
	}
	return lines
}

// estimateRowHeight estimates the row height a cell's text needs in a column
// colWidth wide, and rounds up. Empty text returns 0: it asks for no change.
func estimateRowHeight(text string, colWidth, fontSize float64, bold bool) float64 {
	if text == "" {
		return 0
	}
	inner := colWidth - 2*tableTextPadding
	if inner < 1 {
		// A column narrower than its padding has no text area. The app gives
		// maxWidth <= 0 one line; a 1 px floor instead keeps the estimate on the
		// tall side, breaking per character.
		inner = 1
	}
	lines := 0
	for _, p := range strings.Split(text, "\n") {
		lines += wrapLineCount([]rune(p), inner, fontSize, bold)
	}
	return math.Ceil(float64(lines)*fontSize*tableLineHeight + 2*tableTextPadding)
}

// cellIsBold reports whether the app draws the cell at (rowIndex) bold: the
// header row (table.ts cellTextElement) or a table styled bold.
func cellIsBold(t *data.CanvasTable, st tableStyle, rowIndex int) bool {
	return st.Bold || (t.HeaderRow && rowIndex == 0)
}

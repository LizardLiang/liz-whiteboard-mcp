package summary

import (
	"sort"
	"strconv"
	"strings"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

// canvasSummaryTextLimit is the number of runes of an element's text the summary
// prints before eliding. One element must stay one readable line: a canvas text
// element can legally hold 10,000 characters, which would defeat the purpose of
// a compact summary.
const canvasSummaryTextLimit = 80

// canvasTextReplacer collapses the whitespace that would break the one-line-per
// -element contract. Windows line endings are handled by the \r\n arm first.
var canvasTextReplacer = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

// formatCanvasNumber renders a coordinate or dimension with no trailing zeros,
// so a whole number prints as "100" and not "100.000000".
func formatCanvasNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// canvasSummaryText renders an element's text as a single quoted, length-capped
// segment. Returns "" when the element carries no text.
func canvasSummaryText(text *string) string {
	if text == nil || *text == "" {
		return ""
	}
	flat := strings.Join(strings.Fields(canvasTextReplacer.Replace(*text)), " ")
	if flat == "" {
		return ""
	}
	runes := []rune(flat)
	if len(runes) > canvasSummaryTextLimit {
		flat = string(runes[:canvasSummaryTextLimit]) + "…"
	}
	return ` "` + flat + `"`
}

// canvasKindCounts renders "kind=n" pairs in ascending kind order, so the same
// board always produces the same summary text.
func canvasKindCounts(elements []data.CanvasElement) string {
	counts := make(map[string]int, len(elements))
	for _, e := range elements {
		counts[e.Kind]++
	}
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, kind+"="+strconv.Itoa(counts[kind]))
	}
	return strings.Join(parts, ", ")
}

// FormatCanvasSummary formats a canvas board into a compact multi-line summary
// for the get_canvas_summary tool. It omits UUIDs, style, and props — this is
// the orientation read; get_canvas_board is the detail read.
//
// board.Elements must already be in paint order and may already be truncated;
// totalElements is the board's full element count. When the two differ, the
// truncation is stated in the header AND in a trailing marker, so the output can
// never be mistaken for a complete board.
//
// Format:
//
//	CANVAS <name>
//	ELEMENTS <n> (<kind>=<n>, ...)
//
//	<kind> @(<x>,<y>) <w>x<h> "<text>"
//	...
//	TRUNCATED <shown> of <total> elements shown, in paint order from the bottom.
func FormatCanvasSummary(board *data.CanvasBoardWithElements, totalElements int) string {
	shown := len(board.Elements)
	truncated := shown < totalElements

	countLine := "ELEMENTS " + strconv.Itoa(shown)
	if truncated {
		countLine += " of " + strconv.Itoa(totalElements)
	}
	if shown > 0 {
		countLine += " (" + canvasKindCounts(board.Elements) + ")"
	}

	lines := []string{"CANVAS " + board.Name, countLine}

	if shown > 0 {
		lines = append(lines, "")
		for _, e := range board.Elements {
			lines = append(lines,
				e.Kind+
					" @("+formatCanvasNumber(e.PositionX)+","+formatCanvasNumber(e.PositionY)+")"+
					" "+formatCanvasNumber(e.Width)+"x"+formatCanvasNumber(e.Height)+
					canvasSummaryText(e.Text))
		}
	}

	if truncated {
		lines = append(lines, "",
			"TRUNCATED "+strconv.Itoa(shown)+" of "+strconv.Itoa(totalElements)+
				" elements shown, in paint order from the bottom.")
	}

	return strings.Join(lines, "\n")
}

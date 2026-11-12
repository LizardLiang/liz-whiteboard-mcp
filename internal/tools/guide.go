package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolGuides holds the long how-to text that tool descriptions leave out. The
// key is the tool name. Descriptions that depend on a guide end with
// "Guide: get_tool_guide."
var toolGuides = map[string]string{
	"batch_schema_update": "batch_schema_update creates many things in one call.\n" +
		"- Order: tables first (each may carry inline columns), then addColumns (columns for existing tables), then addRelations.\n" +
		"- Non-atomic: every item is tried on its own. The result splits into created and failed; items that succeeded stay.\n" +
		"- Relations may name a table or column made in this same batch (sourceTableName, sourceColumnName, targetTableName, targetColumnName) instead of a UUID. Table names must be unique within the batch for this to resolve.\n" +
		"- Limits: at most 50 tables and 500 addColumns. At least one of tables, addColumns, addRelations must be non-empty.\n" +
		"- Use list_data_types and list_cardinalities for valid dataType and cardinality values.",

	"create_table_reference": "A reference node shows a table that lives on ANOTHER whiteboard of the same project.\n" +
		"1. list_whiteboards finds the source board.\n" +
		"2. get_board on the source board gives the table and column UUIDs. get_schema_summary omits UUIDs and cannot supply them.\n" +
		"3. create_table_reference names the source board, the table, and 1 to 100 columns to expose.\n" +
		"Each exposed column becomes a connectable handle that create_relationship can target. Without positionX and positionY the first browser client places the node. Needs the EDITOR role or higher.",

	"create_canvas_element": "Canvas boards are freeform FigJam-style boards. They are not ER whiteboards.\n" +
		"- kind is rectangle, ellipse, diamond, triangle, or text.\n" +
		"- The new element goes on top of the board automatically.\n" +
		"- style keys: fill, stroke, strokeWidth (0-64), fontSize (1-512), color, cornerRadius (0-512), textAlign (left, center, right), verticalAlign (top, middle, bottom). Omitted keys use the engine defaults.\n" +
		"- Use create_canvas_connector for lines, create_canvas_table for grids. For ER tables use create_table.",

	"update_canvas_element": "Supply canvasBoardId, elementId, and at least one field to change.\n" +
		"- zIndex sets paint order: a higher value draws on top.\n" +
		"- style is written whole: omitted style keys reset to the engine defaults. Send the full style you want.\n" +
		"- text: an empty string clears the label.\n" +
		"- For a table element, width and height are ignored. Use edit_canvas_table to change its grid.\n" +
		"- To change a connector use update_canvas_connector.",

	"create_canvas_connector": "A connector joins two ends. Each end is EITHER attached to an element (sourceElementId, targetElementId) OR pinned to a free board coordinate (sourcePoint, targetPoint). Supply exactly one form per end.\n" +
		"- A connector cannot join an element to itself.\n" +
		"- routing is straight, elbow, or curved. curvature (-2 to 2) adds an optional bow.\n" +
		"- The path is derived from the endpoints live, so the line follows the elements when they move.\n" +
		"- For ER foreign keys use create_relationship.",

	"create_canvas_table": "create_canvas_table makes a grid on a canvas board (not an ER table).\n" +
		"- rows x columns is at most 500 cells.\n" +
		"- cells is row-major: cells[row][column]. It may be shorter than the grid, never longer. Each text is at most 5000 characters.\n" +
		"- The first row is a bold header unless headerRow is false.\n" +
		"- columnWidths has one entry per column, at least 40 each; the default is 120.\n" +
		"- Rows grow to fit their text.\n" +
		"- The result has the element plus rowIds and columnIds in order. Change the table afterwards with edit_canvas_table.",

	"edit_canvas_table": "edit_canvas_table applies an ordered list of changes (1 to 200) to one table.\n" +
		"Change types and their fields (send only the fields the type uses):\n" +
		"- setText {row, column, text}\n" +
		"- insertRow {at}: at runs 0 to the row count; the row count appends\n" +
		"- removeRow {row}; insertColumn {at}; removeColumn {column}\n" +
		"- resizeRow {row, height}: height >= 24; resizeColumn {column, width}: width >= 40\n" +
		"- setHeaderRow {on}\n" +
		"- setFill {fromRow, fromColumn, toRow, toColumn, fill}: fill is required; any CSS colour; null or an empty string clears it\n" +
		"- setAlign {fromRow, fromColumn, toRow, toColumn, align}: align is left, center, or right\n" +
		"Rules:\n" +
		"- Text is at most 5000 characters. Rows and columns are numbered from 0 and renumber after each insert or remove.\n" +
		"- setText grows the row to fit the text, counting header text as bold. It never shrinks a row; use resizeRow.\n" +
		"- The list stops at the first refused change and reports applied, failed and skipped. Earlier changes stay applied.\n" +
		"- If the connection drops mid-change, the server re-reads the table. A change that landed counts as applied; otherwise it is failed. A failure with code CONNECTION_ERROR means the outcome is unknown: re-read the table before you retry.",
}

type toolGuideInput struct {
	Tool string `json:"tool,omitempty" jsonschema:"Tool name; omit to list the tools that have a guide"`
}

// RegisterGuideTools registers get_tool_guide.
func RegisterGuideTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_tool_guide",
		Description: "Get the long how-to for one tool (rules, limits, examples). Without a tool name, list the tools that have a guide. Read-only.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in toolGuideInput) (*mcp.CallToolResult, any, error) {
		name := strings.TrimSpace(in.Tool)
		names := make([]string, 0, len(toolGuides))
		for n := range toolGuides {
			names = append(names, n)
		}
		sort.Strings(names)
		if name == "" {
			return text("Tools with a guide: " + strings.Join(names, ", "))
		}
		guide, ok := toolGuides[name]
		if !ok {
			return validationError(fmt.Sprintf("No guide for %q. Tools with a guide: %s.", name, strings.Join(names, ", ")), "tool")
		}
		return text(guide)
	})
}

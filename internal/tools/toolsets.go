package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Toolset names a group of tools a client can ask for with the X-Liz-Toolsets
// request header. Core is always on.
type Toolset string

const (
	ToolsetCore   Toolset = "core"
	ToolsetER     Toolset = "er"
	ToolsetCanvas Toolset = "canvas"
	ToolsetManage Toolset = "manage"
)

// SelectableToolsets are the names a client may put in the header, in a stable order.
var SelectableToolsets = []Toolset{ToolsetER, ToolsetCanvas, ToolsetManage}

// ToolsetHeader is the request header that selects toolsets.
const ToolsetHeader = "X-Liz-Toolsets"

// toolToolset is the single source of truth for toolset membership. Every
// registered tool appears exactly once; TestEveryToolHasOneToolset fails for a
// tool that is missing here or a name here that no tool registers.
var toolToolset = map[string]Toolset{
	// core: discovery and help, always on.
	"list_projects":      ToolsetCore,
	"get_project_tree":   ToolsetCore,
	"list_folders":       ToolsetCore,
	"list_whiteboards":   ToolsetCore,
	"list_canvas_boards": ToolsetCore,
	"get_tool_guide":     ToolsetCore,

	// er: ER whiteboard reads and writes.
	"get_board":              ToolsetER,
	"get_schema_summary":     ToolsetER,
	"create_table":           ToolsetER,
	"update_table":           ToolsetER,
	"delete_table":           ToolsetER,
	"create_column":          ToolsetER,
	"update_column":          ToolsetER,
	"delete_column":          ToolsetER,
	"reorder_columns":        ToolsetER,
	"create_relationship":    ToolsetER,
	"update_relationship":    ToolsetER,
	"delete_relationship":    ToolsetER,
	"bulk_update_positions":  ToolsetER,
	"list_data_types":        ToolsetER,
	"list_cardinalities":     ToolsetER,
	"batch_schema_update":    ToolsetER,
	"get_table_ddl":          ToolsetER,
	"create_area":            ToolsetER,
	"add_table_to_area":      ToolsetER,
	"remove_table_from_area": ToolsetER,
	"move_area":              ToolsetER,
	"create_table_reference": ToolsetER,
	"update_table_reference": ToolsetER,
	"delete_table_reference": ToolsetER,
	"list_table_references":  ToolsetER,
	"get_board_image":        ToolsetER,

	// canvas: freeform canvas boards.
	"get_canvas_board":        ToolsetCanvas,
	"get_canvas_summary":      ToolsetCanvas,
	"create_canvas_element":   ToolsetCanvas,
	"update_canvas_element":   ToolsetCanvas,
	"delete_canvas_element":   ToolsetCanvas,
	"create_canvas_connector": ToolsetCanvas,
	"update_canvas_connector": ToolsetCanvas,
	"create_canvas_table":     ToolsetCanvas,
	"edit_canvas_table":       ToolsetCanvas,
	"get_canvas_board_image":  ToolsetCanvas,

	// manage: lifecycle of projects, folders and boards.
	"create_project":      ToolsetManage,
	"update_project":      ToolsetManage,
	"delete_project":      ToolsetManage,
	"create_folder":       ToolsetManage,
	"update_folder":       ToolsetManage,
	"delete_folder":       ToolsetManage,
	"create_whiteboard":   ToolsetManage,
	"update_whiteboard":   ToolsetManage,
	"delete_whiteboard":   ToolsetManage,
	"create_canvas_board": ToolsetManage,
	"update_canvas_board": ToolsetManage,
	"delete_canvas_board": ToolsetManage,
}

// RegisterAll registers every tool on s.
func RegisterAll(s *mcp.Server) {
	RegisterDiscoveryTools(s)
	RegisterReadTools(s)
	RegisterTableTools(s)
	RegisterColumnTools(s)
	RegisterRelationshipTools(s)
	RegisterPositionsTools(s)
	RegisterStaticTools(s)
	RegisterBatchTools(s)
	RegisterDDLTools(s)
	RegisterAreaTools(s)
	RegisterCanvasReadTools(s)
	RegisterCanvasWriteTools(s)
	RegisterCanvasConnectorTools(s)
	RegisterCanvasTableTools(s)
	RegisterCanvasBoardTools(s)
	RegisterProjectTools(s)
	RegisterFolderTools(s)
	RegisterWhiteboardTools(s)
	RegisterTableReferenceTools(s)
	RegisterBoardImageTools(s)
	RegisterGuideTools(s)
}

// ToolsetSet is a set of selectable toolsets. Core is implied.
type ToolsetSet map[Toolset]bool

// AllToolsets is the default set: every selectable toolset.
func AllToolsets() ToolsetSet {
	set := ToolsetSet{}
	for _, ts := range SelectableToolsets {
		set[ts] = true
	}
	return set
}

// sortedKeys returns the keys of m as sorted strings.
func sortedKeys[K ~string, V any](m map[K]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

// Key is the sorted, comma-joined set. It names the cached server.
func (s ToolsetSet) Key() string {
	return strings.Join(sortedKeys(s), ",")
}

// validToolsetNames lists the names the header accepts, for error text.
func validToolsetNames() string {
	names := []string{"all", string(ToolsetCore)}
	for _, ts := range SelectableToolsets {
		names = append(names, string(ts))
	}
	return strings.Join(names, ", ")
}

// ParseToolsets reads an X-Liz-Toolsets value: comma-separated, case and space
// insensitive, duplicates ignored. An empty value, or "all", selects every
// toolset. "core" is accepted and adds nothing (it is always on). Every name is
// checked, so "all,nope" fails. An unknown name returns an error that lists the
// valid names.
func ParseToolsets(header string) (ToolsetSet, error) {
	if strings.TrimSpace(strings.ReplaceAll(header, ",", "")) == "" {
		return AllToolsets(), nil
	}
	selectable := make(map[string]Toolset, len(SelectableToolsets))
	for _, ts := range SelectableToolsets {
		selectable[string(ts)] = ts
	}
	set := ToolsetSet{}
	sawAll := false
	for _, part := range strings.Split(header, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if ts, ok := selectable[name]; ok {
			set[ts] = true
			continue
		}
		switch name {
		case "", string(ToolsetCore):
		case "all":
			sawAll = true
		default:
			return nil, fmt.Errorf("unknown toolset %q in %s; valid toolsets: %s",
				name, ToolsetHeader, validToolsetNames())
		}
	}
	if sawAll {
		return AllToolsets(), nil
	}
	return set, nil
}

// NewServer builds a server that advertises core plus the selected toolsets.
func NewServer(impl *mcp.Implementation, set ToolsetSet) *mcp.Server {
	s := mcp.NewServer(impl, &mcp.ServerOptions{Instructions: Instructions})
	s.AddReceivingMiddleware(recoverPanics)
	RegisterAll(s)
	var drop []string
	for name, ts := range toolToolset {
		if ts != ToolsetCore && !set[ts] {
			drop = append(drop, name)
		}
	}
	s.RemoveTools(drop...)
	return s
}

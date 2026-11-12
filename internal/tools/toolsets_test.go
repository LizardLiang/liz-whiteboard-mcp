package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

// listTools returns the tools a session on a server for the set advertises.
func listTools(t *testing.T, set tools.ToolsetSet) []*mcp.Tool {
	t.Helper()
	ctx := context.Background()
	server := tools.NewServer(&mcp.Implementation{Name: "toolset-test", Version: "0"}, set)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "toolset-test-client", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientSession.Close() })
	res, err := clientSession.ListTools(ctx, nil)
	require.NoError(t, err)
	return res.Tools
}

func names(ts []*mcp.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, tl := range ts {
		out = append(out, tl.Name)
	}
	sort.Strings(out)
	return out
}

func mustParse(t *testing.T, header string) tools.ToolsetSet {
	t.Helper()
	set, err := tools.ParseToolsets(header)
	require.NoError(t, err)
	return set
}

// A tool missing from the toolset map, or a map entry no tool registers, fails here.
func TestEveryToolHasOneToolset(t *testing.T) {
	registered := names(listTools(t, tools.AllToolsets()))
	assert.Equal(t, tools.ToolCount, len(registered))
	mapped := tools.ToolsetNames()
	assert.Equal(t, registered, mapped,
		"every registered tool must be tagged in toolToolset exactly once, and no tag may name an absent tool")
}

func TestParseToolsets(t *testing.T) {
	assert.Equal(t, "canvas,er,manage", mustParse(t, "").Key())
	assert.Equal(t, "canvas,er,manage", mustParse(t, " , ").Key())
	assert.Equal(t, "canvas,er,manage", mustParse(t, "all").Key())
	assert.Equal(t, "canvas", mustParse(t, "canvas").Key())
	assert.Equal(t, "canvas,er", mustParse(t, " ER , Canvas, er").Key())
	assert.Equal(t, "", mustParse(t, "core").Key())
	assert.Equal(t, "canvas,er,manage", mustParse(t, "core, all").Key())

	for _, bad := range []string{"canvas,nope", "all,nope", "nope,all"} {
		_, err := tools.ParseToolsets(bad)
		require.Error(t, err, "header %q", bad)
		assert.Contains(t, err.Error(), `"nope"`)
		assert.Contains(t, err.Error(), "all, core, er, canvas, manage")
	}
}

func TestToolsetSelection(t *testing.T) {
	core := names(listTools(t, mustParse(t, "core")))
	assert.Contains(t, core, "get_tool_guide")
	assert.Contains(t, core, "list_projects")
	assert.NotContains(t, core, "create_table")

	canvas := names(listTools(t, mustParse(t, "canvas")))
	assert.Contains(t, canvas, "edit_canvas_table")
	assert.Contains(t, canvas, "get_tool_guide")
	assert.NotContains(t, canvas, "create_table")
	assert.NotContains(t, canvas, "create_project")

	assert.Len(t, listTools(t, mustParse(t, "")), tools.ToolCount)
}

func TestGuideTool(t *testing.T) {
	ctx := context.Background()
	server := tools.NewServer(&mcp.Implementation{Name: "g", Version: "0"}, mustParse(t, "core"))
	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	require.NoError(t, err)
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "gc", Version: "0"}, nil).Connect(ctx, b, nil)
	require.NoError(t, err)
	defer cs.Close()

	call := func(args map[string]any) *mcp.CallToolResult {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_tool_guide", Arguments: args})
		require.NoError(t, err)
		return res
	}
	body := func(r *mcp.CallToolResult) string { return r.Content[0].(*mcp.TextContent).Text }

	assert.Contains(t, body(call(map[string]any{"tool": "edit_canvas_table"})), "CONNECTION_ERROR")
	assert.Contains(t, body(call(map[string]any{})), "batch_schema_update")
	bad := call(map[string]any{"tool": "nope"})
	assert.True(t, bad.IsError)
	assert.Contains(t, body(bad), "Tools with a guide")

	for _, tool := range tools.GuideToolNames() {
		assert.Contains(t, tools.ToolsetNames(), tool, "a guide must belong to a registered tool")
	}
}

// Instructions and the README name toolsets and core tools by hand; this test
// fails when either drifts from SelectableToolsets and the core tag.
func TestDocsNameEveryToolsetAndCoreTool(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	require.NoError(t, err)
	for _, doc := range []struct{ name, text string }{{"Instructions", tools.Instructions}, {"README.md", string(readme)}} {
		for _, ts := range tools.SelectableToolsets {
			assert.Contains(t, doc.text, string(ts), "%s must name toolset %s", doc.name, ts)
		}
		for _, tool := range names(listTools(t, mustParse(t, "core"))) {
			assert.Contains(t, doc.text, tool, "%s must name core tool %s", doc.name, tool)
		}
	}
}

// create_area has no enum on color in its schema, so the tag must list every
// palette id, and the validation error must too.
func TestCreateAreaColorTagListsPalette(t *testing.T) {
	for _, tl := range listTools(t, mustParse(t, "er")) {
		if tl.Name != "create_area" {
			continue
		}
		raw, err := json.Marshal(tl.InputSchema)
		require.NoError(t, err)
		for _, id := range schema.AreaColors {
			assert.Contains(t, string(raw), id)
		}
		return
	}
	t.Fatal("create_area not listed")
}

func TestInstructionsFitClaudeCodeLimit(t *testing.T) {
	assert.Less(t, len(tools.Instructions), 4096)
}

// TestToolsListBudgets measures the tools/list payload per toolset combination
// and pins each against its budget. Before slimming (bcf4e70) all = 45,023 chars.
func TestToolsListBudgets(t *testing.T) {
	cases := []struct {
		header string
		budget int
	}{
		{"all", 32000},
		{"core", 2000},
		{"er", 15000},
		{"canvas", 14000},
		{"manage", 8000},
	}
	for _, c := range cases {
		list := listTools(t, mustParse(t, c.header))
		raw, err := json.Marshal(list)
		require.NoError(t, err)
		t.Logf("toolsets=%-7s tools=%2d chars=%6d budget=%6d", c.header, len(list), len(raw), c.budget)
		assert.LessOrEqual(t, len(raw), c.budget, "tools/list for %q is over budget", c.header)
	}
}

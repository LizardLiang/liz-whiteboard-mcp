package tools_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

// TestToolCountMatchesDocumentation pins the size of the MCP tool surface to the
// single source of truth, tools.ToolCount, at both ends:
//
//  1. the REGISTERED end — every Register* function cmd/mcp/main.go calls is
//     called here against a real mcp.Server, and the tool list the server
//     advertises over a live client session must be exactly ToolCount long;
//  2. the DOCUMENTED end — README.md's tool-surface heading and its table-of-
//     contents anchor must both name ToolCount.
//
// Why this test exists: the count was hand-maintained in three places
// (cmd/mcp/main.go's comment, this package's doc comment, and README.md) with
// nothing asserting it, so it went stale silently — the README claimed 23 tools
// while 31 were registered. The two comments no longer carry a number; they
// name ToolCount instead. README.md still must, because a reader cannot follow
// a Go constant, so this test checks it.
//
// When a wave adds tools: register them below, bump tools.ToolCount, and update
// README.md. Forgetting any one of the three fails here rather than shipping a
// lie. A new Register* function that is added to main.go but not to this test
// fails too — the registered count comes up short of the bumped constant.
func TestToolCountMatchesDocumentation(t *testing.T) {
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "count-test", Version: "0"}, nil)

	// Mirrors the registration block in cmd/mcp/main.go.
	tools.RegisterDiscoveryTools(server)
	tools.RegisterReadTools(server)
	tools.RegisterTableTools(server)
	tools.RegisterColumnTools(server)
	tools.RegisterRelationshipTools(server)
	tools.RegisterPositionsTools(server)
	tools.RegisterStaticTools(server)
	tools.RegisterBatchTools(server)
	tools.RegisterDDLTools(server)
	tools.RegisterAreaTools(server)
	tools.RegisterCanvasReadTools(server)
	tools.RegisterCanvasWriteTools(server)
	tools.RegisterCanvasConnectorTools(server)

	// Ask the server what it advertises, over a real session. mcp.Server keeps
	// its tool list unexported, and the advertised list is what an agent
	// actually sees. Default page size is 1000, well above the tool count, so a
	// single ListTools call is the whole surface.
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "count-test-client", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, res.NextCursor, "tool list paginated; the assertion below would undercount")

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	require.Len(t, names, tools.ToolCount,
		"registered tool count drifted from tools.ToolCount; registered: %s", strings.Join(names, ", "))

	readme, err := os.ReadFile("../../README.md")
	require.NoError(t, err)
	doc := string(readme)

	// Checked with strings.Contains rather than require.Contains so a failure
	// prints the missing line, not the whole README.
	for _, want := range []string{
		fmt.Sprintf("## The %d MCP tools", tools.ToolCount),
		fmt.Sprintf("(#the-%d-mcp-tools)", tools.ToolCount),
	} {
		require.Truef(t, strings.Contains(doc, want),
			"README.md is missing %q; it must document tools.ToolCount = %d", want, tools.ToolCount)
	}
}

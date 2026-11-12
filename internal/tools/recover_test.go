package tools_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

// A tool handler that panics must answer with the masked INTERNAL_ERROR, log
// the panic value and stack, and leave the session serving the next call.
func TestToolPanicReturnsInternalErrorAndSessionKeepsServing(t *testing.T) {
	var log bytes.Buffer
	defer tools.SetPanicLog(&log)()

	ctx := context.Background()
	server := tools.NewServer(&mcp.Implementation{Name: "p", Version: "0"}, tools.AllToolsets())
	mcp.AddTool(server, &mcp.Tool{Name: "panic_probe", Description: "panics"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			var m map[string]int
			m["boom"] = 1 // assignment to a nil map panics
			return nil, nil, nil
		})

	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	require.NoError(t, err)
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "pc", Version: "0"}, nil).Connect(ctx, b, nil)
	require.NoError(t, err)
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "panic_probe", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, res.IsError)
	body := res.Content[0].(*mcp.TextContent).Text
	assert.JSONEq(t, `{"code":"INTERNAL_ERROR","message":"An internal error occurred."}`, body)
	assert.NotContains(t, body, "nil map", "the panic value must not reach the caller")

	assert.Contains(t, log.String(), "panic_probe")
	assert.Contains(t, log.String(), "assignment to entry in nil map")
	assert.Contains(t, log.String(), "goroutine", "the stack must reach the log")

	next, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_cardinalities", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.False(t, next.IsError)
	assert.NotEmpty(t, next.Content)
}

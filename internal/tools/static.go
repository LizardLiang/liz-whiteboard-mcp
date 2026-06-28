package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
)

// RegisterStaticTools registers list_data_types and list_cardinalities.
// These return the live enum values and perform no auth/session checks,
// matching src/mcp/tools/static.ts.
func RegisterStaticTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_data_types",
		Description: "List all valid column data types supported by liz-whiteboard.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		return success(schema.DataTypes)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_cardinalities",
		Description: "List all valid relationship cardinalities supported by liz-whiteboard.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		return success(schema.Cardinalities)
	})
}

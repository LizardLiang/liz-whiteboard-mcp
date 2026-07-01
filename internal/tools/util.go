// Package tools registers the 19 MCP tools and their handlers. Each handler
// performs project-access scoping and the Socket.IO write path.
// Per-request identity comes from auth.UserID(ctx) (the bearer token validated
// by the middleware), replacing the former process-global LIZ_SESSION_TOKEN model.
package tools

import (
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// emptyInput is the input type for tools that take no arguments.
type emptyInput struct{}

// toCallResult converts an internal McpToolResult into the SDK's CallToolResult.
func toCallResult(r mcperr.McpToolResult) *mcp.CallToolResult {
	content := make([]mcp.Content, 0, len(r.Content))
	for _, c := range r.Content {
		content = append(content, &mcp.TextContent{Text: c.Text})
	}
	return &mcp.CallToolResult{IsError: r.IsError, Content: content}
}

// success wraps data as a JSON success CallToolResult.
func success(data any) (*mcp.CallToolResult, any, error) {
	return toCallResult(mcperr.MakeMcpSuccess(data)), nil, nil
}

// text wraps a raw string as a success CallToolResult.
func text(s string) (*mcp.CallToolResult, any, error) {
	return toCallResult(mcperr.MakeMcpText(s)), nil, nil
}

// fail converts an error into an error CallToolResult using the taxonomy mapping.
func fail(err error) (*mcp.CallToolResult, any, error) {
	return toCallResult(mcperr.ToMcpErrorResponse(err)), nil, nil
}

// validationError returns a VALIDATION_ERROR CallToolResult.
func validationError(message, field string) (*mcp.CallToolResult, any, error) {
	return toCallResult(mcperr.MakeMcpError(mcperr.ValidationError, message, field)), nil, nil
}

// mcpError returns a CallToolResult for a specific code/message.
func mcpError(code mcperr.Code, message string) (*mcp.CallToolResult, any, error) {
	return toCallResult(mcperr.MakeMcpError(code, message, "")), nil, nil
}

// validateUUID reports a VALIDATION_ERROR message if value is not a valid UUID.
func validateUUID(field, value string) *mcperr.McpError {
	if _, err := uuid.Parse(value); err != nil {
		return mcperr.NewField(mcperr.ValidationError, "Invalid uuid", field)
	}
	return nil
}

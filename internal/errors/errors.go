// Package errors defines the error taxonomy and MCP response mapping for the
// liz-whiteboard MCP server. All errors return
// { isError: true, content: [{ type: 'text', text: JSON.stringify(...) }] }.
// Tokens are redacted before output.
//
// Ported from src/mcp/errors.ts.
package errors

import (
	"encoding/json"
	"os"
	"strings"
)

// Code is a typed MCP error code.
type Code string

const (
	ValidationError Code = "VALIDATION_ERROR"
	NotFound        Code = "NOT_FOUND"
	Forbidden       Code = "FORBIDDEN"
	ConnectionError Code = "CONNECTION_ERROR"
	SessionExpired  Code = "SESSION_EXPIRED"
	InternalError   Code = "INTERNAL_ERROR"
)

// Well-known error messages (mirror ERROR_MESSAGES in errors.ts).
const (
	MsgConnectionError = "Cannot connect to liz-whiteboard collaboration server at localhost:3010. " +
		"Start the app with 'bun run dev' before using write tools."
	MsgSessionExpired = "Session token has expired. Update LIZ_SESSION_TOKEN with a fresh token from " +
		"the session_token cookie, then retry."
)

// McpError is a typed error carrying an MCP error code and optional field.
type McpError struct {
	Code    Code
	Message string
	Field   string
}

// Error implements the error interface.
func (e *McpError) Error() string {
	return e.Message
}

// New creates a new McpError.
func New(code Code, message string) *McpError {
	return &McpError{Code: code, Message: message}
}

// NewField creates a new McpError with a field.
func NewField(code Code, message, field string) *McpError {
	return &McpError{Code: code, Message: message, Field: field}
}

// McpContent is a single content entry in an MCP tool result.
type McpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// McpToolResult is the content-array shape expected by the MCP SDK.
// IsError is omitted when false so success responses match the TypeScript output.
type McpToolResult struct {
	IsError bool         `json:"isError,omitempty"`
	Content []McpContent `json:"content"`
}

// errorPayload is the JSON payload embedded in an error response's text field.
type errorPayload struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// redactToken replaces the LIZ_SESSION_TOKEN value with [REDACTED] anywhere it
// appears in text. No-op when the token is unset or shorter than 8 chars.
func redactToken(text string) string {
	token := os.Getenv("LIZ_SESSION_TOKEN")
	if len(token) < 8 {
		return text
	}
	return strings.ReplaceAll(text, token, "[REDACTED]")
}

// buildErrorResult serializes an error payload into an McpToolResult.
func buildErrorResult(code Code, message, field string) McpToolResult {
	payload := errorPayload{Code: code, Message: redactToken(message), Field: field}
	text, _ := json.Marshal(payload)
	return McpToolResult{
		IsError: true,
		Content: []McpContent{{Type: "text", Text: string(text)}},
	}
}

// ToMcpErrorResponse converts any error into an MCP error response object.
// Never surfaces raw driver errors or session tokens. Generic (non-McpError)
// errors emit "An internal error occurred.".
func ToMcpErrorResponse(err error) McpToolResult {
	if mcpErr, ok := err.(*McpError); ok {
		return buildErrorResult(mcpErr.Code, mcpErr.Message, mcpErr.Field)
	}
	// Generic error: mask the raw message; never propagate driver internals.
	return buildErrorResult(InternalError, "An internal error occurred.", "")
}

// MakeMcpError creates an MCP error response for a known code with a custom message.
func MakeMcpError(code Code, message, field string) McpToolResult {
	return buildErrorResult(code, message, field)
}

// MakeMcpSuccess creates a success MCP response with JSON-stringified data.
func MakeMcpSuccess(data any) McpToolResult {
	text, _ := json.Marshal(data)
	return McpToolResult{
		Content: []McpContent{{Type: "text", Text: string(text)}},
	}
}

// MakeMcpText creates a success MCP response with raw text (no JSON wrapping).
// Used by get_schema_summary, which returns a plain-text summary.
func MakeMcpText(text string) McpToolResult {
	return McpToolResult{
		Content: []McpContent{{Type: "text", Text: text}},
	}
}

// AckCodeToMcpCode maps a raw ack error code string to a typed Code.
// Used by all write-tool handlers that call socket.SocketEmitWithAck.
func AckCodeToMcpCode(code string) Code {
	switch code {
	case "FORBIDDEN":
		return Forbidden
	case "NOT_FOUND":
		return NotFound
	case "SESSION_EXPIRED":
		return SessionExpired
	default:
		return ValidationError
	}
}

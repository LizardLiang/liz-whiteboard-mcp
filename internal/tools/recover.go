package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// panicLog receives the panic value and stack of a recovered request.
var panicLog io.Writer = os.Stderr

// recoverPanics is receiving middleware that stops a panic in a request handler
// from reaching the SDK's request goroutine, where it would exit the process and
// drop every connected session. The go-sdk does not recover there itself.
//
// A panic in tools/call becomes the masked INTERNAL_ERROR tool result; a panic in
// any other method becomes a JSON-RPC error with the same masked message. The
// panic value and stack go to panicLog, never to the caller.
func recoverPanics(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			name := method
			if ctr, ok := req.(*mcp.CallToolRequest); ok && ctr.Params != nil {
				name = method + " " + ctr.Params.Name
			}
			fmt.Fprintf(panicLog, "[liz-whiteboard MCP] Recovered panic in %s: %v\n%s", name, v, debug.Stack())
			if method == "tools/call" {
				result, err = toCallResult(mcperr.MakeMcpError(mcperr.InternalError, internalErrorMessage, "")), nil
				return
			}
			result, err = nil, errors.New(internalErrorMessage)
		}()
		return next(ctx, method, req)
	}
}

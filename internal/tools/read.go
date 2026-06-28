package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/summary"
)

type whiteboardIDInput struct {
	WhiteboardID string `json:"whiteboardId" jsonschema:"The whiteboard UUID"`
}

// loadAuthorizedBoard runs the shared read-path: UUID validation,
// project resolution + access scoping, and board load. Returns the board or an
// error suitable for fail().
//
// Mid-session expiry (FR-021) is now handled by bearer token exp + 401 from the
// middleware — the former ensureSession / IsSessionTokenValid call is removed.
func loadAuthorizedBoard(ctx context.Context, whiteboardID string) (*data.WhiteboardWithDiagram, error) {
	if e := validateUUID("whiteboardId", whiteboardID); e != nil {
		return nil, e
	}
	userID := auth.UserID(ctx)
	projectID, err := data.GetWhiteboardProjectID(ctx, whiteboardID)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, mcperr.New(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", whiteboardID))
	}
	if err := auth.AssertProjectAccess(ctx, userID, projectID); err != nil {
		return nil, err
	}
	board, err := data.FindWhiteboardByIDWithDiagram(ctx, whiteboardID)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, mcperr.New(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", whiteboardID))
	}
	return board, nil
}

// RegisterReadTools registers get_board and get_schema_summary.
func RegisterReadTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_board",
		Description: "Get the full ER diagram state for a whiteboard (tables, columns, relationships).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in whiteboardIDInput) (*mcp.CallToolResult, any, error) {
		board, err := loadAuthorizedBoard(ctx, in.WhiteboardID)
		if err != nil {
			return fail(err)
		}
		return success(board)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_schema_summary",
		Description: "Get a compact text summary of the ER schema for a whiteboard. " +
			"Omits UUIDs and positions. Suitable for feeding into AI prompts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in whiteboardIDInput) (*mcp.CallToolResult, any, error) {
		board, err := loadAuthorizedBoard(ctx, in.WhiteboardID)
		if err != nil {
			return fail(err)
		}
		return text(summary.FormatSchemaSummary(board))
	})
}

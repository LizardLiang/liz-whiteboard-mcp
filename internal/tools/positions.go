package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

type positionEntry struct {
	ID        string  `json:"id" jsonschema:"Table UUID"`
	PositionX float64 `json:"positionX" jsonschema:"New X position"`
	PositionY float64 `json:"positionY" jsonschema:"New Y position"`
}

type bulkUpdatePositionsInput struct {
	WhiteboardID string          `json:"whiteboardId" jsonschema:"The whiteboard UUID"`
	Positions    []positionEntry `json:"positions" jsonschema:"Array of table position updates"`
}

type posUpdated struct {
	ID string `json:"id"`
}

type posFailed struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// emitFunc emits a Socket.IO event and returns the decoded ack. Abstracted so
// the bulk aggregation can be unit tested without a live collaboration server.
// It is pre-bound to the whiteboard and user for this request.
type emitFunc func(event string, payload any) (socket.AckResult, error)

// aggregatePositions emits one table:move per position and splits the results
// into updated/failed, mirroring the non-atomic loop in src/mcp/tools/positions.ts.
func aggregatePositions(positions []positionEntry, emit emitFunc) ([]posUpdated, []posFailed) {
	updated := make([]posUpdated, 0)
	failed := make([]posFailed, 0)

	for _, pos := range positions {
		ack, emitErr := emit("table:move", map[string]any{
			"tableId":   pos.ID,
			"positionX": pos.PositionX,
			"positionY": pos.PositionY,
		})
		if emitErr != nil {
			code := string(mcperr.InternalError)
			message := "Unknown error"
			if me, ok := emitErr.(*mcperr.McpError); ok {
				code = string(me.Code)
				message = me.Message
			} else {
				message = emitErr.Error()
			}
			failed = append(failed, posFailed{ID: pos.ID, Code: code, Message: message})
			continue
		}
		if !ack.OK() {
			code := ack.Code()
			if code == "" {
				code = string(mcperr.ValidationError)
			}
			failed = append(failed, posFailed{
				ID:      pos.ID,
				Code:    code,
				Message: msgOr(ack.Message(), "Server rejected position update."),
			})
			continue
		}
		updated = append(updated, posUpdated{ID: pos.ID})
	}
	return updated, failed
}

// RegisterPositionsTools registers bulk_update_positions. Implemented as a loop
// of individual table:move emits (non-atomic); partial results are reported via
// {updated, failed}. Mirrors src/mcp/tools/positions.ts.
func RegisterPositionsTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "bulk_update_positions",
		Description: "Update positions of multiple tables at once. " +
			"Non-atomic: failures on individual tables are reported in the \"failed\" array " +
			"without rolling back already-persisted updates.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in bulkUpdatePositionsInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
			return fail(e)
		}
		if len(in.Positions) < 1 || len(in.Positions) > 500 {
			return validationError("positions must contain between 1 and 500 entries.", "positions")
		}
		for _, p := range in.Positions {
			if e := validateUUID("positions.id", p.ID); e != nil {
				return fail(e)
			}
		}

		userID := auth.UserID(ctx)
		projectID, err := data.GetWhiteboardProjectID(ctx, in.WhiteboardID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", in.WhiteboardID))
		}
		if err := auth.AssertProjectAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		emit := func(event string, payload any) (socket.AckResult, error) {
			return socket.SocketEmitWithAck(ctx, in.WhiteboardID, userID, event, payload)
		}
		updated, failed := aggregatePositions(in.Positions, emit)
		return success(map[string]any{"updated": updated, "failed": failed})
	})
}

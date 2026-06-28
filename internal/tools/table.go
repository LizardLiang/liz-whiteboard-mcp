package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/positioning"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

type createTableInput struct {
	WhiteboardID string   `json:"whiteboardId" jsonschema:"The whiteboard UUID"`
	Name         string   `json:"name" jsonschema:"Table name"`
	Description  *string  `json:"description,omitempty" jsonschema:"Optional description"`
	PositionX    *float64 `json:"positionX,omitempty" jsonschema:"X position (auto-assigned if omitted)"`
	PositionY    *float64 `json:"positionY,omitempty" jsonschema:"Y position (auto-assigned if omitted)"`
	Width        *float64 `json:"width,omitempty" jsonschema:"Table width"`
	Height       *float64 `json:"height,omitempty" jsonschema:"Table height"`
}

type updateTableInput struct {
	TableID     string   `json:"tableId" jsonschema:"The table UUID"`
	Name        *string  `json:"name,omitempty" jsonschema:"New name"`
	Description *string  `json:"description,omitempty" jsonschema:"New description"`
	PositionX   *float64 `json:"positionX,omitempty" jsonschema:"New X position"`
	PositionY   *float64 `json:"positionY,omitempty" jsonschema:"New Y position"`
	Width       *float64 `json:"width,omitempty" jsonschema:"New width"`
	Height      *float64 `json:"height,omitempty" jsonschema:"New height"`
}

type tableIDInput struct {
	TableID string `json:"tableId" jsonschema:"The table UUID"`
}

// RegisterTableTools registers create_table, update_table, delete_table.
func RegisterTableTools(s *mcp.Server) {
	registerCreateTable(s)
	registerUpdateTable(s)
	registerDeleteTable(s)
}

func registerCreateTable(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_table",
		Description: "Create a new table in a whiteboard. If positionX/positionY are omitted, " +
			"the server assigns a non-overlapping grid position.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTableInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
			return fail(e)
		}
		if e := checkLen("name", in.Name, 1, 255); e != nil {
			return fail(e)
		}
		if in.PositionX != nil {
			if e := checkFinite("positionX", *in.PositionX); e != nil {
				return fail(e)
			}
		}
		if in.PositionY != nil {
			if e := checkFinite("positionY", *in.PositionY); e != nil {
				return fail(e)
			}
		}
		if in.Width != nil {
			if e := checkPositive("width", *in.Width); e != nil {
				return fail(e)
			}
		}
		if in.Height != nil {
			if e := checkPositive("height", *in.Height); e != nil {
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

		// Fill default position if either axis is omitted.
		var posX, posY float64
		if in.PositionX != nil {
			posX = *in.PositionX
		}
		if in.PositionY != nil {
			posY = *in.PositionY
		}
		if in.PositionX == nil || in.PositionY == nil {
			dx, dy, err := positioning.ComputeDefaultPosition(ctx, in.WhiteboardID, data.CountTablesByWhiteboardID)
			if err != nil {
				return fail(err)
			}
			if in.PositionX == nil {
				posX = dx
			}
			if in.PositionY == nil {
				posY = dy
			}
		}

		payload := map[string]any{
			"name":      in.Name,
			"positionX": posX,
			"positionY": posY,
		}
		if in.Description != nil {
			payload["description"] = *in.Description
		}
		if in.Width != nil {
			payload["width"] = *in.Width
		}
		if in.Height != nil {
			payload["height"] = *in.Height
		}

		ack, err := socket.SocketEmitWithAck(ctx, in.WhiteboardID, userID, "table:create", payload)
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected table creation."))
		}
		return success(ack.Entity())
	})
}

func registerUpdateTable(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "update_table",
		Description: "Update a table's name, description, size, and/or position. " +
			"Internally routes name/description/size → table:update and position → table:move. " +
			"Emits are sequential; a failure on the first aborts the second.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTableInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("tableId", in.TableID); e != nil {
			return fail(e)
		}
		metaChanged := in.Name != nil || in.Description != nil || in.Width != nil || in.Height != nil
		posChanged := in.PositionX != nil || in.PositionY != nil
		if !metaChanged && !posChanged {
			return validationError("update_table requires at least one field.", "")
		}
		// W3: value constraints mirroring the TypeScript Zod schema.
		if in.Name != nil {
			if e := checkLen("name", *in.Name, 1, 255); e != nil {
				return fail(e)
			}
		}
		if in.PositionX != nil {
			if e := checkFinite("positionX", *in.PositionX); e != nil {
				return fail(e)
			}
		}
		if in.PositionY != nil {
			if e := checkFinite("positionY", *in.PositionY); e != nil {
				return fail(e)
			}
		}
		if in.Width != nil {
			if e := checkPositive("width", *in.Width); e != nil {
				return fail(e)
			}
		}
		if in.Height != nil {
			if e := checkPositive("height", *in.Height); e != nil {
				return fail(e)
			}
		}

		userID := auth.UserID(ctx)
		projectID, err := data.GetTableProjectID(ctx, in.TableID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Table %s not found.", in.TableID))
		}
		if err := auth.AssertProjectAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		table, err := data.FindDiagramTableByID(ctx, in.TableID)
		if err != nil {
			return fail(err)
		}
		if table == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Table %s not found.", in.TableID))
		}
		whiteboardID := table.WhiteboardID

		var resultEntity any = table

		if metaChanged {
			metaPayload := map[string]any{"tableId": in.TableID}
			if in.Name != nil {
				metaPayload["name"] = *in.Name
			}
			if in.Description != nil {
				metaPayload["description"] = *in.Description
			}
			if in.Width != nil {
				metaPayload["width"] = *in.Width
			}
			if in.Height != nil {
				metaPayload["height"] = *in.Height
			}
			ackMeta, err := socket.SocketEmitWithAck(ctx, whiteboardID, userID, "table:update", metaPayload)
			if err != nil {
				return fail(err)
			}
			if !ackMeta.OK() {
				return mcpError(mcperr.AckCodeToMcpCode(ackMeta.Code()), msgOr(ackMeta.Message(), "Server rejected table update."))
			}
			if e := ackMeta.Entity(); e != nil {
				resultEntity = e
			}
		}

		if posChanged {
			finalX := table.PositionX
			finalY := table.PositionY
			if in.PositionX != nil {
				finalX = *in.PositionX
			}
			if in.PositionY != nil {
				finalY = *in.PositionY
			}
			movePayload := map[string]any{"tableId": in.TableID, "positionX": finalX, "positionY": finalY}
			ackMove, err := socket.SocketEmitWithAck(ctx, whiteboardID, userID, "table:move", movePayload)
			if err != nil {
				return fail(err)
			}
			if !ackMove.OK() {
				return mcpError(mcperr.AckCodeToMcpCode(ackMove.Code()), msgOr(ackMove.Message(), "Server rejected table move."))
			}
			// W4: only merge position when the move ack carries an entity
			// (mirrors table.ts:229 which only spreads when entity is present).
			if me, ok := ackMove.Entity().(map[string]any); ok {
				m := toMap(resultEntity)
				if v, exists := me["positionX"]; exists && v != nil {
					m["positionX"] = v
				} else {
					m["positionX"] = finalX
				}
				if v, exists := me["positionY"]; exists && v != nil {
					m["positionY"] = v
				} else {
					m["positionY"] = finalY
				}
				resultEntity = m
			}
		}

		return success(resultEntity)
	})
}

func registerDeleteTable(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_table",
		Description: "Delete a table and all its columns and relationships (cascade). " +
			"Returns the deleted table ID and cascade counts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in tableIDInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("tableId", in.TableID); e != nil {
			return fail(e)
		}
		userID := auth.UserID(ctx)
		projectID, err := data.GetTableProjectID(ctx, in.TableID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Table %s not found.", in.TableID))
		}
		if err := auth.AssertProjectAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		table, err := data.FindDiagramTableByID(ctx, in.TableID)
		if err != nil {
			return fail(err)
		}
		if table == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Table %s not found.", in.TableID))
		}

		ack, err := socket.SocketEmitWithAck(ctx, table.WhiteboardID, userID, "table:delete", map[string]any{"tableId": in.TableID})
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected table deletion."))
		}

		cascadeVal := ack["cascade"]
		var warnings []string
		if n := cascadeCount(cascadeVal, "columns"); n > 0 {
			warnings = append(warnings, fmt.Sprintf("%d column(s) deleted", n))
		}
		if n := cascadeCount(cascadeVal, "relationships"); n > 0 {
			warnings = append(warnings, fmt.Sprintf("%d relationship(s) deleted", n))
		}
		out := map[string]any{"id": in.TableID}
		if cascadeVal != nil {
			out["cascade"] = cascadeVal
		}
		if len(warnings) > 0 {
			out["warning"] = "Cascade deleted: " + strings.Join(warnings, ", ") + "."
		}
		return success(out)
	})
}

// msgOr returns msg if non-empty, otherwise fallback.
func msgOr(msg, fallback string) string {
	if msg == "" {
		return fallback
	}
	return msg
}

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

type createTableInput struct {
	WhiteboardID string   `json:"whiteboardId"`
	Name         string   `json:"name"`
	Description  *string  `json:"description,omitempty"`
	PositionX    *float64 `json:"positionX,omitempty"`
	PositionY    *float64 `json:"positionY,omitempty"`
	Width        *float64 `json:"width,omitempty"`
	Height       *float64 `json:"height,omitempty"`
}

type updateTableInput struct {
	TableID     string   `json:"tableId"`
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	PositionX   *float64 `json:"positionX,omitempty"`
	PositionY   *float64 `json:"positionY,omitempty"`
	Width       *float64 `json:"width,omitempty"`
	Height      *float64 `json:"height,omitempty"`
}

type tableIDInput struct {
	TableID string `json:"tableId"`
}

// RegisterTableTools registers create_table, update_table, delete_table.
func RegisterTableTools(s *mcp.Server) {
	registerCreateTable(s)
	registerUpdateTable(s)
	registerDeleteTable(s)
}

func registerCreateTable(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_table",
		Description: "Create a table in an ER whiteboard. Without positionX/positionY the client places it on first load.",
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
		if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		// Build the payload. Position keys are only included when explicitly
		// provided by the caller. When omitted, the server stores NULL and the
		// first browser client to load the whiteboard resolves the position via
		// React Flow's measured dimensions (client-side position resolution).
		payload := map[string]any{
			"name": in.Name,
		}
		if in.PositionX != nil {
			payload["positionX"] = *in.PositionX
		}
		if in.PositionY != nil {
			payload["positionY"] = *in.PositionY
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

		entity, e := createERRow(prodERWriteFns(ctx, userID), in.WhiteboardID, erTableKind, in.WhiteboardID, payload)
		if e != nil {
			return fail(e)
		}
		return success(entity)
	})
}

func registerUpdateTable(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_table",
		Description: "Update a table's name, description, size, or position. Not atomic: it applies the update, then the move; a move error can follow a saved rename.",
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
		if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
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
			ackMeta, err := emitTableUpdate(prodERWriteFns(ctx, userID), whiteboardID, metaPayload, in)
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
			// Merge partial position update with current DB values.
			// table.PositionX/Y are *float64 (nullable) after client-side position
			// resolution was introduced. Treat NULL as 0 when only one axis is
			// overridden (unusual, but keep behaviour deterministic).
			var finalX, finalY float64
			if table.PositionX != nil {
				finalX = *table.PositionX
			}
			if table.PositionY != nil {
				finalY = *table.PositionY
			}
			if in.PositionX != nil {
				finalX = *in.PositionX
			}
			if in.PositionY != nil {
				finalY = *in.PositionY
			}
			ackMove, err := emitTableMove(prodERWriteFns(ctx, userID), whiteboardID, in.TableID, finalX, finalY)
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
		Name:        "delete_table",
		Description: "Delete a table with its columns and relationships. Returns the deleted id and cascade counts.",
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
		if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		table, err := data.FindDiagramTableByID(ctx, in.TableID)
		if err != nil {
			return fail(err)
		}
		if table == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Table %s not found.", in.TableID))
		}

		ack, err := emitDelete(prodERWriteFns(ctx, userID), table.WhiteboardID, "table:delete", map[string]any{"tableId": in.TableID},
			"delete of table "+in.TableID, deletedCheck(prodERReaders(ctx).table, in.TableID))
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

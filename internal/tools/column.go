package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

type createColumnInput struct {
	TableID      string  `json:"tableId" jsonschema:"The table UUID"`
	Name         string  `json:"name" jsonschema:"Column name"`
	DataType     string  `json:"dataType" jsonschema:"Column data type"`
	IsPrimaryKey *bool   `json:"isPrimaryKey,omitempty" jsonschema:"Is primary key"`
	IsForeignKey *bool   `json:"isForeignKey,omitempty" jsonschema:"Is foreign key"`
	IsUnique     *bool   `json:"isUnique,omitempty" jsonschema:"Is unique"`
	IsNullable   *bool   `json:"isNullable,omitempty" jsonschema:"Is nullable"`
	Description  *string `json:"description,omitempty" jsonschema:"Optional description"`
	Order        *int    `json:"order,omitempty" jsonschema:"Column order position"`
}

type updateColumnInput struct {
	ColumnID     string  `json:"columnId" jsonschema:"The column UUID"`
	Name         *string `json:"name,omitempty" jsonschema:"New name"`
	DataType     *string `json:"dataType,omitempty" jsonschema:"New data type"`
	IsPrimaryKey *bool   `json:"isPrimaryKey,omitempty" jsonschema:"Is primary key"`
	IsForeignKey *bool   `json:"isForeignKey,omitempty" jsonschema:"Is foreign key"`
	IsUnique     *bool   `json:"isUnique,omitempty" jsonschema:"Is unique"`
	IsNullable   *bool   `json:"isNullable,omitempty" jsonschema:"Is nullable"`
	Description  *string `json:"description,omitempty" jsonschema:"New description"`
}

type columnIDInput struct {
	ColumnID string `json:"columnId" jsonschema:"The column UUID"`
}

type reorderColumnsInput struct {
	TableID          string   `json:"tableId" jsonschema:"The table UUID"`
	OrderedColumnIDs []string `json:"orderedColumnIds" jsonschema:"Column IDs in the desired order"`
}

// getWhiteboardIDForTable resolves the whiteboard ID for a table, returning a
// NOT_FOUND McpError if the table does not exist.
func getWhiteboardIDForTable(ctx context.Context, tableID string) (string, error) {
	table, err := data.FindDiagramTableByID(ctx, tableID)
	if err != nil {
		return "", err
	}
	if table == nil {
		return "", mcperr.New(mcperr.NotFound, fmt.Sprintf("Table %s not found.", tableID))
	}
	return table.WhiteboardID, nil
}

// RegisterColumnTools registers create_column, update_column, delete_column,
// reorder_columns.
func RegisterColumnTools(s *mcp.Server) {
	registerCreateColumn(s)
	registerUpdateColumn(s)
	registerDeleteColumn(s)
	registerReorderColumns(s)
}

func registerCreateColumn(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_column",
		Description: "Create a new column in a table.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createColumnInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("tableId", in.TableID); e != nil {
			return fail(e)
		}
		if e := checkLen("name", in.Name, 1, 255); e != nil {
			return fail(e)
		}
		if !schema.IsValidDataType(in.DataType) {
			return validationError("Invalid enum value.", "dataType")
		}
		// W6: order z.number().int().min(0)
		if in.Order != nil {
			if e := checkMinOrder("order", *in.Order); e != nil {
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

		whiteboardID, err := getWhiteboardIDForTable(ctx, in.TableID)
		if err != nil {
			return fail(err)
		}

		payload := map[string]any{"tableId": in.TableID, "name": in.Name, "dataType": in.DataType}
		if in.IsPrimaryKey != nil {
			payload["isPrimaryKey"] = *in.IsPrimaryKey
		}
		if in.IsForeignKey != nil {
			payload["isForeignKey"] = *in.IsForeignKey
		}
		if in.IsUnique != nil {
			payload["isUnique"] = *in.IsUnique
		}
		if in.IsNullable != nil {
			payload["isNullable"] = *in.IsNullable
		}
		if in.Description != nil {
			payload["description"] = *in.Description
		}
		if in.Order != nil {
			payload["order"] = *in.Order
		}

		ack, err := socket.SocketEmitWithAck(ctx, whiteboardID, userID, "column:create", payload)
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected column creation."))
		}
		return success(ack.Entity())
	})
}

func registerUpdateColumn(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_column",
		Description: "Update a column's properties.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateColumnInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("columnId", in.ColumnID); e != nil {
			return fail(e)
		}
		// W5: name z.string().min(1).max(255).optional()
		if in.Name != nil {
			if e := checkLen("name", *in.Name, 1, 255); e != nil {
				return fail(e)
			}
		}
		if in.DataType != nil && !schema.IsValidDataType(*in.DataType) {
			return validationError("Invalid enum value.", "dataType")
		}

		userID := auth.UserID(ctx)
		column, err := data.FindColumnByID(ctx, in.ColumnID)
		if err != nil {
			return fail(err)
		}
		if column == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Column %s not found.", in.ColumnID))
		}
		projectID, err := data.GetTableProjectID(ctx, column.TableID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Column %s not found.", in.ColumnID))
		}
		if err := auth.AssertProjectAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		whiteboardID, err := getWhiteboardIDForTable(ctx, column.TableID)
		if err != nil {
			return fail(err)
		}

		payload := map[string]any{"columnId": in.ColumnID}
		if in.Name != nil {
			payload["name"] = *in.Name
		}
		if in.DataType != nil {
			payload["dataType"] = *in.DataType
		}
		if in.IsPrimaryKey != nil {
			payload["isPrimaryKey"] = *in.IsPrimaryKey
		}
		if in.IsForeignKey != nil {
			payload["isForeignKey"] = *in.IsForeignKey
		}
		if in.IsUnique != nil {
			payload["isUnique"] = *in.IsUnique
		}
		if in.IsNullable != nil {
			payload["isNullable"] = *in.IsNullable
		}
		if in.Description != nil {
			payload["description"] = *in.Description
		}

		ack, err := socket.SocketEmitWithAck(ctx, whiteboardID, userID, "column:update", payload)
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected column update."))
		}
		return success(ack.Entity())
	})
}

func registerDeleteColumn(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_column",
		Description: "Delete a column. Returns the deleted column ID and cascade relationship count.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in columnIDInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("columnId", in.ColumnID); e != nil {
			return fail(e)
		}
		userID := auth.UserID(ctx)
		column, err := data.FindColumnByID(ctx, in.ColumnID)
		if err != nil {
			return fail(err)
		}
		if column == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Column %s not found.", in.ColumnID))
		}
		projectID, err := data.GetTableProjectID(ctx, column.TableID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Column %s not found.", in.ColumnID))
		}
		if err := auth.AssertProjectAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		whiteboardID, err := getWhiteboardIDForTable(ctx, column.TableID)
		if err != nil {
			return fail(err)
		}

		ack, err := socket.SocketEmitWithAck(ctx, whiteboardID, userID, "column:delete", map[string]any{"columnId": in.ColumnID})
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected column deletion."))
		}

		cascadeVal := ack["cascade"]
		var warnings []string
		if n := cascadeCount(cascadeVal, "relationships"); n > 0 {
			warnings = append(warnings, fmt.Sprintf("%d relationship(s) deleted", n))
		}
		out := map[string]any{"id": in.ColumnID}
		if cascadeVal != nil {
			out["cascade"] = cascadeVal
		}
		if len(warnings) > 0 {
			out["warning"] = "Cascade deleted: " + strings.Join(warnings, ", ") + "."
		}
		return success(out)
	})
}

func registerReorderColumns(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "reorder_columns",
		Description: "Reorder columns within a table by providing the desired column ID order.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in reorderColumnsInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("tableId", in.TableID); e != nil {
			return fail(e)
		}
		if len(in.OrderedColumnIDs) < 1 || len(in.OrderedColumnIDs) > 500 {
			return validationError("orderedColumnIds must contain between 1 and 500 entries.", "orderedColumnIds")
		}
		for _, id := range in.OrderedColumnIDs {
			if e := validateUUID("orderedColumnIds", id); e != nil {
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

		whiteboardID, err := getWhiteboardIDForTable(ctx, in.TableID)
		if err != nil {
			return fail(err)
		}

		ack, err := socket.SocketEmitWithAck(ctx, whiteboardID, userID, "column:reorder", map[string]any{
			"tableId":          in.TableID,
			"orderedColumnIds": in.OrderedColumnIDs,
		})
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected column reorder."))
		}
		if e := ack.Entity(); e != nil {
			return success(e)
		}
		return success(map[string]any{"tableId": in.TableID, "orderedColumnIds": in.OrderedColumnIDs})
	})
}

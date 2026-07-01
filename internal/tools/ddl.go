package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/ddl"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

type getTableDDLInput struct {
	TableID string  `json:"tableId" jsonschema:"The table UUID"`
	Dialect *string `json:"dialect,omitempty" jsonschema:"SQL dialect: \"postgres\" | \"mysql\" | \"mssql\" (default \"postgres\")"`
}

// RegisterDDLTools registers get_table_ddl.
func RegisterDDLTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_table_ddl",
		Description: "Get a CREATE TABLE SQL statement for a single table, including its columns and " +
			"outgoing foreign-key relationships. Supported dialects: postgres (default), mysql, mssql. Read-only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getTableDDLInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("tableId", in.TableID); e != nil {
			return fail(e)
		}
		dialect := ""
		if in.Dialect != nil {
			dialect = *in.Dialect
			if !ddl.IsValidDialect(dialect) {
				return validationError("Invalid enum value.", "dialect")
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

		board, err := data.FindWhiteboardByIDWithDiagram(ctx, table.WhiteboardID)
		if err != nil {
			return fail(err)
		}
		if board == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", table.WhiteboardID))
		}

		out, err := ddl.GenerateTableDDL(board, in.TableID, dialect)
		if err != nil {
			if strings.Contains(err.Error(), "has no columns") {
				return fail(mcperr.New(mcperr.InternalError, fmt.Sprintf("Table %s has no columns; cannot generate DDL.", in.TableID)))
			}
			return fail(mcperr.New(mcperr.NotFound, fmt.Sprintf("Table %s not found.", in.TableID)))
		}
		return text(out)
	})
}

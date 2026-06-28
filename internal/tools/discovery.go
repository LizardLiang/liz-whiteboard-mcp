package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

type projectOut struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type listWhiteboardsInput struct {
	ProjectID string `json:"projectId" jsonschema:"The project UUID"`
}

// RegisterDiscoveryTools registers list_projects and list_whiteboards.
func RegisterDiscoveryTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_projects",
		Description: "List all ER diagram projects accessible to the authenticated user.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		userID := auth.UserID(ctx)
		projects, err := auth.ListAccessibleProjects(ctx, userID)
		if err != nil {
			return fail(err)
		}
		out := make([]projectOut, 0, len(projects))
		for _, p := range projects {
			out = append(out, projectOut{ID: p.ID, Name: p.Name, Description: p.Description})
		}
		return success(out)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_whiteboards",
		Description: "List all whiteboards in a project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listWhiteboardsInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("projectId", in.ProjectID); e != nil {
			return fail(e)
		}
		userID := auth.UserID(ctx)
		if err := auth.AssertProjectAccess(ctx, userID, in.ProjectID); err != nil {
			return fail(err)
		}
		boards, err := data.ListWhiteboards(ctx, in.ProjectID)
		if err != nil {
			return fail(err)
		}
		return success(boards)
	})
}

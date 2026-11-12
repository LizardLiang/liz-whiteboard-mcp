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
	ProjectID string `json:"projectId"`
}

type projectScopedInput struct {
	ProjectID string `json:"projectId"`
}

// RegisterDiscoveryTools registers list_projects, list_whiteboards,
// list_folders and get_project_tree.
func RegisterDiscoveryTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_projects",
		Description: "List the projects you can access (id, name, description).",
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
		Description: "List the ER whiteboards in a project.",
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

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_folders",
		Description: "List a project's folders with parent and child counts (folders, ER whiteboards, canvas boards). Use it to find a folderId, and to read a folder's exact name for delete_folder.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectScopedInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("projectId", in.ProjectID); e != nil {
			return fail(e)
		}
		userID := auth.UserID(ctx)
		if err := auth.AssertProjectAccess(ctx, userID, in.ProjectID); err != nil {
			return fail(err)
		}
		folders, err := data.ListFolders(ctx, in.ProjectID)
		if err != nil {
			return fail(err)
		}
		return success(folders)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_project_tree",
		Description: "Get a project's folders and boards nested in one call; each board has kind whiteboard (ER) or canvasBoard (freeform). Prefer it over repeated list calls.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectScopedInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("projectId", in.ProjectID); e != nil {
			return fail(e)
		}
		userID := auth.UserID(ctx)
		if err := auth.AssertProjectAccess(ctx, userID, in.ProjectID); err != nil {
			return fail(err)
		}
		tree, err := data.GetProjectTree(ctx, in.ProjectID)
		if err != nil {
			return fail(err)
		}
		return success(tree)
	})
}

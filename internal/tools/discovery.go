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

type projectScopedInput struct {
	ProjectID string `json:"projectId" jsonschema:"The project UUID"`
}

// RegisterDiscoveryTools registers list_projects, list_whiteboards,
// list_folders and get_project_tree.
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

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_folders",
		Description: "List the folders in a project. Returns each folder's id, name, parent folder, " +
			"and how many child folders, ER whiteboards and canvas boards it holds. " +
			"Use it to find a folderId for the board tools, and to read a folder's exact name " +
			"before delete_folder, which requires it as confirmation.",
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
		Name: "get_project_tree",
		Description: "Get a project's whole navigable shape in one call: folders nested, each with " +
			"the boards filed in it, plus the boards at the project root. Every board carries a " +
			"kind of \"whiteboard\" (ER diagram) or \"canvasBoard\" (freeform), because the two " +
			"board kinds have separate tool sets. Prefer this over repeated list_ calls when " +
			"orienting in an unfamiliar project.",
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

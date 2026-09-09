// Project lifecycle tools: create_project, update_project, delete_project.
//
// These close the cold-start hole. Every other write tool in this server takes
// a projectId or whiteboardId it had no way to mint, so an agent could not
// begin work without a human clicking "new project" in the web app first.
//
// TRANSPORT. Like the canvas board lifecycle tools, these do NOT use the
// Socket.IO write path. There is no co-viewing client to broadcast a project
// rename to — the home page and navigator read the project list on load, not
// through a subscription. They call the app's JWT-authenticated
// POST /api/mcp-lifecycle route through internal/appapi instead.
//
// AUTHORIZATION. Three different gates, mirroring the app's own server
// functions exactly (src/routes/api/projects.ts):
//
//	create_project  any authenticated user — there is no project to check yet
//	update_project  ADMIN or higher
//	delete_project  the OWNER alone; ADMIN is not enough
//
// The app's route re-checks each one independently with requireServerFnRole.
// Both layers stay: the near-side check gives a clean error without a round
// trip, the route's check is the one that protects the data.
//
// DESTRUCTIVE GUARD. delete_project requires a confirmName that matches the
// project's real stored name. The delete cascades every folder, whiteboard,
// canvas board, table, column and relationship in the project — the single
// largest destructive surface this server exposes — and this project has
// already lost production data once to an unintended cascade.
package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// projectNameMaxLength mirrors createProjectSchema's z.string().min(1).max(255).
const projectNameMaxLength = 255

// projectDescriptionMaxLength mirrors z.string().max(1000).
const projectDescriptionMaxLength = 1000

type createProjectInput struct {
	Name        string  `json:"name" jsonschema:"Project name, 1 to 255 characters"`
	Description *string `json:"description,omitempty" jsonschema:"Optional description, up to 1000 characters"`
}

type updateProjectInput struct {
	ProjectID   string  `json:"projectId" jsonschema:"The project UUID"`
	Name        *string `json:"name,omitempty" jsonschema:"New project name, 1 to 255 characters"`
	Description *string `json:"description,omitempty" jsonschema:"New description, up to 1000 characters"`
}

type deleteProjectInput struct {
	ProjectID   string `json:"projectId" jsonschema:"The project UUID"`
	ConfirmName string `json:"confirmName" jsonschema:"The project's exact current name, required as confirmation; the delete is refused if it does not match"`
}

// projectLifecycleFns is the injectable pipeline, mirroring
// canvasBoardLifecycleFns so every guard is unit-testable without a database or
// a running app.
type projectLifecycleFns struct {
	assertAdmin   func(ctx context.Context, userID, projectID string) error
	assertOwner   func(ctx context.Context, userID, projectID string) error
	findProject   func(ctx context.Context, projectID string) (*data.Project, error)
	createProject func(ctx context.Context, userID string, req appapi.CreateProjectRequest) (map[string]any, error)
	updateProject func(ctx context.Context, userID, projectID string, req appapi.UpdateProjectRequest) (map[string]any, error)
	deleteProject func(ctx context.Context, userID, projectID string) (map[string]any, error)
}

// prodProjectLifecycleFns is the production pipeline.
// TestProdProjectLifecycleFns_WireTheDistinctRoleGates pins the two gates that
// would fail open if mis-wired to each other.
func prodProjectLifecycleFns() projectLifecycleFns {
	return projectLifecycleFns{
		assertAdmin:   auth.AssertProjectAdminAccess,
		assertOwner:   auth.AssertProjectOwnerAccess,
		findProject:   data.FindProjectByID,
		createProject: appapi.CreateProject,
		updateProject: appapi.UpdateProject,
		deleteProject: appapi.DeleteProject,
	}
}

// projectNotFound builds the single NOT_FOUND message used by every project
// miss, so a typo reads the same wherever it lands.
func projectNotFound(projectID string) *mcperr.McpError {
	return mcperr.New(mcperr.NotFound, fmt.Sprintf("Project %s not found.", projectID))
}

// validateProjectFields checks name and description bounds. Both are optional
// here; the callers decide which are required.
func validateProjectFields(name, description *string) *mcperr.McpError {
	if name != nil {
		if e := checkLen("name", *name, 1, projectNameMaxLength); e != nil {
			return e
		}
	}
	if description != nil {
		if e := checkLen("description", *description, 0, projectDescriptionMaxLength); e != nil {
			return e
		}
	}
	return nil
}

// createProjectWithFns validates and creates one project.
//
// There is no authorization step, and that is not an omission: a project does
// not exist yet, so there is nothing to hold a role on. Any caller whose bearer
// token the middleware accepted may create one, exactly as createProjectFn
// allows any authenticated session. The app re-derives the owner from the JWT
// subject, so a caller cannot create a project owned by someone else.
func createProjectWithFns(
	ctx context.Context,
	fns projectLifecycleFns,
	userID string,
	in createProjectInput,
) (map[string]any, error) {
	if e := checkLen("name", in.Name, 1, projectNameMaxLength); e != nil {
		return nil, e
	}
	if e := validateProjectFields(nil, in.Description); e != nil {
		return nil, e
	}
	return fns.createProject(ctx, userID, appapi.CreateProjectRequest{
		Name:        in.Name,
		Description: in.Description,
	})
}

// updateProjectWithFns validates, gates at ADMIN+, and renames or re-describes.
//
// An update naming no field is refused rather than sent: it would be a no-op
// round trip that still bumps updatedAt and reorders the project list.
func updateProjectWithFns(
	ctx context.Context,
	fns projectLifecycleFns,
	userID string,
	in updateProjectInput,
) (map[string]any, error) {
	if e := validateUUID("projectId", in.ProjectID); e != nil {
		return nil, e
	}
	if in.Name == nil && in.Description == nil {
		return nil, mcperr.New(mcperr.ValidationError,
			"Supply at least one of name or description to update.")
	}
	if e := validateProjectFields(in.Name, in.Description); e != nil {
		return nil, e
	}
	if err := fns.assertAdmin(ctx, userID, in.ProjectID); err != nil {
		return nil, err
	}
	return fns.updateProject(ctx, userID, in.ProjectID, appapi.UpdateProjectRequest{
		Name:        in.Name,
		Description: in.Description,
	})
}

// deleteProjectWithFns validates, gates at OWNER, checks confirmName, deletes.
//
// Order is load-bearing, and matches deleteCanvasBoardWithFns. The OWNER gate
// runs BEFORE the project is read, so a non-owner cannot use confirmName
// mismatches as a project-name oracle. The name comparison runs before the
// route call, so a mismatch destroys nothing.
func deleteProjectWithFns(
	ctx context.Context,
	fns projectLifecycleFns,
	userID string,
	in deleteProjectInput,
) (map[string]any, error) {
	if e := validateUUID("projectId", in.ProjectID); e != nil {
		return nil, e
	}
	if in.ConfirmName == "" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"confirmName is required: pass the project's exact current name to confirm this delete. "+
				"Read it with list_projects first.", "confirmName")
	}
	if err := fns.assertOwner(ctx, userID, in.ProjectID); err != nil {
		return nil, err
	}

	project, err := fns.findProject(ctx, in.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, projectNotFound(in.ProjectID)
	}

	// Exact comparison: no trimming, no case folding. Whitespace is exactly what
	// a careless paste gets wrong, and the stored name is not echoed back — a
	// caller that could blind-retry with the revealed value would defeat the
	// guard's real purpose, catching a wrong project id before it destroys the
	// wrong project.
	if project.Name != in.ConfirmName {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("confirmName does not match the current name of project %s. "+
				"Nothing was deleted. Read the project with list_projects and pass its exact name.",
				in.ProjectID), "confirmName")
	}

	return fns.deleteProject(ctx, userID, in.ProjectID)
}

// RegisterProjectTools registers create_project, update_project and
// delete_project.
func RegisterProjectTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_project",
		Description: "Create a new project owned by you. A project is the top-level container for " +
			"folders, ER diagram whiteboards and canvas boards. Returns the new project, whose id " +
			"every other tool takes. Use this first when starting from nothing — no role is needed, " +
			"because there is no project to hold a role on yet.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createProjectInput) (*mcp.CallToolResult, any, error) {
		project, err := createProjectWithFns(ctx, prodProjectLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(project)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_project",
		Description: "Rename a project or change its description. Supply projectId and at least one " +
			"of name or description. Requires the ADMIN role or higher — the EDITOR role that lets " +
			"you change a schema is not enough to rename the project holding it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateProjectInput) (*mcp.CallToolResult, any, error) {
		project, err := updateProjectWithFns(ctx, prodProjectLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(project)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_project",
		Description: "Permanently delete a project and EVERYTHING in it: every folder, ER whiteboard, " +
			"canvas board, table, column and relationship. This cannot be undone and is the most " +
			"destructive tool on this server. Only the project owner may call it. confirmName is " +
			"required and must equal the project's exact current name — read it with list_projects " +
			"first. A mismatch deletes nothing.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteProjectInput) (*mcp.CallToolResult, any, error) {
		project, err := deleteProjectWithFns(ctx, prodProjectLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(project)
	})
}

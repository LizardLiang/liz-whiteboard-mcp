// Folder lifecycle tools: create_folder, update_folder, delete_folder.
//
// Folders are how a project with more than a handful of boards stays
// navigable. Until this wave the server could neither create one nor even see
// one — `folderId` was a parameter with no discoverable values. list_folders
// and get_project_tree (internal/tools/discovery.go) supply the ids these tools
// consume.
//
// AUTHORIZATION. EDITOR or higher for all three operations, matching the app's
// folders.ts server functions. Unlike projects, folders have no ADMIN or OWNER
// tier: anyone who may edit a board may organise the boards.
//
// DESTRUCTIVE GUARD. delete_folder requires a confirmName, and this is the
// least obvious of the three deletes. Deleting a folder cascades its child
// folders AND every whiteboard filed inside them, because Whiteboard.folderId
// is ON DELETE CASCADE in the schema. An agent tidying up an empty-looking
// folder tree can destroy boards it never named. The mismatch error therefore
// reports the cascade size rather than only refusing.
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

// folderNameMaxLength mirrors createFolderSchema's z.string().min(1).max(255).
const folderNameMaxLength = 255

type createFolderInput struct {
	ProjectID      string  `json:"projectId" jsonschema:"The project UUID the folder belongs to"`
	Name           string  `json:"name" jsonschema:"Folder name, 1 to 255 characters"`
	ParentFolderID *string `json:"parentFolderId,omitempty" jsonschema:"Optional parent folder UUID; omit to place the folder at the project root"`
}

type updateFolderInput struct {
	FolderID string `json:"folderId" jsonschema:"The folder UUID"`
	Name     string `json:"name" jsonschema:"New folder name, 1 to 255 characters"`
}

type deleteFolderInput struct {
	FolderID    string `json:"folderId" jsonschema:"The folder UUID"`
	ConfirmName string `json:"confirmName" jsonschema:"The folder's exact current name, required as confirmation; the delete is refused if it does not match"`
}

// folderLifecycleFns is the injectable pipeline, mirroring
// canvasBoardLifecycleFns.
type folderLifecycleFns struct {
	resolveProject func(ctx context.Context, folderID string) (string, error)
	assertEdit     func(ctx context.Context, userID, projectID string) error
	findFolder     func(ctx context.Context, folderID string) (*data.Folder, error)
	listFolders    func(ctx context.Context, projectID string) ([]data.FolderSummary, error)
	createFolder   func(ctx context.Context, userID string, req appapi.CreateFolderRequest) (map[string]any, error)
	updateFolder   func(ctx context.Context, userID, folderID, name string) (map[string]any, error)
	deleteFolder   func(ctx context.Context, userID, folderID string) (map[string]any, error)
}

// prodFolderLifecycleFns is the production pipeline.
func prodFolderLifecycleFns() folderLifecycleFns {
	return folderLifecycleFns{
		resolveProject: data.GetFolderProjectID,
		assertEdit:     auth.AssertSchemaEditAccess,
		findFolder:     data.FindFolderByID,
		listFolders:    data.ListFolders,
		createFolder:   appapi.CreateFolder,
		updateFolder:   appapi.UpdateFolder,
		deleteFolder:   appapi.DeleteFolder,
	}
}

// folderNotFound builds the single NOT_FOUND message used by every folder miss.
func folderNotFound(folderID string) *mcperr.McpError {
	return mcperr.New(mcperr.NotFound, fmt.Sprintf("Folder %s not found.", folderID))
}

// authorizeExistingFolder runs the shared gate for update and delete: UUID
// validation, project resolution, EDITOR+ check.
func authorizeExistingFolder(ctx context.Context, fns folderLifecycleFns, userID, folderID string) (string, error) {
	if e := validateUUID("folderId", folderID); e != nil {
		return "", e
	}
	projectID, err := fns.resolveProject(ctx, folderID)
	if err != nil {
		return "", err
	}
	if projectID == "" {
		return "", folderNotFound(folderID)
	}
	return projectID, fns.assertEdit(ctx, userID, projectID)
}

// createFolderWithFns validates, gates, and creates one folder.
func createFolderWithFns(
	ctx context.Context,
	fns folderLifecycleFns,
	userID string,
	in createFolderInput,
) (map[string]any, error) {
	if e := validateUUID("projectId", in.ProjectID); e != nil {
		return nil, e
	}
	if e := checkLen("name", in.Name, 1, folderNameMaxLength); e != nil {
		return nil, e
	}
	if in.ParentFolderID != nil {
		if e := validateUUID("parentFolderId", *in.ParentFolderID); e != nil {
			return nil, e
		}
	}
	if err := fns.assertEdit(ctx, userID, in.ProjectID); err != nil {
		return nil, err
	}
	return fns.createFolder(ctx, userID, appapi.CreateFolderRequest{
		ProjectID:      in.ProjectID,
		Name:           in.Name,
		ParentFolderID: in.ParentFolderID,
	})
}

// updateFolderWithFns validates, gates, and renames one folder.
//
// Renaming is the only update the app offers — there is no re-parenting server
// function — so this tool does not invent one.
func updateFolderWithFns(
	ctx context.Context,
	fns folderLifecycleFns,
	userID string,
	in updateFolderInput,
) (map[string]any, error) {
	if e := checkLen("name", in.Name, 1, folderNameMaxLength); e != nil {
		return nil, e
	}
	if _, err := authorizeExistingFolder(ctx, fns, userID, in.FolderID); err != nil {
		return nil, err
	}
	return fns.updateFolder(ctx, userID, in.FolderID, in.Name)
}

// deleteFolderWithFns validates, gates, checks confirmName, and deletes.
//
// Same load-bearing order as the project and canvas board deletes: the EDITOR
// gate runs BEFORE the folder is read, so a VIEWER cannot use confirmName
// mismatches as a folder-name oracle.
func deleteFolderWithFns(
	ctx context.Context,
	fns folderLifecycleFns,
	userID string,
	in deleteFolderInput,
) (map[string]any, error) {
	if e := validateUUID("folderId", in.FolderID); e != nil {
		return nil, e
	}
	if in.ConfirmName == "" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"confirmName is required: pass the folder's exact current name to confirm this delete. "+
				"Read it with list_folders first.", "confirmName")
	}
	projectID, err := authorizeExistingFolder(ctx, fns, userID, in.FolderID)
	if err != nil {
		return nil, err
	}

	folder, err := fns.findFolder(ctx, in.FolderID)
	if err != nil {
		return nil, err
	}
	if folder == nil {
		return nil, folderNotFound(in.FolderID)
	}

	if folder.Name != in.ConfirmName {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("confirmName does not match the current name of folder %s. "+
				"Nothing was deleted.%s Read the folder with list_folders and pass its exact name.",
				in.FolderID, cascadeWarning(ctx, fns, projectID, in.FolderID)), "confirmName")
	}

	return fns.deleteFolder(ctx, userID, in.FolderID)
}

// cascadeWarning renders what the delete would have destroyed, so the agent
// learns the stakes at the moment it is told to try again rather than after a
// successful second attempt.
//
// Best-effort by design: a counting failure must not turn a refused delete into
// an error about counting. It returns the empty string and the mismatch message
// still stands on its own.
func cascadeWarning(ctx context.Context, fns folderLifecycleFns, projectID, folderID string) string {
	summaries, err := fns.listFolders(ctx, projectID)
	if err != nil {
		return ""
	}
	for _, s := range summaries {
		if s.ID != folderID {
			continue
		}
		total := s.WhiteboardCount + s.CanvasBoardCount
		if total == 0 && s.ChildFolderCount == 0 {
			return ""
		}
		return fmt.Sprintf(" Deleting it would have destroyed %d board(s) and %d child folder(s), "+
			"including everything inside them.", total, s.ChildFolderCount)
	}
	return ""
}

// RegisterFolderTools registers create_folder, update_folder and delete_folder.
func RegisterFolderTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_folder",
		Description: "Create a folder in a project to organise ER whiteboards and canvas boards. " +
			"Pass parentFolderId to nest it, or omit that to place it at the project root. " +
			"Returns the new folder, whose id create_whiteboard and create_canvas_board accept as " +
			"folderId. Requires the EDITOR role or higher.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createFolderInput) (*mcp.CallToolResult, any, error) {
		folder, err := createFolderWithFns(ctx, prodFolderLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(folder)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_folder",
		Description: "Rename a folder. Renaming is the only change available: there is no way to move " +
			"a folder to a different parent through this server, because the app exposes none. " +
			"Requires the EDITOR role or higher.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateFolderInput) (*mcp.CallToolResult, any, error) {
		folder, err := updateFolderWithFns(ctx, prodFolderLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(folder)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_folder",
		Description: "Permanently delete a folder, EVERY folder nested inside it, and EVERY ER " +
			"whiteboard and canvas board filed in any of them — with all their tables, columns and " +
			"relationships. This cannot be undone. A folder that looks empty in a listing may still " +
			"hold boards; call list_folders to see the counts first. confirmName is required and " +
			"must equal the folder's exact current name. A mismatch deletes nothing. Requires the " +
			"EDITOR role or higher.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteFolderInput) (*mcp.CallToolResult, any, error) {
		folder, err := deleteFolderWithFns(ctx, prodFolderLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(folder)
	})
}

// ER whiteboard lifecycle tools: create_whiteboard, update_whiteboard,
// delete_whiteboard.
//
// These are the tools that make the whole schema surface reachable from zero.
// create_table takes a whiteboardId, and until this wave nothing in this server
// could produce one — an agent could design a schema only on a board a human
// had already made in the app. create_project, create_whiteboard, create_table
// is now a complete chain.
//
// TRANSPORT. Board lifecycle goes through the app's POST /api/mcp-lifecycle
// route (internal/appapi), not Socket.IO. The socket carries writes that open
// clients must re-render — tables, columns, relationships. Creating the board
// those live on has no co-viewing client to broadcast to: the navigator reads
// the board list on load.
//
// AUTHORIZATION. EDITOR or higher on the owning project for all three, matching
// the app's whiteboards.ts server functions. A board is schema, and the EDITOR
// role is the schema role.
//
// SCOPE NOTE. update_whiteboard renames and re-files only. It deliberately
// exposes no projectId: moving a board to another project would check the role
// on the source project and write into the destination. canvasState and
// textSource are likewise out — they are board content with their own server
// functions, not lifecycle.
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

// whiteboardNameMaxLength mirrors createWhiteboardSchema's
// z.string().min(1).max(255).
const whiteboardNameMaxLength = 255

type createWhiteboardInput struct {
	ProjectID string  `json:"projectId"`
	Name      string  `json:"name" jsonschema:"1-255 characters"`
	FolderID  *string `json:"folderId,omitempty" jsonschema:"Omit for the project root"`
}

type updateWhiteboardInput struct {
	WhiteboardID string  `json:"whiteboardId"`
	Name         *string `json:"name,omitempty" jsonschema:"1-255 characters"`
	FolderID     *string `json:"folderId,omitempty" jsonschema:"Move target; omit to keep"`
}

type deleteWhiteboardInput struct {
	WhiteboardID string `json:"whiteboardId"`
	ConfirmName  string `json:"confirmName" jsonschema:"Exact current name; a mismatch deletes nothing"`
}

// whiteboardLifecycleFns is the injectable pipeline, mirroring
// canvasBoardLifecycleFns.
type whiteboardLifecycleFns struct {
	resolveProject   func(ctx context.Context, whiteboardID string) (string, error)
	assertEdit       func(ctx context.Context, userID, projectID string) error
	findWhiteboard   func(ctx context.Context, whiteboardID string) (*data.Whiteboard, error)
	createWhiteboard func(ctx context.Context, userID string, req appapi.CreateWhiteboardRequest) (map[string]any, error)
	updateWhiteboard func(ctx context.Context, userID, whiteboardID string, req appapi.UpdateWhiteboardRequest) (map[string]any, error)
	deleteWhiteboard func(ctx context.Context, userID, whiteboardID string) (map[string]any, error)
}

// prodWhiteboardLifecycleFns is the production pipeline.
func prodWhiteboardLifecycleFns() whiteboardLifecycleFns {
	return whiteboardLifecycleFns{
		resolveProject:   data.GetWhiteboardProjectID,
		assertEdit:       auth.AssertSchemaEditAccess,
		findWhiteboard:   data.FindWhiteboardByID,
		createWhiteboard: appapi.CreateWhiteboard,
		updateWhiteboard: appapi.UpdateWhiteboard,
		deleteWhiteboard: appapi.DeleteWhiteboard,
	}
}

// whiteboardNotFound builds the single NOT_FOUND message used by every miss.
//
// It deliberately names the ER whiteboard specifically: passing a canvas board
// id here resolves to "" and lands on this message, and an agent that mixed the
// two board kinds needs to be told which one this tool wanted.
func whiteboardNotFound(whiteboardID string) *mcperr.McpError {
	return mcperr.New(mcperr.NotFound,
		fmt.Sprintf("ER whiteboard %s not found. If this is a canvas board id, use the canvas board tools instead.",
			whiteboardID))
}

// authorizeExistingWhiteboard runs the shared gate for update and delete: UUID
// validation, project resolution, EDITOR+ check.
func authorizeExistingWhiteboard(ctx context.Context, fns whiteboardLifecycleFns, userID, whiteboardID string) error {
	if e := validateUUID("whiteboardId", whiteboardID); e != nil {
		return e
	}
	projectID, err := fns.resolveProject(ctx, whiteboardID)
	if err != nil {
		return err
	}
	if projectID == "" {
		return whiteboardNotFound(whiteboardID)
	}
	return fns.assertEdit(ctx, userID, projectID)
}

// createWhiteboardWithFns validates, gates, and creates one ER whiteboard.
func createWhiteboardWithFns(
	ctx context.Context,
	fns whiteboardLifecycleFns,
	userID string,
	in createWhiteboardInput,
) (map[string]any, error) {
	if e := validateUUID("projectId", in.ProjectID); e != nil {
		return nil, e
	}
	if e := checkLen("name", in.Name, 1, whiteboardNameMaxLength); e != nil {
		return nil, e
	}
	if in.FolderID != nil {
		if e := validateUUID("folderId", *in.FolderID); e != nil {
			return nil, e
		}
	}
	if err := fns.assertEdit(ctx, userID, in.ProjectID); err != nil {
		return nil, err
	}
	return fns.createWhiteboard(ctx, userID, appapi.CreateWhiteboardRequest{
		ProjectID: in.ProjectID,
		Name:      in.Name,
		FolderID:  in.FolderID,
	})
}

// updateWhiteboardWithFns validates, gates, and renames or re-files one board.
//
// An update naming no field is refused rather than sent: it would be a no-op
// round trip that still bumps updatedAt and reorders the board list.
func updateWhiteboardWithFns(
	ctx context.Context,
	fns whiteboardLifecycleFns,
	userID string,
	in updateWhiteboardInput,
) (map[string]any, error) {
	if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
		return nil, e
	}
	if in.Name == nil && in.FolderID == nil {
		return nil, mcperr.New(mcperr.ValidationError,
			"Supply at least one of name or folderId to update.")
	}
	if in.Name != nil {
		if e := checkLen("name", *in.Name, 1, whiteboardNameMaxLength); e != nil {
			return nil, e
		}
	}
	if in.FolderID != nil {
		if e := validateUUID("folderId", *in.FolderID); e != nil {
			return nil, e
		}
	}
	if err := authorizeExistingWhiteboard(ctx, fns, userID, in.WhiteboardID); err != nil {
		return nil, err
	}
	return fns.updateWhiteboard(ctx, userID, in.WhiteboardID, appapi.UpdateWhiteboardRequest{
		Name:     in.Name,
		FolderID: in.FolderID,
	})
}

// deleteWhiteboardWithFns validates, gates, checks confirmName, and deletes.
//
// Same load-bearing order as every other delete here: the EDITOR gate runs
// BEFORE the board is read, so a VIEWER cannot use confirmName mismatches as a
// board-name oracle. The name comparison runs before the route call, so a
// mismatch destroys nothing.
func deleteWhiteboardWithFns(
	ctx context.Context,
	fns whiteboardLifecycleFns,
	userID string,
	in deleteWhiteboardInput,
) (map[string]any, error) {
	if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
		return nil, e
	}
	if in.ConfirmName == "" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"confirmName is required: pass the whiteboard's exact current name to confirm this delete. "+
				"Read it with list_whiteboards first.", "confirmName")
	}
	if err := authorizeExistingWhiteboard(ctx, fns, userID, in.WhiteboardID); err != nil {
		return nil, err
	}

	board, err := fns.findWhiteboard(ctx, in.WhiteboardID)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, whiteboardNotFound(in.WhiteboardID)
	}

	if board.Name != in.ConfirmName {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("confirmName does not match the current name of whiteboard %s. "+
				"Nothing was deleted. Read the board with list_whiteboards and pass its exact name.",
				in.WhiteboardID), "confirmName")
	}

	return fns.deleteWhiteboard(ctx, userID, in.WhiteboardID)
}

// RegisterWhiteboardTools registers create_whiteboard, update_whiteboard and
// delete_whiteboard.
func RegisterWhiteboardTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_whiteboard",
		Description: "Create an ER whiteboard in a project (optional folderId). Returns its id for the schema tools. EDITOR role.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createWhiteboardInput) (*mcp.CallToolResult, any, error) {
		board, err := createWhiteboardWithFns(ctx, prodWhiteboardLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(board)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_whiteboard",
		Description: "Rename an ER whiteboard or move it into a folder. Needs name or folderId; it cannot return to the root. EDITOR role.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateWhiteboardInput) (*mcp.CallToolResult, any, error) {
		board, err := updateWhiteboardWithFns(ctx, prodWhiteboardLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(board)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_whiteboard",
		Description: "Permanently delete an ER whiteboard with its tables, columns and relationships. confirmName must equal the exact name (see list_whiteboards). EDITOR role.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteWhiteboardInput) (*mcp.CallToolResult, any, error) {
		board, err := deleteWhiteboardWithFns(ctx, prodWhiteboardLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(board)
	})
}

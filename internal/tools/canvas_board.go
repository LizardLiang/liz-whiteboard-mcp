// Canvas board lifecycle tools (Wave 5 of the canvas-whiteboard-mcp-support
// tactical plan): create_canvas_board, update_canvas_board, delete_canvas_board.
//
// These three tools do NOT use the Socket.IO write path the element tools use,
// and that is deliberate. Element writes ride the socket so every open browser
// client re-renders without a reload. Board lifecycle has no co-viewing client
// to broadcast to: the navigator and home page read the board list on load, not
// through a subscription. So these tools call the app's JWT-authenticated
// POST /api/canvas-boards route through internal/appapi instead, with the same
// collab-audience credential the socket dial uses.
//
// The authorization shape is identical to the element tools': resolve the
// project through the CanvasBoard table, then EDITOR+ via
// auth.AssertSchemaEditAccess. The app route re-checks with requireServerFnRole
// independently. Both layers stay — the near-side check gives a clean error
// without a round trip, the route's check is the one that protects the data.
//
// A9: delete_canvas_board requires a confirmName that matches the board's real
// stored name. The delete cascades every CanvasElement and CanvasBoardShareLink
// through ON DELETE CASCADE, and this project has already lost production data
// once to an unintended cascade. A destructive, irreversible, agent-invocable
// tool needs a confirmation argument an LLM cannot supply by accident.
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

// canvasBoardNameMaxLength mirrors createCanvasBoardSchema's
// z.string().min(1).max(255) in the app.
const canvasBoardNameMaxLength = 255

type createCanvasBoardInput struct {
	ProjectID string  `json:"projectId"`
	Name      string  `json:"name" jsonschema:"1-255 characters"`
	FolderID  *string `json:"folderId,omitempty" jsonschema:"Omit for the project root"`
}

type updateCanvasBoardInput struct {
	CanvasBoardID string  `json:"canvasBoardId"`
	Name          *string `json:"name,omitempty" jsonschema:"1-255 characters"`
	FolderID      *string `json:"folderId,omitempty" jsonschema:"Move target; omit to keep"`
}

type deleteCanvasBoardInput struct {
	CanvasBoardID string `json:"canvasBoardId"`
	ConfirmName   string `json:"confirmName" jsonschema:"Exact current name; a mismatch deletes nothing"`
}

// canvasBoardLifecycleFns is the injectable lifecycle pipeline, mirroring
// canvasEditFns in canvas_write.go so every guard is unit-testable without a
// database or a running app.
type canvasBoardLifecycleFns struct {
	resolveProject func(ctx context.Context, canvasBoardID string) (string, error)
	assertEdit     func(ctx context.Context, userID, projectID string) error
	findBoard      func(ctx context.Context, canvasBoardID string) (*data.CanvasBoard, error)
	createBoard    func(ctx context.Context, userID string, req appapi.CreateCanvasBoardRequest) (map[string]any, error)
	updateBoard    func(ctx context.Context, userID, canvasBoardID string, req appapi.UpdateCanvasBoardRequest) (map[string]any, error)
	deleteBoard    func(ctx context.Context, userID, canvasBoardID string) (map[string]any, error)
}

// prodCanvasBoardLifecycleFns is the production pipeline.
// TestProdCanvasBoardLifecycleFns_UseEditorGateAndCanvasResolver pins the three
// stages that would fail silently if mis-wired.
func prodCanvasBoardLifecycleFns() canvasBoardLifecycleFns {
	return canvasBoardLifecycleFns{
		resolveProject: data.GetCanvasBoardProjectID,
		assertEdit:     auth.AssertSchemaEditAccess,
		findBoard:      data.FindCanvasBoardByID,
		createBoard:    appapi.CreateCanvasBoard,
		updateBoard:    appapi.UpdateCanvasBoard,
		deleteBoard:    appapi.DeleteCanvasBoard,
	}
}

// authorizeExistingCanvasBoard runs the shared gate for update and delete:
// UUID validation, canvas project resolution, EDITOR+ check. An unknown board
// reports the same NOT_FOUND message the read tools use.
func authorizeExistingCanvasBoard(ctx context.Context, fns canvasBoardLifecycleFns, userID, canvasBoardID string) error {
	if e := validateUUID("canvasBoardId", canvasBoardID); e != nil {
		return e
	}
	projectID, err := fns.resolveProject(ctx, canvasBoardID)
	if err != nil {
		return err
	}
	if projectID == "" {
		return canvasBoardNotFound(canvasBoardID)
	}
	return fns.assertEdit(ctx, userID, projectID)
}

// createCanvasBoardWithFns validates, gates, and creates one canvas board.
func createCanvasBoardWithFns(
	ctx context.Context,
	fns canvasBoardLifecycleFns,
	userID string,
	in createCanvasBoardInput,
) (map[string]any, error) {
	if e := validateUUID("projectId", in.ProjectID); e != nil {
		return nil, e
	}
	if e := checkLen("name", in.Name, 1, canvasBoardNameMaxLength); e != nil {
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
	return fns.createBoard(ctx, userID, appapi.CreateCanvasBoardRequest{
		ProjectID: in.ProjectID,
		Name:      in.Name,
		FolderID:  in.FolderID,
	})
}

// updateCanvasBoardWithFns validates, gates, and renames or re-files one board.
//
// An update naming no field is refused rather than sent: it would be a no-op
// round trip that still bumps updatedAt and reorders the board list.
func updateCanvasBoardWithFns(
	ctx context.Context,
	fns canvasBoardLifecycleFns,
	userID string,
	in updateCanvasBoardInput,
) (map[string]any, error) {
	if e := validateUUID("canvasBoardId", in.CanvasBoardID); e != nil {
		return nil, e
	}
	if in.Name == nil && in.FolderID == nil {
		return nil, mcperr.New(mcperr.ValidationError,
			"Supply at least one of name or folderId to update.")
	}
	if in.Name != nil {
		if e := checkLen("name", *in.Name, 1, canvasBoardNameMaxLength); e != nil {
			return nil, e
		}
	}
	if in.FolderID != nil {
		if e := validateUUID("folderId", *in.FolderID); e != nil {
			return nil, e
		}
	}
	if err := authorizeExistingCanvasBoard(ctx, fns, userID, in.CanvasBoardID); err != nil {
		return nil, err
	}
	return fns.updateBoard(ctx, userID, in.CanvasBoardID, appapi.UpdateCanvasBoardRequest{
		Name:     in.Name,
		FolderID: in.FolderID,
	})
}

// deleteCanvasBoardWithFns validates, gates, checks confirmName, and deletes.
//
// Order is load-bearing. The EDITOR gate runs BEFORE the board is read, so a
// VIEWER cannot use confirmName mismatches as a board-name oracle. The name
// comparison runs before the route call, so a mismatch destroys nothing.
func deleteCanvasBoardWithFns(
	ctx context.Context,
	fns canvasBoardLifecycleFns,
	userID string,
	in deleteCanvasBoardInput,
) (map[string]any, error) {
	if e := validateUUID("canvasBoardId", in.CanvasBoardID); e != nil {
		return nil, e
	}
	if in.ConfirmName == "" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"confirmName is required: pass the board's exact current name to confirm this delete. "+
				"Read it with list_canvas_boards first.", "confirmName")
	}
	if err := authorizeExistingCanvasBoard(ctx, fns, userID, in.CanvasBoardID); err != nil {
		return nil, err
	}

	board, err := fns.findBoard(ctx, in.CanvasBoardID)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, canvasBoardNotFound(in.CanvasBoardID)
	}

	// Exact comparison: no trimming, no case folding. Whitespace is exactly what
	// a careless paste gets wrong, and the stored name is not echoed back — a
	// caller that could blind-retry with the revealed value would defeat the
	// guard's real purpose, catching a wrong board id before it destroys the
	// wrong board.
	if board.Name != in.ConfirmName {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("confirmName does not match the current name of canvas board %s. "+
				"Nothing was deleted. Read the name with list_canvas_boards and pass it exactly.",
				in.CanvasBoardID), "confirmName")
	}

	return fns.deleteBoard(ctx, userID, in.CanvasBoardID)
}

// RegisterCanvasBoardTools registers create_canvas_board, update_canvas_board,
// and delete_canvas_board.
func RegisterCanvasBoardTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_canvas_board",
		Description: "Create a canvas board in a project (optional folderId). Returns its id. EDITOR role. ER diagrams: create_whiteboard.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCanvasBoardInput) (*mcp.CallToolResult, any, error) {
		board, err := createCanvasBoardWithFns(ctx, prodCanvasBoardLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(board)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_canvas_board",
		Description: "Rename a canvas board or move it into a folder. Needs name or folderId; it cannot return to the root. EDITOR role.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCanvasBoardInput) (*mcp.CallToolResult, any, error) {
		board, err := updateCanvasBoardWithFns(ctx, prodCanvasBoardLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(board)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_canvas_board",
		Description: "Permanently delete a canvas board with all its elements and share links. confirmName must equal the exact name (see list_canvas_boards). EDITOR role.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteCanvasBoardInput) (*mcp.CallToolResult, any, error) {
		board, err := deleteCanvasBoardWithFns(ctx, prodCanvasBoardLifecycleFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(board)
	})
}

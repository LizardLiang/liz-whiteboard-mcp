package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/render"
)

// get_board_image and get_canvas_board_image let an agent SEE a board. The
// server screenshots the app's own render page in a headless Chromium sidecar
// (internal/render), so the image shows what a person sees.
//
// Framing:
//
//	fit    (default) the whole board in one image; with focusIds, only those items
//	tiles  several images at readable zoom, one per occupied tile, at most 12
//
// Each image is 1568 x 1568 CSS pixels at device pixel ratio 1.

const maxFocusIDs = 50

type boardImageArgs struct {
	Mode     *string
	FocusIDs []string
	Theme    *string
}

type whiteboardImageInput struct {
	WhiteboardID string   `json:"whiteboardId" jsonschema:"The whiteboard UUID"`
	Mode         *string  `json:"mode,omitempty" jsonschema:"Framing: fit (default, whole board in one image) or tiles (several images at readable zoom, at most 12)"`
	FocusIDs     []string `json:"focusIds,omitempty" jsonschema:"Up to 50 table, area, or shape UUIDs to frame instead of the whole board; only with mode fit"`
	Theme        *string  `json:"theme,omitempty" jsonschema:"light (default) or dark"`
}

type canvasBoardImageInput struct {
	CanvasBoardID string   `json:"canvasBoardId" jsonschema:"The canvas board UUID"`
	Mode          *string  `json:"mode,omitempty" jsonschema:"Framing: fit (default, whole board in one image) or tiles (several images at readable zoom, at most 12)"`
	FocusIDs      []string `json:"focusIds,omitempty" jsonschema:"Up to 50 canvas element UUIDs to frame instead of the whole board; only with mode fit"`
	Theme         *string  `json:"theme,omitempty" jsonschema:"light (default) or dark"`
}

// validatedImageArgs holds arguments after validation, with defaults applied.
type validatedImageArgs struct {
	Mode     string
	FocusIDs []string
	Theme    string
}

// validateBoardImageArgs checks the framing arguments. It runs before any
// access check or network call, so a malformed request costs nothing.
func validateBoardImageArgs(a boardImageArgs) (validatedImageArgs, *mcperr.McpError) {
	out := validatedImageArgs{Mode: render.ModeFit, Theme: "light", FocusIDs: a.FocusIDs}
	if a.Mode != nil {
		switch *a.Mode {
		case render.ModeFit, render.ModeTiles:
			out.Mode = *a.Mode
		default:
			return out, mcperr.NewField(mcperr.ValidationError, `mode must be "fit" or "tiles".`, "mode")
		}
	}
	if a.Theme != nil {
		switch *a.Theme {
		case "light", "dark":
			out.Theme = *a.Theme
		default:
			return out, mcperr.NewField(mcperr.ValidationError, `theme must be "light" or "dark".`, "theme")
		}
	}
	if len(a.FocusIDs) > maxFocusIDs {
		return out, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("focusIds takes at most %d ids; got %d.", maxFocusIDs, len(a.FocusIDs)), "focusIds")
	}
	for _, id := range a.FocusIDs {
		if e := validateUUID("focusIds", id); e != nil {
			return out, e
		}
	}
	if len(a.FocusIDs) > 0 && out.Mode == render.ModeTiles {
		return out, mcperr.NewField(mcperr.ValidationError,
			`focusIds cannot be combined with mode "tiles". Use mode "fit" with focusIds, or drop focusIds.`, "focusIds")
	}
	return out, nil
}

// boardImageRenderer is the slice of *render.Renderer the tools use.
type boardImageRenderer interface {
	Render(ctx context.Context, mint render.Minter, req render.Request) (*render.Result, error)
}

// boardImageDeps are the seams the tests replace.
type boardImageDeps struct {
	// authorize reports NOT_FOUND for an unknown board and the access error for
	// a board the caller cannot read.
	authorize func(ctx context.Context, userID, boardID string) error
	mint      func(ctx context.Context, userID, kind, boardID, notFound string) (appapi.RenderToken, error)
	renderer  func() boardImageRenderer
}

func prodBoardImageDeps() boardImageDeps {
	return boardImageDeps{
		mint:     appapi.MintRenderToken,
		renderer: func() boardImageRenderer { return render.New() },
	}
}

// authorizeWhiteboard mirrors loadAuthorizedBoard's gate without loading rows.
func authorizeWhiteboard(ctx context.Context, userID, id string) error {
	projectID, err := data.GetWhiteboardProjectID(ctx, id)
	if err != nil {
		return err
	}
	if projectID == "" {
		return plainWhiteboardNotFound(id)
	}
	return auth.AssertProjectAccess(ctx, userID, projectID)
}

// authorizeCanvasBoard mirrors loadAuthorizedCanvasBoardWithFns's gate without loading rows.
func authorizeCanvasBoard(ctx context.Context, userID, id string) error {
	projectID, err := data.GetCanvasBoardProjectID(ctx, id)
	if err != nil {
		return err
	}
	if projectID == "" {
		return canvasBoardNotFound(id)
	}
	return auth.AssertProjectAccess(ctx, userID, projectID)
}

// plainWhiteboardNotFound is the message get_board returns for a missing board.
func plainWhiteboardNotFound(id string) *mcperr.McpError {
	return mcperr.New(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", id))
}

// runBoardImage is the shared handler body for both tools.
func runBoardImage(
	ctx context.Context,
	deps boardImageDeps,
	kind, idField, boardID, notFound string,
	args boardImageArgs,
) (*mcp.CallToolResult, any, error) {
	if e := validateUUID(idField, boardID); e != nil {
		return fail(e)
	}
	v, e := validateBoardImageArgs(args)
	if e != nil {
		return fail(e)
	}
	userID := auth.UserID(ctx)
	if err := deps.authorize(ctx, userID, boardID); err != nil {
		return fail(err)
	}

	mint := func(ctx context.Context, kind, boardID string) (appapi.RenderToken, error) {
		return deps.mint(ctx, userID, kind, boardID, notFound)
	}
	res, err := deps.renderer().Render(ctx, mint, render.Request{
		Kind:     kind,
		BoardID:  boardID,
		Mode:     v.Mode,
		FocusIDs: v.FocusIDs,
		Theme:    v.Theme,
	})
	if err != nil {
		return fail(err)
	}
	if res.Empty {
		return text("board is empty")
	}

	content := make([]mcp.Content, 0, len(res.Images)*2)
	for _, img := range res.Images {
		content = append(content,
			&mcp.ImageContent{Data: img.PNG, MIMEType: "image/png"},
			&mcp.TextContent{Text: fmt.Sprintf("%s World rect: x=%.0f y=%.0f width=%.0f height=%.0f.",
				img.Label, img.WorldRect.X, img.WorldRect.Y, img.WorldRect.Width, img.WorldRect.Height)},
		)
	}
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// RegisterBoardImageTools registers get_board_image and get_canvas_board_image.
func RegisterBoardImageTools(s *mcp.Server) {
	wb, cv := prodBoardImageDeps(), prodBoardImageDeps()
	wb.authorize = authorizeWhiteboard
	cv.authorize = authorizeCanvasBoard
	registerBoardImageToolsWith(s, wb, cv)
}

func registerBoardImageToolsWith(s *mcp.Server, wb, cv boardImageDeps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_board_image",
		Description: "See an ER whiteboard as a PNG image (tables, columns, relationships, areas as a person sees them). " +
			"mode fit (default) returns the whole board in one image; focusIds frames only those tables, areas, or shapes. " +
			"mode tiles returns several readable-zoom images (at most 12). Each image is 1568x1568 px; a text block after it " +
			"gives the board-coordinate rectangle it shows. Use get_board for the data itself.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in whiteboardImageInput) (*mcp.CallToolResult, any, error) {
		return runBoardImage(ctx, wb, appapi.RenderKindWhiteboard, "whiteboardId", in.WhiteboardID,
			fmt.Sprintf("Whiteboard %s not found.", in.WhiteboardID),
			boardImageArgs{Mode: in.Mode, FocusIDs: in.FocusIDs, Theme: in.Theme})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_canvas_board_image",
		Description: "See a canvas board (freeform shapes, text, connectors; NOT an ER whiteboard) as a PNG image. " +
			"mode fit (default) returns the whole board in one image; focusIds frames only those elements. " +
			"mode tiles returns several readable-zoom images (at most 12). Each image is 1568x1568 px; a text block after it " +
			"gives the board-coordinate rectangle it shows. Use get_canvas_board for the data itself.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in canvasBoardImageInput) (*mcp.CallToolResult, any, error) {
		return runBoardImage(ctx, cv, appapi.RenderKindCanvas, "canvasBoardId", in.CanvasBoardID,
			fmt.Sprintf("Canvas board %s not found.", in.CanvasBoardID),
			boardImageArgs{Mode: in.Mode, FocusIDs: in.FocusIDs, Theme: in.Theme})
	})
}

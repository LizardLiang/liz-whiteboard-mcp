package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/summary"
)

// A canvas board is a DIFFERENT board kind from an ER whiteboard: freeform
// shapes, text, and connectors in "CanvasBoard"/"CanvasElement", sharing no rows
// with the ER diagram. Every tool description below says so, because an LLM
// choosing between list_whiteboards and list_canvas_boards has only the
// descriptions to go on.
//
// Read authorization is project MEMBERSHIP (auth.AssertProjectAccess), the same
// gate the ER read tools use. The EDITOR+ gate from a9b9e95 applies to mutations,
// which arrive in Wave 2.

// canvasMaxElementsDefault bounds a full canvas board read. A board can hold
// thousands of elements; returning them all would blow the caller's context
// window. The default is also the ceiling: a caller may ask for fewer, never
// more.
const canvasMaxElementsDefault = 500

type listCanvasBoardsInput struct {
	ProjectID string `json:"projectId" jsonschema:"The project UUID"`
}

type canvasBoardIDInput struct {
	CanvasBoardID string `json:"canvasBoardId" jsonschema:"The canvas board UUID"`
	MaxElements   *int   `json:"maxElements,omitempty" jsonschema:"Maximum elements to return, 1-500 (default 500)"`
}

// canvasBoardOut is the get_canvas_board response: the board, a bounded slice of
// its elements in paint order, and the counts that make any truncation explicit.
type canvasBoardOut struct {
	data.CanvasBoard
	Elements         []data.CanvasElement `json:"elements"`
	TotalElements    int                  `json:"totalElements"`
	ReturnedElements int                  `json:"returnedElements"`
	Truncated        bool                 `json:"truncated"`
	MaxElements      int                  `json:"maxElements"`
}

// canvasBoardLoaderFns is the injectable read pipeline, mirroring the ...WithFn
// pattern in internal/auth/querier.go so the authz path is unit-testable without
// a database.
type canvasBoardLoaderFns struct {
	resolveProject func(ctx context.Context, canvasBoardID string) (string, error)
	assertAccess   func(ctx context.Context, userID, projectID string) error
	loadBoard      func(ctx context.Context, canvasBoardID string) (*data.CanvasBoardWithElements, error)
}

// prodCanvasBoardLoaderFns is the production pipeline.
//
// resolveProject MUST be data.GetCanvasBoardProjectID. data.GetWhiteboardProjectID
// reads the "Whiteboard" table, so a canvas board id returns "" there and every
// canvas tool would answer NOT_FOUND. TestProdCanvasBoardLoaderFns_UseCanvasProjectResolver
// pins this.
func prodCanvasBoardLoaderFns() canvasBoardLoaderFns {
	return canvasBoardLoaderFns{
		resolveProject: data.GetCanvasBoardProjectID,
		assertAccess:   auth.AssertProjectAccess,
		loadBoard:      data.FindCanvasBoardByIDWithElements,
	}
}

// loadAuthorizedCanvasBoardWithFns runs the shared canvas read path: UUID
// validation, canvas project resolution, membership scoping, and board load.
// An unknown board and a board the caller cannot see both report NOT_FOUND
// before any access check, so the error never leaks a board's existence.
func loadAuthorizedCanvasBoardWithFns(
	ctx context.Context,
	fns canvasBoardLoaderFns,
	userID, canvasBoardID string,
) (*data.CanvasBoardWithElements, error) {
	if e := validateUUID("canvasBoardId", canvasBoardID); e != nil {
		return nil, e
	}
	projectID, err := fns.resolveProject(ctx, canvasBoardID)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, canvasBoardNotFound(canvasBoardID)
	}
	if err := fns.assertAccess(ctx, userID, projectID); err != nil {
		return nil, err
	}
	board, err := fns.loadBoard(ctx, canvasBoardID)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, canvasBoardNotFound(canvasBoardID)
	}
	return board, nil
}

// canvasBoardNotFound builds the single NOT_FOUND message used by every miss on
// the canvas read path.
func canvasBoardNotFound(canvasBoardID string) *mcperr.McpError {
	return mcperr.New(mcperr.NotFound, fmt.Sprintf("Canvas board %s not found.", canvasBoardID))
}

// loadAuthorizedCanvasBoard is the production entry point for the read gate.
// Mirrors loadAuthorizedBoard in read.go, against the canvas tables.
func loadAuthorizedCanvasBoard(ctx context.Context, canvasBoardID string) (*data.CanvasBoardWithElements, error) {
	return loadAuthorizedCanvasBoardWithFns(ctx, prodCanvasBoardLoaderFns(), auth.UserID(ctx), canvasBoardID)
}

// resolveMaxElements validates the caller's element cap and applies the default.
// Rejects rather than clamps a request above the ceiling: silently returning
// fewer elements than asked for is exactly the ambiguity the truncation flags
// exist to remove.
func resolveMaxElements(requested *int) (int, *mcperr.McpError) {
	if requested == nil {
		return canvasMaxElementsDefault, nil
	}
	if *requested < 1 || *requested > canvasMaxElementsDefault {
		return 0, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("maxElements must be between 1 and %d.", canvasMaxElementsDefault), "maxElements")
	}
	return *requested, nil
}

// truncateCanvasElements keeps the first max elements. The input is already in
// paint order ("zIndex" ASC, "createdAt" ASC), so the kept slice is the bottom of
// the stack. Always returns a non-nil slice, so an empty board serialises as []
// rather than null.
func truncateCanvasElements(elements []data.CanvasElement, max int) ([]data.CanvasElement, bool) {
	if elements == nil {
		return []data.CanvasElement{}, false
	}
	if len(elements) <= max {
		return elements, false
	}
	return elements[:max], true
}

// newCanvasBoardOut assembles the bounded read response, always reporting the
// total, the returned count, and the truncation flag.
func newCanvasBoardOut(board *data.CanvasBoardWithElements, maxElements int) canvasBoardOut {
	total := len(board.Elements)
	shown, truncated := truncateCanvasElements(board.Elements, maxElements)
	return canvasBoardOut{
		CanvasBoard:      board.CanvasBoard,
		Elements:         shown,
		TotalElements:    total,
		ReturnedElements: len(shown),
		Truncated:        truncated,
		MaxElements:      maxElements,
	}
}

// RegisterCanvasReadTools registers list_canvas_boards, get_canvas_board, and
// get_canvas_summary.
func RegisterCanvasReadTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_canvas_boards",
		Description: "List all canvas boards in a project. Canvas boards are freeform FigJam-style " +
			"boards of shapes, text, and connectors — NOT ER diagram whiteboards. " +
			"Use list_whiteboards for ER diagrams.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCanvasBoardsInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("projectId", in.ProjectID); e != nil {
			return fail(e)
		}
		userID := auth.UserID(ctx)
		if err := auth.AssertProjectAccess(ctx, userID, in.ProjectID); err != nil {
			return fail(err)
		}
		boards, err := data.ListCanvasBoards(ctx, in.ProjectID)
		if err != nil {
			return fail(err)
		}
		return success(boards)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_canvas_board",
		Description: "Get the full state of a canvas board (freeform shapes, text, and connectors) " +
			"with every element's geometry, style, and props, in paint order. " +
			"This is the detail read: prefer get_canvas_summary for orientation. " +
			"Returns at most 500 elements and reports totalElements and truncated. " +
			"For ER diagram whiteboards use get_board instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in canvasBoardIDInput) (*mcp.CallToolResult, any, error) {
		maxElements, e := resolveMaxElements(in.MaxElements)
		if e != nil {
			return fail(e)
		}
		board, err := loadAuthorizedCanvasBoard(ctx, in.CanvasBoardID)
		if err != nil {
			return fail(err)
		}
		return success(newCanvasBoardOut(board, maxElements))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_canvas_summary",
		Description: "Get a compact text summary of a canvas board: element counts by kind and one " +
			"line per element with its kind, position, size, and text. Omits UUIDs, style, " +
			"and props. Use this first to understand a canvas board, then get_canvas_board " +
			"for full detail. For ER diagram whiteboards use get_schema_summary instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in canvasBoardIDInput) (*mcp.CallToolResult, any, error) {
		maxElements, e := resolveMaxElements(in.MaxElements)
		if e != nil {
			return fail(e)
		}
		board, err := loadAuthorizedCanvasBoard(ctx, in.CanvasBoardID)
		if err != nil {
			return fail(err)
		}
		// The summary is bounded by the same cap as the detail read: one line per
		// element on a 10,000-element board is still 10,000 lines.
		total := len(board.Elements)
		shown, _ := truncateCanvasElements(board.Elements, maxElements)
		bounded := &data.CanvasBoardWithElements{CanvasBoard: board.CanvasBoard, Elements: shown}
		return text(summary.FormatCanvasSummary(bounded, total))
	})
}

package data

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// Canvas boards are a DELIBERATELY separate board kind from whiteboards: the
// canvas engine draws every pixel itself and stores its own generic elements in
// "CanvasBoard"/"CanvasElement", sharing no rows with the ER diagram. Nothing in
// this file may be reached through a Whiteboard id, and nothing in whiteboard.go
// may be reached through a CanvasBoard id.
//
// "createdAt"/"updatedAt" are declared DATETIME but the app writes them through
// nowMs(), i.e. unix milliseconds, exactly like every other table here. The
// existing Timestamp scanner therefore applies unchanged.

// CanvasBoard mirrors the app's CanvasBoard row.
type CanvasBoard struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ProjectID string    `json:"projectId"`
	FolderID  *string   `json:"folderId"`
	CreatedAt Timestamp `json:"createdAt"`
	UpdatedAt Timestamp `json:"updatedAt"`
}

// CanvasElement mirrors the app's CanvasElement row: geometry in real columns,
// kind-specific data in the validated JSON "props" blob, appearance in "style".
// Revision is the monotonic write counter the app's undo path compares against.
type CanvasElement struct {
	ID        string    `json:"id"`
	BoardID   string    `json:"boardId"`
	Kind      string    `json:"kind"`
	PositionX float64   `json:"positionX"`
	PositionY float64   `json:"positionY"`
	Width     float64   `json:"width"`
	Height    float64   `json:"height"`
	Rotation  float64   `json:"rotation"`
	ZIndex    int       `json:"zIndex"`
	Text      *string   `json:"text"`
	Style     JSONText  `json:"style"`
	Props     JSONText  `json:"props"`
	Revision  int       `json:"revision"`
	CreatedAt Timestamp `json:"createdAt"`
	UpdatedAt Timestamp `json:"updatedAt"`
}

// CanvasBoardWithElements is a canvas board with its elements in paint order.
type CanvasBoardWithElements struct {
	CanvasBoard
	Elements []CanvasElement `json:"elements"`
}

// CanvasBoardSummary is a list entry for list_canvas_boards. Mirrors
// WhiteboardSummary, counting elements instead of tables.
type CanvasBoardSummary struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	UpdatedAt    Timestamp `json:"updatedAt"`
	ElementCount int       `json:"elementCount"`
}

// Shared column lists, so the single-row and multi-row queries can never drift.
const canvasBoardSelectColumns = `"id", "name", "projectId", "folderId", "createdAt", "updatedAt"`

const canvasElementSelectColumns = `"id", "boardId", "kind", "positionX", "positionY", "width", "height", ` +
	`"rotation", "zIndex", "text", "style", "props", "revision", "createdAt", "updatedAt"`

// scanCanvasBoard scans one CanvasBoard row (order must match canvasBoardSelectColumns).
func scanCanvasBoard(s scanner) (*CanvasBoard, error) {
	var b CanvasBoard
	if err := s.Scan(&b.ID, &b.Name, &b.ProjectID, &b.FolderID, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

// scanCanvasElement scans one CanvasElement row (order must match canvasElementSelectColumns).
func scanCanvasElement(s scanner) (*CanvasElement, error) {
	var e CanvasElement
	if err := s.Scan(&e.ID, &e.BoardID, &e.Kind, &e.PositionX, &e.PositionY,
		&e.Width, &e.Height, &e.Rotation, &e.ZIndex, &e.Text, &e.Style, &e.Props,
		&e.Revision, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

// FindCanvasBoardsByProjectID returns all canvas boards in a project, most
// recently updated first. Always returns a non-nil (possibly empty) slice.
func FindCanvasBoardsByProjectID(ctx context.Context, projectID string) ([]CanvasBoard, error) {
	rows, err := db.Pool().Query(ctx,
		`SELECT `+canvasBoardSelectColumns+`
		   FROM "CanvasBoard"
		  WHERE "projectId" = $1
		  ORDER BY "updatedAt" DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CanvasBoard, 0)
	for rows.Next() {
		b, err := scanCanvasBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// FindCanvasBoardByID returns a canvas board by ID, or nil if it does not exist.
func FindCanvasBoardByID(ctx context.Context, id string) (*CanvasBoard, error) {
	b, err := scanCanvasBoard(db.Pool().QueryRow(ctx,
		`SELECT `+canvasBoardSelectColumns+` FROM "CanvasBoard" WHERE "id" = $1`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

// FindCanvasElementsByBoardID returns the elements of a canvas board in paint
// order: "zIndex" ASC, ties broken by "createdAt" ASC. That order is the app's
// own paint order and must be reproduced exactly, or "what is on top" differs
// between this view and the rendered board. Always returns a non-nil slice.
func FindCanvasElementsByBoardID(ctx context.Context, boardID string) ([]CanvasElement, error) {
	rows, err := db.Pool().Query(ctx,
		`SELECT `+canvasElementSelectColumns+`
		   FROM "CanvasElement"
		  WHERE "boardId" = $1
		  ORDER BY "zIndex" ASC, "createdAt" ASC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CanvasElement, 0)
	for rows.Next() {
		e, err := scanCanvasElement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// ListCanvasBoards returns the canvas boards in a project with an element count
// for each. Uses a single grouped COUNT for the counts (avoids N+1), mirroring
// ListWhiteboards.
func ListCanvasBoards(ctx context.Context, projectID string) ([]CanvasBoardSummary, error) {
	boards, err := FindCanvasBoardsByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(boards) == 0 {
		return []CanvasBoardSummary{}, nil
	}

	ids := make([]string, len(boards))
	for i, b := range boards {
		ids[i] = b.ID
	}

	marks, args := inPlaceholders(ids)
	countRows, err := db.Pool().Query(ctx,
		`SELECT "boardId", COUNT(*) FROM "CanvasElement" WHERE "boardId" IN (`+marks+`) GROUP BY "boardId"`,
		args...)
	if err != nil {
		return nil, err
	}
	defer countRows.Close()

	counts := make(map[string]int, len(boards))
	for countRows.Next() {
		var boardID string
		var n int
		if err := countRows.Scan(&boardID, &n); err != nil {
			return nil, err
		}
		counts[boardID] = n
	}
	if err := countRows.Err(); err != nil {
		return nil, err
	}

	out := make([]CanvasBoardSummary, 0, len(boards))
	for _, b := range boards {
		out = append(out, CanvasBoardSummary{
			ID:           b.ID,
			Name:         b.Name,
			UpdatedAt:    b.UpdatedAt,
			ElementCount: counts[b.ID], // 0 for boards with no elements (map zero value)
		})
	}
	return out, nil
}

// FindCanvasBoardByIDWithElements loads a canvas board with every element in
// paint order. Returns nil if the board does not exist, matching
// FindWhiteboardByIDWithDiagram. An element-free board returns an empty (non-nil)
// Elements slice.
func FindCanvasBoardByIDWithElements(ctx context.Context, id string) (*CanvasBoardWithElements, error) {
	board, err := FindCanvasBoardByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, nil
	}

	elements, err := FindCanvasElementsByBoardID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &CanvasBoardWithElements{CanvasBoard: *board, Elements: elements}, nil
}

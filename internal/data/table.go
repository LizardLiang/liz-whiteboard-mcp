package data

import (
	"context"
	"errors"

	"database/sql"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// FindDiagramTableByID returns a table by ID, or nil if it does not exist.
func FindDiagramTableByID(ctx context.Context, id string) (*DiagramTable, error) {
	var t DiagramTable
	err := db.Pool().QueryRow(ctx,
		`SELECT id, "whiteboardId", name, description, "positionX", "positionY", width, height, "createdAt", "updatedAt"
		   FROM "DiagramTable" WHERE id = $1`, id).
		Scan(&t.ID, &t.WhiteboardID, &t.Name, &t.Description,
			&t.PositionX, &t.PositionY, &t.Width, &t.Height, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

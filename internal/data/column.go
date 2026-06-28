package data

import (
	"context"
	"errors"

	"database/sql"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// FindColumnByID returns a column by ID, or nil if it does not exist.
func FindColumnByID(ctx context.Context, id string) (*Column, error) {
	var c Column
	err := db.Pool().QueryRow(ctx,
		`SELECT id, "tableId", name, "dataType", "isPrimaryKey", "isForeignKey", "isUnique", "isNullable", description, "order", "createdAt", "updatedAt"
		   FROM "Column" WHERE id = $1`, id).
		Scan(&c.ID, &c.TableID, &c.Name, &c.DataType,
			&c.IsPrimaryKey, &c.IsForeignKey, &c.IsUnique, &c.IsNullable,
			&c.Description, &c.Order, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

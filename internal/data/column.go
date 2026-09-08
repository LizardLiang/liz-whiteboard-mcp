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

// CountColumnsByTableID returns the number of columns on a table. Used by the
// MCP area-membership tools (add_table_to_area/remove_table_from_area) to
// recompute area bounds without loading the whole board.
func CountColumnsByTableID(ctx context.Context, tableID string) (int, error) {
	var n int
	err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM "Column" WHERE "tableId" = $1`, tableID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

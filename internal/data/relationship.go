package data

import (
	"context"
	"errors"

	"database/sql"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// FindRelationshipByID returns a relationship by ID, or nil if it does not exist.
func FindRelationshipByID(ctx context.Context, id string) (*Relationship, error) {
	var r Relationship
	err := db.Pool().QueryRow(ctx,
		`SELECT id, "whiteboardId", "sourceTableId", "targetTableId", "sourceColumnId", "targetColumnId", cardinality, label, "routingPoints", "createdAt", "updatedAt"
		   FROM "Relationship" WHERE id = $1`, id).
		Scan(&r.ID, &r.WhiteboardID, &r.SourceTableID, &r.TargetTableID,
			&r.SourceColumnID, &r.TargetColumnID, &r.Cardinality, &r.Label,
			&r.RoutingPoints, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

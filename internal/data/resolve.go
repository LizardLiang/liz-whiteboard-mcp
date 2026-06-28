package data

import (
	"context"
	"errors"

	"database/sql"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// resolveProjectID runs a single-column query and returns the projectId, or ""
// if no row matched. Mirrors the *?.projectId ?? null pattern from
// src/data/resolve-project.ts.
func resolveProjectID(ctx context.Context, query string, arg string) (string, error) {
	var projectID string
	err := db.Pool().QueryRow(ctx, query, arg).Scan(&projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return projectID, nil
}

// GetWhiteboardProjectID resolves the projectId for a whiteboard by ID.
// Returns "" if the whiteboard does not exist.
func GetWhiteboardProjectID(ctx context.Context, whiteboardID string) (string, error) {
	return resolveProjectID(ctx,
		`SELECT "projectId" FROM "Whiteboard" WHERE id = $1`, whiteboardID)
}

// GetTableProjectID resolves the projectId for a table by ID (via its whiteboard).
// Returns "" if the table does not exist.
func GetTableProjectID(ctx context.Context, tableID string) (string, error) {
	return resolveProjectID(ctx,
		`SELECT w."projectId"
		   FROM "DiagramTable" t
		   JOIN "Whiteboard" w ON w.id = t."whiteboardId"
		  WHERE t.id = $1`, tableID)
}

// GetColumnProjectID resolves the projectId for a column by ID
// (via its table's whiteboard). Returns "" if the column does not exist.
func GetColumnProjectID(ctx context.Context, columnID string) (string, error) {
	return resolveProjectID(ctx,
		`SELECT w."projectId"
		   FROM "Column" c
		   JOIN "DiagramTable" t ON t.id = c."tableId"
		   JOIN "Whiteboard" w ON w.id = t."whiteboardId"
		  WHERE c.id = $1`, columnID)
}

// GetRelationshipProjectID resolves the projectId for a relationship by ID
// (via its whiteboard). Returns "" if the relationship does not exist.
func GetRelationshipProjectID(ctx context.Context, relationshipID string) (string, error) {
	return resolveProjectID(ctx,
		`SELECT w."projectId"
		   FROM "Relationship" r
		   JOIN "Whiteboard" w ON w.id = r."whiteboardId"
		  WHERE r.id = $1`, relationshipID)
}

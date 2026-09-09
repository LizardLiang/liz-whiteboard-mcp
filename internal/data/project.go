// Project reads.
//
// The list path lives in internal/auth (ListAccessibleProjects), because it is
// membership-scoped and that scoping is the IDOR boundary. This file holds the
// by-id read the lifecycle tools need: delete_project's confirmName guard has to
// compare against the project's stored name, and it can only do that after the
// role check has already passed.
package data

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// FindProjectByID returns a project by ID, or nil if it does not exist.
//
// This is deliberately NOT membership-scoped: every caller runs an auth assert
// first, and a second scoping here would turn a clean FORBIDDEN into a
// misleading NOT_FOUND. Never call it without a preceding role check.
func FindProjectByID(ctx context.Context, id string) (*Project, error) {
	var p Project
	err := db.Pool().QueryRow(ctx,
		`SELECT id, name, description, "createdAt", "updatedAt", "ownerId"
		   FROM "Project" WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt, &p.OwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

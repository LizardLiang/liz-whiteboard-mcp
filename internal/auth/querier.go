// Package auth — injectable helpers for unit-testing auth logic without a real DB.
// The public functions (AssertProjectAccess, ListAccessibleProjects) delegate to
// these internal helpers so tests can substitute mock implementations.
//
// The session-token injectable (isSessionTokenValidWithFn / tokenValidatorFn) was
// removed in Phase 2 — per-request identity replaces the process-global token model.
package auth

import (
	"context"
	"errors"
	"fmt"

	"database/sql"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// ---------------------------------------------------------------------------
// SQL constants — exposed for test inspection (TC-SEC-IDOR-07).
// These are the exact WHERE clauses that enforce the IDOR boundary.
// ---------------------------------------------------------------------------

// projectAccessSQL is the membership-scoped query used by AssertProjectAccess.
// Must check ownerId OR ProjectMember membership — not a plain findById.
const projectAccessSQL = `SELECT id FROM "Project"
  WHERE id = $1
    AND ("ownerId" = $2
     OR EXISTS (SELECT 1 FROM "ProjectMember" WHERE "projectId" = $1 AND "userId" = $2))`

// listProjectsSQL is the query used by ListAccessibleProjects.
// Must include both ownerId and ProjectMember membership.
const listProjectsSQL = `SELECT id, name, description
   FROM "Project"
  WHERE "ownerId" = $1
     OR id IN (SELECT "projectId" FROM "ProjectMember" WHERE "userId" = $1)
  ORDER BY "createdAt" DESC`

// ---------------------------------------------------------------------------
// Injectable function types — production code passes real DB calls;
// tests pass mocks.
// ---------------------------------------------------------------------------

// checkAccessFn returns (hasMembership bool, err error) for (projectID, userID).
type checkAccessFn func(ctx context.Context, projectID, userID string) (bool, error)

// ---------------------------------------------------------------------------
// Production implementations
// ---------------------------------------------------------------------------

// checkProjectAccessDB is the production project-membership checker.
func checkProjectAccessDB(ctx context.Context, projectID, userID string) (bool, error) {
	var id string
	err := db.Pool().QueryRow(ctx, projectAccessSQL, projectID, userID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// Injectable cores — called by the public functions and by tests.
// ---------------------------------------------------------------------------

// assertProjectAccessWithFn is the injectable core of AssertProjectAccess.
// Tests call this directly with a mock checkFn to avoid a real DB.
func assertProjectAccessWithFn(
	ctx context.Context,
	check checkAccessFn,
	userID, projectID string,
) error {
	if projectID == "" {
		return mcperr.New(mcperr.NotFound, "Resource not found.")
	}
	found, err := check(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if !found {
		return mcperr.New(mcperr.Forbidden,
			fmt.Sprintf("User %s has no access to project %s.", userID, projectID))
	}
	return nil
}

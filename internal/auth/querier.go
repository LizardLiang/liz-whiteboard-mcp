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

// schemaEditAccessSQL checks EDITOR+ role: owner or ProjectMember with role EDITOR or ADMIN.
// OWNER role is implicit (Project.ownerId); the ProjectMember.role enum is VIEWER|EDITOR|ADMIN.
const schemaEditAccessSQL = `SELECT id FROM "Project"
  WHERE id = $1
    AND (
      "ownerId" = $2
      OR EXISTS (
        SELECT 1 FROM "ProjectMember"
        WHERE "projectId" = $1
          AND "userId" = $2
          AND "role" IN ('EDITOR', 'ADMIN')
      )
    )`

// projectAdminAccessSQL checks ADMIN+ role: owner or ProjectMember with role
// ADMIN. Used by update_project, which the app gates at ADMIN
// (updateProjectFn in src/routes/api/projects.ts). An EDITOR who may create
// tables all day still may not rename the project they live in.
const projectAdminAccessSQL = `SELECT id FROM "Project"
  WHERE id = $1
    AND (
      "ownerId" = $2
      OR EXISTS (
        SELECT 1 FROM "ProjectMember"
        WHERE "projectId" = $1
          AND "userId" = $2
          AND "role" = 'ADMIN'
      )
    )`

// projectOwnerAccessSQL checks OWNER: the Project.ownerId column alone. There
// is no OWNER value in the ProjectMember.role enum — ownership is the column,
// which is why this query has no EXISTS arm at all. Used by delete_project,
// which the app restricts to the owner (deleteProjectFn).
const projectOwnerAccessSQL = `SELECT id FROM "Project"
  WHERE id = $1 AND "ownerId" = $2`

// ---------------------------------------------------------------------------
// Injectable function types — production code passes real DB calls;
// tests pass mocks.
// ---------------------------------------------------------------------------

// checkAccessFn returns (hasMembership bool, err error) for (projectID, userID).
type checkAccessFn func(ctx context.Context, projectID, userID string) (bool, error)

// ---------------------------------------------------------------------------
// Production implementations
// ---------------------------------------------------------------------------

// checkAccessDB is the shared production access checker, parameterized on the
// scoping SQL. Both checkProjectAccessDB (owner-or-any-member) and
// checkSchemaEditAccessDB (owner-or-EDITOR/ADMIN-member) delegate to this —
// the only difference between the two access tiers is which query is passed.
func checkAccessDB(ctx context.Context, sqlStr string, projectID, userID string) (bool, error) {
	var id string
	err := db.Pool().QueryRow(ctx, sqlStr, projectID, userID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// checkProjectAccessDB is the production project-membership checker
// (owner or any ProjectMember role — VIEWER/EDITOR/ADMIN).
func checkProjectAccessDB(ctx context.Context, projectID, userID string) (bool, error) {
	return checkAccessDB(ctx, projectAccessSQL, projectID, userID)
}

// checkSchemaEditAccessDB is the production EDITOR+ role checker used by
// schema-mutating tools (create/update/delete table, column, relationship,
// batch_schema_update). Returns false for VIEWER-role members or non-members.
func checkSchemaEditAccessDB(ctx context.Context, projectID, userID string) (bool, error) {
	return checkAccessDB(ctx, schemaEditAccessSQL, projectID, userID)
}

// ---------------------------------------------------------------------------
// Injectable cores — called by the public functions and by tests.
// ---------------------------------------------------------------------------

// assertAccessWithFn is the shared injectable core behind both
// assertProjectAccessWithFn and assertSchemaEditAccessWithFn. It differs only
// in the FORBIDDEN message, since the two access tiers report distinct denial
// reasons (no access at all vs. insufficient role).
func assertAccessWithFn(
	ctx context.Context,
	check checkAccessFn,
	userID, projectID string,
	forbiddenMessage string,
) error {
	if projectID == "" {
		return mcperr.New(mcperr.NotFound, "Resource not found.")
	}
	found, err := check(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if !found {
		return mcperr.New(mcperr.Forbidden, forbiddenMessage)
	}
	return nil
}

// assertProjectAccessWithFn is the injectable core of AssertProjectAccess.
// Tests call this directly with a mock checkFn to avoid a real DB.
func assertProjectAccessWithFn(
	ctx context.Context,
	check checkAccessFn,
	userID, projectID string,
) error {
	return assertAccessWithFn(ctx, check, userID, projectID,
		fmt.Sprintf("User %s has no access to project %s.", userID, projectID))
}

// assertSchemaEditAccessWithFn is the injectable core of AssertSchemaEditAccess.
// Tests call this directly with a mock checkFn to avoid a real DB.
func assertSchemaEditAccessWithFn(
	ctx context.Context,
	check checkAccessFn,
	userID, projectID string,
) error {
	return assertAccessWithFn(ctx, check, userID, projectID,
		fmt.Sprintf("User %s does not have EDITOR access to project %s.", userID, projectID))
}

// checkProjectAdminAccessDB is the production ADMIN+ role checker used by
// update_project. Returns false for EDITOR and VIEWER members.
func checkProjectAdminAccessDB(ctx context.Context, projectID, userID string) (bool, error) {
	return checkAccessDB(ctx, projectAdminAccessSQL, projectID, userID)
}

// checkProjectOwnerAccessDB is the production OWNER checker used by
// delete_project. Returns false for every member role, ADMIN included.
func checkProjectOwnerAccessDB(ctx context.Context, projectID, userID string) (bool, error) {
	return checkAccessDB(ctx, projectOwnerAccessSQL, projectID, userID)
}

// assertProjectAdminAccessWithFn is the injectable core of
// AssertProjectAdminAccess.
func assertProjectAdminAccessWithFn(
	ctx context.Context,
	check checkAccessFn,
	userID, projectID string,
) error {
	return assertAccessWithFn(ctx, check, userID, projectID,
		fmt.Sprintf("User %s does not have ADMIN access to project %s.", userID, projectID))
}

// assertProjectOwnerAccessWithFn is the injectable core of
// AssertProjectOwnerAccess.
func assertProjectOwnerAccessWithFn(
	ctx context.Context,
	check checkAccessFn,
	userID, projectID string,
) error {
	return assertAccessWithFn(ctx, check, userID, projectID,
		fmt.Sprintf("User %s is not the owner of project %s.", userID, projectID))
}

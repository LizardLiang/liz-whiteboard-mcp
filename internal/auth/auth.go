// Package auth implements per-request identity resolution and project-membership
// scoping for the MCP server.
//
// Identity model (Phase 2+):
//   - The bearer middleware (RequireBearerToken) validates the access token and
//     stores *auth.TokenInfo in the request context.
//   - Tool handlers call auth.UserID(ctx) to read the user id from the validated
//     token for THIS request — there is no process-global identity.
//
// Ported from src/mcp/auth.ts and src/lib/auth/session.ts.
// The process-global LIZ_SESSION_TOKEN / ValidateStartupToken / GetAuthedUserID
// model was removed in Phase 2 (oauth-remote-mcp plan).
package auth

import (
	"context"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// UserID returns the authenticated user's ID from the request context.
// The bearer middleware (auth.RequireBearerToken) stores a *TokenInfo in the
// context after validating the token; this is a thin accessor over that value.
//
// Returns "" if no TokenInfo is present in the context (should never happen in a
// properly wired handler — the middleware rejects the request before it reaches
// tool code when no valid token is present).
func UserID(ctx context.Context) string {
	ti := sdkauth.TokenInfoFromContext(ctx)
	if ti == nil {
		return ""
	}
	return ti.UserID
}

// ListAccessibleProjects lists only projects accessible to the given user
// (owner or ProjectMember row), newest first.
// Uses listProjectsSQL (see querier.go) — the constant is inspected by IDOR tests.
func ListAccessibleProjects(ctx context.Context, userID string) ([]data.Project, error) {
	rows, err := db.Pool().Query(ctx, listProjectsSQL, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []data.Project
	for rows.Next() {
		var p data.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AssertProjectAccess asserts that a user has access to a project (owner or
// ProjectMember row). Returns an McpError NOT_FOUND if projectID is empty, or
// FORBIDDEN if the user has no access.
// Delegates to assertProjectAccessWithFn so tests can inject a mock checker.
func AssertProjectAccess(ctx context.Context, userID, projectID string) error {
	return assertProjectAccessWithFn(ctx, checkProjectAccessDB, userID, projectID)
}

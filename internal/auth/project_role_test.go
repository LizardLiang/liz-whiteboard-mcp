package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const roleUserID = "role-user-1"
const roleProjectID = "role-project-1"

func allowRoleCheck(_ context.Context, _, _ string) (bool, error) { return true, nil }
func denyRoleCheck(_ context.Context, _, _ string) (bool, error)  { return false, nil }
func erroringRoleCheck(_ context.Context, _, _ string) (bool, error) {
	return false, errors.New("db exploded")
}

func requireCode(t *testing.T, err error, want mcperr.Code) {
	t.Helper()
	require.Error(t, err)
	var mcpErr *mcperr.McpError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, want, mcpErr.Code)
	assert.NotEmpty(t, mcpErr.Message)
}

// ---------------------------------------------------------------------------
// ADMIN gate (update_project)
// ---------------------------------------------------------------------------

func TestAssertProjectAdminAccess_Allowed(t *testing.T) {
	err := assertProjectAdminAccessWithFn(context.Background(), allowRoleCheck, roleUserID, roleProjectID)
	assert.NoError(t, err)
}

func TestAssertProjectAdminAccess_Forbidden(t *testing.T) {
	err := assertProjectAdminAccessWithFn(context.Background(), denyRoleCheck, roleUserID, roleProjectID)
	requireCode(t, err, mcperr.Forbidden)
	assert.Contains(t, err.Error(), "ADMIN")
}

func TestAssertProjectAdminAccess_DBErrorPropagates(t *testing.T) {
	err := assertProjectAdminAccessWithFn(context.Background(), erroringRoleCheck, roleUserID, roleProjectID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db exploded")
}

// An empty projectID must not reach the DB — same anti-enumeration rule the
// other asserts follow.
func TestAssertProjectAdminAccess_EmptyProjectIDNotFound(t *testing.T) {
	err := AssertProjectAdminAccess(context.Background(), roleUserID, "")
	requireCode(t, err, mcperr.NotFound)
}

// ---------------------------------------------------------------------------
// OWNER gate (delete_project)
// ---------------------------------------------------------------------------

func TestAssertProjectOwnerAccess_Allowed(t *testing.T) {
	err := assertProjectOwnerAccessWithFn(context.Background(), allowRoleCheck, roleUserID, roleProjectID)
	assert.NoError(t, err)
}

func TestAssertProjectOwnerAccess_Forbidden(t *testing.T) {
	err := assertProjectOwnerAccessWithFn(context.Background(), denyRoleCheck, roleUserID, roleProjectID)
	requireCode(t, err, mcperr.Forbidden)
	assert.Contains(t, err.Error(), "owner")
}

func TestAssertProjectOwnerAccess_EmptyProjectIDNotFound(t *testing.T) {
	err := AssertProjectOwnerAccess(context.Background(), roleUserID, "")
	requireCode(t, err, mcperr.NotFound)
}

// ---------------------------------------------------------------------------
// SQL shape — these clauses ARE the authorization boundary. A silent edit that
// widened them would otherwise only surface in production.
// ---------------------------------------------------------------------------

// ADMIN must not admit EDITOR. The schema-edit query deliberately accepts both;
// this one must not, or renaming a project becomes an EDITOR power.
func TestProjectAdminAccessSQL_ExcludesEditor(t *testing.T) {
	assert.Contains(t, projectAdminAccessSQL, `"ownerId" = $2`,
		"SQL must admit the project owner")
	assert.Contains(t, projectAdminAccessSQL, `"role" = 'ADMIN'`,
		"SQL must restrict membership to ADMIN")
	assert.NotContains(t, projectAdminAccessSQL, "EDITOR",
		"ADMIN gate must not admit EDITOR members")
	assert.NotContains(t, projectAdminAccessSQL, "VIEWER",
		"ADMIN gate must not admit VIEWER members")
}

// OWNER is the Project.ownerId column, not a ProjectMember role. A ProjectMember
// clause here would hand project deletion to an ADMIN.
func TestProjectOwnerAccessSQL_HasNoMembershipClause(t *testing.T) {
	assert.Contains(t, projectOwnerAccessSQL, `"ownerId" = $2`,
		"SQL must scope by ownerId")
	assert.NotContains(t, projectOwnerAccessSQL, "ProjectMember",
		"OWNER gate must not consult ProjectMember — ADMIN is not OWNER")
	assert.NotContains(t, projectOwnerAccessSQL, "ADMIN",
		"OWNER gate must not admit ADMIN members")
}

// The four gates must stay distinct. Collapsing any two would silently widen
// one of them.
func TestProjectRoleSQL_TiersAreDistinct(t *testing.T) {
	queries := []string{
		projectAccessSQL,
		schemaEditAccessSQL,
		projectAdminAccessSQL,
		projectOwnerAccessSQL,
	}
	for i := range queries {
		for j := i + 1; j < len(queries); j++ {
			assert.NotEqual(t, queries[i], queries[j],
				"access tiers %d and %d share one query", i, j)
		}
	}
}

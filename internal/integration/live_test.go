//go:build integration

// Package integration — live integration tests against a real database.
// Run: DATABASE_URL=... go test -tags=integration ./internal/integration/...
package integration

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// requireDB connects to the database. Skips the test if DATABASE_URL is unset.
func requireDB(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; run with DATABASE_URL=... -tags=integration")
	}
	ctx := context.Background()
	if _, err := db.Connect(ctx); err != nil {
		t.Skipf("cannot connect to database: %v", err)
	}
	return ctx
}

// ---------------------------------------------------------------------------
// TC-INTG-IDOR-01: live IDOR check — random user/project denied
// ---------------------------------------------------------------------------

func TestIntg_AssertProjectAccess_RandomUserDenied(t *testing.T) {
	ctx := requireDB(t)

	randomUserID := "00000000-dead-4000-8000-000000000001"
	randomProjectID := "00000000-dead-4000-8000-000000000002"

	err := auth.AssertProjectAccess(ctx, randomUserID, randomProjectID)
	require.Error(t, err, "non-existent user/project must be denied")

	if mcpErr, ok := err.(*mcperr.McpError); ok {
		// Either FORBIDDEN (project exists but user isn't member) or
		// FORBIDDEN (project doesn't exist — asserted as no-membership).
		assert.Equal(t, mcperr.Forbidden, mcpErr.Code)
	}
}

// ---------------------------------------------------------------------------
// TC-INTG-CRUD-01: live list projects
// ---------------------------------------------------------------------------

func TestIntg_ListAccessibleProjects_AuthedUser(t *testing.T) {
	ctx := requireDB(t)

	userID := os.Getenv("INTG_TEST_USER_ID")
	if userID == "" {
		t.Skip("INTG_TEST_USER_ID not set; skipping live project list test")
	}

	projects, err := auth.ListAccessibleProjects(ctx, userID)
	require.NoError(t, err)
	assert.NotNil(t, projects,
		"ListAccessibleProjects must return a non-nil slice (may be empty for new user)")
	t.Logf("found %d project(s) for user %s", len(projects), userID)
}

// TC-INTG-CRUD-01b: random user owns no projects.
func TestIntg_ListAccessibleProjects_RandomUser_Empty(t *testing.T) {
	ctx := requireDB(t)

	// Use a random UUID that doesn't exist in the DB.
	randomID := "00000000-0000-4000-8000-000000000099"
	projects, err := auth.ListAccessibleProjects(ctx, randomID)
	require.NoError(t, err)
	assert.Empty(t, projects, "random user must own/be-member-of no projects")
}

// ---------------------------------------------------------------------------
// TC-INTG-CRUD-02: live list whiteboards
// ---------------------------------------------------------------------------

func TestIntg_ListWhiteboards_NoError(t *testing.T) {
	ctx := requireDB(t)

	projectID := os.Getenv("INTG_TEST_PROJECT_ID")
	if projectID == "" {
		t.Skip("INTG_TEST_PROJECT_ID not set; skipping live whiteboard list test")
	}

	whiteboards, err := data.ListWhiteboards(ctx, projectID)
	require.NoError(t, err)
	assert.NotNil(t, whiteboards)
	t.Logf("found %d whiteboard(s) in project %s", len(whiteboards), projectID)
}

// ---------------------------------------------------------------------------
// TC-INTG-BULK-01: bulk position load (exercises N+1 fix W1)
// ---------------------------------------------------------------------------

func TestIntg_BulkPositionLoad(t *testing.T) {
	ctx := requireDB(t)

	whiteboardID := os.Getenv("INTG_TEST_WHITEBOARD_ID")
	if whiteboardID == "" {
		t.Skip("INTG_TEST_WHITEBOARD_ID not set; skipping bulk position test")
	}

	wb, err := data.FindWhiteboardByIDWithDiagram(ctx, whiteboardID)
	require.NoError(t, err)
	if wb == nil {
		t.Skipf("whiteboard %s not found in DB", whiteboardID)
	}

	assert.NotNil(t, wb.Tables, "whiteboard must return non-nil tables slice")
	t.Logf("loaded %d tables for whiteboard %s", len(wb.Tables), whiteboardID)

	// Verify columns are populated (tests the 3-query load path).
	for _, tbl := range wb.Tables {
		assert.NotNil(t, tbl.Columns, "columns must be non-nil for each table")
	}
}

// ---------------------------------------------------------------------------
// TC-INTG-CRUD-03: FindWhiteboardByIDWithDiagram returns nil for missing whiteboard
// ---------------------------------------------------------------------------

func TestIntg_FindWhiteboardByID_NonExistentReturnsNil(t *testing.T) {
	ctx := requireDB(t)

	wb, err := data.FindWhiteboardByIDWithDiagram(ctx, "00000000-0000-4000-8000-000000000099")
	require.NoError(t, err)
	assert.Nil(t, wb, "non-existent whiteboard must return nil, not an error")
}

// ---------------------------------------------------------------------------
// TC-INTG-REFINT-01: live relationship endpoint validation
// ---------------------------------------------------------------------------

func TestIntg_AssertRelationshipEndpoints_InvalidColumn(t *testing.T) {
	ctx := requireDB(t)

	// Use non-existent IDs — column lookup returns nil → error.
	err := data.AssertRelationshipEndpointsValid(ctx, data.RelationshipEndpoints{
		SourceTableID:  "00000000-0000-4000-8000-000000000001",
		TargetTableID:  "00000000-0000-4000-8000-000000000002",
		SourceColumnID: "00000000-0000-4000-8000-000000000003",
		TargetColumnID: "00000000-0000-4000-8000-000000000004",
		WhiteboardID:   "00000000-0000-4000-8000-000000000005",
	})
	// Column not found → treated as wrong-table → error.
	require.Error(t, err, "non-existent column must fail endpoint validation")
	assert.Contains(t, err.Error(), "does not belong to")
}

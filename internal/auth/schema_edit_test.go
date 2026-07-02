// Package auth — tests for AssertSchemaEditAccess (EDITOR+ role gate).
// Added per the "Schema Mutation Permission Gate" tactical plan, Step 6.
//
// Strategy: unit-mockable — injects a checkAccessFn (owner-or-EDITOR+ check)
// so tests never touch a real database.
package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const (
	schemaEditUserID    = "11112222-1111-4111-8111-111122223333"
	schemaEditProjectID = "44445555-4444-4444-8444-444455556666"
)

// allowSchemaEditCheck simulates a DB where the caller is owner or EDITOR/ADMIN member.
func allowSchemaEditCheck(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

// denySchemaEditCheck simulates a DB where the caller is VIEWER-role or a non-member.
func denySchemaEditCheck(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// erroringSchemaEditCheck simulates a DB error during the membership lookup.
func erroringSchemaEditCheck(_ context.Context, _, _ string) (bool, error) {
	return false, assert.AnError
}

// TestAssertSchemaEditAccess_Allowed: owner or EDITOR/ADMIN member → nil error.
func TestAssertSchemaEditAccess_Allowed(t *testing.T) {
	err := assertSchemaEditAccessWithFn(context.Background(), allowSchemaEditCheck,
		schemaEditUserID, schemaEditProjectID)
	require.NoError(t, err)
}

// TestAssertSchemaEditAccess_Forbidden: VIEWER-role or no membership → FORBIDDEN McpError.
func TestAssertSchemaEditAccess_Forbidden(t *testing.T) {
	err := assertSchemaEditAccessWithFn(context.Background(), denySchemaEditCheck,
		schemaEditUserID, schemaEditProjectID)
	require.Error(t, err)
	mcpErr, ok := err.(*mcperr.McpError)
	require.True(t, ok, "expected *McpError, got %T", err)
	assert.Equal(t, mcperr.Forbidden, mcpErr.Code)
	assert.Contains(t, mcpErr.Message, schemaEditUserID)
	assert.Contains(t, mcpErr.Message, schemaEditProjectID)
}

// TestAssertSchemaEditAccess_DBError: a check error propagates (not swallowed as FORBIDDEN).
func TestAssertSchemaEditAccess_DBError(t *testing.T) {
	err := assertSchemaEditAccessWithFn(context.Background(), erroringSchemaEditCheck,
		schemaEditUserID, schemaEditProjectID)
	require.Error(t, err)
	_, ok := err.(*mcperr.McpError)
	assert.False(t, ok, "a raw check error should not be wrapped as an McpError")
	assert.Equal(t, assert.AnError, err)
}

// TestAssertSchemaEditAccess_EmptyProjectID: empty projectID yields NOT_FOUND,
// same anti-enumeration behavior as assertProjectAccessWithFn.
func TestAssertSchemaEditAccess_EmptyProjectID(t *testing.T) {
	err := assertSchemaEditAccessWithFn(context.Background(), denySchemaEditCheck,
		schemaEditUserID, "")
	require.Error(t, err)
	mcpErr, ok := err.(*mcperr.McpError)
	require.True(t, ok)
	assert.Equal(t, mcperr.NotFound, mcpErr.Code)
}

// TestSchemaEditAccessSQL_HasOwnerOrEditorAdminClause: schemaEditAccessSQL scopes
// to owner OR ProjectMember with role EDITOR/ADMIN (not VIEWER).
func TestSchemaEditAccessSQL_HasOwnerOrEditorAdminClause(t *testing.T) {
	assert.Contains(t, schemaEditAccessSQL, `"ownerId" = $2`,
		"SQL must scope by ownerId")
	assert.Contains(t, schemaEditAccessSQL, `"ProjectMember"`,
		"SQL must check ProjectMember for membership")
	assert.Contains(t, schemaEditAccessSQL, `"role" IN ('EDITOR', 'ADMIN')`,
		"SQL must restrict membership to EDITOR or ADMIN roles, excluding VIEWER")
}

// TestAssertSchemaEditAccess_PublicWrapper: AssertSchemaEditAccess (the public,
// DB-backed entry point) delegates through assertSchemaEditAccessWithFn — verified
// indirectly by confirming it requires a live DB connection to succeed and returns
// NOT_FOUND for an empty projectID without touching the DB (anti-enumeration,
// mirrors TestAssertProjectAccess_NotFound).
func TestAssertSchemaEditAccess_PublicWrapper_EmptyProjectIDNotFound(t *testing.T) {
	err := AssertSchemaEditAccess(context.Background(), schemaEditUserID, "")
	require.Error(t, err)
	mcpErr, ok := err.(*mcperr.McpError)
	require.True(t, ok)
	assert.Equal(t, mcperr.NotFound, mcpErr.Code)
}

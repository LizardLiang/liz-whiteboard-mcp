package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// requireDB connects to the database or skips the test if DATABASE_URL is unset.
func requireDB(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	if _, err := db.Connect(ctx); err != nil {
		t.Skipf("cannot connect to database: %v", err)
	}
	return ctx
}

// TestUserID_EmptyWhenNoToken: UserID returns "" if no TokenInfo is in the context.
func TestUserID_EmptyWhenNoToken(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", UserID(ctx))
}

// TestUserID_FromStubToken: UserID returns the user id carried by a validated stub token.
// This exercises the full path: RequireBearerToken middleware validates the token,
// stores TokenInfo in the context, and UserID extracts UserID from it.
func TestUserID_FromStubToken(t *testing.T) {
	t.Setenv("MCP_DEV_AUTH", "stub")
	t.Setenv("MCP_DEV_STUB_TOKEN", "test-bearer-token")
	t.Setenv("MCP_DEV_USER_ID", "user-abc-123")

	verifier := NewStubVerifier()

	var capturedUserID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserID = UserID(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	protected := sdkauth.RequireBearerToken(verifier, nil)(inner)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer test-bearer-token")
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "user-abc-123", capturedUserID)
}

// TestUserID_NoTokenInHeader: requests without a bearer token receive 401.
func TestUserID_NoTokenInHeader(t *testing.T) {
	t.Setenv("MCP_DEV_AUTH", "stub")
	t.Setenv("MCP_DEV_STUB_TOKEN", "test-bearer-token")

	verifier := NewStubVerifier()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	protected := sdkauth.RequireBearerToken(verifier, nil)(inner)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// TestListAccessibleProjects_RandomUser: an unknown user owns/joins no projects.
func TestListAccessibleProjects_RandomUser(t *testing.T) {
	ctx := requireDB(t)
	projects, err := ListAccessibleProjects(ctx, uuid.NewString())
	require.NoError(t, err)
	assert.Empty(t, projects)
}

// TestAssertProjectAccess_Forbidden: a non-member is denied with FORBIDDEN.
func TestAssertProjectAccess_Forbidden(t *testing.T) {
	ctx := requireDB(t)
	err := AssertProjectAccess(ctx, uuid.NewString(), uuid.NewString())
	require.Error(t, err)
	mcpErr, ok := err.(*mcperr.McpError)
	require.True(t, ok, "expected *McpError, got %T", err)
	assert.Equal(t, mcperr.Forbidden, mcpErr.Code)
}

// TestAssertProjectAccess_NotFound: an empty projectID yields NOT_FOUND.
func TestAssertProjectAccess_NotFound(t *testing.T) {
	err := AssertProjectAccess(context.Background(), uuid.NewString(), "")
	require.Error(t, err)
	mcpErr, ok := err.(*mcperr.McpError)
	require.True(t, ok)
	assert.Equal(t, mcperr.NotFound, mcpErr.Code)
}

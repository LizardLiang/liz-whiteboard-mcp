// Package auth — token lifecycle tests.
//
// Phase 2 replaces the session-token model (FR-021 / isSessionTokenValidWithFn)
// with per-request bearer-token identity. Token expiry is now enforced by the
// bearer middleware (token exp claim + 401), not by re-validating a session cookie.
//
// TC-LIFECYCLE-STUB tests exercise the Phase-2 stub verifier behaviour:
//   - Without MCP_DEV_AUTH=stub: every token is rejected.
//   - With MCP_DEV_AUTH=stub + MCP_DEV_STUB_TOKEN=X: only token X is accepted.
//   - Wrong token: rejected with ErrInvalidToken.
//   - Missing MCP_DEV_STUB_TOKEN: rejected even if MCP_DEV_AUTH=stub.
package auth

import (
	"context"
	"net/http"
	"testing"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TC-LIFECYCLE-STUB-01: without MCP_DEV_AUTH=stub, all tokens are rejected.
func TestLifecycle_Stub01_ProdMode_RejectsAll(t *testing.T) {
	t.Setenv("MCP_DEV_AUTH", "")           // explicitly absent
	t.Setenv("MCP_DEV_STUB_TOKEN", "test") // would-be valid token

	v := NewStubVerifier()
	_, err := v(context.Background(), "test", &http.Request{})
	require.Error(t, err)
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken,
		"prod mode must reject all tokens regardless of MCP_DEV_STUB_TOKEN")
}

// TC-LIFECYCLE-STUB-02: with MCP_DEV_AUTH=stub but no MCP_DEV_STUB_TOKEN, reject.
func TestLifecycle_Stub02_StubModeNoToken_Rejects(t *testing.T) {
	t.Setenv("MCP_DEV_AUTH", "stub")
	t.Setenv("MCP_DEV_STUB_TOKEN", "")

	v := NewStubVerifier()
	_, err := v(context.Background(), "anything", &http.Request{})
	require.Error(t, err)
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken)
}

// TC-LIFECYCLE-STUB-03: correct stub token accepted, UserID populated.
func TestLifecycle_Stub03_CorrectToken_Accepted(t *testing.T) {
	t.Setenv("MCP_DEV_AUTH", "stub")
	t.Setenv("MCP_DEV_STUB_TOKEN", "valid-dev-token")
	t.Setenv("MCP_DEV_USER_ID", "user-dev-1")

	v := NewStubVerifier()
	ti, err := v(context.Background(), "valid-dev-token", &http.Request{})
	require.NoError(t, err)
	require.NotNil(t, ti)
	assert.Equal(t, "user-dev-1", ti.UserID)
	assert.Contains(t, ti.Scopes, "whiteboard")
	assert.False(t, ti.Expiration.IsZero(), "expiration must be set")
}

// TC-LIFECYCLE-STUB-04: wrong token rejected.
func TestLifecycle_Stub04_WrongToken_Rejected(t *testing.T) {
	t.Setenv("MCP_DEV_AUTH", "stub")
	t.Setenv("MCP_DEV_STUB_TOKEN", "correct-token")

	v := NewStubVerifier()
	_, err := v(context.Background(), "wrong-token", &http.Request{})
	require.Error(t, err)
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken)
}

// TC-LIFECYCLE-STUB-05: MCP_DEV_USER_ID defaults to "dev-user" when unset.
func TestLifecycle_Stub05_DefaultUserID(t *testing.T) {
	t.Setenv("MCP_DEV_AUTH", "stub")
	t.Setenv("MCP_DEV_STUB_TOKEN", "tok")
	t.Setenv("MCP_DEV_USER_ID", "")

	v := NewStubVerifier()
	ti, err := v(context.Background(), "tok", &http.Request{})
	require.NoError(t, err)
	assert.Equal(t, "dev-user", ti.UserID)
}

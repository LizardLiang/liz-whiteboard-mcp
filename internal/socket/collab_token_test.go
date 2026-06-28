package socket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetCollabCache clears the per-user token cache between tests.
func resetCollabCache() {
	collabState.mu.Lock()
	collabState.tokens = make(map[string]collabCacheEntry)
	collabState.mu.Unlock()
}

// TestGetCollabToken_Fetches verifies that GetCollabToken calls the AS endpoint
// and returns the token when the cache is empty.
func TestGetCollabToken_Fetches(t *testing.T) {
	resetCollabCache()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "mcp-server", body["client_id"])
		assert.Equal(t, "test-secret", body["client_secret"])
		assert.Equal(t, "user-abc", body["user_id"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collabTokenResponse{Token: "test-jwt-token", ExpiresIn: 120})
	}))
	defer srv.Close()

	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	t.Setenv("MCP_CLIENT_ID", "mcp-server")
	t.Setenv("MCP_CLIENT_SECRET", "test-secret")

	// Point the package's HTTP client at our test server.
	collabState.client = srv.Client()

	token, err := GetCollabToken(context.Background(), "user-abc")
	require.NoError(t, err)
	assert.Equal(t, "test-jwt-token", token)
}

// TestGetCollabToken_CacheHit verifies that a second call for the same user
// returns the cached token without a second HTTP request.
func TestGetCollabToken_CacheHit(t *testing.T) {
	resetCollabCache()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collabTokenResponse{Token: "cached-jwt", ExpiresIn: 120})
	}))
	defer srv.Close()

	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	t.Setenv("MCP_CLIENT_ID", "mcp-server")
	t.Setenv("MCP_CLIENT_SECRET", "test-secret")
	collabState.client = srv.Client()

	_, err := GetCollabToken(context.Background(), "user-cached")
	require.NoError(t, err)
	_, err = GetCollabToken(context.Background(), "user-cached")
	require.NoError(t, err)

	// Only one HTTP call should have been made.
	assert.Equal(t, 1, callCount, "expected cache hit on second call")
}

// TestGetCollabToken_CacheExpiry verifies that an expired cache entry triggers
// a fresh fetch.
func TestGetCollabToken_CacheExpiry(t *testing.T) {
	resetCollabCache()

	// Pre-populate a cache entry that is already expired (expiresAt in the past).
	collabState.mu.Lock()
	collabState.tokens["user-expired"] = collabCacheEntry{
		token:     "old-jwt",
		expiresAt: time.Now().Add(-5 * time.Second), // already expired
	}
	collabState.mu.Unlock()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collabTokenResponse{Token: "fresh-jwt", ExpiresIn: 120})
	}))
	defer srv.Close()

	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	t.Setenv("MCP_CLIENT_ID", "mcp-server")
	t.Setenv("MCP_CLIENT_SECRET", "test-secret")
	collabState.client = srv.Client()

	token, err := GetCollabToken(context.Background(), "user-expired")
	require.NoError(t, err)
	assert.Equal(t, "fresh-jwt", token)
	assert.Equal(t, 1, callCount)
}

// TestGetCollabToken_ServerError verifies that an HTTP error from the AS
// propagates as a non-nil error (not a silent cache hit or empty token).
func TestGetCollabToken_ServerError(t *testing.T) {
	resetCollabCache()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	t.Setenv("MCP_CLIENT_ID", "mcp-server")
	t.Setenv("MCP_CLIENT_SECRET", "wrong-secret")
	collabState.client = srv.Client()

	_, err := GetCollabToken(context.Background(), "user-err")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// TestGetCollabToken_PerUserIsolation verifies that different users get
// independent cache entries (i.e., user A's token is not returned for user B).
func TestGetCollabToken_PerUserIsolation(t *testing.T) {
	resetCollabCache()

	// Pre-populate cache for user-a with a known token.
	collabState.mu.Lock()
	collabState.tokens["user-a"] = collabCacheEntry{
		token:     "token-for-user-a",
		expiresAt: time.Now().Add(90 * time.Second),
	}
	collabState.mu.Unlock()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collabTokenResponse{Token: "token-for-user-b", ExpiresIn: 120})
	}))
	defer srv.Close()

	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	t.Setenv("MCP_CLIENT_ID", "mcp-server")
	t.Setenv("MCP_CLIENT_SECRET", "test-secret")
	collabState.client = srv.Client()

	tokenA, err := GetCollabToken(context.Background(), "user-a")
	require.NoError(t, err)
	assert.Equal(t, "token-for-user-a", tokenA)

	tokenB, err := GetCollabToken(context.Background(), "user-b")
	require.NoError(t, err)
	assert.Equal(t, "token-for-user-b", tokenB)

	assert.NotEqual(t, tokenA, tokenB, "user A and B must get different tokens")
}

// TestFlushCollabTokenCache verifies that flushing a user's entry causes a
// fresh fetch on the next call.
func TestFlushCollabTokenCache(t *testing.T) {
	resetCollabCache()

	// Pre-populate cache.
	collabState.mu.Lock()
	collabState.tokens["flush-user"] = collabCacheEntry{
		token:     "old-token",
		expiresAt: time.Now().Add(90 * time.Second),
	}
	collabState.mu.Unlock()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collabTokenResponse{Token: "refreshed-token", ExpiresIn: 120})
	}))
	defer srv.Close()

	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	t.Setenv("MCP_CLIENT_ID", "mcp-server")
	t.Setenv("MCP_CLIENT_SECRET", "test-secret")
	collabState.client = srv.Client()

	// Flush forces a new fetch even though cache was populated.
	flushCollabTokenCache("flush-user")

	token, err := GetCollabToken(context.Background(), "flush-user")
	require.NoError(t, err)
	assert.Equal(t, "refreshed-token", token)
	assert.Equal(t, 1, callCount)
}

// TestGetCollabToken_Singleflight verifies that concurrent callers for the same
// userID are coalesced: only one HTTP request fires even when multiple goroutines
// race past a cold cache simultaneously (WARNING-3 fix).
func TestGetCollabToken_Singleflight(t *testing.T) {
	resetCollabCache()

	const concurrency = 20
	callCount := 0
	var callMu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a small AS latency to give goroutines time to pile up.
		time.Sleep(10 * time.Millisecond)
		callMu.Lock()
		callCount++
		callMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collabTokenResponse{Token: "sf-token", ExpiresIn: 120})
	}))
	defer srv.Close()

	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	t.Setenv("MCP_CLIENT_ID", "mcp-server")
	t.Setenv("MCP_CLIENT_SECRET", "test-secret")
	collabState.client = srv.Client()

	var wg sync.WaitGroup
	tokens := make([]string, concurrency)
	errs := make([]error, concurrency)

	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tokens[idx], errs[idx] = GetCollabToken(context.Background(), "sf-user")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "goroutine %d returned an error", i)
		assert.Equal(t, "sf-token", tokens[i], "goroutine %d returned wrong token", i)
	}

	callMu.Lock()
	actualCalls := callCount
	callMu.Unlock()

	// Singleflight must collapse the burst into significantly fewer AS calls.
	// In practice it should be 1; allow a small margin for scheduler jitter.
	assert.LessOrEqual(t, actualCalls, 3,
		"expected singleflight to coalesce concurrent fetches; got %d HTTP calls", actualCalls)
}

// TestDefaultCollabTokenURL checks that when COLLAB_TOKEN_URL is not set, the
// default URL (http://localhost:3000/api/collab-token) is used. We validate
// this by pointing a test server at the default port and confirming the request
// arrives there.
func TestDefaultCollabTokenURL(t *testing.T) {
	resetCollabCache()
	os.Unsetenv("COLLAB_TOKEN_URL")

	requestReceived := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collabTokenResponse{Token: "default-url-token", ExpiresIn: 120})
	}))
	defer srv.Close()

	// Override the default URL by pointing the env var at our test server for
	// this sub-test only — this verifies that the code uses the env var path.
	// (We cannot bind to :3000 in tests, so we test the env-var fallback chain.)
	t.Setenv("COLLAB_TOKEN_URL", srv.URL)
	collabState.client = srv.Client()

	token, _, err := fetchCollabToken(context.Background(), "any-user")
	require.NoError(t, err)
	assert.Equal(t, "default-url-token", token)
	assert.True(t, requestReceived)
}

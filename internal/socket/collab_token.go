package socket

// collabToken.go — fetches and caches collab-audience JWTs from the AS.
//
// The MCP server is a confidential client. Per request (per user), it calls
// POST <COLLAB_TOKEN_URL> with its client_id + client_secret and the acting
// userId, and receives a short-lived JWT (exp=120s) with:
//   iss=AS issuer, aud=COLLAB_RESOURCE_URI, sub=userId
//
// The JWT is cached per userId for (exp - 30s) to avoid a round-trip on every
// socket connection. The collab server validates this JWT via the AS public key.
//
// Env vars consumed (read at call time, not init):
//   COLLAB_TOKEN_URL   URL of the AS collab-token endpoint
//                      (default: http://localhost:3000/api/collab-token)
//   MCP_CLIENT_ID      client_id for this MCP server (default: mcp-server)
//   MCP_CLIENT_SECRET  client_secret for this MCP server (required in prod)
//
// Security: the secret is sent only over the server-to-server channel to the
// AS (localhost in practice). It is never forwarded to the collab server.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// collabCacheEntry holds one cached collab-audience JWT for a single user.
type collabCacheEntry struct {
	token     string
	expiresAt time.Time // when the cached entry should be refreshed
}

// collabTokenState holds the per-user JWT cache, the shared HTTP client, and a
// singleflight group to deduplicate concurrent AS fetches for the same userID.
type collabTokenState struct {
	mu       sync.Mutex
	tokens   map[string]collabCacheEntry
	client   *http.Client
	sfGroup  singleflight.Group
}

var collabState = &collabTokenState{
	tokens: make(map[string]collabCacheEntry),
	client: &http.Client{Timeout: 5 * time.Second},
}

// collabTokenResponse mirrors the JSON body returned by /api/collab-token.
type collabTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// collabFetchResult carries the token and TTL out of the singleflight closure.
type collabFetchResult struct {
	token     string
	expiresIn int
}

// GetCollabToken returns a valid collab-audience JWT for the given userId.
// On cache hit (≥30s remaining), returns the cached token.
// On miss, fetches from the AS and caches the result. Concurrent callers for
// the same userID are coalesced via singleflight so only one AS request fires.
func GetCollabToken(ctx context.Context, userID string) (string, error) {
	collabState.mu.Lock()
	cached, ok := collabState.tokens[userID]
	collabState.mu.Unlock()

	// Keep 30s buffer before expiry so the collab server never receives an
	// already-expired (or about-to-expire) JWT.
	if ok && time.Now().Before(cached.expiresAt.Add(-30*time.Second)) {
		return cached.token, nil
	}

	// WARNING-3 fix: coalesce concurrent fetches for the same user via
	// singleflight so only one AS call fires under burst traffic.
	v, err, _ := collabState.sfGroup.Do(userID, func() (any, error) {
		// Re-check cache inside the singleflight: a parallel goroutine may have
		// just stored a fresh token while we were waiting to enter.
		collabState.mu.Lock()
		recheck, ok := collabState.tokens[userID]
		collabState.mu.Unlock()
		if ok && time.Now().Before(recheck.expiresAt.Add(-30*time.Second)) {
			return &collabFetchResult{token: recheck.token, expiresIn: 0}, nil
		}

		token, expiresIn, err := fetchCollabToken(ctx, userID)
		if err != nil {
			return nil, err
		}

		// Cache with (expiresIn - 30s buffer). If expiresIn ≤ 30 use 1s to force
		// re-fetch immediately on next call.
		cacheTTL := time.Duration(expiresIn)*time.Second - 30*time.Second
		if cacheTTL < time.Second {
			cacheTTL = time.Second
		}

		collabState.mu.Lock()
		collabState.tokens[userID] = collabCacheEntry{
			token:     token,
			expiresAt: time.Now().Add(cacheTTL + 30*time.Second),
		}
		collabState.mu.Unlock()

		return &collabFetchResult{token: token, expiresIn: expiresIn}, nil
	})
	if err != nil {
		return "", err
	}

	return v.(*collabFetchResult).token, nil
}

// fetchCollabToken calls POST COLLAB_TOKEN_URL to obtain a fresh collab JWT.
func fetchCollabToken(ctx context.Context, userID string) (string, int, error) {
	url := os.Getenv("COLLAB_TOKEN_URL")
	if url == "" {
		url = "http://localhost:3000/api/collab-token"
	}
	clientID := os.Getenv("MCP_CLIENT_ID")
	if clientID == "" {
		clientID = "mcp-server"
	}
	clientSecret := os.Getenv("MCP_CLIENT_SECRET")

	body, err := json.Marshal(map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"user_id":       userID,
	})
	if err != nil {
		return "", 0, fmt.Errorf("marshal collab token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("build collab token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := collabState.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("collab-token endpoint returned HTTP %d", resp.StatusCode)
	}

	var result collabTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("decode collab token response: %w", err)
	}
	if result.Token == "" {
		return "", 0, fmt.Errorf("collab-token endpoint returned empty token")
	}

	return result.Token, result.ExpiresIn, nil
}

// flushCollabTokenCache removes the cached token for a user (used in tests and
// on session_expired events where the token may have been invalidated).
func flushCollabTokenCache(userID string) {
	collabState.mu.Lock()
	delete(collabState.tokens, userID)
	collabState.mu.Unlock()
}

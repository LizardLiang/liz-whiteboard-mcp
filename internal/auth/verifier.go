// Package auth — TokenVerifier implementations for the MCP resource server.
//
// Phase 3 (this file): NewJWKSVerifier(issuer, audience string) is the production
// TokenVerifier. It:
//   - Fetches JWKS from {issuer}/.well-known/jwks.json
//   - Caches RS256 public keys (5-min TTL) and refetches on unknown kid (rotation)
//   - Validates RS256 signature, iss == issuer, aud contains audience (RFC 8707),
//     exp/nbf/iat, algorithm == RS256
//   - Reads sub directly as liz-whiteboard User.id (self-hosted AS sets sub=User.id)
//   - Returns *sdkauth.TokenInfo{UserID: sub, Scopes: scope fields, Expiration: exp}
//
// NewStubVerifier() is DEV-ONLY:
//   - INERT/refuses by default in any normal or production run.
//   - Only accepts a single hardcoded developer test token when BOTH:
//       MCP_DEV_AUTH=stub   (explicit opt-in; must never be set in production)
//       MCP_DEV_STUB_TOKEN=<token>   (the specific token to accept)
//   - Returns TokenInfo{UserID: MCP_DEV_USER_ID (or "dev-user" if unset)}.
//
// WARNING: Do NOT set MCP_DEV_AUTH=stub in production or staging environments.
package auth

import (
	"context"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// stubTokenTTL is the lifetime granted to dev stub tokens. Long enough that
// developers aren't constantly refreshing, short enough that it exercises the
// expiry path in tests.
const stubTokenTTL = 24 * time.Hour

// NewStubVerifier returns the Phase-2 DEV-ONLY TokenVerifier.
//
// In a normal (non-dev-stub) run this verifier rejects every token with
// ErrInvalidToken — the middleware will emit 401 for every request.
//
// To enable the stub:
//
//	export MCP_DEV_AUTH=stub
//	export MCP_DEV_STUB_TOKEN=<your-dev-token>
//	export MCP_DEV_USER_ID=<liz-whiteboard-user-id>   # optional, defaults to "dev-user"
//
// Phase 3: use NewJWKSVerifier(issuer, audience string) for production.
// Stub is kept only for local dev (MCP_DEV_AUTH=stub).
func NewStubVerifier() sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		// Safety check #1: stub mode must be explicitly opted into.
		if os.Getenv("MCP_DEV_AUTH") != "stub" {
			// Normal / production path — always refuse.
			return nil, fmt.Errorf("%w: no token verifier configured (Phase 3 not yet implemented)", sdkauth.ErrInvalidToken)
		}

		// Safety check #2: a specific token must be configured.
		stubToken := os.Getenv("MCP_DEV_STUB_TOKEN")
		if stubToken == "" {
			return nil, fmt.Errorf("%w: MCP_DEV_AUTH=stub requires MCP_DEV_STUB_TOKEN to be set", sdkauth.ErrInvalidToken)
		}

		// Constant-time comparison so the dev stub doesn't model a timing-leaky check.
		if subtle.ConstantTimeCompare([]byte(token), []byte(stubToken)) != 1 {
			return nil, fmt.Errorf("%w: token does not match MCP_DEV_STUB_TOKEN", sdkauth.ErrInvalidToken)
		}

		// Resolve the dev user id.
		userID := os.Getenv("MCP_DEV_USER_ID")
		if userID == "" {
			userID = "dev-user"
		}

		return &sdkauth.TokenInfo{
			UserID:     userID,
			Scopes:     []string{"whiteboard"},
			Expiration: time.Now().Add(stubTokenTTL),
		}, nil
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Phase 3: JWKS-backed JWT verifier (production path)
// ──────────────────────────────────────────────────────────────────────────────

// jwksCacheTTL is how long we consider a fetched JWKS to be fresh.
// On cache miss for a kid (possible key rotation) we always refetch immediately.
const jwksCacheTTL = 5 * time.Minute

// jwksEntry holds one parsed RSA public key from a JWKS document.
type jwksEntry struct {
	key *rsa.PublicKey
}

// jwksCache fetches and caches the RS256 public keys from a JWKS endpoint.
// Concurrent-safe via RWMutex.
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]jwksEntry // kid → entry
	fetchedAt time.Time
	jwksURL   string
	client    *http.Client
}

// rawJWKSDocument mirrors the subset of the JWKS JSON we care about.
type rawJWKSDocument struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		N   string `json:"n"` // base64url-encoded modulus
		E   string `json:"e"` // base64url-encoded exponent
	} `json:"keys"`
}

// getKey returns the RSA public key for kid, fetching the JWKS if needed.
//
// Refresh logic:
//   - Cache hit + fresh → return immediately.
//   - Cache miss OR expired → refetch, then try again.
//   - Refetch fails but stale key exists → return stale key (graceful degradation).
//   - Key genuinely not in JWKS → return ErrInvalidToken (unknown kid).
func (c *jwksCache) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	entry, ok := c.keys[kid]
	age := time.Since(c.fetchedAt)
	c.mu.RUnlock()

	if ok && age < jwksCacheTTL {
		return entry.key, nil
	}

	// Cache miss or expired — fetch the JWKS now.
	if err := c.refresh(ctx); err != nil {
		// If we have a stale key for this kid, prefer it over hard-failing the
		// request (e.g., JWKS endpoint is temporarily down).
		if ok {
			return entry.key, nil
		}
		return nil, fmt.Errorf("%w: cannot fetch JWKS: %v", sdkauth.ErrInvalidToken, err)
	}

	c.mu.RLock()
	entry, ok = c.keys[kid]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: unknown signing key kid=%q (not found in JWKS)", sdkauth.ErrInvalidToken, kid)
	}
	return entry.key, nil
}

// refresh downloads and parses the JWKS document, replacing the in-memory cache.
func (c *jwksCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", c.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}

	var doc rawJWKSDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	parsed := make(map[string]jwksEntry, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue // skip non-RSA keys (e.g., EC keys we don't support)
		}
		if k.Use != "" && k.Use != "sig" {
			continue // skip encryption keys
		}
		if k.Alg != "" && k.Alg != "RS256" {
			continue // skip non-RS256 algorithms
		}
		pub, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue // skip malformed keys rather than failing the whole fetch
		}
		parsed[k.Kid] = jwksEntry{key: pub}
	}

	c.mu.Lock()
	c.keys = parsed
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	return nil
}

// parseRSAPublicKey decodes the base64url-encoded modulus (n) and exponent (e)
// from a JWK and returns a *rsa.PublicKey.
func parseRSAPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}
	if len(eBytes) == 0 {
		return nil, fmt.Errorf("empty exponent")
	}

	var eInt int
	for _, b := range eBytes {
		eInt = eInt<<8 | int(b)
	}
	if eInt < 2 {
		return nil, fmt.Errorf("exponent too small: %d", eInt)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: eInt,
	}, nil
}

// NewJWKSVerifier returns a production TokenVerifier that validates RS256 JWTs
// issued by the self-hosted Authorization Server.
//
// Parameters:
//   - issuer:   the AS's issuer URL (must match the iss claim in every token,
//     e.g. "http://localhost:3000" or the public gateway "https://app.example").
//   - audience: the canonical MCP resource URI this RS expects in the aud claim
//     (e.g. "http://localhost:3011/mcp").  MUST match MCP_RESOURCE_URI on the
//     AS side so token audience binding (RFC 8707) is satisfied.
//   - jwksURL:  where to FETCH the JWKS. Normally {issuer}/.well-known/jwks.json,
//     but split-horizon / reverse-proxy deployments (the AS and RS behind one
//     public domain, reached over an internal network) need an internal URL here
//     while `issuer` stays the public value used to validate the iss claim.
//
// Validation performed on each token:
//  1. Algorithm must be RS256 (rejects HS256, ES256, "none", etc.)
//  2. Signature verified against the JWKS public key matching the token's kid.
//  3. iss == issuer (exact string match).
//  4. aud contains audience (RFC 8707 — the AS may include additional audiences;
//     we require our own is present).
//  5. exp not in the past (token not expired).
//  6. nbf not in the future (token not yet valid, if present).
//
// On any failure the error wraps sdkauth.ErrInvalidToken → middleware emits 401.
func NewJWKSVerifier(issuer, audience, jwksURL string) sdkauth.TokenVerifier {
	if jwksURL == "" {
		jwksURL = strings.TrimRight(issuer, "/") + "/.well-known/jwks.json"
	}
	cache := &jwksCache{
		jwksURL: jwksURL,
		client:  &http.Client{Timeout: 10 * time.Second},
		keys:    make(map[string]jwksEntry),
	}

	return func(ctx context.Context, tokenStr string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		// Parse and validate the JWT. The keyfunc is called by the parser to
		// retrieve the public key; we use it to drive the JWKS cache lookup.
		token, err := jwt.Parse(tokenStr,
			func(t *jwt.Token) (interface{}, error) {
				// Reject any algorithm that is not RS256 before touching the key.
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("%w: unexpected signing algorithm %q (require RS256)",
						sdkauth.ErrInvalidToken, t.Header["alg"])
				}
				kid, _ := t.Header["kid"].(string)
				return cache.getKey(ctx, kid)
			},
			jwt.WithIssuer(issuer),
			jwt.WithAudience(audience),
			jwt.WithValidMethods([]string{"RS256"}),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(5*time.Second), // tolerate small clock skew
		)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", sdkauth.ErrInvalidToken, err)
		}
		if !token.Valid {
			return nil, fmt.Errorf("%w: token is not valid", sdkauth.ErrInvalidToken)
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return nil, fmt.Errorf("%w: unexpected claims type", sdkauth.ErrInvalidToken)
		}

		// sub = liz-whiteboard User.id (the AS sets sub = User.id directly).
		sub, _ := claims["sub"].(string)
		if sub == "" {
			return nil, fmt.Errorf("%w: missing or empty sub claim", sdkauth.ErrInvalidToken)
		}

		// scope is a space-delimited string per RFC 6749 §3.3, e.g. "whiteboard".
		scopeStr, _ := claims["scope"].(string)
		var scopes []string
		for _, s := range strings.Fields(scopeStr) {
			if s != "" {
				scopes = append(scopes, s)
			}
		}

		// exp was already validated by jwt.Parse; read it back for TokenInfo.
		expClaim, err := claims.GetExpirationTime()
		if err != nil || expClaim == nil {
			return nil, fmt.Errorf("%w: missing exp claim", sdkauth.ErrInvalidToken)
		}

		return &sdkauth.TokenInfo{
			UserID:     sub,
			Scopes:     scopes,
			Expiration: expClaim.Time,
		}, nil
	}
}

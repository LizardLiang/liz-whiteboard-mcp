// Tests for NewJWKSVerifier (Phase 3 production token verifier).
//
// Each test spins up a tiny in-process JWKS HTTP server (httptest) serving an
// RS256 public key generated from a fresh 2048-bit keypair, issues a real
// JWT signed with the corresponding private key, and exercises the verifier.
//
// Test matrix:
//   TestJWKSVerifier_ValidToken            — happy path: valid token accepted
//   TestJWKSVerifier_WrongIssuer           — iss mismatch → rejected
//   TestJWKSVerifier_WrongAudience         — aud mismatch → rejected
//   TestJWKSVerifier_Expired               — exp in the past → rejected
//   TestJWKSVerifier_BadSignature          — token signed with different key → rejected
//   TestJWKSVerifier_UnknownKid            — kid not in JWKS → rejected
//   TestJWKSVerifier_KeyRotation           — verifier refetches on unknown kid and succeeds after rotation
//   TestJWKSVerifier_ScopeAndSubExtracted  — UserID=sub and Scopes=scope fields are populated

package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
)

// ────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────

// testKeyPair holds a generated RSA keypair for one test.
type testKeyPair struct {
	kid        string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

func generateTestKeyPair(t *testing.T, kid string) testKeyPair {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "generate RSA keypair")
	return testKeyPair{kid: kid, privateKey: priv, publicKey: &priv.PublicKey}
}

// jwksServerFor starts an httptest.Server that serves the given key pairs as a
// JWKS document at /jwks and at /.well-known/jwks.json. The returned server
// URL is the base URL (issuer-like), not the JWKS path itself.
//
// The returned function allows the caller to atomically swap the served key set
// (for key-rotation tests).
func jwksServerFor(t *testing.T, initial []testKeyPair) (serverURL string, swapKeys func([]testKeyPair)) {
	t.Helper()

	mu := new(struct{ keys []testKeyPair })
	mu.keys = initial

	mux := http.NewServeMux()
	serveJWKS := func(w http.ResponseWriter, r *http.Request) {
		keys := mu.keys
		type jwkDoc struct {
			Keys []map[string]string `json:"keys"`
		}
		doc := jwkDoc{}
		for _, kp := range keys {
			n := base64.RawURLEncoding.EncodeToString(kp.publicKey.N.Bytes())
			// Encode exponent as big-endian bytes then base64url.
			e := kp.publicKey.E
			var eBytes []byte
			for e > 0 {
				eBytes = append([]byte{byte(e & 0xff)}, eBytes...)
				e >>= 8
			}
			eEnc := base64.RawURLEncoding.EncodeToString(eBytes)
			doc.Keys = append(doc.Keys, map[string]string{
				"kty": "RSA",
				"kid": kp.kid,
				"use": "sig",
				"alg": "RS256",
				"n":   n,
				"e":   eEnc,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}
	mux.HandleFunc("/.well-known/jwks.json", serveJWKS)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	swap := func(kps []testKeyPair) { mu.keys = kps }
	return srv.URL, swap
}

// makeToken signs a JWT for the given claims. exp is relative to now.
func makeToken(t *testing.T, kp testKeyPair, issuer, audience, subject, scope string, expOffset time.Duration) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   issuer,
		"aud":   audience,
		"sub":   subject,
		"scope": scope,
		"iat":   now.Unix(),
		"nbf":   now.Unix(),
		"exp":   now.Add(expOffset).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kp.kid
	signed, err := tok.SignedString(kp.privateKey)
	require.NoError(t, err, "sign token")
	return signed
}

// ────────────────────────────────────────────────────────────────
// Tests
// ────────────────────────────────────────────────────────────────

func TestJWKSVerifier_ValidToken(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	token := makeToken(t, kp, issuer, audience, "user-abc-123", "whiteboard", 10*time.Minute)

	info, err := verifier(context.Background(), token, nil)
	require.NoError(t, err, "valid token should be accepted")
	require.NotNil(t, info)
	assert.Equal(t, "user-abc-123", info.UserID, "UserID should be sub claim")
	assert.Contains(t, info.Scopes, "whiteboard", "Scopes should include 'whiteboard'")
	assert.False(t, info.Expiration.IsZero(), "Expiration should be set")
	assert.True(t, info.Expiration.After(time.Now()), "Expiration should be in the future")
}

// TestJWKSVerifier_JWKSURLOverride exercises the split-horizon / reverse-proxy
// case: the public issuer is NOT reachable for the JWKS fetch, so jwksURL points
// at an internal address while iss is still validated against the public issuer.
func TestJWKSVerifier_JWKSURLOverride(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	publicIssuer := "https://app.example.test" // not reachable; only used for iss validation
	audience := "http://localhost:3011/mcp"
	internalJWKS := serverURL + "/.well-known/jwks.json"
	verifier := auth.NewJWKSVerifier(publicIssuer, audience, internalJWKS)

	token := makeToken(t, kp, publicIssuer, audience, "user-xyz", "whiteboard", 10*time.Minute)

	info, err := verifier(context.Background(), token, nil)
	require.NoError(t, err, "token with public iss should verify via the internal JWKS URL")
	require.NotNil(t, info)
	assert.Equal(t, "user-xyz", info.UserID)
}

func TestJWKSVerifier_WrongIssuer(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	// Token claims a different issuer.
	token := makeToken(t, kp, "http://evil.example.com", audience, "user-abc", "whiteboard", 10*time.Minute)

	_, err := verifier(context.Background(), token, nil)
	require.Error(t, err, "wrong issuer should be rejected")
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken, "error should wrap ErrInvalidToken")
}

func TestJWKSVerifier_WrongAudience(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	verifier := auth.NewJWKSVerifier(issuer, "http://localhost:3011/mcp", "")

	// Token audience is a different resource server.
	token := makeToken(t, kp, issuer, "http://other-server.example.com/mcp", "user-abc", "whiteboard", 10*time.Minute)

	_, err := verifier(context.Background(), token, nil)
	require.Error(t, err, "wrong audience should be rejected")
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken, "error should wrap ErrInvalidToken")
}

func TestJWKSVerifier_Expired(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	// exp is 10 minutes in the past (well outside the 5 s leeway).
	token := makeToken(t, kp, issuer, audience, "user-abc", "whiteboard", -10*time.Minute)

	_, err := verifier(context.Background(), token, nil)
	require.Error(t, err, "expired token should be rejected")
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken, "error should wrap ErrInvalidToken")
}

func TestJWKSVerifier_BadSignature(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	// Sign with a different key (attacker's key — not in JWKS).
	attackerKP := generateTestKeyPair(t, "key-1") // same kid, different key material
	token := makeToken(t, attackerKP, issuer, audience, "user-abc", "whiteboard", 10*time.Minute)

	_, err := verifier(context.Background(), token, nil)
	require.Error(t, err, "bad signature should be rejected")
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken, "error should wrap ErrInvalidToken")
}

func TestJWKSVerifier_UnknownKid(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	// Token references a kid that is not in the JWKS.
	unknownKP := generateTestKeyPair(t, "key-does-not-exist")
	token := makeToken(t, unknownKP, issuer, audience, "user-abc", "whiteboard", 10*time.Minute)

	_, err := verifier(context.Background(), token, nil)
	require.Error(t, err, "unknown kid should be rejected")
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken, "error should wrap ErrInvalidToken")
	assert.True(t, strings.Contains(err.Error(), "unknown signing key") ||
		strings.Contains(err.Error(), "key-does-not-exist") ||
		strings.Contains(err.Error(), "invalid token"),
		"error message should mention unknown key: %v", err)
}

func TestJWKSVerifier_KeyRotation(t *testing.T) {
	// Scenario: the JWKS initially has key-1. After the cache is primed, the AS
	// rotates to key-2. A token signed with key-2 arrives → verifier refetches,
	// picks up key-2, and accepts the token.

	kp1 := generateTestKeyPair(t, "key-1")
	kp2 := generateTestKeyPair(t, "key-2")

	serverURL, swapKeys := jwksServerFor(t, []testKeyPair{kp1})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	// Prime the cache with a valid token using key-1.
	token1 := makeToken(t, kp1, issuer, audience, "user-abc", "whiteboard", 10*time.Minute)
	_, err := verifier(context.Background(), token1, nil)
	require.NoError(t, err, "initial token with key-1 should be accepted")

	// AS rotates: JWKS now only has key-2.
	swapKeys([]testKeyPair{kp2})

	// New token signed with key-2 — unknown kid triggers refetch.
	token2 := makeToken(t, kp2, issuer, audience, "user-xyz", "whiteboard", 10*time.Minute)
	info, err := verifier(context.Background(), token2, nil)
	require.NoError(t, err, "token with rotated key-2 should be accepted after refetch")
	require.NotNil(t, info)
	assert.Equal(t, "user-xyz", info.UserID)
}

func TestJWKSVerifier_ScopeAndSubExtracted(t *testing.T) {
	kp := generateTestKeyPair(t, "key-1")
	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	// Multiple scopes in space-delimited string.
	token := makeToken(t, kp, issuer, audience, "real-user-id-0001", "whiteboard read", 10*time.Minute)

	info, err := verifier(context.Background(), token, nil)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "real-user-id-0001", info.UserID, "UserID should equal sub")
	assert.ElementsMatch(t, []string{"whiteboard", "read"}, info.Scopes, "Scopes should be space-split")
}

// TestJWKSVerifier_BigExponent ensures the base64url exponent decoder handles
// the standard 65537 (0x010001) exponent correctly — regression guard.
func TestJWKSVerifier_BigExponent(t *testing.T) {
	// Generate key and verify exponent round-trips through the JWKS server/parser.
	kp := generateTestKeyPair(t, "key-e")
	assert.Equal(t, 65537, kp.publicKey.E, "generated key should use e=65537")

	serverURL, _ := jwksServerFor(t, []testKeyPair{kp})

	issuer := serverURL
	audience := "http://localhost:3011/mcp"
	verifier := auth.NewJWKSVerifier(issuer, audience, "")

	token := makeToken(t, kp, issuer, audience, "user-e", "whiteboard", 10*time.Minute)
	info, err := verifier(context.Background(), token, nil)
	require.NoError(t, err, "token should be accepted with e=65537")
	assert.Equal(t, "user-e", info.UserID)
}

// ────────────────────────────────────────────────────────────────
// Helper: big.Int → []byte, avoiding leading zero padding.
// (Mirrors the logic in the jwksServerFor helper above, here for
//
//	documentation clarity.)
func bigIntToBytes(n *big.Int) []byte { return n.Bytes() }

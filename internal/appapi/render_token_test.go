package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

func serveRenderTokenAPI(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("LIZ_RENDER_TOKEN_URL", srv.URL)
	prev := client
	client = srv.Client()
	t.Cleanup(func() { client = prev })
}

func TestMintRenderToken_PostsKindAndBoardWithCollabBearer(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var gotAuth string
	var gotBody map[string]string
	serveRenderTokenAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "render-tok", "expiresAt": 1900000000})
	})

	tok, err := MintRenderToken(context.Background(), "user-1", RenderKindCanvas, testBoardID, "nf")
	require.NoError(t, err)
	assert.Equal(t, "Bearer collab-jwt", gotAuth)
	assert.Equal(t, map[string]string{"kind": "canvas", "boardId": testBoardID}, gotBody)
	assert.Equal(t, "render-tok", tok.Token)
	assert.Equal(t, int64(1900000000), tok.ExpiresAt.Unix())
}

func TestMintRenderToken_StatusMapping(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	cases := []struct {
		status int
		code   mcperr.Code
		msg    string
	}{
		{http.StatusUnauthorized, mcperr.SessionExpired, ""},
		{http.StatusNotFound, mcperr.NotFound, "Whiteboard X not found."},
		{http.StatusTooManyRequests, mcperr.ConnectionError, "Retry in 17 seconds"},
		{http.StatusInternalServerError, mcperr.ConnectionError, "HTTP 500"},
	}
	for _, tc := range cases {
		serveRenderTokenAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "17")
			w.WriteHeader(tc.status)
		})
		_, err := MintRenderToken(context.Background(), "u", RenderKindWhiteboard, testBoardID, "Whiteboard X not found.")
		var me *mcperr.McpError
		require.ErrorAs(t, err, &me, "status %d", tc.status)
		assert.Equal(t, tc.code, me.Code)
		assert.Contains(t, me.Message, tc.msg)
	}
}

func TestMintRenderToken_UnreadableBody(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveRenderTokenAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"nope":1}`))
	})
	_, err := MintRenderToken(context.Background(), "u", RenderKindWhiteboard, testBoardID, "nf")
	require.Error(t, err)
}

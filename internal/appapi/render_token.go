package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// defaultRenderTokenURL mirrors the other route defaults so a local dev run
// needs no extra configuration.
const defaultRenderTokenURL = "http://localhost:3000/api/render-token"

// Board kinds the render-token route accepts.
const (
	RenderKindWhiteboard = "whiteboard"
	RenderKindCanvas     = "canvas"
)

// RenderToken is a short-lived credential that opens one render page.
type RenderToken struct {
	Token     string
	ExpiresAt time.Time
}

// renderTokenURL resolves the route URL at call time.
func renderTokenURL() string {
	if url := os.Getenv("LIZ_RENDER_TOKEN_URL"); url != "" {
		return url
	}
	return defaultRenderTokenURL
}

// MintRenderToken asks the app for a render token for one board.
//
// The request carries the collab-audience JWT, never the client's MCP access
// token (see the package comment). The app re-checks the VIEWER role itself.
// notFoundMessage is the exact error text the caller's read tool returns for a
// missing board, so a board the user cannot see looks the same here as there.
func MintRenderToken(ctx context.Context, userID, kind, boardID, notFoundMessage string) (RenderToken, error) {
	token, err := getToken(ctx, userID)
	if err != nil {
		return RenderToken{}, mcperr.New(mcperr.ConnectionError,
			"Could not obtain a collaboration credential from the liz-whiteboard app. "+
				"Check that the app is running and that MCP_CLIENT_SECRET matches the app's.")
	}

	payload, err := json.Marshal(map[string]string{"kind": kind, "boardId": boardID})
	if err != nil {
		return RenderToken{}, mcperr.New(mcperr.InternalError, "Could not encode the render token request.")
	}

	url := renderTokenURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return RenderToken{}, mcperr.New(mcperr.InternalError, "Could not build the render token request.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return RenderToken{}, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("Cannot reach the liz-whiteboard app at %s. "+
				"Start the app, or set LIZ_RENDER_TOKEN_URL to its address.", url))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return RenderToken{}, mcperr.New(mcperr.SessionExpired,
			"The liz-whiteboard app rejected this server's collaboration credential. "+
				"Check that MCP_CLIENT_SECRET, OAUTH_ISSUER, and COLLAB_RESOURCE_URI match the app's values.")
	case resp.StatusCode == http.StatusNotFound:
		return RenderToken{}, mcperr.New(mcperr.NotFound, notFoundMessage)
	case resp.StatusCode == http.StatusTooManyRequests:
		wait := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if wait == "" {
			wait = "60"
		}
		return RenderToken{}, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The liz-whiteboard app is rate limiting board images. Retry in %s seconds.", wait))
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return RenderToken{}, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The liz-whiteboard app at %s returned HTTP %d for a render token request.", url, resp.StatusCode))
	}

	var decoded struct {
		Token     string  `json:"token"`
		ExpiresAt float64 `json:"expiresAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil || decoded.Token == "" || decoded.ExpiresAt <= 0 {
		return RenderToken{}, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The liz-whiteboard app at %s returned an unreadable render token response.", url))
	}
	return RenderToken{Token: decoded.Token, ExpiresAt: time.Unix(int64(decoded.ExpiresAt), 0)}, nil
}

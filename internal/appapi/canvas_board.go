// Package appapi is the HTTP client for the liz-whiteboard app's
// server-to-server routes.
//
// It exists for exactly one thing the Socket.IO path cannot do: canvas board
// LIFECYCLE. Element writes ride the collaboration socket because open browser
// clients must re-render without a reload. Board creation has no co-viewing
// client to broadcast to — the navigator reads the board list on load, not
// through a subscription — so a socket namespace whose only job is board
// lifecycle would broadcast into an empty room and add a third handshake to
// maintain. The app therefore exposes POST /api/canvas-boards, and this package
// calls it.
//
// CREDENTIAL. Every call authenticates with the collab-audience JWT minted by
// internal/collabtoken, the same credential the socket dial uses. The client's
// own MCP access token (aud = the MCP resource URI) is NEVER forwarded: that is
// the confused-deputy hole /api/collab-token was built to close, and the app's
// route validates the collab audience through the very same verifier the socket
// handshake uses, so the two paths cannot drift.
//
// Env vars consumed (read at call time, not init):
//
//	LIZ_CANVAS_BOARD_API_URL  URL of the app's canvas-board route
//	                          (default: http://localhost:3000/api/canvas-boards)
//
// Plus everything internal/collabtoken reads to mint the JWT.
package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/collabtoken"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// defaultCanvasBoardAPIURL mirrors COLLAB_TOKEN_URL's localhost default so a
// local dev run needs no extra configuration.
const defaultCanvasBoardAPIURL = "http://localhost:3000/api/canvas-boards"

// client carries the same 5s timeout as the collab-token client. Package-level
// so tests can point it at an httptest server, matching collab_token.go.
var client = &http.Client{Timeout: 5 * time.Second}

// getToken is indirected so tests can run without an Authorization Server.
var getToken = collabtoken.Get

// canvasBoardAPIURL resolves the route URL at call time.
func canvasBoardAPIURL() string {
	if url := os.Getenv("LIZ_CANVAS_BOARD_API_URL"); url != "" {
		return url
	}
	return defaultCanvasBoardAPIURL
}

// CreateCanvasBoardRequest is the create arm of the route's discriminated union.
type CreateCanvasBoardRequest struct {
	ProjectID string
	Name      string
	// FolderID places the board in a folder. Absent leaves it at the project root.
	FolderID *string
}

// UpdateCanvasBoardRequest is the update arm. Both fields are optional; absent
// fields are omitted from the request so the app leaves them untouched.
type UpdateCanvasBoardRequest struct {
	Name     *string
	FolderID *string
}

// CreateCanvasBoard creates a canvas board in a project and returns the new row.
//
// The route re-checks EDITOR+ on the project independently of the caller's own
// gate. Both layers stay: the MCP check gives a clean error without a round
// trip, the route's check is the one that actually protects the data.
func CreateCanvasBoard(ctx context.Context, userID string, req CreateCanvasBoardRequest) (map[string]any, error) {
	body := map[string]any{
		"op":        "create",
		"projectId": req.ProjectID,
		"name":      req.Name,
	}
	if req.FolderID != nil {
		body["folderId"] = *req.FolderID
	}
	return post(ctx, userID, body, "The project was not found.")
}

// UpdateCanvasBoard renames or re-files a canvas board and returns the new row.
func UpdateCanvasBoard(ctx context.Context, userID, canvasBoardID string, req UpdateCanvasBoardRequest) (map[string]any, error) {
	body := map[string]any{
		"op":            "update",
		"canvasBoardId": canvasBoardID,
	}
	if req.Name != nil {
		body["name"] = *req.Name
	}
	if req.FolderID != nil {
		body["folderId"] = *req.FolderID
	}
	return post(ctx, userID, body, fmt.Sprintf("Canvas board %s not found.", canvasBoardID))
}

// DeleteCanvasBoard deletes a canvas board and returns the removed row.
//
// The delete cascades every CanvasElement and CanvasBoardShareLink on the board.
// The confirmName guard that makes this safe to expose to an agent lives in the
// tool layer, not here — this function is the transport and performs no
// confirmation of its own.
func DeleteCanvasBoard(ctx context.Context, userID, canvasBoardID string) (map[string]any, error) {
	body := map[string]any{
		"op":            "delete",
		"canvasBoardId": canvasBoardID,
	}
	return post(ctx, userID, body, fmt.Sprintf("Canvas board %s not found.", canvasBoardID))
}

// canvasBoardResponse is the route's success envelope.
type canvasBoardResponse struct {
	Board map[string]any `json:"board"`
}

// post sends one operation to the route and maps the reply onto the MCP error
// taxonomy. The response body is never echoed into an error: it is attacker- or
// misconfiguration-shaped text that can contain the request's own headers.
func post(ctx context.Context, userID string, body map[string]any, notFoundMessage string) (map[string]any, error) {
	token, err := getToken(ctx, userID)
	if err != nil {
		return nil, mcperr.New(mcperr.ConnectionError,
			"Could not obtain a collaboration credential from the liz-whiteboard app. "+
				"Check that the app is running and that MCP_CLIENT_SECRET matches the app's.")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, mcperr.New(mcperr.InternalError, "Could not encode the canvas board request.")
	}

	url := canvasBoardAPIURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, mcperr.New(mcperr.InternalError, "Could not build the canvas board request.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("Cannot reach the liz-whiteboard app at %s. "+
				"Start the app, or set LIZ_CANVAS_BOARD_API_URL to its address.", url))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, statusError(resp.StatusCode, url, notFoundMessage)
	}

	var decoded canvasBoardResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil || decoded.Board == nil {
		return nil, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The liz-whiteboard app at %s returned an unreadable canvas board response.", url))
	}
	return decoded.Board, nil
}

// statusError maps one HTTP status onto the MCP error taxonomy.
//
// 401 is SESSION_EXPIRED rather than FORBIDDEN on purpose: it means the
// collab-audience JWT was rejected, which is a credential problem on this
// server's side, not a permission problem on the user's.
func statusError(status int, url, notFoundMessage string) error {
	switch status {
	case http.StatusUnauthorized:
		return mcperr.New(mcperr.SessionExpired,
			"The liz-whiteboard app rejected this server's collaboration credential. "+
				"Check that MCP_CLIENT_SECRET, OAUTH_ISSUER, and COLLAB_RESOURCE_URI match the app's values.")
	case http.StatusForbidden:
		return mcperr.New(mcperr.Forbidden,
			"You need the EDITOR role or higher on this project to create, rename, or delete a canvas board.")
	case http.StatusNotFound:
		return mcperr.New(mcperr.NotFound, notFoundMessage)
	default:
		return mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The liz-whiteboard app at %s returned HTTP %d for a canvas board request.", url, status))
	}
}

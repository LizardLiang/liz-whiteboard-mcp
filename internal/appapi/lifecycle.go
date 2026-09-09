// Project, folder and ER whiteboard lifecycle, over the app's
// POST /api/mcp-lifecycle route.
//
// WHY A SECOND ROUTE CLIENT AND NOT AN EXTENSION OF canvas_board.go:
// /api/canvas-boards and /api/mcp-lifecycle are two routes with two request
// shapes. canvas-boards keys on `op` alone and answers with `{board}`;
// mcp-lifecycle keys on `{entity, op}` and answers with `{project}`,
// `{folder}` or `{whiteboard}`. The transport, the credential and the status
// mapping are identical, so those are shared below; the envelopes are not.
//
// The FORBIDDEN text differs per operation because the required role does:
// renaming a project needs ADMIN, deleting one needs OWNER, and everything else
// needs EDITOR. A single "you need EDITOR" message would send an agent to fix
// the wrong thing.
//
// Env vars consumed (read at call time, not init):
//
//	LIZ_MCP_LIFECYCLE_API_URL  URL of the app's lifecycle route
//	                           (default: http://localhost:3000/api/mcp-lifecycle)
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

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// defaultLifecycleAPIURL mirrors COLLAB_TOKEN_URL's localhost default so a
// local dev run needs no extra configuration.
const defaultLifecycleAPIURL = "http://localhost:3000/api/mcp-lifecycle"

// lifecycleAPIURL resolves the route URL at call time.
func lifecycleAPIURL() string {
	if url := os.Getenv("LIZ_MCP_LIFECYCLE_API_URL"); url != "" {
		return url
	}
	return defaultLifecycleAPIURL
}

// Role messages, one per gate the route enforces. They name the role the app
// actually checks, so a denied agent is told the right thing to fix.
const (
	forbiddenEditor = "You need the EDITOR role or higher on this project to perform this action."
	forbiddenAdmin  = "You need the ADMIN role or higher on this project to change its name or description."
	forbiddenOwner  = "Only the project owner can delete a project."
)

// ── Request types ────────────────────────────────────────────────────────────

// CreateProjectRequest creates a project owned by the acting user. There is no
// ownerId field: the route takes the owner from the JWT subject and ignores any
// ownerId in the body.
type CreateProjectRequest struct {
	Name        string
	Description *string
}

// UpdateProjectRequest renames a project or changes its description. Both are
// optional; absent fields are omitted so the app leaves them untouched.
type UpdateProjectRequest struct {
	Name        *string
	Description *string
}

// CreateFolderRequest creates a folder, optionally nested under another.
type CreateFolderRequest struct {
	ProjectID string
	Name      string
	// ParentFolderID nests the folder. Absent places it at the project root.
	ParentFolderID *string
}

// CreateWhiteboardRequest creates an ER diagram whiteboard.
type CreateWhiteboardRequest struct {
	ProjectID string
	Name      string
	// FolderID files the board in a folder. Absent leaves it at the root.
	FolderID *string
}

// UpdateWhiteboardRequest renames or re-files an ER whiteboard. The route does
// not accept a projectId here: moving a board across projects would check the
// role on the source and write into the destination.
type UpdateWhiteboardRequest struct {
	Name     *string
	FolderID *string
}

// ── Project ──────────────────────────────────────────────────────────────────

// CreateProject creates a project owned by the acting user and returns the row.
func CreateProject(ctx context.Context, userID string, req CreateProjectRequest) (map[string]any, error) {
	body := map[string]any{
		"entity": "project",
		"op":     "create",
		"name":   req.Name,
	}
	if req.Description != nil {
		body["description"] = *req.Description
	}
	// A create names no existing row, so the route answers 403, never 404.
	return postLifecycle(ctx, userID, body, "project", "", forbiddenEditor)
}

// UpdateProject renames a project or changes its description. Requires ADMIN+.
func UpdateProject(ctx context.Context, userID, projectID string, req UpdateProjectRequest) (map[string]any, error) {
	body := map[string]any{
		"entity":    "project",
		"op":        "update",
		"projectId": projectID,
	}
	if req.Name != nil {
		body["name"] = *req.Name
	}
	if req.Description != nil {
		body["description"] = *req.Description
	}
	return postLifecycle(ctx, userID, body, "project",
		fmt.Sprintf("Project %s not found.", projectID), forbiddenAdmin)
}

// DeleteProject deletes a project and returns the removed row.
//
// The delete cascades every folder, whiteboard, canvas board, table, column and
// relationship in the project. The confirmName guard that makes this safe to
// expose to an agent lives in the tool layer, not here — this function is the
// transport and performs no confirmation of its own.
func DeleteProject(ctx context.Context, userID, projectID string) (map[string]any, error) {
	body := map[string]any{
		"entity":    "project",
		"op":        "delete",
		"projectId": projectID,
	}
	return postLifecycle(ctx, userID, body, "project",
		fmt.Sprintf("Project %s not found.", projectID), forbiddenOwner)
}

// ── Folder ───────────────────────────────────────────────────────────────────

// CreateFolder creates a folder in a project and returns the new row.
func CreateFolder(ctx context.Context, userID string, req CreateFolderRequest) (map[string]any, error) {
	body := map[string]any{
		"entity":    "folder",
		"op":        "create",
		"projectId": req.ProjectID,
		"name":      req.Name,
	}
	if req.ParentFolderID != nil {
		body["parentFolderId"] = *req.ParentFolderID
	}
	return postLifecycle(ctx, userID, body, "folder", "", forbiddenEditor)
}

// UpdateFolder renames a folder. The app exposes no re-parenting, so neither
// does this.
func UpdateFolder(ctx context.Context, userID, folderID, name string) (map[string]any, error) {
	body := map[string]any{
		"entity":   "folder",
		"op":       "update",
		"folderId": folderID,
		"name":     name,
	}
	return postLifecycle(ctx, userID, body, "folder",
		fmt.Sprintf("Folder %s not found.", folderID), forbiddenEditor)
}

// DeleteFolder deletes a folder and returns the removed row.
//
// The delete cascades child folders AND every whiteboard filed in them, because
// Whiteboard.folderId is ON DELETE CASCADE. As with DeleteProject, the
// confirmName guard lives in the tool layer.
func DeleteFolder(ctx context.Context, userID, folderID string) (map[string]any, error) {
	body := map[string]any{
		"entity":   "folder",
		"op":       "delete",
		"folderId": folderID,
	}
	return postLifecycle(ctx, userID, body, "folder",
		fmt.Sprintf("Folder %s not found.", folderID), forbiddenEditor)
}

// ── Whiteboard ───────────────────────────────────────────────────────────────

// CreateWhiteboard creates an ER diagram whiteboard and returns the new row.
func CreateWhiteboard(ctx context.Context, userID string, req CreateWhiteboardRequest) (map[string]any, error) {
	body := map[string]any{
		"entity":    "whiteboard",
		"op":        "create",
		"projectId": req.ProjectID,
		"name":      req.Name,
	}
	if req.FolderID != nil {
		body["folderId"] = *req.FolderID
	}
	return postLifecycle(ctx, userID, body, "whiteboard", "", forbiddenEditor)
}

// UpdateWhiteboard renames or re-files an ER whiteboard.
func UpdateWhiteboard(ctx context.Context, userID, whiteboardID string, req UpdateWhiteboardRequest) (map[string]any, error) {
	body := map[string]any{
		"entity":       "whiteboard",
		"op":           "update",
		"whiteboardId": whiteboardID,
	}
	if req.Name != nil {
		body["name"] = *req.Name
	}
	if req.FolderID != nil {
		body["folderId"] = *req.FolderID
	}
	return postLifecycle(ctx, userID, body, "whiteboard",
		fmt.Sprintf("Whiteboard %s not found.", whiteboardID), forbiddenEditor)
}

// DeleteWhiteboard deletes an ER whiteboard and returns the removed row.
//
// The delete cascades its tables, columns and relationships. The confirmName
// guard lives in the tool layer.
func DeleteWhiteboard(ctx context.Context, userID, whiteboardID string) (map[string]any, error) {
	body := map[string]any{
		"entity":       "whiteboard",
		"op":           "delete",
		"whiteboardId": whiteboardID,
	}
	return postLifecycle(ctx, userID, body, "whiteboard",
		fmt.Sprintf("Whiteboard %s not found.", whiteboardID), forbiddenEditor)
}

// ── Transport ────────────────────────────────────────────────────────────────

// postLifecycle sends one operation to the route and maps the reply onto the
// MCP error taxonomy.
//
// resultKey names the field the route wraps the row in ("project", "folder" or
// "whiteboard"). notFoundMessage is empty for create operations, which address
// no existing row and so never draw a 404 — an empty message would only ever be
// reachable through a route bug, and reports as such rather than pretending a
// named row was missing.
//
// The response body is never echoed into an error: it is attacker- or
// misconfiguration-shaped text that can contain the request's own headers.
func postLifecycle(
	ctx context.Context,
	userID string,
	body map[string]any,
	resultKey string,
	notFoundMessage string,
	forbiddenMessage string,
) (map[string]any, error) {
	token, err := getToken(ctx, userID)
	if err != nil {
		return nil, mcperr.New(mcperr.ConnectionError,
			"Could not obtain a collaboration credential from the liz-whiteboard app. "+
				"Check that the app is running and that MCP_CLIENT_SECRET matches the app's.")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, mcperr.New(mcperr.InternalError, "Could not encode the lifecycle request.")
	}

	url := lifecycleAPIURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, mcperr.New(mcperr.InternalError, "Could not build the lifecycle request.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("Cannot reach the liz-whiteboard app at %s. "+
				"Start the app, or set LIZ_MCP_LIFECYCLE_API_URL to its address.", url))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, lifecycleStatusError(resp.StatusCode, url, notFoundMessage, forbiddenMessage)
	}

	var decoded map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, unreadableLifecycleResponse(url)
	}
	raw, ok := decoded[resultKey]
	if !ok {
		return nil, unreadableLifecycleResponse(url)
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil || row == nil {
		return nil, unreadableLifecycleResponse(url)
	}
	return row, nil
}

func unreadableLifecycleResponse(url string) error {
	return mcperr.New(mcperr.ConnectionError,
		fmt.Sprintf("The liz-whiteboard app at %s returned an unreadable lifecycle response.", url))
}

// lifecycleStatusError maps one HTTP status onto the MCP error taxonomy.
//
// 401 is SESSION_EXPIRED rather than FORBIDDEN on purpose: it means the
// collab-audience JWT was rejected, which is a credential problem on this
// server's side, not a permission problem on the user's.
//
// 429 is surfaced distinctly because the route rate-limits at 15 requests per
// 60 seconds per IP, and an agent looping over a batch of boards will hit it.
// Reporting that as a generic connection failure would send it to restart the
// app instead of waiting.
func lifecycleStatusError(status int, url, notFoundMessage, forbiddenMessage string) error {
	switch status {
	case http.StatusUnauthorized:
		return mcperr.New(mcperr.SessionExpired,
			"The liz-whiteboard app rejected this server's collaboration credential. "+
				"Check that MCP_CLIENT_SECRET, OAUTH_ISSUER, and COLLAB_RESOURCE_URI match the app's values.")
	case http.StatusForbidden:
		return mcperr.New(mcperr.Forbidden, forbiddenMessage)
	case http.StatusNotFound:
		if notFoundMessage == "" {
			// Unreachable for a create through this client; a route that answers
			// 404 anyway is misconfigured, and saying so beats inventing a row.
			return mcperr.New(mcperr.ConnectionError,
				fmt.Sprintf("The liz-whiteboard app at %s answered 404 for an operation that names no existing row.", url))
		}
		return mcperr.New(mcperr.NotFound, notFoundMessage)
	case http.StatusTooManyRequests:
		return mcperr.New(mcperr.ConnectionError,
			"The liz-whiteboard app is rate limiting this server (15 lifecycle requests per minute). "+
				"Wait 60 seconds and retry.")
	default:
		return mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The liz-whiteboard app at %s returned HTTP %d for a lifecycle request.", url, status))
	}
}

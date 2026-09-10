// Cross-file table reference lifecycle, over the app's
// POST /api/table-references route. LizMeter #83.
//
// WHY A THIRD ROUTE CLIENT:
// /api/table-references is its own route for the same reason lifecycle.go is
// not an extension of canvas_board.go — the request shape differs. A reference
// is addressed by the board it lives on PLUS the source it points at, not by a
// single entity id, and its `list` op returns an array rather than one row. The
// transport, the credential and the status mapping are shared with lifecycle.go
// through postReference below; only the envelope differs.
//
// EVERY OPERATION NEEDS EDITOR except list, which needs VIEWER — so there is
// one forbidden message for writes and one for reads, rather than the three
// tiers lifecycle.go carries.
//
// Env vars consumed (read at call time, not init):
//
//	LIZ_MCP_TABLE_REFERENCE_API_URL  URL of the app's reference route
//	                                 (default: http://localhost:3000/api/table-references)
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

// defaultTableReferenceAPIURL mirrors the other clients' localhost default.
const defaultTableReferenceAPIURL = "http://localhost:3000/api/table-references"

// tableReferenceAPIURL resolves the route URL at call time.
func tableReferenceAPIURL() string {
	if url := os.Getenv("LIZ_MCP_TABLE_REFERENCE_API_URL"); url != "" {
		return url
	}
	return defaultTableReferenceAPIURL
}

const (
	forbiddenReferenceWrite = "You need the EDITOR role or higher on this project to change table references."
	forbiddenReferenceRead  = "You need access to this project to read its table references."
)

// ── Request types ────────────────────────────────────────────────────────────

// CreateTableReferenceRequest places a reference to a table in another
// whiteboard of the SAME project.
type CreateTableReferenceRequest struct {
	// WhiteboardID is the board the reference node is placed on.
	WhiteboardID string
	// SourceWhiteboardID is the board that owns the referenced table.
	SourceWhiteboardID string
	SourceTableID      string
	// SourceColumnIDs are the columns to expose as connectable handles.
	SourceColumnIDs []string
	// PositionX/PositionY place the node. Absent leaves it unpositioned, and
	// the first browser client to measure it assigns a spot.
	PositionX *float64
	PositionY *float64
}

// UpdateTableReferenceRequest re-targets an existing reference. Every field is
// optional; absent fields leave that part of the target unchanged.
type UpdateTableReferenceRequest struct {
	SourceWhiteboardID *string
	SourceTableID      *string
	SourceColumnIDs    []string
}

// UpdateTableReferenceResult carries the re-targeted reference plus how many
// relationships the change destroyed, so the tool can report it rather than
// leaving the agent to discover the loss later.
type UpdateTableReferenceResult struct {
	Reference            map[string]any
	DeletedRelationships int
}

// ── Operations ───────────────────────────────────────────────────────────────

// CreateTableReference creates a reference node and returns it.
//
// Same-project scope is enforced by the app, not here: this client cannot see
// which project either board belongs to.
func CreateTableReference(ctx context.Context, userID string, req CreateTableReferenceRequest) (map[string]any, error) {
	body := map[string]any{
		"op":                 "create",
		"whiteboardId":       req.WhiteboardID,
		"sourceWhiteboardId": req.SourceWhiteboardID,
		"sourceTableId":      req.SourceTableID,
		"sourceColumnIds":    req.SourceColumnIDs,
	}
	if req.PositionX != nil {
		body["positionX"] = *req.PositionX
	}
	if req.PositionY != nil {
		body["positionY"] = *req.PositionY
	}
	decoded, err := postReference(ctx, userID, body, "", forbiddenReferenceWrite)
	if err != nil {
		return nil, err
	}
	return decodeReference(decoded)
}

// UpdateTableReference re-targets a reference and reports the relationships the
// change deleted.
func UpdateTableReference(ctx context.Context, userID, tableID string, req UpdateTableReferenceRequest) (*UpdateTableReferenceResult, error) {
	body := map[string]any{
		"op":      "update",
		"tableId": tableID,
	}
	if req.SourceWhiteboardID != nil {
		body["sourceWhiteboardId"] = *req.SourceWhiteboardID
	}
	if req.SourceTableID != nil {
		body["sourceTableId"] = *req.SourceTableID
	}
	if req.SourceColumnIDs != nil {
		body["sourceColumnIds"] = req.SourceColumnIDs
	}
	decoded, err := postReference(ctx, userID, body,
		fmt.Sprintf("Table reference %s not found.", tableID), forbiddenReferenceWrite)
	if err != nil {
		return nil, err
	}
	reference, err := decodeReference(decoded)
	if err != nil {
		return nil, err
	}
	// A missing count is zero, not an error: the field is informational, and
	// failing the whole re-target over it would be worse than under-reporting.
	var deleted int
	if raw, ok := decoded["deletedRelationships"]; ok {
		_ = json.Unmarshal(raw, &deleted)
	}
	return &UpdateTableReferenceResult{Reference: reference, DeletedRelationships: deleted}, nil
}

// DeleteTableReference deletes a reference node and returns the removed row.
//
// The delete cascades the reference's stub columns and every relationship drawn
// from them. The confirmName guard that makes this safe to expose to an agent
// lives in the tool layer, not here.
func DeleteTableReference(ctx context.Context, userID, tableID string) (map[string]any, error) {
	body := map[string]any{
		"op":      "delete",
		"tableId": tableID,
	}
	decoded, err := postReference(ctx, userID, body,
		fmt.Sprintf("Table reference %s not found.", tableID), forbiddenReferenceWrite)
	if err != nil {
		return nil, err
	}
	return decodeReference(decoded)
}

// ListTableReferences returns every reference on a whiteboard, already resolved
// against its source file — including the `missing` flag for a reference whose
// source has been deleted.
func ListTableReferences(ctx context.Context, userID, whiteboardID string) ([]map[string]any, error) {
	body := map[string]any{
		"op":           "list",
		"whiteboardId": whiteboardID,
	}
	decoded, err := postReference(ctx, userID, body,
		fmt.Sprintf("Whiteboard %s not found.", whiteboardID), forbiddenReferenceRead)
	if err != nil {
		return nil, err
	}
	raw, ok := decoded["references"]
	if !ok {
		return nil, unreadableReferenceResponse(tableReferenceAPIURL())
	}
	var references []map[string]any
	if err := json.Unmarshal(raw, &references); err != nil {
		return nil, unreadableReferenceResponse(tableReferenceAPIURL())
	}
	// A board with no references decodes to nil; hand back an empty slice so a
	// caller can range over it without a nil check.
	if references == nil {
		references = []map[string]any{}
	}
	return references, nil
}

// ── Transport ────────────────────────────────────────────────────────────────

// postReference sends one operation to the route and returns the decoded
// envelope, leaving the caller to pull out the field it wants — `create`,
// `update` and `delete` answer with `{reference}`, `list` with `{references}`,
// and `update` adds `{deletedRelationships}`.
//
// The response body is never echoed into an error: it is attacker- or
// misconfiguration-shaped text that can contain the request's own headers.
func postReference(
	ctx context.Context,
	userID string,
	body map[string]any,
	notFoundMessage string,
	forbiddenMessage string,
) (map[string]json.RawMessage, error) {
	token, err := getToken(ctx, userID)
	if err != nil {
		return nil, mcperr.New(mcperr.ConnectionError,
			"Could not obtain a collaboration credential from the liz-whiteboard app. "+
				"Check that the app is running and that MCP_CLIENT_SECRET matches the app's.")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, mcperr.New(mcperr.InternalError, "Could not encode the table reference request.")
	}

	url := tableReferenceAPIURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, mcperr.New(mcperr.InternalError, "Could not build the table reference request.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("Cannot reach the liz-whiteboard app at %s. "+
				"Start the app, or set LIZ_MCP_TABLE_REFERENCE_API_URL to its address.", url))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, referenceStatusError(resp, url, notFoundMessage, forbiddenMessage)
	}

	var decoded map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, unreadableReferenceResponse(url)
	}
	return decoded, nil
}

// decodeReference pulls the `reference` field out of an envelope.
func decodeReference(decoded map[string]json.RawMessage) (map[string]any, error) {
	raw, ok := decoded["reference"]
	if !ok {
		return nil, unreadableReferenceResponse(tableReferenceAPIURL())
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil || row == nil {
		return nil, unreadableReferenceResponse(tableReferenceAPIURL())
	}
	return row, nil
}

func unreadableReferenceResponse(url string) error {
	return mcperr.New(mcperr.ConnectionError,
		fmt.Sprintf("The liz-whiteboard app at %s returned an unreadable table reference response.", url))
}

// referenceStatusError maps one HTTP status onto the MCP error taxonomy.
//
// It differs from lifecycleStatusError in one way that matters: the reference
// route answers 400 with a validation message written FOR the caller (a source
// board in another project, a column that belongs to another table), so that
// message is passed through instead of being flattened into a generic failure
// the agent cannot act on. Only the `message` field is read, and only for a
// 400 — no other status body is trusted.
func referenceStatusError(resp *http.Response, url, notFoundMessage, forbiddenMessage string) error {
	switch resp.StatusCode {
	case http.StatusBadRequest:
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.Message != "" {
			return mcperr.New(mcperr.ValidationError, payload.Message)
		}
		return mcperr.New(mcperr.ValidationError,
			"The liz-whiteboard app rejected this table reference request as invalid.")
	case http.StatusUnauthorized:
		return mcperr.New(mcperr.SessionExpired,
			"The liz-whiteboard app rejected this server's collaboration credential. "+
				"Check that MCP_CLIENT_SECRET, OAUTH_ISSUER, and COLLAB_RESOURCE_URI match the app's values.")
	case http.StatusForbidden:
		return mcperr.New(mcperr.Forbidden, forbiddenMessage)
	case http.StatusNotFound:
		if notFoundMessage == "" {
			return mcperr.New(mcperr.ConnectionError,
				fmt.Sprintf("The liz-whiteboard app at %s answered 404 for an operation that names no existing row.", url))
		}
		return mcperr.New(mcperr.NotFound, notFoundMessage)
	case http.StatusTooManyRequests:
		return mcperr.New(mcperr.ConnectionError,
			"The liz-whiteboard app is rate limiting this server (15 requests per minute). "+
				"Wait 60 seconds and retry.")
	default:
		return mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The liz-whiteboard app at %s returned HTTP %d for a table reference request.", url, resp.StatusCode))
	}
}

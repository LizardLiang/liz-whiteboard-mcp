// Package appapi — cross-file table reference client tests (LizMeter #83).
// Same strategy as lifecycle_test.go: a real httptest server stands in for the
// app route, so the request body, the credential header, and the status mapping
// are all asserted against the wire rather than a mock.
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

// serveReferenceAPI points the package at a test server for one test.
func serveReferenceAPI(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	withStubbedToken(t, "stub-collab-jwt")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("LIZ_MCP_TABLE_REFERENCE_API_URL", srv.URL)
	prev := client
	client = srv.Client()
	t.Cleanup(func() { client = prev })
	return srv
}

func referenceEnvelope(body map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}

func statusOnly(code int, body map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}
}

func TestCreateTableReferenceSendsTheWholeRequest(t *testing.T) {
	var got map[string]any
	var authHeader string
	x := 10.0
	serveReferenceAPI(t, func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"reference": map[string]any{"id": "r1"}})
	})

	out, err := CreateTableReference(context.Background(), "user-1", CreateTableReferenceRequest{
		WhiteboardID:       "wb-local",
		SourceWhiteboardID: "wb-source",
		SourceTableID:      "tbl-1",
		SourceColumnIDs:    []string{"col-1", "col-2"},
		PositionX:          &x,
	})

	require.NoError(t, err)
	assert.Equal(t, "r1", out["id"])
	assert.Equal(t, "create", got["op"])
	assert.Equal(t, "wb-local", got["whiteboardId"])
	assert.Equal(t, "wb-source", got["sourceWhiteboardId"])
	assert.Equal(t, []any{"col-1", "col-2"}, got["sourceColumnIds"])
	assert.InDelta(t, 10.0, got["positionX"], 0.0001)
	// An omitted position must not be sent as a zero, which would pin the node
	// to the origin instead of leaving it unplaced.
	_, hasY := got["positionY"]
	assert.False(t, hasY)
	assert.NotEmpty(t, authHeader)
}

func TestUpdateTableReferenceReadsTheDeletedCount(t *testing.T) {
	serveReferenceAPI(t, referenceEnvelope(map[string]any{
		"reference":            map[string]any{"id": "r1"},
		"deletedRelationships": 4,
	}))
	next := "tbl-2"

	result, err := UpdateTableReference(context.Background(), "user-1", "r1", UpdateTableReferenceRequest{
		SourceTableID: &next,
	})

	require.NoError(t, err)
	assert.Equal(t, 4, result.DeletedRelationships)
	assert.Equal(t, "r1", result.Reference["id"])
}

// The count is informational: a route that omits it must not fail the whole
// re-target, which has already happened by then.
func TestUpdateTableReferenceTreatsAMissingCountAsZero(t *testing.T) {
	serveReferenceAPI(t, referenceEnvelope(map[string]any{
		"reference": map[string]any{"id": "r1"},
	}))
	next := "tbl-2"

	result, err := UpdateTableReference(context.Background(), "user-1", "r1", UpdateTableReferenceRequest{
		SourceTableID: &next,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, result.DeletedRelationships)
}

func TestUpdateTableReferenceOmitsAbsentFields(t *testing.T) {
	var got map[string]any
	serveReferenceAPI(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"reference": map[string]any{"id": "r1"}})
	})
	next := "tbl-2"

	_, err := UpdateTableReference(context.Background(), "user-1", "r1", UpdateTableReferenceRequest{
		SourceTableID: &next,
	})

	require.NoError(t, err)
	assert.Equal(t, "tbl-2", got["sourceTableId"])
	// Absent fields must be omitted, not sent as empty strings — the app would
	// read those as a request to point the reference at nothing.
	_, hasBoard := got["sourceWhiteboardId"]
	assert.False(t, hasBoard)
	_, hasColumns := got["sourceColumnIds"]
	assert.False(t, hasColumns)
}

func TestListTableReferencesReturnsEveryRow(t *testing.T) {
	serveReferenceAPI(t, referenceEnvelope(map[string]any{
		"references": []map[string]any{
			{"sourceTableName": "orders", "missing": false},
			{"sourceTableName": "payments", "missing": true},
		},
	}))

	references, err := ListTableReferences(context.Background(), "user-1", "wb-local")

	require.NoError(t, err)
	require.Len(t, references, 2)
	assert.Equal(t, "orders", references[0]["sourceTableName"])
	assert.Equal(t, true, references[1]["missing"])
}

// A board with no references decodes to nil; the caller must be able to range
// over the result without a nil check.
func TestListTableReferencesReturnsAnEmptySliceNotNil(t *testing.T) {
	serveReferenceAPI(t, referenceEnvelope(map[string]any{"references": nil}))

	references, err := ListTableReferences(context.Background(), "user-1", "wb-local")

	require.NoError(t, err)
	require.NotNil(t, references)
	assert.Empty(t, references)
}

// The route's 400 message is written for the caller — pass it through instead
// of flattening it into something the agent cannot act on.
func TestReferenceValidationMessageReachesTheAgent(t *testing.T) {
	serveReferenceAPI(t, statusOnly(http.StatusBadRequest, map[string]any{
		"message": "A table reference must point at a whiteboard in the same project",
	}))

	_, err := CreateTableReference(context.Background(), "user-1", CreateTableReferenceRequest{
		WhiteboardID: "wb-local", SourceWhiteboardID: "wb-other",
		SourceTableID: "tbl-1", SourceColumnIDs: []string{"col-1"},
	})

	require.Error(t, err)
	assert.Equal(t, mcperr.ValidationError, codeOf(t, err))
	assert.Contains(t, err.Error(), "same project")
}

func TestReferenceStatusMapping(t *testing.T) {
	cases := map[int]mcperr.Code{
		http.StatusUnauthorized:        mcperr.SessionExpired,
		http.StatusForbidden:           mcperr.Forbidden,
		http.StatusNotFound:            mcperr.NotFound,
		http.StatusTooManyRequests:     mcperr.ConnectionError,
		http.StatusInternalServerError: mcperr.ConnectionError,
	}
	for status, want := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			serveReferenceAPI(t, statusOnly(status, nil))

			_, err := DeleteTableReference(context.Background(), "user-1", "r1")

			require.Error(t, err)
			assert.Equal(t, want, codeOf(t, err))
		})
	}
}

func TestReferenceUnreachableAppIsAConnectionError(t *testing.T) {
	// Stub the credential, or this passes for the wrong reason: an unstubbed
	// token fetch also fails, and also reports CONNECTION_ERROR.
	withStubbedToken(t, "stub-collab-jwt")
	t.Setenv("LIZ_MCP_TABLE_REFERENCE_API_URL", "http://127.0.0.1:1/nope")

	_, err := ListTableReferences(context.Background(), "user-1", "wb-local")

	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
}

func TestReferenceUnreadableResponseIsAConnectionError(t *testing.T) {
	// A 200 with no `reference` field is a route bug, not a caller error.
	serveReferenceAPI(t, referenceEnvelope(map[string]any{"unexpected": true}))

	_, err := CreateTableReference(context.Background(), "user-1", CreateTableReferenceRequest{
		WhiteboardID: "wb-local", SourceWhiteboardID: "wb-source",
		SourceTableID: "tbl-1", SourceColumnIDs: []string{"col-1"},
	})

	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
}

func TestReferenceTokenFetchFailureIsConnectionError(t *testing.T) {
	prev := getToken
	getToken = func(context.Context, string) (string, error) { return "", assertAnError{} }
	t.Cleanup(func() { getToken = prev })

	_, err := ListTableReferences(context.Background(), "user-1", "wb-local")

	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
}

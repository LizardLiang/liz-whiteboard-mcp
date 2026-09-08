package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const testBoardID = "11111111-2222-4333-8444-555555555555"
const testProjectID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

// withStubbedToken replaces the collab-token fetch with a fixed JWT so no test
// needs a running Authorization Server.
func withStubbedToken(t *testing.T, token string) {
	t.Helper()
	prev := getToken
	getToken = func(context.Context, string) (string, error) { return token, nil }
	t.Cleanup(func() { getToken = prev })
}

// serveCanvasBoardAPI points the package at a test server for one test.
func serveCanvasBoardAPI(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("LIZ_CANVAS_BOARD_API_URL", srv.URL)
	prev := client
	client = srv.Client()
	t.Cleanup(func() { client = prev })
	return srv
}

// okBoard replies with the success envelope the route returns.
func okBoard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"board": map[string]any{"id": testBoardID, "name": "Sprint board"},
	})
}

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Request shape
// ---------------------------------------------------------------------------

// The route is a single POST discriminated on `op`; every call must carry the
// collab-audience JWT as a bearer token. The client's own MCP access token is
// never forwarded — that is the confused-deputy hole /api/collab-token exists
// to close.
func TestCreateCanvasBoard_PostsOpCreateWithBearer(t *testing.T) {
	withStubbedToken(t, "collab-jwt")

	var gotMethod, gotAuth, gotContentType string
	var gotBody map[string]any
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		okBoard(w, r)
	})

	board, err := CreateCanvasBoard(context.Background(), "user-1", CreateCanvasBoardRequest{
		ProjectID: testProjectID,
		Name:      "Sprint board",
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "Bearer collab-jwt", gotAuth)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "create", gotBody["op"])
	assert.Equal(t, testProjectID, gotBody["projectId"])
	assert.Equal(t, "Sprint board", gotBody["name"])
	assert.NotContains(t, gotBody, "folderId", "absent folderId must not be sent")
	assert.Equal(t, testBoardID, board["id"])
}

func TestCreateCanvasBoard_SendsFolderIDWhenSupplied(t *testing.T) {
	withStubbedToken(t, "collab-jwt")

	var gotBody map[string]any
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		okBoard(w, r)
	})

	_, err := CreateCanvasBoard(context.Background(), "user-1", CreateCanvasBoardRequest{
		ProjectID: testProjectID,
		Name:      "Sprint board",
		FolderID:  strPtr("ffffffff-1111-4222-8333-444444444444"),
	})
	require.NoError(t, err)
	assert.Equal(t, "ffffffff-1111-4222-8333-444444444444", gotBody["folderId"])
}

func TestUpdateCanvasBoard_PostsOpUpdateWithOnlySuppliedFields(t *testing.T) {
	withStubbedToken(t, "collab-jwt")

	var gotBody map[string]any
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		okBoard(w, r)
	})

	_, err := UpdateCanvasBoard(context.Background(), "user-1", testBoardID,
		UpdateCanvasBoardRequest{Name: strPtr("Renamed")})
	require.NoError(t, err)

	assert.Equal(t, "update", gotBody["op"])
	assert.Equal(t, testBoardID, gotBody["canvasBoardId"])
	assert.Equal(t, "Renamed", gotBody["name"])
	assert.NotContains(t, gotBody, "folderId")
}

func TestDeleteCanvasBoard_PostsOpDelete(t *testing.T) {
	withStubbedToken(t, "collab-jwt")

	var gotBody map[string]any
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		okBoard(w, r)
	})

	board, err := DeleteCanvasBoard(context.Background(), "user-1", testBoardID)
	require.NoError(t, err)

	assert.Equal(t, "delete", gotBody["op"])
	assert.Equal(t, testBoardID, gotBody["canvasBoardId"])
	assert.Equal(t, testBoardID, board["id"], "delete returns the removed board")
}

// ---------------------------------------------------------------------------
// HTTP status -> mcperr code mapping
// ---------------------------------------------------------------------------

// Every status the route can answer maps to exactly one MCP code. Getting this
// wrong turns "you are a VIEWER" into "an internal error occurred", which is
// the difference between an agent that retries usefully and one that gives up.
func TestCanvasBoardAPI_StatusToMcpCode(t *testing.T) {
	cases := []struct {
		status int
		want   mcperr.Code
	}{
		{http.StatusUnauthorized, mcperr.SessionExpired},
		{http.StatusForbidden, mcperr.Forbidden},
		{http.StatusNotFound, mcperr.NotFound},
		{http.StatusInternalServerError, mcperr.ConnectionError},
		{http.StatusBadRequest, mcperr.ConnectionError},
		{http.StatusTooManyRequests, mcperr.ConnectionError},
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			withStubbedToken(t, "collab-jwt")
			serveCanvasBoardAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"nope"}`))
			})

			_, err := DeleteCanvasBoard(context.Background(), "user-1", testBoardID)
			require.Error(t, err)

			var mcpErr *mcperr.McpError
			require.ErrorAs(t, err, &mcpErr)
			assert.Equal(t, tc.want, mcpErr.Code)
			assert.NotEmpty(t, mcpErr.Message)
		})
	}
}

// A 404 must name the board so the agent can tell a typo from a deletion.
func TestCanvasBoardAPI_NotFoundNamesTheBoard(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := UpdateCanvasBoard(context.Background(), "user-1", testBoardID,
		UpdateCanvasBoardRequest{Name: strPtr("x")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), testBoardID)
}

// A 2xx that is not 200 must still be accepted rather than falling into the
// non-2xx arm.
func TestCanvasBoardAPI_Accepts201(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		okBoard(w, r)
	})

	board, err := CreateCanvasBoard(context.Background(), "user-1", CreateCanvasBoardRequest{
		ProjectID: testProjectID, Name: "n",
	})
	require.NoError(t, err)
	assert.Equal(t, testBoardID, board["id"])
}

// ---------------------------------------------------------------------------
// Transport and credential failures
// ---------------------------------------------------------------------------

// The app being down is a CONNECTION_ERROR, not an INTERNAL_ERROR: the message
// has to tell the user to start the app.
func TestCanvasBoardAPI_UnreachableAppIsConnectionError(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	srv := httptest.NewServer(http.HandlerFunc(okBoard))
	t.Setenv("LIZ_CANVAS_BOARD_API_URL", srv.URL)
	prev := client
	client = srv.Client()
	t.Cleanup(func() { client = prev })
	srv.Close() // the app is now down

	_, err := DeleteCanvasBoard(context.Background(), "user-1", testBoardID)
	require.Error(t, err)
	var mcpErr *mcperr.McpError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, mcperr.ConnectionError, mcpErr.Code)
}

// A failure to mint the collab JWT must not surface as a raw AS error: the AS
// response can carry the client secret's rejection detail.
func TestCanvasBoardAPI_TokenFetchFailureIsConnectionError(t *testing.T) {
	prev := getToken
	getToken = func(context.Context, string) (string, error) {
		return "", assertAnError{}
	}
	t.Cleanup(func() { getToken = prev })

	_, err := DeleteCanvasBoard(context.Background(), "user-1", testBoardID)
	require.Error(t, err)
	var mcpErr *mcperr.McpError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, mcperr.ConnectionError, mcpErr.Code)
}

type assertAnError struct{}

func (assertAnError) Error() string { return "collab-token endpoint returned HTTP 401" }

// A malformed success body is a server contract break, not a silent nil board.
func TestCanvasBoardAPI_UndecodableBodyIsConnectionError(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	})

	_, err := DeleteCanvasBoard(context.Background(), "user-1", testBoardID)
	require.Error(t, err)
	var mcpErr *mcperr.McpError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, mcperr.ConnectionError, mcpErr.Code)
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Mirrors COLLAB_TOKEN_URL's pattern: env var read at call time, localhost
// default so a local dev run needs no extra configuration.
func TestCanvasBoardAPIURL_EnvOverridesDefault(t *testing.T) {
	t.Setenv("LIZ_CANVAS_BOARD_API_URL", "")
	assert.Equal(t, "http://localhost:3000/api/canvas-boards", canvasBoardAPIURL())

	t.Setenv("LIZ_CANVAS_BOARD_API_URL", "http://app:3000/api/canvas-boards")
	assert.Equal(t, "http://app:3000/api/canvas-boards", canvasBoardAPIURL())
}

// The error text must not leak the bearer token even if a server echoes it.
func TestCanvasBoardAPI_ErrorDoesNotEchoToken(t *testing.T) {
	withStubbedToken(t, "super-secret-jwt")
	serveCanvasBoardAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(r.Header.Get("Authorization")))
	})

	_, err := DeleteCanvasBoard(context.Background(), "user-1", testBoardID)
	require.Error(t, err)
	assert.False(t, strings.Contains(err.Error(), "super-secret-jwt"),
		"the response body must not be echoed into the error")
}

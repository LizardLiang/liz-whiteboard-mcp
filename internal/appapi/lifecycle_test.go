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

const testFolderID = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
const testWhiteboardID = "cccccccc-dddd-4eee-8fff-000000000000"

// codeOf unwraps the MCP error code from a client error, matching the
// ErrorAs assertion canvas_board_test.go uses.
func codeOf(t *testing.T, err error) mcperr.Code {
	t.Helper()
	var mcpErr *mcperr.McpError
	require.ErrorAs(t, err, &mcpErr)
	require.NotEmpty(t, mcpErr.Message)
	return mcpErr.Code
}

// serveLifecycleAPI points the package at a test server for one test.
func serveLifecycleAPI(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("LIZ_MCP_LIFECYCLE_API_URL", srv.URL)
	prev := client
	client = srv.Client()
	t.Cleanup(func() { client = prev })
	return srv
}

// okRow replies with the success envelope the route returns for one entity.
func okRow(key string, row map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{key: row})
	}
}

// captureBody records the decoded request body and replies with a success row.
func captureBody(t *testing.T, key string, into *map[string]any, auth *string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*auth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(into))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{key: map[string]any{"id": "x"}})
	}
}

// ---------------------------------------------------------------------------
// Request shape — every call carries the collab bearer and the {entity, op}
// envelope the route discriminates on.
// ---------------------------------------------------------------------------

func TestCreateProject_PostsEntityProjectOpCreateWithBearer(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "project", &body, &auth))

	_, err := CreateProject(context.Background(), "user-1",
		CreateProjectRequest{Name: "Cold start"})
	require.NoError(t, err)

	assert.Equal(t, "Bearer collab-jwt", auth)
	assert.Equal(t, "project", body["entity"])
	assert.Equal(t, "create", body["op"])
	assert.Equal(t, "Cold start", body["name"])
	// The owner is the JWT subject; the client must not send one.
	assert.NotContains(t, body, "ownerId")
	// An absent description is omitted, not sent as null.
	assert.NotContains(t, body, "description")
}

func TestCreateProject_SendsDescriptionWhenSupplied(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "project", &body, &auth))

	_, err := CreateProject(context.Background(), "user-1",
		CreateProjectRequest{Name: "P", Description: strPtr("notes")})
	require.NoError(t, err)
	assert.Equal(t, "notes", body["description"])
}

func TestUpdateProject_SendsOnlySuppliedFields(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "project", &body, &auth))

	_, err := UpdateProject(context.Background(), "user-1", testProjectID,
		UpdateProjectRequest{Name: strPtr("Renamed")})
	require.NoError(t, err)

	assert.Equal(t, "update", body["op"])
	assert.Equal(t, testProjectID, body["projectId"])
	assert.Equal(t, "Renamed", body["name"])
	assert.NotContains(t, body, "description")
}

func TestDeleteProject_PostsOpDelete(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "project", &body, &auth))

	_, err := DeleteProject(context.Background(), "user-1", testProjectID)
	require.NoError(t, err)
	assert.Equal(t, "project", body["entity"])
	assert.Equal(t, "delete", body["op"])
	assert.Equal(t, testProjectID, body["projectId"])
}

func TestCreateFolder_SendsParentOnlyWhenSupplied(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "folder", &body, &auth))

	_, err := CreateFolder(context.Background(), "user-1",
		CreateFolderRequest{ProjectID: testProjectID, Name: "Docs"})
	require.NoError(t, err)
	assert.Equal(t, "folder", body["entity"])
	assert.NotContains(t, body, "parentFolderId")

	_, err = CreateFolder(context.Background(), "user-1",
		CreateFolderRequest{ProjectID: testProjectID, Name: "Sub", ParentFolderID: strPtr(testFolderID)})
	require.NoError(t, err)
	assert.Equal(t, testFolderID, body["parentFolderId"])
}

func TestUpdateFolder_PostsNameOnly(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "folder", &body, &auth))

	_, err := UpdateFolder(context.Background(), "user-1", testFolderID, "Renamed")
	require.NoError(t, err)
	assert.Equal(t, "update", body["op"])
	assert.Equal(t, testFolderID, body["folderId"])
	assert.Equal(t, "Renamed", body["name"])
}

func TestDeleteFolder_PostsOpDelete(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "folder", &body, &auth))

	_, err := DeleteFolder(context.Background(), "user-1", testFolderID)
	require.NoError(t, err)
	assert.Equal(t, "delete", body["op"])
	assert.Equal(t, testFolderID, body["folderId"])
}

func TestCreateWhiteboard_SendsFolderOnlyWhenSupplied(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "whiteboard", &body, &auth))

	_, err := CreateWhiteboard(context.Background(), "user-1",
		CreateWhiteboardRequest{ProjectID: testProjectID, Name: "Schema"})
	require.NoError(t, err)
	assert.Equal(t, "whiteboard", body["entity"])
	assert.NotContains(t, body, "folderId")
}

// The route refuses a projectId on a whiteboard update, so the client must not
// have a way to send one. This pins the request shape, not the route's guard.
func TestUpdateWhiteboard_NeverSendsProjectID(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "whiteboard", &body, &auth))

	_, err := UpdateWhiteboard(context.Background(), "user-1", testWhiteboardID,
		UpdateWhiteboardRequest{Name: strPtr("New"), FolderID: strPtr(testFolderID)})
	require.NoError(t, err)

	assert.Equal(t, testWhiteboardID, body["whiteboardId"])
	assert.Equal(t, "New", body["name"])
	assert.Equal(t, testFolderID, body["folderId"])
	assert.NotContains(t, body, "projectId")
}

func TestDeleteWhiteboard_PostsOpDelete(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var body map[string]any
	var auth string
	serveLifecycleAPI(t, captureBody(t, "whiteboard", &body, &auth))

	_, err := DeleteWhiteboard(context.Background(), "user-1", testWhiteboardID)
	require.NoError(t, err)
	assert.Equal(t, "delete", body["op"])
	assert.Equal(t, testWhiteboardID, body["whiteboardId"])
}

// ---------------------------------------------------------------------------
// Response envelope — the route wraps the row in a per-entity key.
// ---------------------------------------------------------------------------

func TestLifecycleAPI_ReadsThePerEntityResultKey(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveLifecycleAPI(t, okRow("folder", map[string]any{"id": testFolderID, "name": "Docs"}))

	folder, err := CreateFolder(context.Background(), "user-1",
		CreateFolderRequest{ProjectID: testProjectID, Name: "Docs"})
	require.NoError(t, err)
	assert.Equal(t, testFolderID, folder["id"])
}

// A reply carrying the wrong entity key is a route/client mismatch, not a row.
func TestLifecycleAPI_WrongResultKeyIsConnectionError(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveLifecycleAPI(t, okRow("board", map[string]any{"id": testFolderID}))

	_, err := CreateFolder(context.Background(), "user-1",
		CreateFolderRequest{ProjectID: testProjectID, Name: "Docs"})
	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
}

func TestLifecycleAPI_UndecodableBodyIsConnectionError(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveLifecycleAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})

	_, err := CreateProject(context.Background(), "user-1", CreateProjectRequest{Name: "P"})
	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
}

func TestLifecycleAPI_Accepts201(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveLifecycleAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"project": map[string]any{"id": testProjectID}})
	})

	project, err := CreateProject(context.Background(), "user-1", CreateProjectRequest{Name: "P"})
	require.NoError(t, err)
	assert.Equal(t, testProjectID, project["id"])
}

// ---------------------------------------------------------------------------
// Status mapping
// ---------------------------------------------------------------------------

func TestLifecycleAPI_StatusToMcpCode(t *testing.T) {
	cases := []struct {
		status int
		want   mcperr.Code
	}{
		{http.StatusUnauthorized, mcperr.SessionExpired},
		{http.StatusForbidden, mcperr.Forbidden},
		{http.StatusNotFound, mcperr.NotFound},
		{http.StatusTooManyRequests, mcperr.ConnectionError},
		{http.StatusInternalServerError, mcperr.ConnectionError},
		{http.StatusBadRequest, mcperr.ConnectionError},
	}
	for _, tc := range cases {
		withStubbedToken(t, "collab-jwt")
		serveLifecycleAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		})
		_, err := DeleteProject(context.Background(), "user-1", testProjectID)
		require.Error(t, err, "status %d", tc.status)
		assert.Equalf(t, tc.want, codeOf(t, err), "status %d", tc.status)
	}
}

// The FORBIDDEN text must name the role the route actually checks, so a denied
// agent is not told to acquire the wrong one.
func TestLifecycleAPI_ForbiddenNamesTheRequiredRole(t *testing.T) {
	cases := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "delete project needs OWNER",
			call: func() error {
				_, err := DeleteProject(context.Background(), "user-1", testProjectID)
				return err
			},
			want: "owner",
		},
		{
			name: "update project needs ADMIN",
			call: func() error {
				_, err := UpdateProject(context.Background(), "user-1", testProjectID,
					UpdateProjectRequest{Name: strPtr("X")})
				return err
			},
			want: "ADMIN",
		},
		{
			name: "create whiteboard needs EDITOR",
			call: func() error {
				_, err := CreateWhiteboard(context.Background(), "user-1",
					CreateWhiteboardRequest{ProjectID: testProjectID, Name: "S"})
				return err
			},
			want: "EDITOR",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withStubbedToken(t, "collab-jwt")
			serveLifecycleAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			})
			err := tc.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// A 404 names the row that was not found, so an agent can tell a typo from a
// permission problem.
func TestLifecycleAPI_NotFoundNamesTheRow(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveLifecycleAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := DeleteFolder(context.Background(), "user-1", testFolderID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), testFolderID)
}

// A create names no existing row, so a 404 there is a route bug, reported as a
// connection problem rather than as a missing row.
func TestLifecycleAPI_NotFoundOnCreateIsConnectionError(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	serveLifecycleAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := CreateProject(context.Background(), "user-1", CreateProjectRequest{Name: "P"})
	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
}

// ---------------------------------------------------------------------------
// Transport failures and secret hygiene
// ---------------------------------------------------------------------------

func TestLifecycleAPI_UnreachableAppIsConnectionError(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	t.Setenv("LIZ_MCP_LIFECYCLE_API_URL", "http://127.0.0.1:1/mcp-lifecycle")

	_, err := CreateProject(context.Background(), "user-1", CreateProjectRequest{Name: "P"})
	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
	assert.Contains(t, err.Error(), "LIZ_MCP_LIFECYCLE_API_URL")
}

func TestLifecycleAPI_TokenFetchFailureIsConnectionError(t *testing.T) {
	prev := getToken
	getToken = func(context.Context, string) (string, error) { return "", assertAnError{} }
	t.Cleanup(func() { getToken = prev })

	_, err := CreateProject(context.Background(), "user-1", CreateProjectRequest{Name: "P"})
	require.Error(t, err)
	assert.Equal(t, mcperr.ConnectionError, codeOf(t, err))
}

// No error path may echo the bearer token, whatever the app replies with.
func TestLifecycleAPI_ErrorDoesNotEchoToken(t *testing.T) {
	const secret = "super-secret-collab-jwt"
	withStubbedToken(t, secret)
	serveLifecycleAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// The app echoing the header back is exactly the case that must not
		// reach the agent.
		_, _ = w.Write([]byte(r.Header.Get("Authorization")))
	})

	_, err := DeleteProject(context.Background(), "user-1", testProjectID)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
}

func TestLifecycleAPIURL_EnvOverridesDefault(t *testing.T) {
	assert.Equal(t, "http://localhost:3000/api/mcp-lifecycle", lifecycleAPIURL())

	t.Setenv("LIZ_MCP_LIFECYCLE_API_URL", "http://app:3000/api/mcp-lifecycle")
	assert.Equal(t, "http://app:3000/api/mcp-lifecycle", lifecycleAPIURL())
}

// The lifecycle client must not silently pick up the canvas-board route: the
// two have different request shapes and a shared URL would fail obscurely.
func TestLifecycleAPIURL_IgnoresCanvasBoardEnvVar(t *testing.T) {
	t.Setenv("LIZ_CANVAS_BOARD_API_URL", "http://elsewhere:9999/api/canvas-boards")
	assert.Equal(t, "http://localhost:3000/api/mcp-lifecycle", lifecycleAPIURL())
}

func TestLifecycleAPI_SendsJSONContentType(t *testing.T) {
	withStubbedToken(t, "collab-jwt")
	var got string
	serveLifecycleAPI(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"project": map[string]any{"id": "x"}})
	})

	_, err := CreateProject(context.Background(), "user-1", CreateProjectRequest{Name: "P"})
	require.NoError(t, err)
	assert.True(t, strings.Contains(got, "application/json"))
}

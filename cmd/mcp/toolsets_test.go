package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

func TestToolsetHeaderGuard(t *testing.T) {
	reached := false
	h := toolsetHeaderGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached = true }))

	for _, v := range []string{"", "all", "canvas", " ER , canvas ", "core"} {
		reached = false
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if v != "" {
			req.Header.Set(tools.ToolsetHeader, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.True(t, reached, "header %q must pass", v)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	for _, bad := range []string{"nope", "all,nope", "canvas,nope"} {
		reached = false
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set(tools.ToolsetHeader, bad)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.False(t, reached, "header %q must be refused", bad)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, strings.Contains(rec.Body.String(), "all, core, er, canvas, manage"))
	}
}

// A request inside a session carries Mcp-Session-Id: the guard must not parse
// the header there, so a session can still DELETE whatever header it sends.
func TestToolsetHeaderGuardSkipsSessionRequests(t *testing.T) {
	reached := false
	h := toolsetHeaderGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached = true }))
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "abc")
	req.Header.Set(tools.ToolsetHeader, "nope")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.True(t, reached)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// getServer reads the set the guard parsed, and refuses a request the guard did not see.
func TestGetServerUsesGuardedSet(t *testing.T) {
	c := newToolsetServers()
	var got *mcp.Server
	h := toolsetHeaderGuard(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = c.getServer(r) }))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(tools.ToolsetHeader, "canvas")
	h.ServeHTTP(httptest.NewRecorder(), req)
	want, err := tools.ParseToolsets("canvas")
	require.NoError(t, err)
	assert.Same(t, c.get(want), got)

	assert.Nil(t, c.getServer(httptest.NewRequest(http.MethodPost, "/mcp", nil)))
}

func TestToolsetServersCachePerSet(t *testing.T) {
	c := newToolsetServers()
	a, err := tools.ParseToolsets("canvas,er")
	require.NoError(t, err)
	b, err := tools.ParseToolsets("er, canvas")
	require.NoError(t, err)
	assert.Same(t, c.get(a), c.get(b))
	assert.NotSame(t, c.get(a), c.get(tools.AllToolsets()))
}

// Concurrent lookups of every combination must return the same server, with no
// data race (run with -race).
func TestToolsetServersConcurrent(t *testing.T) {
	c := newToolsetServers()
	headers := []string{"", "er", "canvas", "manage", "er,canvas", "er,manage", "canvas,manage", "core"}
	assert.Len(t, c.servers, 8)
	want := make([]*mcp.Server, len(headers))
	for i, h := range headers {
		set, err := tools.ParseToolsets(h)
		require.NoError(t, err)
		want[i] = c.get(set)
		require.NotNil(t, want[i], "header %q", h)
	}
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, h := range headers {
				set, err := tools.ParseToolsets(h)
				if !assert.NoError(t, err) {
					return
				}
				assert.Same(t, want[i], c.get(set))
			}
		}()
	}
	wg.Wait()
}

// Every prebuilt server must turn a panicking tool into an error result and keep
// the session serving: a panic in one tool must not take the process down.
func TestEveryPrebuiltServerRecoversPanics(t *testing.T) {
	ctx := context.Background()
	c := newToolsetServers()
	require.Len(t, c.servers, 8)
	for key, server := range c.servers {
		mcp.AddTool(server, &mcp.Tool{Name: "panic_probe", Description: "panics"},
			func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				panic("probe panic for server " + key)
			})

		a, b := mcp.NewInMemoryTransports()
		ss, err := server.Connect(ctx, a, nil)
		require.NoError(t, err, "server %q", key)
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "pc", Version: "0"}, nil).Connect(ctx, b, nil)
		require.NoError(t, err, "server %q", key)

		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "panic_probe", Arguments: map[string]any{}})
		require.NoError(t, err, "server %q", key)
		assert.True(t, res.IsError, "server %q", key)
		assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, "INTERNAL_ERROR", "server %q", key)

		next, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_tool_guide", Arguments: map[string]any{}})
		require.NoError(t, err, "server %q", key)
		assert.False(t, next.IsError, "server %q", key)

		cs.Close()
		ss.Close()
	}
}

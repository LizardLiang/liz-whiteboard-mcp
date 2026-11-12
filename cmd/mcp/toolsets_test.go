package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

func TestToolsetHeaderGuard(t *testing.T) {
	reached := false
	h := toolsetHeaderGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached = true }))

	for _, v := range []string{"", "all", "canvas", " ER , canvas "} {
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

	reached = false
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(tools.ToolsetHeader, "nope")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.False(t, reached)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "all, core, er, canvas, manage"))
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

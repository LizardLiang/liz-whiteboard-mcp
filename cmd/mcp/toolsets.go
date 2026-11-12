package main

import (
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

// toolsetServers caches one *mcp.Server per toolset combination.
type toolsetServers struct {
	mu      sync.Mutex
	servers map[string]*mcp.Server
}

func newToolsetServers() *toolsetServers {
	return &toolsetServers{servers: map[string]*mcp.Server{}}
}

// get returns the server for the set, building it on first use.
func (c *toolsetServers) get(set tools.ToolsetSet) *mcp.Server {
	key := set.Key()
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.servers[key]; ok {
		return s
	}
	s := tools.NewServer(&mcp.Implementation{Name: "liz-whiteboard", Version: "1.0.0"}, set)
	c.servers[key] = s
	return s
}

// toolsetHeaderGuard answers HTTP 400 when X-Liz-Toolsets names an unknown
// toolset. The SDK's server callback cannot return a message, so the check runs
// before the SDK handler.
func toolsetHeaderGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := tools.ParseToolsets(r.Header.Get(tools.ToolsetHeader)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

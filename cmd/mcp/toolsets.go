package main

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/tools"
)

// toolsetServers holds one *mcp.Server per toolset combination. It builds every
// combination (at most 2^len(SelectableToolsets) = 8) at startup, so a request
// never waits on a build and the map is read-only afterwards.
type toolsetServers struct {
	servers map[string]*mcp.Server
}

func newToolsetServers() *toolsetServers {
	selectable := tools.SelectableToolsets
	c := &toolsetServers{servers: map[string]*mcp.Server{}}
	for mask := 0; mask < 1<<len(selectable); mask++ {
		set := tools.ToolsetSet{}
		for i, ts := range selectable {
			if mask&(1<<i) != 0 {
				set[ts] = true
			}
		}
		c.servers[set.Key()] = tools.NewServer(&mcp.Implementation{Name: "liz-whiteboard", Version: "1.0.0"}, set)
	}
	return c
}

// get returns the server for the set.
func (c *toolsetServers) get(set tools.ToolsetSet) *mcp.Server {
	return c.servers[set.Key()]
}

type toolsetCtxKey struct{}

// toolsetsFromContext returns the set the guard parsed for this request.
func toolsetsFromContext(ctx context.Context) (tools.ToolsetSet, bool) {
	set, ok := ctx.Value(toolsetCtxKey{}).(tools.ToolsetSet)
	return set, ok
}

// toolsetHeaderGuard parses X-Liz-Toolsets once per request. It answers HTTP 400
// for an unknown toolset, because the SDK's server callback cannot return a
// message, and stores the parsed set in the request context for getServer.
//
// A request that carries Mcp-Session-Id belongs to a session whose server is
// fixed at session start, so the header is ignored there. That lets a session
// still send DELETE or follow-up calls with any header value.
func toolsetHeaderGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Session-Id") != "" {
			next.ServeHTTP(w, r)
			return
		}
		set, err := tools.ParseToolsets(r.Header.Get(tools.ToolsetHeader))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), toolsetCtxKey{}, set)))
	})
}

// getServer is the SDK's per-request server callback. It returns nil, which the
// SDK answers with HTTP 400, when the guard did not run and so left no parsed set.
func (c *toolsetServers) getServer(r *http.Request) *mcp.Server {
	set, ok := toolsetsFromContext(r.Context())
	if !ok {
		return nil
	}
	return c.get(set)
}

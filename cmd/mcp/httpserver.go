package main

import (
	"net/http"
	"time"
)

const (
	// httpReadTimeout bounds reading one request, headers and body.
	httpReadTimeout = 30 * time.Second
	// httpWriteTimeout bounds writing one response. Board images render up to
	// 13 pages in one call.
	httpWriteTimeout = 150 * time.Second
	// httpIdleTimeout bounds how long a keep-alive connection waits for its
	// next request. It must exceed the upstream keep-alive of the proxy in
	// front (Caddy reverse_proxy: 2m by default), so the proxy closes an idle
	// connection first and never sends a request on one this server closes.
	httpIdleTimeout = 3 * time.Minute
)

// newHTTPServer builds the MCP HTTP server with its timeouts.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}
}

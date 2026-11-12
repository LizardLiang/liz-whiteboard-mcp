package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// caddyUpstreamKeepAlive is how long Caddy's reverse_proxy keeps an idle
// upstream connection open: the http transport's `keepalive` option, default
// 2m (https://caddyserver.com/docs/caddyfile/directives/reverse_proxy#keepalive).
// deploy/Caddyfile does not override it.
const caddyUpstreamKeepAlive = 2 * time.Minute

// The proxy must close an idle upstream connection before the MCP server does.
// When the backend closes first, Caddy can reuse a connection the MCP server is
// closing at that moment, and the request on it fails.
func TestNewHTTPServer_IdleTimeoutOutlivesProxyKeepAlive(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NewServeMux())

	// A zero IdleTimeout makes net/http fall back to ReadTimeout.
	assert.NotZero(t, srv.IdleTimeout, "IdleTimeout must be set explicitly")
	assert.Greater(t, srv.IdleTimeout, caddyUpstreamKeepAlive)
	assert.Equal(t, httpReadTimeout, srv.ReadTimeout)
	assert.Equal(t, httpWriteTimeout, srv.WriteTimeout)
}

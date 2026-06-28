// Package socket implements a lazy per-(whiteboard,user) Socket.IO client
// connection pool. It handles FR-021/FR-022: mid-session token expiry with one
// reconnect attempt, and emit-with-ack with a 5-second timeout.
//
// Phase 4 change (Phase 4 confused-deputy fix):
//   - Pool key changed from whiteboardID alone to (whiteboardID, userID) so each
//     user gets their own socket and connections are never shared across users.
//   - Authentication changed from LIZ_SESSION_TOKEN cookie to a short-lived
//     collab-audience JWT obtained from the AS via /api/collab-token. The JWT is
//     passed via socket.io's SetAuth({token: jwt}), read server-side as
//     socket.handshake.auth.token.
//   - SocketEmitWithAck now accepts a userID parameter which is threaded to
//     GetConnection and used for JWT acquisition.
//
// Ported from src/mcp/socket-manager.ts using
// github.com/zishang520/socket.io-client-go.
package socket

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	clienttransports "github.com/zishang520/engine.io-client-go/transports"
	eiotypes "github.com/zishang520/engine.io/v2/types"
	sio "github.com/zishang520/socket.io-client-go/socket"
	"golang.org/x/sync/singleflight"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const ackTimeout = 5 * time.Second

// connectTimeout bounds how long we wait for the Socket.IO namespace handshake
// (including the server's auth middleware). Defaults to 5s but can be raised via
// LIZ_SOCKET_CONNECT_TIMEOUT_MS for servers whose auth middleware hits a
// high-latency / cold database on first connect.
func connectTimeout() time.Duration {
	if v := os.Getenv("LIZ_SOCKET_CONNECT_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 5 * time.Second
}

// AckResult is the decoded ack object returned by the collaboration server.
type AckResult map[string]any

// OK reports whether the ack indicated success. A missing "ok" field is treated
// as success (matches the TS `ack.ok === false` check).
func (a AckResult) OK() bool {
	v, ok := a["ok"]
	if !ok {
		return true
	}
	b, _ := v.(bool)
	return b
}

// Code returns the ack's error code string, if present.
func (a AckResult) Code() string {
	s, _ := a["code"].(string)
	return s
}

// Message returns the ack's error message string, if present.
func (a AckResult) Message() string {
	s, _ := a["message"].(string)
	return s
}

// Entity returns the ack's entity payload, if present.
func (a AckResult) Entity() any {
	return a["entity"]
}

// socketKey is the pool key: (whiteboardID, userID).
// Scoping by user ensures that users never share a socket connection, which
// would confuse the collab server's per-socket identity model.
type socketKey struct {
	whiteboardID string
	userID       string
}

var (
	connections = make(map[socketKey]*sio.Socket)
	mu          sync.Mutex
	dialGroup   singleflight.Group
)

func connectionError() error {
	return mcperr.New(mcperr.ConnectionError, mcperr.MsgConnectionError)
}

// createSocket builds a fresh Socket.IO client for a whiteboard namespace,
// authenticated as the given user via a collab-audience JWT.
func createSocket(ctx context.Context, whiteboardID, userID string) (*sio.Socket, error) {
	baseURL := os.Getenv("LIZ_SOCKET_URL")
	if baseURL == "" {
		baseURL = "ws://localhost:3010"
	}
	uri := fmt.Sprintf("%s/whiteboard/%s", baseURL, whiteboardID)

	// Obtain a collab-audience JWT for this user from the AS.
	// The JWT has aud=COLLAB_RESOURCE_URI, sub=userID, exp=now+120s.
	// The collab server validates it via the AS public key.
	collabJWT, err := GetCollabToken(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get collab token for user %s: %w", userID, err)
	}

	opts := sio.DefaultOptions()
	opts.SetTransports(eiotypes.NewSet(clienttransports.Polling, clienttransports.WebSocket))
	// Pass JWT via socket.io auth (CONNECT packet), not HTTP headers.
	// The collab server reads socket.handshake.auth.token.
	opts.SetAuth(map[string]any{"token": collabJWT})
	// Remove Cookie header — LIZ_SESSION_TOKEN is no longer used.
	opts.SetExtraHeaders(http.Header{})
	opts.SetReconnection(false) // we manage reconnect explicitly (FR-021)

	return sio.Connect(uri, opts)
}

// waitForConnect blocks until the socket connects or the timeout elapses.
func waitForConnect(sock *sio.Socket, timeout time.Duration) error {
	connected := make(chan struct{}, 1)
	failed := make(chan struct{}, 1)

	// The server registers its write-event handlers (table:create, etc.) only
	// AFTER two awaited DB calls inside its async `connection` handler, and
	// signals readiness with an application-level "connected" event. Waiting for
	// the low-level "connect" alone races that registration and silently drops
	// the first emit, so we gate readiness on the "connected" app event.
	sock.Once("connected", func(...any) {
		select {
		case connected <- struct{}{}:
		default:
		}
	})
	sock.Once("connect_error", func(...any) {
		select {
		case failed <- struct{}{}:
		default:
		}
	})

	select {
	case <-connected:
		return nil
	case <-failed:
		return connectionError()
	case <-time.After(timeout):
		return connectionError()
	}
}

// removeConnection disconnects and removes a pooled socket.
func removeConnection(whiteboardID, userID string) {
	key := socketKey{whiteboardID: whiteboardID, userID: userID}
	mu.Lock()
	defer mu.Unlock()
	if sock, ok := connections[key]; ok {
		sock.Disconnect()
		delete(connections, key)
	}
}

// GetConnection returns (or lazily creates) the Socket.IO connection for
// (whiteboardID, userID). On a stale-but-cached socket it reconnects. On
// connect failure it returns CONNECTION_ERROR.
//
// W2: concurrent callers for the same (whiteboard, user) are coalesced via
// singleflight so that only one dial happens and no socket leaks.
func GetConnection(ctx context.Context, whiteboardID, userID string) (*sio.Socket, error) {
	key := socketKey{whiteboardID: whiteboardID, userID: userID}
	sfKey := whiteboardID + ":" + userID

	mu.Lock()
	existing, ok := connections[key]
	if ok && existing.Connected() {
		mu.Unlock()
		return existing, nil
	}
	if ok {
		existing.Disconnect()
		delete(connections, key)
	}
	mu.Unlock()

	v, err, _ := dialGroup.Do(sfKey, func() (any, error) {
		// Re-check under singleflight: a parallel goroutine may have just stored
		// a fresh connection while we waited to enter.
		mu.Lock()
		if ex, ok := connections[key]; ok && ex.Connected() {
			mu.Unlock()
			return ex, nil
		}
		mu.Unlock()

		sock, err := createSocket(ctx, whiteboardID, userID)
		if err != nil {
			return nil, connectionError()
		}
		if err := waitForConnect(sock, connectTimeout()); err != nil {
			sock.Disconnect()
			return nil, err
		}

		// FR-021: when the server signals session expiry, clean up for reconnect.
		sock.On("session_expired", func(...any) {
			fmt.Fprintf(os.Stderr,
				"[liz-whiteboard MCP] Session expired on whiteboard %s (user %s). "+
					"Will attempt one reconnect on next write.\n", whiteboardID, userID)
			// Flush the cached collab JWT so the next reconnect fetches a fresh one.
			flushCollabTokenCache(userID)
			removeConnection(whiteboardID, userID)
		})

		mu.Lock()
		connections[key] = sock
		mu.Unlock()
		return sock, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*sio.Socket), nil
}

// ackOutcome carries the result of a single emit-with-ack callback.
type ackOutcome struct {
	args []any
	err  error
}

// emitAck performs a single emit-with-ack with timeout, blocking for the result.
func emitAck(sock *sio.Socket, event string, payload any, timeout time.Duration) (AckResult, error) {
	resultCh := make(chan ackOutcome, 1)
	sock.Timeout(timeout).EmitWithAck(event, payload)(func(args []any, err error) {
		resultCh <- ackOutcome{args: args, err: err}
	})
	out := <-resultCh
	if out.err != nil {
		return nil, out.err
	}
	if len(out.args) == 0 {
		return AckResult{}, nil
	}
	if m, ok := out.args[0].(map[string]any); ok {
		return AckResult(m), nil
	}
	// Non-object ack: wrap so callers can still inspect.
	return AckResult{"ok": true, "entity": out.args[0]}, nil
}

// ackErrorIsTimeout returns true when an emit-with-ack error should be treated
// as a connection timeout (FR-022 → CONNECTION_ERROR) rather than a disconnect
// (FR-021 → reconnect attempt).
//
// W7: instead of matching the library's error string ("timeout"/"operation"),
// we use the socket's connection state as the authoritative signal:
//   - sock still connected → the error was a 5s ack timeout → CONNECTION_ERROR
//   - sock disconnected    → network/session drop → try one reconnect (FR-021)
//
// Exposed as a package-level function so manager_test.go can unit-test it
// without a live Socket.IO server.
func ackErrorIsTimeout(_ error, connected bool) bool {
	return connected
}

// SocketEmitWithAck emits a Socket.IO event and awaits an ack (FR-022). On
// timeout it returns CONNECTION_ERROR. On disconnect (session_expired or network
// drop) it attempts ONE reconnect and retries (FR-021); if the reconnect fails
// it returns SESSION_EXPIRED.
//
// userID must be the authenticated User.id from the request context (auth.UserID(ctx)).
// It is used to: (a) scope the connection to this user, (b) obtain a per-user
// collab-audience JWT from the AS.
func SocketEmitWithAck(ctx context.Context, whiteboardID, userID, event string, payload any) (AckResult, error) {
	sock, err := GetConnection(ctx, whiteboardID, userID)
	if err != nil {
		return nil, err
	}

	ack, err := emitAck(sock, event, payload, ackTimeout)
	if err == nil {
		return ack, nil
	}

	// W7: use connection state to classify the error, not string matching.
	if ackErrorIsTimeout(err, sock.Connected()) {
		// Socket still alive → ack timed out → CONNECTION_ERROR (FR-022).
		return nil, connectionError()
	}

	// Socket disconnected → attempt ONE reconnect (FR-021).
	removeConnection(whiteboardID, userID)
	fmt.Fprintf(os.Stderr,
		"[liz-whiteboard MCP] Socket disconnected on %s emit (user %s). "+
			"Attempting one reconnect (FR-021)...\n", event, userID)
	sock, rerr := GetConnection(ctx, whiteboardID, userID)
	if rerr != nil {
		return nil, mcperr.New(mcperr.SessionExpired, mcperr.MsgSessionExpired)
	}
	ack, rerr = emitAck(sock, event, payload, ackTimeout)
	if rerr != nil {
		return nil, mcperr.New(mcperr.SessionExpired, mcperr.MsgSessionExpired)
	}
	return ack, nil
}

// CloseAll disconnects all pooled sockets. Registered on process exit/signals.
func CloseAll() {
	mu.Lock()
	defer mu.Unlock()
	for key, sock := range connections {
		sock.Disconnect()
		delete(connections, key)
	}
}

package socket

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAckErrorIsTimeout verifies the W7 classification logic:
//   - when the socket is still connected, any ack error is treated as a
//     5-second timeout → returns true (→ CONNECTION_ERROR in the caller)
//   - when the socket is disconnected, the error is a drop → returns false
//     (→ reconnect attempt in the caller)
//
// The classification is intentionally independent of the error message text,
// making it robust against changes to the upstream Socket.IO library's error
// string formatting.
func TestAckErrorIsTimeout_ConnectedSocket(t *testing.T) {
	// Socket still connected after ack error → must be a timeout (FR-022).
	err := errors.New("some library-specific timeout description")
	assert.True(t, ackErrorIsTimeout(err, true),
		"connected socket with ack error should classify as timeout→CONNECTION_ERROR")
}

func TestAckErrorIsTimeout_DisconnectedSocket(t *testing.T) {
	// Socket disconnected → session drop, not a timeout.
	err := errors.New("socket disconnected")
	assert.False(t, ackErrorIsTimeout(err, false),
		"disconnected socket should not classify as timeout; needs reconnect attempt")
}

func TestAckErrorIsTimeout_NilError_Connected(t *testing.T) {
	// Nil error with connected=true (degenerate case) → still classified as timeout.
	// In practice SocketEmitWithAck never calls this when err==nil, but the
	// function should be pure and consistent.
	assert.True(t, ackErrorIsTimeout(nil, true))
}

func TestAckErrorIsTimeout_NilError_Disconnected(t *testing.T) {
	assert.False(t, ackErrorIsTimeout(nil, false))
}

// TestAckErrorIsTimeout_OperationCancelled verifies that the old string-match
// heuristic ("operation") is NOT the classification criterion — the connection
// state is. Both connected and disconnected states with an "operation cancelled"
// error must be classified correctly by state alone.
func TestAckErrorIsTimeout_IgnoresErrorText(t *testing.T) {
	opErr := errors.New("operation cancelled")
	// Connected → timeout (true), regardless of message content.
	assert.True(t, ackErrorIsTimeout(opErr, true))
	// Disconnected → reconnect needed (false), regardless of message content.
	assert.False(t, ackErrorIsTimeout(opErr, false))
}

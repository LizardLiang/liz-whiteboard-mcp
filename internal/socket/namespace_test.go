package socket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The canvas board and the ER whiteboard live on two different Socket.IO
// namespaces (/canvas/:boardId and /whiteboard/:whiteboardId), so every socket
// helper must carry the namespace explicitly. These tests lock both the URI
// construction and the pool-key distinctness.

const (
	nsTestUser  = "11111111-1111-4111-8111-111111111111"
	nsTestBoard = "22222222-2222-4222-8222-222222222222"
)

func TestSocketURI_Whiteboard(t *testing.T) {
	assert.Equal(t, "ws://localhost:3010/whiteboard/"+nsTestBoard,
		socketURI("ws://localhost:3010", NamespaceWhiteboard, nsTestBoard))
}

func TestSocketURI_Canvas(t *testing.T) {
	assert.Equal(t, "ws://localhost:3010/canvas/"+nsTestBoard,
		socketURI("ws://localhost:3010", NamespaceCanvas, nsTestBoard))
}

// A trailing slash on LIZ_SOCKET_URL must not produce a double slash, which the
// Socket.IO server would read as a different (empty-prefixed) namespace.
func TestSocketURI_TrimsTrailingSlash(t *testing.T) {
	assert.Equal(t, "http://collab:3010/canvas/"+nsTestBoard,
		socketURI("http://collab:3010/", NamespaceCanvas, nsTestBoard))
}

func TestSocketBaseURL_Default(t *testing.T) {
	t.Setenv("LIZ_SOCKET_URL", "")
	assert.Equal(t, "ws://localhost:3010", socketBaseURL())
}

func TestSocketBaseURL_FromEnv(t *testing.T) {
	t.Setenv("LIZ_SOCKET_URL", "ws://collab.internal:9999")
	assert.Equal(t, "ws://collab.internal:9999", socketBaseURL())
}

// Canvas board ids and whiteboard ids are both randomUUID() values drawn from
// one id space, so the pool key must include the namespace or the two alias.
func TestSocketKey_NamespacesDoNotAlias(t *testing.T) {
	wb := socketKey{namespace: NamespaceWhiteboard, resourceID: nsTestBoard, userID: nsTestUser}
	cv := socketKey{namespace: NamespaceCanvas, resourceID: nsTestBoard, userID: nsTestUser}

	require.NotEqual(t, wb, cv)

	pool := map[socketKey]string{}
	pool[wb] = "whiteboard-socket"
	pool[cv] = "canvas-socket"
	assert.Len(t, pool, 2, "same id under two namespaces must occupy two pool slots")
	assert.Equal(t, "whiteboard-socket", pool[wb])
	assert.Equal(t, "canvas-socket", pool[cv])
}

func TestSocketKey_UsersDoNotAlias(t *testing.T) {
	a := socketKey{namespace: NamespaceCanvas, resourceID: nsTestBoard, userID: nsTestUser}
	b := socketKey{namespace: NamespaceCanvas, resourceID: nsTestBoard, userID: "other-user"}
	assert.NotEqual(t, a, b)
}

// The singleflight key must be as discriminating as the map key, otherwise two
// concurrent dials on different namespaces would be coalesced into one socket.
func TestDialKey_DistinctPerNamespace(t *testing.T) {
	wb := socketKey{namespace: NamespaceWhiteboard, resourceID: nsTestBoard, userID: nsTestUser}
	cv := socketKey{namespace: NamespaceCanvas, resourceID: nsTestBoard, userID: nsTestUser}
	assert.NotEqual(t, wb.dialKey(), cv.dialKey())
	assert.Equal(t, NamespaceCanvas+":"+nsTestBoard+":"+nsTestUser, cv.dialKey())
}

// removeConnection must evict only the entry for its own namespace.
func TestRemoveConnection_ScopedToNamespace(t *testing.T) {
	wb := socketKey{namespace: NamespaceWhiteboard, resourceID: nsTestBoard, userID: nsTestUser}
	cv := socketKey{namespace: NamespaceCanvas, resourceID: nsTestBoard, userID: nsTestUser}

	mu.Lock()
	connections[wb] = nil // nil socket: removeConnection must not dereference it
	connections[cv] = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		delete(connections, wb)
		delete(connections, cv)
		mu.Unlock()
	})

	removeConnection(NamespaceCanvas, nsTestBoard, nsTestUser)

	mu.Lock()
	_, wbStillPooled := connections[wb]
	_, cvStillPooled := connections[cv]
	mu.Unlock()

	assert.True(t, wbStillPooled, "removing the canvas socket must not evict the whiteboard socket")
	assert.False(t, cvStillPooled, "the canvas socket must be evicted")
}

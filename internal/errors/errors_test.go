package errors

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func parsePayload(t *testing.T, r McpToolResult) errorPayload {
	t.Helper()
	assert.True(t, r.IsError)
	assert.Len(t, r.Content, 1)
	var p errorPayload
	assert.NoError(t, json.Unmarshal([]byte(r.Content[0].Text), &p))
	return p
}

// TC-UNIT-ERR-02: NOT_FOUND McpError maps correctly.
func TestToMcpErrorResponse_NotFound(t *testing.T) {
	r := ToMcpErrorResponse(New(NotFound, "Table abc not found."))
	p := parsePayload(t, r)
	assert.Equal(t, NotFound, p.Code)
	assert.Contains(t, p.Message, "not found")
}

// Generic (non-McpError) errors are masked.
func TestToMcpErrorResponse_GenericMasked(t *testing.T) {
	r := ToMcpErrorResponse(assertGenericError())
	p := parsePayload(t, r)
	assert.Equal(t, InternalError, p.Code)
	assert.Equal(t, "An internal error occurred.", p.Message)
}

func assertGenericError() error { return &genericErr{} }

type genericErr struct{}

func (*genericErr) Error() string { return "raw driver internals leaking 12345" }

// TC-UNIT-ERR-03: token value is redacted from error output.
func TestToMcpErrorResponse_RedactsToken(t *testing.T) {
	orig, had := os.LookupEnv("LIZ_SESSION_TOKEN")
	os.Setenv("LIZ_SESSION_TOKEN", "secret-token-abc123")
	defer func() {
		if had {
			os.Setenv("LIZ_SESSION_TOKEN", orig)
		} else {
			os.Unsetenv("LIZ_SESSION_TOKEN")
		}
	}()

	r := ToMcpErrorResponse(New(InternalError, "Something went wrong with secret-token-abc123 in it."))
	assert.NotContains(t, r.Content[0].Text, "secret-token-abc123")
	assert.Contains(t, r.Content[0].Text, "[REDACTED]")
}

// TC-UNIT-ERR-04/05: well-known messages carry guidance.
func TestMakeMcpError_WellKnownMessages(t *testing.T) {
	conn := MakeMcpError(ConnectionError, MsgConnectionError, "")
	cp := parsePayload(t, conn)
	assert.Contains(t, cp.Message, "localhost:3010")
	assert.Contains(t, cp.Message, "bun run dev")

	sess := MakeMcpError(SessionExpired, MsgSessionExpired, "")
	sp := parsePayload(t, sess)
	assert.Contains(t, sp.Message, "LIZ_SESSION_TOKEN")
	assert.Contains(t, sp.Message, "session_token cookie")
}

func TestMakeMcpSuccess_Shape(t *testing.T) {
	r := MakeMcpSuccess(map[string]any{"id": "x"})
	assert.False(t, r.IsError)
	assert.Len(t, r.Content, 1)
	assert.JSONEq(t, `{"id":"x"}`, r.Content[0].Text)
}

func TestAckCodeToMcpCode(t *testing.T) {
	assert.Equal(t, Forbidden, AckCodeToMcpCode("FORBIDDEN"))
	assert.Equal(t, NotFound, AckCodeToMcpCode("NOT_FOUND"))
	assert.Equal(t, SessionExpired, AckCodeToMcpCode("SESSION_EXPIRED"))
	assert.Equal(t, ValidationError, AckCodeToMcpCode("anything-else"))
}

// Success responses must omit the isError key entirely.
func TestMakeMcpSuccess_OmitsIsError(t *testing.T) {
	r := MakeMcpSuccess([]string{"a"})
	b, err := json.Marshal(r)
	assert.NoError(t, err)
	assert.NotContains(t, string(b), "isError")
}

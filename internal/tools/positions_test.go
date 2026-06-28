package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// TestBulkUpdatePositions_PartialFailure verifies the non-atomic updated/failed
// split across a successful ack, a server-rejected ack, and an emit error.
func TestBulkUpdatePositions_PartialFailure(t *testing.T) {
	positions := []positionEntry{
		{ID: "id-ok", PositionX: 1, PositionY: 2},
		{ID: "id-reject", PositionX: 3, PositionY: 4},
		{ID: "id-error", PositionX: 5, PositionY: 6},
	}

	emit := func(_ string, payload any) (socket.AckResult, error) {
		tableID := payload.(map[string]any)["tableId"].(string)
		switch tableID {
		case "id-ok":
			return socket.AckResult{"ok": true, "entity": map[string]any{"id": tableID}}, nil
		case "id-reject":
			return socket.AckResult{"ok": false, "code": "FORBIDDEN", "message": "no access"}, nil
		default:
			return nil, mcperr.New(mcperr.ConnectionError, mcperr.MsgConnectionError)
		}
	}

	updated, failed := aggregatePositions(positions, emit)

	assert.Len(t, updated, 1)
	assert.Equal(t, "id-ok", updated[0].ID)

	assert.Len(t, failed, 2)
	byID := map[string]posFailed{}
	for _, f := range failed {
		byID[f.ID] = f
	}
	assert.Equal(t, "FORBIDDEN", byID["id-reject"].Code)
	assert.Equal(t, "no access", byID["id-reject"].Message)
	assert.Equal(t, string(mcperr.ConnectionError), byID["id-error"].Code)
	assert.Contains(t, byID["id-error"].Message, "localhost:3010")
}

// An ack with ok=false but no code defaults to VALIDATION_ERROR.
func TestBulkUpdatePositions_DefaultCode(t *testing.T) {
	positions := []positionEntry{{ID: "x", PositionX: 0, PositionY: 0}}
	emit := func(_ string, _ any) (socket.AckResult, error) {
		return socket.AckResult{"ok": false}, nil
	}
	updated, failed := aggregatePositions(positions, emit)
	assert.Empty(t, updated)
	assert.Len(t, failed, 1)
	assert.Equal(t, string(mcperr.ValidationError), failed[0].Code)
	assert.Equal(t, "Server rejected position update.", failed[0].Message)
}

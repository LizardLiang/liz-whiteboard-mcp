package tools

// Canvas element:update and element:delete after a lost ack: the tool re-reads
// the element and reports the change applied when the stored row shows it,
// otherwise "Outcome unknown" (resolveOpAfterEmitError).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

const canvasOpElementID = "44445555-4444-4444-8444-444455556666"

// lostCanvasAckFns applies each emit to *stored when apply is true, then drops
// the ack. loadElement reads *stored.
func lostCanvasAckFns(stored **data.CanvasElement, apply func(el **data.CanvasElement, event string, payload map[string]any)) canvasTableFns {
	return canvasTableFns{
		authorize: func(context.Context, string) (string, error) { return canvasTestUserID, nil },
		loadElement: func(context.Context, string) (*data.CanvasElement, error) {
			return *stored, nil
		},
		emit: func(_ context.Context, _, _, event string, payload map[string]any) (socket.AckResult, error) {
			if apply != nil {
				apply(stored, event, payload)
			}
			return nil, mcperr.New(mcperr.SessionExpired, mcperr.MsgSessionExpired)
		},
	}
}

func rectangleOnBoard() *data.CanvasElement {
	return &data.CanvasElement{
		ID: canvasOpElementID, BoardID: canvasTestBoardID, Kind: "rectangle",
		PositionX: 1, PositionY: 2, Width: 10, Height: 10,
		Style: data.JSONText(`{"fill":"#0090ff","stroke":"#0090ff","strokeWidth":2,"fontSize":16}`),
	}
}

func TestUpdateCanvasElement_LostAckThatLandedIsApplied(t *testing.T) {
	el := rectangleOnBoard()
	fns := lostCanvasAckFns(&el, func(el **data.CanvasElement, _ string, p map[string]any) {
		next := **el
		next.PositionX = p["positionX"].(float64)
		style, _ := json.Marshal(map[string]any{"fill": "#e5484d", "stroke": "#1c2024", "strokeWidth": 2, "fontSize": 16})
		next.Style = data.JSONText(style)
		*el = &next
	})
	x, fill := 300.0, "#e5484d"

	res, _, err := updateCanvasElement(context.Background(), fns, updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID, ElementID: canvasOpElementID,
		PositionX: &x, Style: &canvasStyleInput{Fill: &fill},
	})

	require.NoError(t, err)
	require.False(t, res.IsError, resultText(t, res))
	assert.Contains(t, resultText(t, res), `"positionX":300`)
}

func TestUpdateCanvasElement_LostAckNotLandedIsUnknown(t *testing.T) {
	el := rectangleOnBoard()
	fns := lostCanvasAckFns(&el, nil)
	x := 300.0

	res, _, err := updateCanvasElement(context.Background(), fns, updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID, ElementID: canvasOpElementID, PositionX: &x,
	})

	require.NoError(t, err)
	require.True(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Outcome unknown")
	assert.Contains(t, text, canvasOpElementID)
	assert.Contains(t, text, "get_canvas_board")
}

func TestDeleteCanvasElement_LostAck(t *testing.T) {
	t.Run("gone is applied", func(t *testing.T) {
		el := rectangleOnBoard()
		fns := lostCanvasAckFns(&el, func(el **data.CanvasElement, _ string, _ map[string]any) { *el = nil })

		res, _, err := deleteCanvasElement(context.Background(), fns,
			deleteCanvasElementInput{CanvasBoardID: canvasTestBoardID, ElementID: canvasOpElementID})

		require.NoError(t, err)
		require.False(t, res.IsError, resultText(t, res))
		assert.Contains(t, resultText(t, res), canvasOpElementID)
	})
	t.Run("still present is unknown", func(t *testing.T) {
		el := rectangleOnBoard()
		fns := lostCanvasAckFns(&el, nil)

		res, _, err := deleteCanvasElement(context.Background(), fns,
			deleteCanvasElementInput{CanvasBoardID: canvasTestBoardID, ElementID: canvasOpElementID})

		require.NoError(t, err)
		require.True(t, res.IsError)
		assert.Contains(t, resultText(t, res), "Outcome unknown")
	})
}

// An answered refusal is reported as before, with no re-read.
func TestUpdateCanvasElement_RefusedAckIsReported(t *testing.T) {
	el := rectangleOnBoard()
	fns := lostCanvasAckFns(&el, nil)
	fns.emit = func(context.Context, string, string, string, map[string]any) (socket.AckResult, error) {
		return socket.AckResult{"ok": false, "code": "NOT_FOUND", "message": "Element not found"}, nil
	}
	fns.loadElement = func(context.Context, string) (*data.CanvasElement, error) {
		t.Error("loadElement read after an answered ack")
		return nil, nil
	}
	x := 1.0

	res, _, err := updateCanvasElement(context.Background(), fns, updateCanvasElementInput{
		CanvasBoardID: canvasTestBoardID, ElementID: canvasOpElementID, PositionX: &x,
	})

	require.NoError(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, resultText(t, res), "NOT_FOUND")
}

func TestUpdateCanvasConnector_LostAck(t *testing.T) {
	stored := &data.CanvasElement{
		ID: cnConnectID, BoardID: canvasTestBoardID, Kind: "connector",
		Props: data.JSONText(storedConnectorProps),
	}
	for name, lands := range map[string]bool{"landed is applied": true, "not landed is unknown": false} {
		t.Run(name, func(t *testing.T) {
			el := *stored
			cur := &el
			fns := canvasConnectorFns{
				authorize:   func(context.Context, string) (string, error) { return canvasTestUserID, nil },
				loadElement: func(context.Context, string) (*data.CanvasElement, error) { return cur, nil },
				emit: func(_ context.Context, _, _, _ string, p map[string]any) (socket.AckResult, error) {
					if lands {
						props, _ := json.Marshal(p["props"])
						next := *cur
						next.Props = data.JSONText(props)
						cur = &next
					}
					return nil, mcperr.New(mcperr.ConnectionError, mcperr.MsgConnectionError)
				},
			}

			res, _, err := updateCanvasConnector(context.Background(), fns, updateRoutingOnly())

			require.NoError(t, err)
			if lands {
				require.False(t, res.IsError, resultText(t, res))
				assert.Contains(t, resultText(t, res), "curved")
				return
			}
			require.True(t, res.IsError)
			assert.Contains(t, resultText(t, res), "Outcome unknown")
		})
	}
}

func TestCanvasElementHolds_UnknownFieldIsNotAMatch(t *testing.T) {
	assert.False(t, canvasElementHolds(rectangleOnBoard(), map[string]any{"rotation": 1.0}))
	assert.True(t, canvasElementHolds(rectangleOnBoard(), map[string]any{
		"elementId": canvasOpElementID, "width": 10.0, "style": map[string]any{"fill": "#0090ff"},
	}))
}

// SocketEmitWithAckNS re-sends once after a reconnect. A delete that landed
// before its ack was lost comes back NOT_FOUND on the re-send: the element the
// pre-read saw is gone, so the delete is applied.
func TestDeleteCanvasElement_ResentDeleteNotFoundIsApplied(t *testing.T) {
	el := rectangleOnBoard()
	fns := lostCanvasAckFns(&el, nil)
	fns.emit = func(context.Context, string, string, string, map[string]any) (socket.AckResult, error) {
		el = nil // the first send landed; its ack was lost
		return socket.AckResult{"ok": false, "code": "NOT_FOUND", "message": "Canvas element not found on this board"}, nil
	}

	res, _, err := deleteCanvasElement(context.Background(), fns,
		deleteCanvasElementInput{CanvasBoardID: canvasTestBoardID, ElementID: canvasOpElementID})

	require.NoError(t, err)
	require.False(t, res.IsError, resultText(t, res))
	assert.Contains(t, resultText(t, res), canvasOpElementID)
}

// An element the pre-read never saw on this board stays NOT_FOUND, with the
// same answer for a missing id and an id on another board.
func TestDeleteCanvasElement_UnseenElementIsNotFound(t *testing.T) {
	notFound := socket.AckResult{"ok": false, "code": "NOT_FOUND", "message": "Canvas element not found on this board"}
	other := rectangleOnBoard()
	other.BoardID = "99998888-7777-4777-8777-777788889999"
	texts := map[string]string{}
	for name, stored := range map[string]*data.CanvasElement{"never existed": nil, "another board": other} {
		el := stored
		fns := lostCanvasAckFns(&el, nil)
		fns.emit = func(context.Context, string, string, string, map[string]any) (socket.AckResult, error) {
			return notFound, nil
		}

		res, _, err := deleteCanvasElement(context.Background(), fns,
			deleteCanvasElementInput{CanvasBoardID: canvasTestBoardID, ElementID: canvasOpElementID})

		require.NoError(t, err)
		require.True(t, res.IsError, name)
		assert.Contains(t, resultText(t, res), "NOT_FOUND", name)
		assert.NotContains(t, resultText(t, res), other.BoardID, name)
		texts[name] = resultText(t, res)
	}
	assert.Equal(t, texts["never existed"], texts["another board"])
}

package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/area"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// Each ER update and move tool re-reads the row after a lost ack and reports
// the op applied only when the row sits on the whiteboard (or table) the
// tool's pre-read authorized. These tests lose every ack and serve a row that
// holds the sent values: on the right board it is applied, on another board it
// is an unknown outcome.
const (
	pinBoard      = "11111111-1111-4111-8111-111111111111"
	pinOtherBoard = "22222222-2222-4222-8222-222222222222"
	pinTable      = "33333333-3333-4333-8333-333333333333"
	pinOtherTable = "44444444-4444-4444-8444-444444444444"
)

func lostEmitFns(reads erReaders) erWriteFns {
	return erWriteFns{
		emit: func(string, string, any) (socket.AckResult, error) {
			return nil, mcperr.New(mcperr.ConnectionError, mcperr.MsgConnectionError)
		},
		reads: reads,
	}
}

// pinCase runs one op against a stored row on rowBoard. The op authorized pinBoard.
type pinCase struct {
	name string
	run  func(boardOfRow string) (socket.AckResult, error)
}

func pinCases() []pinCase {
	name := "orders"
	width := 300.0
	pk := true
	card := "ONE_TO_MANY"
	x, y := 10.0, 20.0
	return []pinCase{
		{"update_table meta", func(b string) (socket.AckResult, error) {
			fns := lostEmitFns(erReaders{table: func(string) (*data.DiagramTable, error) {
				return &data.DiagramTable{ID: pinTable, WhiteboardID: b, Name: name, Width: &width}, nil
			}})
			return emitTableUpdate(fns, pinBoard, map[string]any{"tableId": pinTable},
				updateTableInput{TableID: pinTable, Name: &name, Width: &width})
		}},
		{"update_table move", func(b string) (socket.AckResult, error) {
			fns := lostEmitFns(erReaders{table: func(string) (*data.DiagramTable, error) {
				return &data.DiagramTable{ID: pinTable, WhiteboardID: b, PositionX: &x, PositionY: &y}, nil
			}})
			return emitTableMove(fns, pinBoard, pinTable, x, y)
		}},
		{"update_column", func(b string) (socket.AckResult, error) {
			tableOfRow := pinTable
			if b != pinBoard {
				tableOfRow = pinOtherTable
			}
			fns := lostEmitFns(erReaders{column: func(string) (*data.Column, error) {
				return &data.Column{ID: "c", TableID: tableOfRow, IsPrimaryKey: true}, nil
			}})
			return emitColumnUpdate(fns, pinBoard, pinTable, map[string]any{"columnId": "c"},
				updateColumnInput{ColumnID: "c", IsPrimaryKey: &pk})
		}},
		{"update_relationship", func(b string) (socket.AckResult, error) {
			fns := lostEmitFns(erReaders{relationship: func(string) (*data.Relationship, error) {
				return &data.Relationship{ID: "r", WhiteboardID: b, Cardinality: card}, nil
			}})
			return emitRelationshipUpdate(fns, pinBoard, map[string]any{"relationshipId": "r"},
				updateRelationshipInput{RelationshipID: "r", Cardinality: &card})
		}},
		{"add_table_to_area", func(b string) (socket.AckResult, error) {
			members := []string{pinTable}
			fns := lostEmitFns(erReaders{area: func(string) (*data.Area, error) {
				return &data.Area{ID: "a", WhiteboardID: b, MemberTableIDs: members}, nil
			}})
			return emitAreaMembership(fns, &data.Area{ID: "a", WhiteboardID: pinBoard},
				map[string]any{"areaId": "a"}, members, (*area.AreaBounds)(nil))
		}},
		{"move_area", func(b string) (socket.AckResult, error) {
			fns := lostEmitFns(erReaders{area: func(string) (*data.Area, error) {
				return &data.Area{ID: "a", WhiteboardID: b, PositionX: x, PositionY: y}, nil
			}})
			return emitAreaMove(fns, &data.Area{ID: "a", WhiteboardID: pinBoard}, map[string]any{"areaId": "a"}, x, y)
		}},
	}
}

func TestERUpdateHandlers_LostAckOnTheSameBoardIsApplied(t *testing.T) {
	for _, tc := range pinCases() {
		t.Run(tc.name, func(t *testing.T) {
			ack, err := tc.run(pinBoard)
			require.NoError(t, err)
			assert.True(t, ack.OK())
		})
	}
}

func TestERUpdateHandlers_LostAckWithAWrongBoardRowIsUnknown(t *testing.T) {
	for _, tc := range pinCases() {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.run(pinOtherBoard)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "Outcome unknown")
		})
	}
}

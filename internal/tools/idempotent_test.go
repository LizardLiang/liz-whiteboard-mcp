package tools

// Idempotent creates: every create sends a client-generated id, and a lost ack
// or a refusal is resolved by reading the row back by that id.
//
// The fake sockets below PERSIST the create into an in-memory store and then
// drop the ack (an emit error), or answer the way the app answers a re-sent
// create (ALREADY_EXISTS for ER rows, a generic VALIDATION_ERROR for canvas
// elements). The readers read the same store.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

const (
	testWhiteboard      = "11111111-1111-4111-8111-111111111111"
	otherTestWhiteboard = "22222222-2222-4222-8222-222222222222"
	testTable           = "33333333-3333-4333-8333-333333333333"
)

// absentRows reads nothing: every lookup reports no row.
func absentRows() erReaders {
	return erReaders{
		table:        func(string) (*data.DiagramTable, error) { return nil, nil },
		column:       func(string) (*data.Column, error) { return nil, nil },
		relationship: func(string) (*data.Relationship, error) { return nil, nil },
		area:         func(string) (*data.Area, error) { return nil, nil },
	}
}

func noTable(string) (*data.DiagramTable, error) { return nil, nil }

// erStore is a fake app database for the ER fakes.
type erStore struct {
	tables  map[string]*data.DiagramTable
	columns map[string]*data.Column
}

func newERStore() *erStore {
	return &erStore{tables: map[string]*data.DiagramTable{}, columns: map[string]*data.Column{}}
}

func (s *erStore) readers() erReaders {
	r := absentRows()
	r.table = func(id string) (*data.DiagramTable, error) { return s.tables[id], nil }
	r.column = func(id string) (*data.Column, error) { return s.columns[id], nil }
	return r
}

// persist stores the create the way the app would, under the payload's id.
func (s *erStore) persist(whiteboardID, event string, payload map[string]any) {
	id, _ := payload["id"].(string)
	switch event {
	case "table:create":
		s.tables[id] = &data.DiagramTable{ID: id, WhiteboardID: whiteboardID, Name: payload["name"].(string)}
	case "column:create":
		s.columns[id] = &data.Column{ID: id, TableID: payload["tableId"].(string), Name: payload["name"].(string)}
	}
}

// lostAckFns persists every create and then answers with ack (nil means the
// ack was lost: an emit error).
func lostAckFns(s *erStore, ack socket.AckResult) erWriteFns {
	return erWriteFns{
		emit: func(whiteboardID, event string, payload any) (socket.AckResult, error) {
			s.persist(whiteboardID, event, payload.(map[string]any))
			if ack == nil {
				return nil, mcperr.New(mcperr.SessionExpired, mcperr.MsgSessionExpired)
			}
			return ack, nil
		},
		reads: s.readers(),
	}
}

var alreadyExistsAck = socket.AckResult{"ok": false, "code": "ALREADY_EXISTS", "message": "A table with this id already exists"}

func TestCreateERRow_TableLostAckFindsTheStoredTable(t *testing.T) {
	for name, ack := range map[string]socket.AckResult{
		"emit error":                 nil,
		"re-send got ALREADY_EXISTS": alreadyExistsAck,
	} {
		t.Run(name, func(t *testing.T) {
			s := newERStore()
			payload := map[string]any{"name": "customers"}

			entity, e := createERRow(lostAckFns(s, ack), testWhiteboard, erTableKind, testWhiteboard, payload)

			require.Nil(t, e)
			require.Len(t, s.tables, 1, "exactly one table exists")
			id := payload["id"].(string)
			_, err := uuid.Parse(id)
			require.NoError(t, err, "the create carried a client UUID")
			assert.Equal(t, id, entityIDStr(entity))
			assert.Equal(t, "customers", entity.(*data.DiagramTable).Name)
		})
	}
}

func TestCreateERRow_ColumnLostAckFindsTheStoredColumn(t *testing.T) {
	s := newERStore()
	payload := map[string]any{"tableId": testTable, "name": "email", "dataType": "string"}

	entity, e := createERRow(lostAckFns(s, nil), testWhiteboard, erColumnKind, testTable, payload)

	require.Nil(t, e)
	require.Len(t, s.columns, 1)
	assert.Equal(t, payload["id"], entityIDStr(entity))
	assert.Equal(t, testTable, entity.(*data.Column).TableID)
}

// The app refuses an id held on ANOTHER whiteboard with its generic create
// failure (it never says ALREADY_EXISTS for a row the caller cannot see). The
// re-read finds the row elsewhere: an error, never a success.
func TestCreateERRow_RowOnAnotherBoardIsAnError(t *testing.T) {
	s := newERStore()
	fns := erWriteFns{
		emit: func(_, event string, payload any) (socket.AckResult, error) {
			s.persist(otherTestWhiteboard, event, payload.(map[string]any))
			return socket.AckResult{"ok": false, "code": "VALIDATION_ERROR", "message": "Failed to create table"}, nil
		},
		reads: s.readers(),
	}
	payload := map[string]any{"name": "customers"}

	entity, e := createERRow(fns, testWhiteboard, erTableKind, testWhiteboard, payload)

	assert.Nil(t, entity)
	require.NotNil(t, e)
	assert.Equal(t, mcperr.AlreadyExists, e.Code)
	assert.Contains(t, e.Message, payload["id"].(string))
	assert.Contains(t, e.Message, "nothing was created")
}

func TestCreateERRow_AbsentAfterEmitErrorIsOutcomeUnknownWithTheID(t *testing.T) {
	fns := erWriteFns{
		emit: func(string, string, any) (socket.AckResult, error) {
			return nil, mcperr.New(mcperr.ConnectionError, mcperr.MsgConnectionError)
		},
		reads: absentRows(),
	}
	payload := map[string]any{"name": "customers"}

	_, e := createERRow(fns, testWhiteboard, erTableKind, testWhiteboard, payload)

	require.NotNil(t, e)
	assert.Equal(t, mcperr.ConnectionError, e.Code)
	assert.Contains(t, e.Message, "Outcome unknown — re-read before retrying")
	assert.Contains(t, e.Message, payload["id"].(string))
	assert.Contains(t, e.Message, "get_board")
}

func TestCreateERRow_AbsentAfterAlreadyExistsIsOutcomeUnknown(t *testing.T) {
	fns := erWriteFns{
		emit:  func(string, string, any) (socket.AckResult, error) { return alreadyExistsAck, nil },
		reads: absentRows(),
	}

	_, e := createERRow(fns, testWhiteboard, erTableKind, testWhiteboard, map[string]any{"name": "x"})

	require.NotNil(t, e)
	assert.Contains(t, e.Message, "Outcome unknown")
}

func TestCreateERRow_RefusalWithNoRowKeepsTheAppsMessage(t *testing.T) {
	fns := erWriteFns{
		emit: func(string, string, any) (socket.AckResult, error) {
			return socket.AckResult{"ok": false, "code": "FORBIDDEN", "message": "Table does not belong to this whiteboard"}, nil
		},
		reads: absentRows(),
	}

	_, e := createERRow(fns, testWhiteboard, erColumnKind, testTable, map[string]any{"name": "x"})

	require.NotNil(t, e)
	assert.Equal(t, mcperr.Forbidden, e.Code)
	assert.Equal(t, "Table does not belong to this whiteboard", e.Message)
}

func TestCreateERRow_SuccessReturnsTheAckEntity(t *testing.T) {
	var sentID string
	fns := erWriteFns{
		emit: func(_, _ string, payload any) (socket.AckResult, error) {
			sentID = payload.(map[string]any)["id"].(string)
			return socket.AckResult{"ok": true, "entity": map[string]any{"id": sentID}}, nil
		},
		reads: absentRows(),
	}

	entity, e := createERRow(fns, testWhiteboard, erTableKind, testWhiteboard, map[string]any{"name": "x"})

	require.Nil(t, e)
	assert.Equal(t, sentID, entityIDStr(entity))
}

// The canvas fake persists the element, then answers the way the app answers a
// re-sent create with a taken id (a generic VALIDATION_ERROR), or drops the ack.
func TestCreateCanvasElement_RectangleLostAckFindsTheStoredElement(t *testing.T) {
	for name, ack := range map[string]socket.AckResult{
		"emit error": nil,
		"re-send refused": {
			"ok": false, "code": "VALIDATION_ERROR", "message": "Failed to create canvas element",
		},
	} {
		t.Run(name, func(t *testing.T) {
			stored := map[string]*data.CanvasElement{}
			var sent map[string]any
			fns := canvasTableFns{
				authorize: func(context.Context, string) (string, error) { return "user-1", nil },
				loadElement: func(_ context.Context, id string) (*data.CanvasElement, error) {
					return stored[id], nil
				},
				emit: func(_ context.Context, _, boardID, event string, payload map[string]any) (socket.AckResult, error) {
					require.Equal(t, "element:create", event)
					sent = payload
					id := payload["id"].(string)
					stored[id] = &data.CanvasElement{ID: id, BoardID: boardID, Kind: payload["kind"].(string)}
					if ack == nil {
						return nil, mcperr.New(mcperr.SessionExpired, mcperr.MsgSessionExpired)
					}
					return ack, nil
				},
			}

			res, _, err := createCanvasElement(context.Background(), fns, createCanvasElementInput{
				CanvasBoardID: testWhiteboard, Kind: "rectangle",
				PositionX: 10, PositionY: 20, Width: 100, Height: 50,
			})

			require.NoError(t, err)
			require.False(t, res.IsError, "a create that landed reports success")
			require.Len(t, stored, 1, "exactly one element exists")
			_, perr := uuid.Parse(sent["id"].(string))
			require.NoError(t, perr, "a preset shape carries a client UUID")
			_, hasStyle := sent["style"]
			assert.False(t, hasStyle, "no fill is sent, so the app applies the kind preset")
			assert.Contains(t, resultText(t, res), sent["id"].(string))
		})
	}
}

func TestCreateCanvasElement_IDOnAnotherBoardIsAnError(t *testing.T) {
	fns := canvasTableFns{
		authorize: func(context.Context, string) (string, error) { return "user-1", nil },
		loadElement: func(_ context.Context, id string) (*data.CanvasElement, error) {
			return &data.CanvasElement{ID: id, BoardID: otherTestWhiteboard, Kind: "rectangle"}, nil
		},
		emit: func(context.Context, string, string, string, map[string]any) (socket.AckResult, error) {
			return socket.AckResult{"ok": false, "code": "VALIDATION_ERROR", "message": "Failed to create canvas element"}, nil
		},
	}

	res, _, err := createCanvasElement(context.Background(), fns, createCanvasElementInput{
		CanvasBoardID: testWhiteboard, Kind: "ellipse", PositionX: 0, PositionY: 0, Width: 10, Height: 10,
	})

	require.NoError(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, resultText(t, res), string(mcperr.AlreadyExists))
}

// A batch table whose ack was lost still gets its inline columns, keyed by the
// id read back.
func TestExecuteBatchSchema_TableLostAckStillCreatesItsColumns(t *testing.T) {
	s := newERStore()
	emit := func(event string, payload any) (socket.AckResult, error) {
		p := payload.(map[string]any)
		s.persist(testWhiteboard, event, p)
		if event == "table:create" {
			return nil, mcperr.New(mcperr.SessionExpired, mcperr.MsgSessionExpired)
		}
		return socket.AckResult{"ok": true, "entity": map[string]any{"id": p["id"], "tableId": p["tableId"]}}, nil
	}
	in := batchSchemaUpdateInput{
		WhiteboardID: testWhiteboard,
		Tables:       []batchTableSpec{{Name: "users", Columns: []batchColumnSpec{{Name: "id", DataType: "uuid"}}}},
	}

	result := executeBatchSchema(in, testWhiteboard, emit, s.readers())

	assert.Empty(t, result.Failed)
	require.Len(t, s.tables, 1)
	require.Len(t, result.Created.Columns, 1)
	var tableID string
	for id := range s.tables {
		tableID = id
	}
	assert.Equal(t, tableID, result.Created.Columns[0].(map[string]any)["tableId"])
}

func TestResolveOpAfterEmitError(t *testing.T) {
	emitErr := mcperr.New(mcperr.ConnectionError, mcperr.MsgConnectionError)

	t.Run("applied state reports the re-read entity", func(t *testing.T) {
		entity, e := resolveOpAfterEmitError(emitErr, "update of table t1", "get_board",
			func() (any, bool, error) { return "row", true, nil })
		require.Nil(t, e)
		assert.Equal(t, "row", entity)
	})
	t.Run("other state is unknown and names what to re-read", func(t *testing.T) {
		_, e := resolveOpAfterEmitError(emitErr, "update of table t1", "get_board",
			func() (any, bool, error) { return "row", false, nil })
		require.NotNil(t, e)
		assert.Equal(t, mcperr.ConnectionError, e.Code)
		assert.Contains(t, e.Message, "Outcome unknown — re-read before retrying")
		assert.Contains(t, e.Message, "update of table t1")
	})
	t.Run("delete is applied when the row is absent", func(t *testing.T) {
		entity, e := resolveOpAfterEmitError(emitErr, "delete of table t1", "get_board",
			deletedCheck(noTable, "t1"))
		require.Nil(t, e)
		assert.Equal(t, map[string]any{"id": "t1"}, entity)
	})
	t.Run("delete is unknown while the row is present", func(t *testing.T) {
		present := func(id string) (*data.DiagramTable, error) { return &data.DiagramTable{ID: id}, nil }
		_, e := resolveOpAfterEmitError(emitErr, "delete of table t1", "get_board",
			deletedCheck(present, "t1"))
		require.NotNil(t, e)
	})
}

func TestColumnsShowOrder(t *testing.T) {
	cols := map[string]*data.Column{
		"a": {ID: "a", TableID: "t", Order: 0},
		"b": {ID: "b", TableID: "t", Order: 1},
	}
	read := func(id string) (*data.Column, error) { return cols[id], nil }

	_, applied, err := columnsShowOrder(read, "t", []string{"a", "b"})()
	require.NoError(t, err)
	assert.True(t, applied)

	_, applied, _ = columnsShowOrder(read, "t", []string{"b", "a"})()
	assert.False(t, applied)
}

func TestShowsUpdateChecks(t *testing.T) {
	name, desc := "orders", ""
	assert.True(t, tableShowsUpdate(&data.DiagramTable{Name: "orders"}, updateTableInput{Name: &name, Description: &desc}))
	other := "other"
	assert.False(t, tableShowsUpdate(&data.DiagramTable{Name: "orders"}, updateTableInput{Name: &other}))

	pk := true
	assert.True(t, columnShowsUpdate(&data.Column{IsPrimaryKey: true}, updateColumnInput{IsPrimaryKey: &pk}))
	assert.False(t, columnShowsUpdate(&data.Column{}, updateColumnInput{IsPrimaryKey: &pk}))

	card := "ONE_TO_MANY"
	assert.True(t, relationshipShowsUpdate(&data.Relationship{Cardinality: card}, updateRelationshipInput{Cardinality: &card}))
	assert.False(t, relationshipShowsUpdate(&data.Relationship{Cardinality: "ONE_TO_ONE"}, updateRelationshipInput{Cardinality: &card}))
}

// A table:move whose ack was lost but whose table already sits at the target
// is reported as updated.
func TestBulkUpdatePositions_LostAckAtTargetIsUpdated(t *testing.T) {
	x, y := 5.0, 6.0
	emit := func(string, any) (socket.AckResult, error) {
		return nil, mcperr.New(mcperr.ConnectionError, mcperr.MsgConnectionError)
	}
	read := func(id string) (*data.DiagramTable, error) {
		return &data.DiagramTable{ID: id, PositionX: &x, PositionY: &y}, nil
	}

	updated, failed := aggregatePositions([]positionEntry{{ID: "t1", PositionX: 5, PositionY: 6}}, emit, read)

	assert.Empty(t, failed)
	require.Len(t, updated, 1)
	assert.Equal(t, "t1", updated[0].ID)
}

// resultText is the first text content of a tool result.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content)
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return tc.Text
}

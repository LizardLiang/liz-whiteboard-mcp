// ER write plumbing shared by the table, column, relationship, area and batch
// tools: the socket emitter and row readers they use, and the create path that
// sends a client-generated id (idempotent.go).
package tools

import (
	"context"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/area"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// erReaders reads ER rows by id, bound to one request's context. A reader
// returns nil with no error when no row has that id.
type erReaders struct {
	table        func(id string) (*data.DiagramTable, error)
	column       func(id string) (*data.Column, error)
	relationship func(id string) (*data.Relationship, error)
	area         func(id string) (*data.Area, error)
}

// prodERReaders reads from the app's SQLite database.
func prodERReaders(ctx context.Context) erReaders {
	return erReaders{
		table:        func(id string) (*data.DiagramTable, error) { return data.FindDiagramTableByID(ctx, id) },
		column:       func(id string) (*data.Column, error) { return data.FindColumnByID(ctx, id) },
		relationship: func(id string) (*data.Relationship, error) { return data.FindRelationshipByID(ctx, id) },
		area:         func(id string) (*data.Area, error) { return data.FindAreaByID(ctx, id) },
	}
}

// erWriteFns is the ER create path's socket emitter and row readers, bound to
// one request's context and user. A test swaps both.
type erWriteFns struct {
	emit  func(whiteboardID, event string, payload any) (socket.AckResult, error)
	reads erReaders
}

// prodERWriteFns emits on /whiteboard/<id> as userID and reads from SQLite.
func prodERWriteFns(ctx context.Context, userID string) erWriteFns {
	return erWriteFns{
		emit: func(whiteboardID, event string, payload any) (socket.AckResult, error) {
			return socket.SocketEmitWithAck(ctx, whiteboardID, userID, event, payload)
		},
		reads: prodERReaders(ctx),
	}
}

// erRowKind is one ER row type a create tool writes.
type erRowKind struct {
	event    string
	noun     string
	fallback string
	// lookup reads a row of this kind by id and names the scope a create of
	// it is checked against: the whiteboard, or the table for a column.
	lookup func(r erReaders) rowLookup
}

var (
	erTableKind = erRowKind{
		event: "table:create", noun: "table", fallback: "Server rejected table creation.",
		lookup: func(r erReaders) rowLookup {
			return lookupRow(r.table, func(t *data.DiagramTable) string { return t.WhiteboardID })
		},
	}
	erColumnKind = erRowKind{
		event: "column:create", noun: "column", fallback: "Server rejected column creation.",
		lookup: func(r erReaders) rowLookup {
			return lookupRow(r.column, func(c *data.Column) string { return c.TableID })
		},
	}
	erRelationshipKind = erRowKind{
		event: "relationship:create", noun: "relationship", fallback: "Server rejected relationship creation.",
		lookup: func(r erReaders) rowLookup {
			return lookupRow(r.relationship, func(rel *data.Relationship) string { return rel.WhiteboardID })
		},
	}
	erAreaKind = erRowKind{
		event: "area:create", noun: "area", fallback: "Server rejected area creation.",
		lookup: func(r erReaders) rowLookup {
			return lookupRow(r.area, func(a *data.Area) string { return a.WhiteboardID })
		},
	}
)

// createERRow emits one ER create on whiteboardID with a client-generated id
// and resolves a lost ack or a refusal by reading the row back. scope is the
// whiteboard id, or the table id for a column.
func createERRow(fns erWriteFns, whiteboardID string, kind erRowKind, scope string, payload map[string]any) (any, *mcperr.McpError) {
	create := idCreate{
		send: ackSend(func(p map[string]any) (socket.AckResult, error) {
			return fns.emit(whiteboardID, kind.event, p)
		}, kind.fallback),
		lookup: kind.lookup(fns.reads),
		scope:  scope,
		noun:   kind.noun,
		reread: "get_board",
	}
	return create.run(payload)
}

// emitOp emits one ER update, move, reorder or delete. After an emit error it
// re-reads with check (resolveOpAfterEmitError): when the stored state shows
// the op applied, the returned ack is ok and carries the re-read entity;
// otherwise the error is "Outcome unknown". what names the change in that
// message, e.g. "update of table <id>".
func emitOp(ctx context.Context, whiteboardID, userID, event string, payload any, what string, check opCheck) (socket.AckResult, error) {
	ack, err := socket.SocketEmitWithAck(ctx, whiteboardID, userID, event, payload)
	if err == nil {
		return ack, nil
	}
	entity, e := resolveOpAfterEmitError(err, what, "get_board", check)
	if e != nil {
		return nil, e
	}
	return socket.AckResult{"ok": true, "entity": entity}, nil
}

// emitDelete emits one ER delete of a row the tool's pre-read found. It is
// emitOp, plus resolveDeleteNotFound for the NOT_FOUND a re-sent delete gets
// when its first send landed.
func emitDelete(ctx context.Context, whiteboardID, userID, event string, payload any, what string, check opCheck) (socket.AckResult, error) {
	ack, err := emitOp(ctx, whiteboardID, userID, event, payload, what, check)
	if err != nil {
		return nil, err
	}
	return resolveDeleteNotFound(ack, check), nil
}

// deletedCheck is the opCheck of a delete: applied when no row has the id.
func deletedCheck[T any](read func(id string) (*T, error), id string) opCheck {
	return func() (any, bool, error) {
		row, err := read(id)
		if err != nil {
			return nil, false, err
		}
		return map[string]any{"id": id}, row == nil, nil
	}
}

// rowCheck is the opCheck of an update or move: applied when the row exists
// and shows reports true for it. Every shows pins the row to the whiteboard or
// table the tool's pre-read authorized, so a re-read never reports a row on a
// board the caller was not checked for.
func rowCheck[T any](read func(id string) (*T, error), id string, shows func(*T) bool) opCheck {
	return func() (any, bool, error) {
		row, err := read(id)
		if err != nil || row == nil {
			return nil, false, err
		}
		return row, shows(row), nil
	}
}

// strMatches reports whether a stored nullable string holds want. A NULL
// column matches an empty want.
func strMatches(stored *string, want string) bool {
	if stored == nil {
		return want == ""
	}
	return *stored == want
}

// floatMatches reports whether a stored nullable number holds want.
func floatMatches(stored *float64, want float64) bool {
	return stored != nil && *stored == want
}

// tableShowsUpdate reports whether a stored table holds every field an
// update_table call set.
func tableShowsUpdate(t *data.DiagramTable, in updateTableInput) bool {
	return (in.Name == nil || t.Name == *in.Name) &&
		(in.Description == nil || strMatches(t.Description, *in.Description)) &&
		(in.Width == nil || floatMatches(t.Width, *in.Width)) &&
		(in.Height == nil || floatMatches(t.Height, *in.Height))
}

// columnShowsUpdate reports whether a stored column holds every field an
// update_column call set.
func columnShowsUpdate(c *data.Column, in updateColumnInput) bool {
	return (in.Name == nil || c.Name == *in.Name) &&
		(in.DataType == nil || c.DataType == *in.DataType) &&
		(in.IsPrimaryKey == nil || c.IsPrimaryKey == *in.IsPrimaryKey) &&
		(in.IsForeignKey == nil || c.IsForeignKey == *in.IsForeignKey) &&
		(in.IsUnique == nil || c.IsUnique == *in.IsUnique) &&
		(in.IsNullable == nil || c.IsNullable == *in.IsNullable) &&
		(in.Description == nil || strMatches(c.Description, *in.Description))
}

// relationshipShowsUpdate reports whether a stored relationship holds every
// field an update_relationship call set.
func relationshipShowsUpdate(r *data.Relationship, in updateRelationshipInput) bool {
	return (in.SourceTableID == nil || r.SourceTableID == *in.SourceTableID) &&
		(in.TargetTableID == nil || r.TargetTableID == *in.TargetTableID) &&
		(in.SourceColumnID == nil || r.SourceColumnID == *in.SourceColumnID) &&
		(in.TargetColumnID == nil || r.TargetColumnID == *in.TargetColumnID) &&
		(in.Cardinality == nil || r.Cardinality == *in.Cardinality) &&
		(in.Label == nil || strMatches(r.Label, *in.Label))
}

// columnsShowOrder is the opCheck of reorder_columns: applied when every
// listed column belongs to tableID and stores its list index as its order,
// which is how the app's reorderColumns writes it.
func columnsShowOrder(read func(id string) (*data.Column, error), tableID string, ordered []string) opCheck {
	return func() (any, bool, error) {
		for i, id := range ordered {
			c, err := read(id)
			if err != nil {
				return nil, false, err
			}
			if c == nil || c.TableID != tableID || c.Order != i {
				return nil, false, nil
			}
		}
		return map[string]any{"tableId": tableID, "orderedColumnIds": ordered}, true, nil
	}
}

// areaShowsMembership reports whether a stored area holds the member list an
// add_table_to_area or remove_table_from_area call sent, and the bounds when
// it sent them.
func areaShowsMembership(a *data.Area, members []string, bounds *area.AreaBounds) bool {
	if len(a.MemberTableIDs) != len(members) {
		return false
	}
	for i, id := range members {
		if a.MemberTableIDs[i] != id {
			return false
		}
	}
	return bounds == nil || (a.PositionX == bounds.PositionX && a.PositionY == bounds.PositionY &&
		a.Width == bounds.Width && a.Height == bounds.Height)
}

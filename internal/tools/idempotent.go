// Idempotent creates and the outcome check after a lost ack.
//
// Every create tool sends a client-generated UUID as the new row's id. The app
// stores the row under that id and refuses an id any row already has
// (ALREADY_EXISTS), so a re-sent create cannot make a second row
// (SocketEmitWithAckNS re-sends once after a reconnect).
//
// When the send fails, or the app refuses the create, the tool reads the row
// by its id:
//
//   - present on the board, whiteboard or table the create named: the create
//     landed, report success with the stored row;
//   - present anywhere else: the id belongs to another row, report an error;
//   - absent: a refusal the app explained stands; after a send error or an
//     ALREADY_EXISTS the outcome is unknown, report it with the id.
//
// ER update, move, reorder and delete ops re-read the same way after a send
// error: resolveOpAfterEmitError reports the op applied when the stored state
// shows its result, otherwise unknown.
package tools

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// createSend sends one create carrying payload. It returns the created entity,
// or refused when the app answered and refused the create, or err when no
// answer arrived (the create may or may not have landed).
type createSend func(payload map[string]any) (entity any, refused *mcperr.McpError, err error)

// rowLookup reads a row by id. found is false when no row has that id. scope
// is the board, whiteboard or table the row lives on.
type rowLookup func(id string) (entity any, scope string, found bool, err error)

// lookupRow adapts a typed row reader to a rowLookup. read returns nil when
// no row has the id; scope names the board, whiteboard or table the row is on.
func lookupRow[T any](read func(id string) (*T, error), scope func(*T) string) rowLookup {
	return func(id string) (any, string, bool, error) {
		row, err := read(id)
		if err != nil || row == nil {
			return nil, "", false, err
		}
		return row, scope(row), true, nil
	}
}

// idCreate is one create with a client-generated id.
type idCreate struct {
	send   createSend
	lookup rowLookup
	// scope is the board, whiteboard or table the create named.
	scope string
	// noun names the row in messages, e.g. "table".
	noun string
	// reread names the tool that reads the row back, e.g. "get_board".
	reread string
}

// run sets payload["id"] to a fresh UUID, sends the create and resolves a
// failed send or a refusal by reading the row back.
func (c idCreate) run(payload map[string]any) (any, *mcperr.McpError) {
	id := uuid.NewString()
	payload["id"] = id
	entity, refused, err := c.send(payload)
	if err == nil && refused == nil {
		return entity, nil
	}
	return c.resolve(id, refused, err)
}

func (c idCreate) resolve(id string, refused *mcperr.McpError, sendErr error) (any, *mcperr.McpError) {
	entity, scope, found, readErr := c.lookup(id)
	if readErr == nil && found {
		if scope == c.scope {
			return entity, nil
		}
		return nil, mcperr.NewField(mcperr.AlreadyExists, fmt.Sprintf(
			"The generated %s id %s already belongs to a row elsewhere, so nothing was created. "+
				"Run the create again; it generates a new id.", c.noun, id), "id")
	}
	if readErr == nil && sendErr == nil && refused.Code != mcperr.AlreadyExists {
		return nil, refused
	}
	cause := sendErr
	if cause == nil {
		cause = refused
	}
	return nil, outcomeUnknown(fmt.Sprintf("The %s may or may not have been created. Its id is %s; "+
		"read it with %s and create it again only if that id is absent.", c.noun, id, c.reread), cause)
}

// outcomeUnknown is the error for a write whose result the re-read could not
// prove. It keeps the cause's code (CONNECTION_ERROR, SESSION_EXPIRED) so the
// caller still sees why the write went unanswered.
func outcomeUnknown(detail string, cause error) *mcperr.McpError {
	code := mcperr.ConnectionError
	message := "Outcome unknown — re-read before retrying. " + detail
	var me *mcperr.McpError
	if errors.As(cause, &me) {
		if me.Code != mcperr.AlreadyExists {
			code = me.Code
		}
		message += " Cause: " + me.Message
	}
	return mcperr.New(code, message)
}

// ackSend adapts a socket emit to createSend: an ok ack is the entity, a failed
// ack is a refusal, an emit error is an unanswered send.
func ackSend(emit func(payload map[string]any) (socket.AckResult, error), fallback string) createSend {
	return func(payload map[string]any) (any, *mcperr.McpError, error) {
		ack, err := emit(payload)
		if err != nil {
			return nil, nil, err
		}
		if !ack.OK() {
			return nil, mcperr.New(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), fallback)), nil
		}
		return ack.Entity(), nil, nil
	}
}

// opCheck re-reads the stored state after an op's send failed. applied is true
// when the state shows the op's result; entity is what the tool reports then.
type opCheck func() (entity any, applied bool, err error)

// resolveOpAfterEmitError reports an ER update, move, reorder or delete whose
// emit failed: the re-read state when it shows the op applied, otherwise an
// "Outcome unknown" error naming what to re-read. what names the change, e.g.
// "update of table <id>"; reread names the tool that reads it back.
func resolveOpAfterEmitError(emitErr error, what, reread string, check opCheck) (any, *mcperr.McpError) {
	entity, applied, err := check()
	if err == nil && applied {
		return entity, nil
	}
	return nil, outcomeUnknown(fmt.Sprintf("The %s may or may not have been applied. "+
		"Read it with %s before you retry.", what, reread), emitErr)
}

// resolveDeleteNotFound resolves a NOT_FOUND ack to a delete of a row the
// tool's pre-read found. SocketEmitWithAckNS re-sends once after a reconnect,
// so a delete that landed before its ack was lost comes back NOT_FOUND. When
// check shows the row gone, the ack becomes ok and carries check's entity.
// Every other ack comes back unchanged.
//
// Call it only after the pre-read found the row. For an id the pre-read did
// not find, NOT_FOUND is the answer: a delete of a row that never existed is
// not applied.
func resolveDeleteNotFound(ack socket.AckResult, check opCheck) socket.AckResult {
	if ack.OK() || ack.Code() != string(mcperr.NotFound) {
		return ack
	}
	entity, applied, err := check()
	if err != nil || !applied {
		return ack
	}
	return socket.AckResult{"ok": true, "entity": entity}
}

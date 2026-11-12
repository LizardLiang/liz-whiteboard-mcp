// Cross-file table reference tools: create_table_reference,
// update_table_reference, delete_table_reference, list_table_references.
// LizMeter #83.
//
// WHAT A REFERENCE IS. A node on one ER whiteboard standing in for a table that
// lives on ANOTHER whiteboard of the same project. It is stored as a
// DiagramTable row carrying the source ids, plus stub Column rows for the
// picked columns — which is what lets an ordinary create_relationship connect a
// local table to it. An agent therefore wires a cross-file relationship with
// the tools it already has; these four only manage the reference itself.
//
// AUTHORIZATION. EDITOR or higher for create, update and delete; VIEWER for
// list. Same-project scope is enforced by the app, which can see both boards'
// projects. Update and delete also run authorizeNode here, before any read of
// the node, so a node the caller cannot see answers NOT_FOUND like a missing id.
//
// DESTRUCTIVE GUARDS. Two of these tools destroy relationships:
//
//   - delete_table_reference removes the node and cascades every relationship
//     drawn from it, so it requires a confirmName like the other deletes. The
//     name is compared against the SOURCE table's name as list_table_references
//     reports it, read straight from SQLite before the delete request is sent.
//   - update_table_reference deletes the relationships whose endpoint column
//     does not survive the re-target. It does NOT require confirmation — the
//     agent asked to point the reference somewhere else, and the result reports
//     how many relationships that cost, so the loss is visible rather than
//     silent.
//
// PANIC SAFETY. Every handler returns an MCP error for a missing whiteboard,
// table or column. recoverPanics turns a handler panic into a masked error, but
// a handler still must not dereference values the app is only expected to send.
package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// referenceColumnsMax mirrors the route's z.array(...).max(100).
const referenceColumnsMax = 100

type createTableReferenceInput struct {
	WhiteboardID       string   `json:"whiteboardId"`
	SourceWhiteboardID string   `json:"sourceWhiteboardId" jsonschema:"Source board; a different board, same project"`
	SourceTableID      string   `json:"sourceTableId"`
	SourceColumnIDs    []string `json:"sourceColumnIds" jsonschema:"Columns to expose, 1 to 100"`
	PositionX          *float64 `json:"positionX,omitempty" jsonschema:"Omit to let the client place it"`
	PositionY          *float64 `json:"positionY,omitempty" jsonschema:"Omit to let the client place it"`
}

type updateTableReferenceInput struct {
	TableID            string   `json:"tableId"`
	SourceWhiteboardID *string  `json:"sourceWhiteboardId,omitempty" jsonschema:"Omit to keep the current one"`
	SourceTableID      *string  `json:"sourceTableId,omitempty" jsonschema:"Omit to keep the current one"`
	SourceColumnIDs    []string `json:"sourceColumnIds,omitempty" jsonschema:"Up to 100; omit to keep the current ones"`
}

type deleteTableReferenceInput struct {
	TableID     string `json:"tableId"`
	ConfirmName string `json:"confirmName" jsonschema:"Referenced table's exact name; a mismatch deletes nothing"`
}

type listTableReferencesInput struct {
	WhiteboardID string `json:"whiteboardId"`
}

// tableReferenceFns is the injectable pipeline, mirroring folderLifecycleFns.
type tableReferenceFns struct {
	createReference func(ctx context.Context, userID string, req appapi.CreateTableReferenceRequest) (map[string]any, error)
	updateReference func(ctx context.Context, userID, tableID string, req appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error)
	deleteReference func(ctx context.Context, userID, tableID string) (map[string]any, error)
	listReferences  func(ctx context.Context, userID, whiteboardID string) ([]map[string]any, error)
	// confirmNameOf reads, straight from SQLite, the name delete_table_reference
	// must match confirmName against. Direct read rather than a route call: the
	// guard has to run BEFORE the destructive request leaves this process.
	confirmNameOf func(ctx context.Context, tableID string) (*data.ReferenceConfirmName, error)
	// findTable reads a reference node's DiagramTable row by id, to resolve a
	// create whose response was lost (idempotent.go).
	findTable func(ctx context.Context, id string) (*data.DiagramTable, error)
	// findColumns reads a reference node's stub columns, so a create resolved
	// by the re-read returns the same {table, columns} shape as a normal one.
	findColumns func(ctx context.Context, tableID string) ([]data.Column, error)
	// referenceTarget reads what a reference node points at, to resolve a
	// re-target whose response was lost.
	referenceTarget func(ctx context.Context, tableID string) (*data.ReferenceTarget, error)
	// tableProject, assertAccess and assertEdit gate update and delete on the
	// reference node's project (authorizeNode).
	tableProject func(ctx context.Context, tableID string) (string, error)
	assertAccess func(ctx context.Context, userID, projectID string) error
	assertEdit   func(ctx context.Context, userID, projectID string) error
}

// prodTableReferenceFns is the production pipeline.
func prodTableReferenceFns() tableReferenceFns {
	return tableReferenceFns{
		createReference: appapi.CreateTableReference,
		updateReference: appapi.UpdateTableReference,
		deleteReference: appapi.DeleteTableReference,
		listReferences:  appapi.ListTableReferences,
		confirmNameOf:   data.FindReferenceConfirmName,
		findTable:       data.FindDiagramTableByID,
		findColumns:     data.ListColumnsByTableID,
		referenceTarget: data.FindReferenceTarget,
		tableProject:    data.GetTableProjectID,
		assertAccess:    auth.AssertProjectAccess,
		assertEdit:      auth.AssertSchemaEditAccess,
	}
}

// validateColumnIDs checks the picked-column list. `required` distinguishes
// create (at least one) from update (an omitted list keeps the current set).
func validateColumnIDs(ids []string, required bool) *mcperr.McpError {
	if len(ids) == 0 {
		if required {
			return mcperr.NewField(mcperr.ValidationError,
				"sourceColumnIds must name at least one column of the source table. "+
					"Read the table's columns with get_board first (get_schema_summary omits UUIDs).",
				"sourceColumnIds")
		}
		return nil
	}
	if len(ids) > referenceColumnsMax {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("sourceColumnIds may name at most %d columns.", referenceColumnsMax),
			"sourceColumnIds")
	}
	for _, id := range ids {
		if e := validateUUID("sourceColumnIds", id); e != nil {
			return e
		}
	}
	return nil
}

// createTableReferenceWithFns validates and creates one reference.
func createTableReferenceWithFns(
	ctx context.Context,
	fns tableReferenceFns,
	userID string,
	in createTableReferenceInput,
) (map[string]any, error) {
	if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
		return nil, e
	}
	if e := validateUUID("sourceWhiteboardId", in.SourceWhiteboardID); e != nil {
		return nil, e
	}
	if e := validateUUID("sourceTableId", in.SourceTableID); e != nil {
		return nil, e
	}
	if e := validateColumnIDs(in.SourceColumnIDs, true); e != nil {
		return nil, e
	}
	// Caught here rather than by the app so the message can say what to do:
	// a reference to a table on the same board is a relationship, not a
	// reference, and create_relationship is the tool for that.
	if in.SourceWhiteboardID == in.WhiteboardID {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"sourceWhiteboardId is the same board the reference would live on. "+
				"To connect two tables on ONE board, use create_relationship instead.",
			"sourceWhiteboardId")
	}
	// The node carries a client-generated id, so a lost response is resolved
	// by reading the node back instead of creating a second one.
	create := idCreate{
		send: func(payload map[string]any) (any, *mcperr.McpError, error) {
			id, _ := payload["id"].(string)
			reference, err := fns.createReference(ctx, userID, appapi.CreateTableReferenceRequest{
				ID:                 id,
				WhiteboardID:       in.WhiteboardID,
				SourceWhiteboardID: in.SourceWhiteboardID,
				SourceTableID:      in.SourceTableID,
				SourceColumnIDs:    in.SourceColumnIDs,
				PositionX:          in.PositionX,
				PositionY:          in.PositionY,
			})
			if err == nil {
				return reference, nil, nil
			}
			// Only a call that got no HTTP status back may have landed
			// (McpError.NoResponse). Every other error is the app's answer or
			// a failure before the request left: a refusal. The re-read still
			// heals a 5xx or an unreadable body whose create did land.
			var me *mcperr.McpError
			if errors.As(err, &me) && !me.NoResponse {
				return nil, me, nil
			}
			return nil, nil, err
		},
		lookup: func(id string) (any, string, bool, error) {
			table, err := fns.findTable(ctx, id)
			if err != nil || table == nil {
				return nil, "", false, err
			}
			columns, err := fns.findColumns(ctx, id)
			if err != nil {
				return nil, "", false, err
			}
			return map[string]any{"table": table, "columns": columns}, table.WhiteboardID, true, nil
		},
		scope:  in.WhiteboardID,
		noun:   "table reference",
		reread: "list_table_references",
	}
	entity, e := create.run(map[string]any{})
	if e != nil {
		return nil, e
	}
	reference, _ := entity.(map[string]any)
	return reference, nil
}

// updateTableReferenceWithFns validates and re-targets one reference.
func updateTableReferenceWithFns(
	ctx context.Context,
	fns tableReferenceFns,
	userID string,
	in updateTableReferenceInput,
) (map[string]any, error) {
	if e := validateUUID("tableId", in.TableID); e != nil {
		return nil, e
	}
	if in.SourceWhiteboardID != nil {
		if e := validateUUID("sourceWhiteboardId", *in.SourceWhiteboardID); e != nil {
			return nil, e
		}
	}
	if in.SourceTableID != nil {
		if e := validateUUID("sourceTableId", *in.SourceTableID); e != nil {
			return nil, e
		}
	}
	if e := validateColumnIDs(in.SourceColumnIDs, false); e != nil {
		return nil, e
	}
	// An update that names nothing is a no-op that still costs a round trip and
	// reads as a success, which is worse than being told to name a change.
	if in.SourceWhiteboardID == nil && in.SourceTableID == nil && len(in.SourceColumnIDs) == 0 {
		return nil, mcperr.New(mcperr.ValidationError,
			"Name at least one of sourceWhiteboardId, sourceTableId or sourceColumnIds. "+
				"Nothing was changed.")
	}
	// Pointing a reference at a table on the board it lives on is the same
	// mistake create guards against, reached the other way round.
	if in.SourceTableID != nil && *in.SourceTableID == in.TableID {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"sourceTableId is the reference node itself. A reference cannot point at itself.",
			"sourceTableId")
	}

	if err := fns.authorizeNode(ctx, userID, in.TableID); err != nil {
		return nil, err
	}

	result, err := fns.updateReference(ctx, userID, in.TableID, appapi.UpdateTableReferenceRequest{
		SourceWhiteboardID: in.SourceWhiteboardID,
		SourceTableID:      in.SourceTableID,
		SourceColumnIDs:    in.SourceColumnIDs,
	})
	if err != nil && isLostResponse(err) {
		entity, e := resolveOpAfterEmitError(err, "re-target of table reference "+in.TableID,
			"list_table_references", func() (any, bool, error) {
				// The re-read runs the gate again: a caller who lost access
				// since the send learns nothing of the node.
				if fns.authorizeNode(ctx, userID, in.TableID) != nil {
					return nil, false, nil
				}
				target, rerr := fns.referenceTarget(ctx, in.TableID)
				if rerr != nil || target == nil {
					return nil, false, rerr
				}
				return map[string]any{
					"reference": target,
					"note":      "Re-targeted; the response was lost, so the count of deleted relationships is not known.",
				}, referenceShowsUpdate(target, in), nil
			})
		if e != nil {
			return nil, e
		}
		out, _ := entity.(map[string]any)
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, mcperr.New(mcperr.InternalError,
			"The table reference was re-targeted but the app returned no result.")
	}
	// The deleted count rides in the payload rather than only in prose: an
	// agent that re-targets in a loop needs to total it, not parse it.
	return map[string]any{
		"reference":            result.Reference,
		"deletedRelationships": result.DeletedRelationships,
	}, nil
}

// deleteTableReferenceWithFns validates, checks confirmName, and deletes.
//
// confirmName is matched against the SOURCE table's name as list_table_
// references reports it — not the local row's stored name, which may carry a
// de-duplicating suffix the agent never saw.
func deleteTableReferenceWithFns(
	ctx context.Context,
	fns tableReferenceFns,
	userID string,
	in deleteTableReferenceInput,
) (map[string]any, error) {
	if e := validateUUID("tableId", in.TableID); e != nil {
		return nil, e
	}
	if in.ConfirmName == "" {
		return nil, mcperr.NewField(mcperr.ValidationError,
			"confirmName is required: pass the referenced table's exact name to confirm this delete. "+
				"Read it with list_table_references first.", "confirmName")
	}

	// The gate runs before the name read: the name checks below tell a missing
	// id, an ordinary table and a wrong name apart, which only a caller who
	// can see the node may learn.
	if err := fns.authorizeNode(ctx, userID, in.TableID); err != nil {
		return nil, err
	}
	expected, err := fns.confirmNameOf(ctx, in.TableID)
	if err != nil {
		return nil, err
	}
	if expected == nil {
		return nil, referenceNotFound(in.TableID)
	}
	if !expected.IsReference {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("Table %s is an ordinary table, not a cross-file reference. "+
				"Nothing was deleted. Use delete_table for a real table.", in.TableID), "tableId")
	}
	if expected.Name != in.ConfirmName {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("confirmName does not match the referenced table's name for %s. "+
				"Nothing was deleted. Read the reference with list_table_references and pass "+
				"its exact sourceTableName.", in.TableID), "confirmName")
	}

	deleted, err := fns.deleteReference(ctx, userID, in.TableID)
	if err != nil && isLostResponse(err) {
		entity, e := resolveOpAfterEmitError(err, "delete of table reference "+in.TableID,
			"list_table_references", deletedCheck(func(id string) (*data.DiagramTable, error) {
				return fns.findTable(ctx, id)
			}, in.TableID))
		if e != nil {
			return nil, e
		}
		out, _ := entity.(map[string]any)
		return out, nil
	}
	return deleted, err
}

// isLostResponse reports whether a reference route call got no HTTP status
// back (McpError.NoResponse: a transport failure or a timeout), so the change
// may or may not have been applied. Every answer from the app (a 403, a 404, a
// 429, a 5xx, an unreadable body) and an error of unknown origin is reported
// as it is, without a re-read.
func isLostResponse(err error) bool {
	var me *mcperr.McpError
	return errors.As(err, &me) && me.NoResponse
}

// referenceNotFound is the one NOT_FOUND answer for a reference node that is
// missing or that the caller cannot see.
func referenceNotFound(tableID string) *mcperr.McpError {
	return mcperr.New(mcperr.NotFound, fmt.Sprintf("Table reference %s not found.", tableID))
}

// authorizeNode gates update and delete on the reference node's project:
// EDITOR or higher, the same gate as the other schema writes. A node the
// caller has no access to gets the same NOT_FOUND as a missing id, so the
// answer does not reveal that the id exists.
func (fns tableReferenceFns) authorizeNode(ctx context.Context, userID, tableID string) error {
	projectID, err := fns.tableProject(ctx, tableID)
	if err != nil {
		return err
	}
	if projectID == "" {
		return referenceNotFound(tableID)
	}
	if err := fns.assertAccess(ctx, userID, projectID); err != nil {
		var me *mcperr.McpError
		if errors.As(err, &me) && me.Code == mcperr.Forbidden {
			return referenceNotFound(tableID)
		}
		return err
	}
	return fns.assertEdit(ctx, userID, projectID)
}

// referenceShowsUpdate reports whether a stored reference points where an
// update_table_reference call asked. The app replaces the stub columns with
// exactly the requested source columns, in any order.
func referenceShowsUpdate(target *data.ReferenceTarget, in updateTableReferenceInput) bool {
	if in.SourceWhiteboardID != nil && (target.SourceWhiteboardID == nil ||
		*target.SourceWhiteboardID != *in.SourceWhiteboardID) {
		return false
	}
	if in.SourceTableID != nil && (target.SourceTableID == nil ||
		*target.SourceTableID != *in.SourceTableID) {
		return false
	}
	if len(in.SourceColumnIDs) == 0 {
		return true
	}
	if len(target.SourceColumnIDs) != len(in.SourceColumnIDs) {
		return false
	}
	want := make(map[string]bool, len(in.SourceColumnIDs))
	for _, id := range in.SourceColumnIDs {
		want[id] = true
	}
	for _, id := range target.SourceColumnIDs {
		if !want[id] {
			return false
		}
	}
	return true
}

// listTableReferencesWithFns validates and lists.
func listTableReferencesWithFns(
	ctx context.Context,
	fns tableReferenceFns,
	userID string,
	in listTableReferencesInput,
) (map[string]any, error) {
	if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
		return nil, e
	}
	references, err := fns.listReferences(ctx, userID, in.WhiteboardID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"whiteboardId": in.WhiteboardID,
		"references":   references,
		"count":        len(references),
	}, nil
}

// RegisterTableReferenceTools registers the four cross-file reference tools.
func RegisterTableReferenceTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_table_reference",
		Description: "Place a reference node on an ER whiteboard for a table on ANOTHER whiteboard of the same project, so local tables can relate to it. Needs UUIDs from get_board (not get_schema_summary). EDITOR role. Guide: get_tool_guide.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTableReferenceInput) (*mcp.CallToolResult, any, error) {
		reference, err := createTableReferenceWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(reference)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_table_reference",
		Description: "Re-target a reference node at another board, table, or columns. Relationships on columns that do not survive are deleted. EDITOR role.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTableReferenceInput) (*mcp.CallToolResult, any, error) {
		result, err := updateTableReferenceWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(result)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_table_reference",
		Description: "Delete a reference node and its relationships. confirmName is the referenced table's exact name (see list_table_references). EDITOR role.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTableReferenceInput) (*mcp.CallToolResult, any, error) {
		reference, err := deleteTableReferenceWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(reference)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_table_references",
		Description: "List a whiteboard's cross-board references with the source table, board, and exposed columns; missing=true marks a deleted source.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTableReferencesInput) (*mcp.CallToolResult, any, error) {
		result, err := listTableReferencesWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(result)
	})
}

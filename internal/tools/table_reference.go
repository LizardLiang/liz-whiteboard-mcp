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
// projects — this layer cannot, and does not pretend to.
//
// DESTRUCTIVE GUARDS. Two of these tools destroy relationships:
//
//   - delete_table_reference removes the node and cascades every relationship
//     drawn from it, so it requires a confirmName like the other deletes.
//   - update_table_reference deletes the relationships whose endpoint column
//     does not survive the re-target. It does NOT require confirmation — the
//     agent asked to point the reference somewhere else, and the result reports
//     how many relationships that cost, so the loss is visible rather than
//     silent.
//
// PANIC SAFETY. Every handler returns an MCP error for a missing whiteboard,
// table or column. This server has no panic recovery: one nil dereference exits
// the process and takes every other tool down with it, so nothing here
// dereferences a value the app is merely expected to have sent.
package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// referenceColumnsMax mirrors the route's z.array(...).max(100).
const referenceColumnsMax = 100

type createTableReferenceInput struct {
	WhiteboardID       string   `json:"whiteboardId" jsonschema:"UUID of the whiteboard the reference node is placed on"`
	SourceWhiteboardID string   `json:"sourceWhiteboardId" jsonschema:"UUID of the whiteboard that owns the referenced table; must be a different board in the SAME project"`
	SourceTableID      string   `json:"sourceTableId" jsonschema:"UUID of the referenced table, in the source whiteboard"`
	SourceColumnIDs    []string `json:"sourceColumnIds" jsonschema:"UUIDs of the referenced table's columns to expose, 1 to 100; each becomes a connectable handle on the node"`
	PositionX          *float64 `json:"positionX,omitempty" jsonschema:"Optional canvas X position; omit to let the first browser client place the node"`
	PositionY          *float64 `json:"positionY,omitempty" jsonschema:"Optional canvas Y position; omit to let the first browser client place the node"`
}

type updateTableReferenceInput struct {
	TableID            string   `json:"tableId" jsonschema:"UUID of the reference node to re-target"`
	SourceWhiteboardID *string  `json:"sourceWhiteboardId,omitempty" jsonschema:"Optional new source whiteboard UUID; omit to keep the current one"`
	SourceTableID      *string  `json:"sourceTableId,omitempty" jsonschema:"Optional new source table UUID; omit to keep the current one"`
	SourceColumnIDs    []string `json:"sourceColumnIds,omitempty" jsonschema:"Optional new set of source column UUIDs, up to 100; omit to keep the current ones"`
}

type deleteTableReferenceInput struct {
	TableID     string `json:"tableId" jsonschema:"UUID of the reference node to delete"`
	ConfirmName string `json:"confirmName" jsonschema:"The referenced table's exact name as list_table_references reports it, required as confirmation; the delete is refused if it does not match"`
}

type listTableReferencesInput struct {
	WhiteboardID string `json:"whiteboardId" jsonschema:"UUID of the whiteboard whose references to list"`
}

// tableReferenceFns is the injectable pipeline, mirroring folderLifecycleFns.
type tableReferenceFns struct {
	createReference func(ctx context.Context, userID string, req appapi.CreateTableReferenceRequest) (map[string]any, error)
	updateReference func(ctx context.Context, userID, tableID string, req appapi.UpdateTableReferenceRequest) (*appapi.UpdateTableReferenceResult, error)
	deleteReference func(ctx context.Context, userID, tableID string) (map[string]any, error)
	listReferences  func(ctx context.Context, userID, whiteboardID string) ([]map[string]any, error)
}

// prodTableReferenceFns is the production pipeline.
func prodTableReferenceFns() tableReferenceFns {
	return tableReferenceFns{
		createReference: appapi.CreateTableReference,
		updateReference: appapi.UpdateTableReference,
		deleteReference: appapi.DeleteTableReference,
		listReferences:  appapi.ListTableReferences,
	}
}

// validateColumnIDs checks the picked-column list. `required` distinguishes
// create (at least one) from update (an omitted list keeps the current set).
func validateColumnIDs(ids []string, required bool) *mcperr.McpError {
	if len(ids) == 0 {
		if required {
			return mcperr.NewField(mcperr.ValidationError,
				"sourceColumnIds must name at least one column of the source table. "+
					"Read the table's columns with get_schema_summary or get_board first.",
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
	return fns.createReference(ctx, userID, appapi.CreateTableReferenceRequest{
		WhiteboardID:       in.WhiteboardID,
		SourceWhiteboardID: in.SourceWhiteboardID,
		SourceTableID:      in.SourceTableID,
		SourceColumnIDs:    in.SourceColumnIDs,
		PositionX:          in.PositionX,
		PositionY:          in.PositionY,
	})
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

	result, err := fns.updateReference(ctx, userID, in.TableID, appapi.UpdateTableReferenceRequest{
		SourceWhiteboardID: in.SourceWhiteboardID,
		SourceTableID:      in.SourceTableID,
		SourceColumnIDs:    in.SourceColumnIDs,
	})
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
	return fns.deleteReference(ctx, userID, in.TableID)
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
		Name: "create_table_reference",
		Description: "Place a node on one ER whiteboard that references a table living on ANOTHER " +
			"whiteboard of the same project, so a local table can be related to it. Name the " +
			"source board, the table, and the columns to expose; each exposed column becomes a " +
			"connectable handle that create_relationship can target. Use list_whiteboards and " +
			"get_schema_summary to find the ids. Requires the EDITOR role or higher.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTableReferenceInput) (*mcp.CallToolResult, any, error) {
		reference, err := createTableReferenceWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(reference)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_table_reference",
		Description: "Point an existing reference node at a different board, table or set of columns, " +
			"keeping the node and every relationship whose column survives the change. " +
			"Relationships drawn from a column that does NOT survive are deleted; the result " +
			"reports how many. Requires the EDITOR role or higher.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTableReferenceInput) (*mcp.CallToolResult, any, error) {
		result, err := updateTableReferenceWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(result)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_table_reference",
		Description: "Delete a reference node. Every relationship drawn from it is deleted too. " +
			"Pass confirmName with the referenced table's exact name, as list_table_references " +
			"reports it. Requires the EDITOR role or higher.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTableReferenceInput) (*mcp.CallToolResult, any, error) {
		reference, err := deleteTableReferenceWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(reference)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_table_references",
		Description: "List every cross-file reference on a whiteboard, resolved against its source: " +
			"the referenced table's current name, the file it lives in, and the exposed columns. " +
			"A reference whose source has been deleted comes back with missing=true, keeping its " +
			"relationships so nothing disappears silently. Requires read access to the project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTableReferencesInput) (*mcp.CallToolResult, any, error) {
		result, err := listTableReferencesWithFns(ctx, prodTableReferenceFns(), auth.UserID(ctx), in)
		if err != nil {
			return fail(err)
		}
		return success(result)
	})
}

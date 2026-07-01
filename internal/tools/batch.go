package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/socket"
)

// ---------------------------------------------------------------------------
// Input types
// ---------------------------------------------------------------------------

// batchColumnSpec describes a column to create inline with a new table.
// Order within the table is determined by its position in the array.
type batchColumnSpec struct {
	Name         string  `json:"name"                    jsonschema:"Column name"`
	DataType     string  `json:"dataType"                jsonschema:"Column data type"`
	IsPrimaryKey *bool   `json:"isPrimaryKey,omitempty"  jsonschema:"Is primary key"`
	IsForeignKey *bool   `json:"isForeignKey,omitempty"  jsonschema:"Is foreign key"`
	IsUnique     *bool   `json:"isUnique,omitempty"      jsonschema:"Is unique"`
	IsNullable   *bool   `json:"isNullable,omitempty"    jsonschema:"Is nullable"`
	Description  *string `json:"description,omitempty"   jsonschema:"Optional description"`
}

// batchTableSpec describes a new table to create. Inline Columns are created
// immediately after the table:create ack succeeds.
type batchTableSpec struct {
	Name        string            `json:"name"                  jsonschema:"Table name (must be unique within this batch for name-based relation resolution)"`
	Description *string           `json:"description,omitempty"`
	PositionX   *float64          `json:"positionX,omitempty"   jsonschema:"X position (auto-assigned if omitted)"`
	PositionY   *float64          `json:"positionY,omitempty"   jsonschema:"Y position (auto-assigned if omitted)"`
	Columns     []batchColumnSpec `json:"columns,omitempty"`
}

// batchAddColumnSpec describes a column to add to a pre-existing table (by UUID).
type batchAddColumnSpec struct {
	TableID      string  `json:"tableId"                 jsonschema:"Existing table UUID"`
	Name         string  `json:"name"`
	DataType     string  `json:"dataType"`
	IsPrimaryKey *bool   `json:"isPrimaryKey,omitempty"`
	IsForeignKey *bool   `json:"isForeignKey,omitempty"`
	IsUnique     *bool   `json:"isUnique,omitempty"`
	IsNullable   *bool   `json:"isNullable,omitempty"`
	Description  *string `json:"description,omitempty"`
}

// batchRelationSpec describes a relation to create. Each endpoint is identified
// by EITHER a UUID (for pre-existing or already-created entities) OR a name
// string (resolved from tables/columns created in this same batch call).
// Exactly one of *TableID / *TableName must be set for each endpoint.
// Exactly one of *ColumnID / *ColumnName must be set for each endpoint.
// When tableId is used, columnId must also be used (name lookup is unavailable
// for externally-referenced tables).
type batchRelationSpec struct {
	SourceTableID    *string `json:"sourceTableId,omitempty"    jsonschema:"UUID of source table (pre-existing)"`
	SourceTableName  *string `json:"sourceTableName,omitempty"  jsonschema:"Name of source table created in this batch"`
	TargetTableID    *string `json:"targetTableId,omitempty"`
	TargetTableName  *string `json:"targetTableName,omitempty"`
	SourceColumnID   *string `json:"sourceColumnId,omitempty"   jsonschema:"UUID of source column (pre-existing)"`
	SourceColumnName *string `json:"sourceColumnName,omitempty" jsonschema:"Name of source column created in this batch"`
	TargetColumnID   *string `json:"targetColumnId,omitempty"`
	TargetColumnName *string `json:"targetColumnName,omitempty"`
	Cardinality      string  `json:"cardinality"`
	Label            *string `json:"label,omitempty"`
}

// batchSchemaUpdateInput is the full input for batch_schema_update.
type batchSchemaUpdateInput struct {
	WhiteboardID string               `json:"whiteboardId"           jsonschema:"The whiteboard UUID"`
	Tables       []batchTableSpec     `json:"tables,omitempty"       jsonschema:"New tables to create (with optional inline columns)"`
	AddColumns   []batchAddColumnSpec `json:"addColumns,omitempty"   jsonschema:"Columns to add to pre-existing tables"`
	AddRelations []batchRelationSpec  `json:"addRelations,omitempty" jsonschema:"Relations to create between tables"`
}

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

type batchFailure struct {
	Kind    string `json:"kind"`           // "table" | "column" | "relation"
	Index   int    `json:"index"`          // 0-based index within its kind array
	Name    string `json:"name,omitempty"` // table or column name, when available
	Code    string `json:"code"`
	Message string `json:"message"`
}

type batchCreated struct {
	Tables    []any `json:"tables"`
	Columns   []any `json:"columns"`
	Relations []any `json:"relations"`
}

type batchSchemaResult struct {
	Created batchCreated   `json:"created"`
	Failed  []batchFailure `json:"failed"`
}

// ---------------------------------------------------------------------------
// nameIndex — maps name → server UUID built from Phase 1 acks
// ---------------------------------------------------------------------------

// nameIndex maps "tableName" → tableID and "tableName/columnName" → columnID.
// Built incrementally from Phase 1 acks for use in Phase 3 relation resolution.
type nameIndex struct {
	tableIDs  map[string]string            // tableName → tableID
	columnIDs map[string]map[string]string // tableName → (columnName → columnID)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// entityIDStr extracts the "id" string from an ack entity (map[string]any).
func entityIDStr(entity any) string {
	if m, ok := entity.(map[string]any); ok {
		if id, ok := m["id"].(string); ok {
			return id
		}
	}
	return ""
}

// batchCodeMsg extracts a (code, message) pair from either an emit error or
// a server-rejected ack result. Mirrors the error-handling pattern in positions.go.
// Uses mcperr.AckCodeToMcpCode to normalise unknown ack codes to VALIDATION_ERROR,
// matching the convention in column.go, table.go, and relationship.go.
func batchCodeMsg(err error, ack socket.AckResult) (string, string) {
	if err != nil {
		if me, ok := err.(*mcperr.McpError); ok {
			return string(me.Code), me.Message
		}
		return string(mcperr.InternalError), err.Error()
	}
	return string(mcperr.AckCodeToMcpCode(ack.Code())), msgOr(ack.Message(), "Server rejected operation.")
}

// buildColumnPayload constructs the column:create socket event payload from
// the typed column fields. Extracted to eliminate duplication between the
// Phase 1 inline-column path and the Phase 2 addColumns path.
func buildColumnPayload(tableID, name, dataType string, isPrimaryKey, isForeignKey, isUnique, isNullable *bool, description *string) map[string]any {
	p := map[string]any{
		"tableId":  tableID,
		"name":     name,
		"dataType": dataType,
	}
	if isPrimaryKey != nil {
		p["isPrimaryKey"] = *isPrimaryKey
	}
	if isForeignKey != nil {
		p["isForeignKey"] = *isForeignKey
	}
	if isUnique != nil {
		p["isUnique"] = *isUnique
	}
	if isNullable != nil {
		p["isNullable"] = *isNullable
	}
	if description != nil {
		p["description"] = *description
	}
	return p
}

// resolveEndpoint resolves a single relationship endpoint to concrete UUIDs
// using the nameIndex built from Phase 1.
//
// Nil-guard: if both tableID and tableName are nil, a VALIDATION_ERROR is
// returned so callers never dereference a nil pointer (defensive; the handler's
// pre-execution validation should prevent this in practice).
//
// When tableID is non-nil it is used verbatim; columnID must also be non-nil
// (name lookup is unavailable for externally-referenced tables — VALIDATION_ERROR).
// When tableName is non-nil it is looked up in idx.tableIDs; the column is then
// resolved from either columnID (verbatim) or columnName (looked up in idx.columnIDs).
// A NOT_FOUND is returned when the table or column is absent from the index (it
// failed to create during Phase 1).
func resolveEndpoint(
	tableID, tableName, columnID, columnName *string,
	idx nameIndex,
) (string, string, *mcperr.McpError) {
	// Nil guard: at least one of tableID/tableName must be set.
	if tableID == nil && tableName == nil {
		return "", "", mcperr.New(mcperr.ValidationError,
			"exactly one of tableId or tableName must be set for each endpoint")
	}

	var rTableID, rColumnID string

	if tableID != nil {
		rTableID = *tableID
		// When a pre-existing tableID is given, columnID must also be given because
		// the nameIndex only covers tables created in this batch (keyed by name).
		// Providing columnName here would be a caller error — VALIDATION_ERROR.
		if columnID == nil {
			return "", "", mcperr.NewField(mcperr.ValidationError,
				"columnId is required when tableId is set (name lookup unavailable for externally-referenced tables)",
				"columnId")
		}
		rColumnID = *columnID
		return rTableID, rColumnID, nil
	}

	// tableName path — tableName != nil (guarded above).
	tid, ok := idx.tableIDs[*tableName]
	if !ok {
		return "", "", mcperr.New(mcperr.NotFound,
			fmt.Sprintf("table %q was not found in batch results (it may have failed to create)", *tableName))
	}
	rTableID = tid

	// Nil guard: at least one of columnID/columnName must be set.
	if columnID == nil && columnName == nil {
		return "", "", mcperr.New(mcperr.ValidationError,
			"exactly one of columnId or columnName must be set for each endpoint")
	}

	if columnID != nil {
		rColumnID = *columnID
	} else {
		// columnName != nil (guarded above).
		colMap := idx.columnIDs[*tableName]
		if colMap == nil {
			return "", "", mcperr.New(mcperr.NotFound,
				fmt.Sprintf("column %q in table %q was not found in batch results (it may have failed to create)", *columnName, *tableName))
		}
		cid, ok := colMap[*columnName]
		if !ok {
			return "", "", mcperr.New(mcperr.NotFound,
				fmt.Sprintf("column %q in table %q was not found in batch results (it may have failed to create)", *columnName, *tableName))
		}
		rColumnID = cid
	}

	return rTableID, rColumnID, nil
}

// ---------------------------------------------------------------------------
// Core batch execution helper (unit-testable, no live socket required)
// ---------------------------------------------------------------------------

// executeBatchSchema runs the three-phase batch operation using a pre-bound emitFunc.
// The whiteboardID parameter is the whiteboard being operated on (same value as
// in.WhiteboardID; provided explicitly so callers and tests make the scope clear).
//
// Phase 1: create tables and inline columns (sequential, name→UUID index built).
// Phase 2: add columns to pre-existing tables (sequential).
// Phase 3: create relations, resolving name references from Phase 1 (sequential).
//
// Non-atomic: each item is attempted independently; successes and failures are
// split into the returned batchSchemaResult.
func executeBatchSchema(in batchSchemaUpdateInput, whiteboardID string, emit emitFunc) batchSchemaResult {
	_ = whiteboardID // socket namespace is already scoped; kept for call-site clarity

	result := batchSchemaResult{
		Created: batchCreated{
			Tables:    make([]any, 0),
			Columns:   make([]any, 0),
			Relations: make([]any, 0),
		},
		Failed: make([]batchFailure, 0),
	}

	idx := nameIndex{
		tableIDs:  make(map[string]string),
		columnIDs: make(map[string]map[string]string),
	}

	// -----------------------------------------------------------------------
	// Phase 1: Tables and inline columns
	// -----------------------------------------------------------------------
	for i, spec := range in.Tables {
		payload := map[string]any{"name": spec.Name}
		if spec.Description != nil {
			payload["description"] = *spec.Description
		}
		if spec.PositionX != nil {
			payload["positionX"] = *spec.PositionX
		}
		if spec.PositionY != nil {
			payload["positionY"] = *spec.PositionY
		}

		ack, err := emit("table:create", payload)
		if err != nil || !ack.OK() {
			code, msg := batchCodeMsg(err, ack)
			result.Failed = append(result.Failed, batchFailure{
				Kind: "table", Index: i, Name: spec.Name,
				Code: code, Message: msg,
			})
			continue // skip inline columns for a table that failed to create
		}

		tableID := entityIDStr(ack.Entity())
		if tableID == "" {
			// Successful ack with no entity ID is treated as a failure: we cannot
			// emit inline columns or record a name→UUID mapping without the ID.
			result.Failed = append(result.Failed, batchFailure{
				Kind: "table", Index: i, Name: spec.Name,
				Code:    string(mcperr.InternalError),
				Message: "Server returned an empty table ID in the ack.",
			})
			continue
		}
		idx.tableIDs[spec.Name] = tableID
		idx.columnIDs[spec.Name] = make(map[string]string)
		result.Created.Tables = append(result.Created.Tables, ack.Entity())

		for j, col := range spec.Columns {
			colPayload := buildColumnPayload(tableID, col.Name, col.DataType,
				col.IsPrimaryKey, col.IsForeignKey, col.IsUnique, col.IsNullable, col.Description)

			colAck, colErr := emit("column:create", colPayload)
			if colErr != nil || !colAck.OK() {
				code, msg := batchCodeMsg(colErr, colAck)
				result.Failed = append(result.Failed, batchFailure{
					Kind: "column", Index: j, Name: col.Name,
					Code: code, Message: msg,
				})
				continue
			}

			colID := entityIDStr(colAck.Entity())
			if colID == "" {
				// Without the server-assigned column ID we cannot record the
				// name→UUID mapping, so name-based relation resolution for this
				// column will fail. Treat it as a failure.
				result.Failed = append(result.Failed, batchFailure{
					Kind: "column", Index: j, Name: col.Name,
					Code:    string(mcperr.InternalError),
					Message: "Server returned an empty column ID in the ack.",
				})
				continue
			}
			idx.columnIDs[spec.Name][col.Name] = colID
			result.Created.Columns = append(result.Created.Columns, colAck.Entity())
		}
	}

	// -----------------------------------------------------------------------
	// Phase 2: Add columns to existing tables
	// -----------------------------------------------------------------------
	for i, spec := range in.AddColumns {
		colPayload := buildColumnPayload(spec.TableID, spec.Name, spec.DataType,
			spec.IsPrimaryKey, spec.IsForeignKey, spec.IsUnique, spec.IsNullable, spec.Description)

		ack, err := emit("column:create", colPayload)
		if err != nil || !ack.OK() {
			code, msg := batchCodeMsg(err, ack)
			result.Failed = append(result.Failed, batchFailure{
				Kind: "column", Index: i, Name: spec.Name,
				Code: code, Message: msg,
			})
			continue
		}

		colID := entityIDStr(ack.Entity())
		if colID == "" {
			result.Failed = append(result.Failed, batchFailure{
				Kind: "column", Index: i, Name: spec.Name,
				Code:    string(mcperr.InternalError),
				Message: "Server returned an empty column ID in the ack.",
			})
			continue
		}
		result.Created.Columns = append(result.Created.Columns, ack.Entity())
	}

	// -----------------------------------------------------------------------
	// Phase 3: Relations
	// -----------------------------------------------------------------------
	for i, spec := range in.AddRelations {
		srcTableID, srcColID, rerr := resolveEndpoint(
			spec.SourceTableID, spec.SourceTableName,
			spec.SourceColumnID, spec.SourceColumnName,
			idx,
		)
		if rerr != nil {
			result.Failed = append(result.Failed, batchFailure{
				Kind: "relation", Index: i,
				Code: string(rerr.Code), Message: rerr.Message,
			})
			continue
		}

		tgtTableID, tgtColID, rerr := resolveEndpoint(
			spec.TargetTableID, spec.TargetTableName,
			spec.TargetColumnID, spec.TargetColumnName,
			idx,
		)
		if rerr != nil {
			result.Failed = append(result.Failed, batchFailure{
				Kind: "relation", Index: i,
				Code: string(rerr.Code), Message: rerr.Message,
			})
			continue
		}

		relPayload := map[string]any{
			"sourceTableId":  srcTableID,
			"targetTableId":  tgtTableID,
			"sourceColumnId": srcColID,
			"targetColumnId": tgtColID,
			"cardinality":    spec.Cardinality,
		}
		if spec.Label != nil {
			relPayload["label"] = *spec.Label
		}

		ack, err := emit("relationship:create", relPayload)
		if err != nil || !ack.OK() {
			code, msg := batchCodeMsg(err, ack)
			result.Failed = append(result.Failed, batchFailure{
				Kind: "relation", Index: i,
				Code: code, Message: msg,
			})
			continue
		}

		relID := entityIDStr(ack.Entity())
		if relID == "" {
			result.Failed = append(result.Failed, batchFailure{
				Kind: "relation", Index: i,
				Code:    string(mcperr.InternalError),
				Message: "Server returned an empty relation ID in the ack.",
			})
			continue
		}
		result.Created.Relations = append(result.Created.Relations, ack.Entity())
	}

	return result
}

// ---------------------------------------------------------------------------
// Tool registration
// ---------------------------------------------------------------------------

// RegisterBatchTools registers the batch_schema_update tool.
func RegisterBatchTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "batch_schema_update",
		Description: "Create multiple tables (with optional inline columns), add columns to existing tables, " +
			"and define relations in a single call. Operations execute in order: tables first, then add-columns, " +
			"then relations. Non-atomic: each item is attempted independently; results are split into " +
			"created/failed. Relations may reference newly-created tables and columns by name instead of UUID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in batchSchemaUpdateInput) (*mcp.CallToolResult, any, error) {
		// --- Step 1: validate whiteboardId ---
		if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
			return fail(e)
		}

		// --- Step 2: at least one operation array must be non-empty ---
		if len(in.Tables) == 0 && len(in.AddColumns) == 0 && len(in.AddRelations) == 0 {
			return validationError(
				"at least one of tables, addColumns, or addRelations must be non-empty.",
				"tables",
			)
		}

		// --- Step 3: size limits ---
		if len(in.Tables) > 50 {
			return validationError("tables must contain at most 50 entries.", "tables")
		}
		if len(in.AddColumns) > 500 {
			return validationError("addColumns must contain at most 500 entries.", "addColumns")
		}
		if len(in.AddRelations) > 200 {
			return validationError("addRelations must contain at most 200 entries.", "addRelations")
		}

		// --- Step 4: validate table specs and inline columns ---
		seenTableNames := make(map[string]struct{}, len(in.Tables))
		for i, t := range in.Tables {
			if e := checkLen(fmt.Sprintf("tables[%d].name", i), t.Name, 1, 255); e != nil {
				return fail(e)
			}
			if t.PositionX != nil {
				if e := checkFinite(fmt.Sprintf("tables[%d].positionX", i), *t.PositionX); e != nil {
					return fail(e)
				}
			}
			if t.PositionY != nil {
				if e := checkFinite(fmt.Sprintf("tables[%d].positionY", i), *t.PositionY); e != nil {
					return fail(e)
				}
			}
			if len(t.Columns) > 100 {
				return validationError(
					fmt.Sprintf("tables[%d].columns must contain at most 100 entries.", i),
					fmt.Sprintf("tables[%d].columns", i),
				)
			}
			for j, col := range t.Columns {
				if e := checkLen(fmt.Sprintf("tables[%d].columns[%d].name", i, j), col.Name, 1, 255); e != nil {
					return fail(e)
				}
				if !schema.IsValidDataType(col.DataType) {
					return validationError(
						"Invalid enum value.",
						fmt.Sprintf("tables[%d].columns[%d].dataType", i, j),
					)
				}
			}
			seenTableNames[t.Name] = struct{}{}
		}

		// --- Step 5: table name uniqueness within the batch ---
		if len(seenTableNames) != len(in.Tables) {
			return validationError(
				"table names within tables[] must be unique (required for name-based relation resolution).",
				"tables",
			)
		}

		// --- Step 6: validate addColumns entries ---
		for i, col := range in.AddColumns {
			if e := validateUUID(fmt.Sprintf("addColumns[%d].tableId", i), col.TableID); e != nil {
				return fail(e)
			}
			if e := checkLen(fmt.Sprintf("addColumns[%d].name", i), col.Name, 1, 255); e != nil {
				return fail(e)
			}
			if !schema.IsValidDataType(col.DataType) {
				return validationError(
					"Invalid enum value.",
					fmt.Sprintf("addColumns[%d].dataType", i),
				)
			}
		}

		// --- Step 7: validate addRelations entries ---
		for i, rel := range in.AddRelations {
			prefix := fmt.Sprintf("addRelations[%d]", i)

			if !schema.IsValidCardinality(rel.Cardinality) {
				return validationError("Invalid enum value.", prefix+".cardinality")
			}
			if rel.Label != nil {
				if e := checkMaxLen(prefix+".label", *rel.Label, 255); e != nil {
					return fail(e)
				}
			}

			// Each of the four endpoint pairs must have exactly one of ID/Name set.
			type endpointPair struct {
				idField   string
				nameField string
				idVal     *string
				nameVal   *string
			}
			pairs := []endpointPair{
				{prefix + ".sourceTableId", prefix + ".sourceTableName", rel.SourceTableID, rel.SourceTableName},
				{prefix + ".targetTableId", prefix + ".targetTableName", rel.TargetTableID, rel.TargetTableName},
				{prefix + ".sourceColumnId", prefix + ".sourceColumnName", rel.SourceColumnID, rel.SourceColumnName},
				{prefix + ".targetColumnId", prefix + ".targetColumnName", rel.TargetColumnID, rel.TargetColumnName},
			}
			for _, p := range pairs {
				if p.idVal != nil && p.nameVal != nil {
					return validationError(
						fmt.Sprintf("specify exactly one of %s or %s, not both", p.idField, p.nameField),
						p.idField,
					)
				}
				if p.idVal == nil && p.nameVal == nil {
					return validationError(
						fmt.Sprintf("exactly one of %s or %s must be set", p.idField, p.nameField),
						p.idField,
					)
				}
			}

			// Cross-field constraint: when a table endpoint uses a UUID, the
			// corresponding column endpoint must also use a UUID. Name lookup is
			// only available for tables created within this batch (keyed by name).
			if rel.SourceTableID != nil && rel.SourceColumnName != nil {
				return validationError(
					fmt.Sprintf("%s: when sourceTableId is set, sourceColumnId is required (sourceColumnName is unavailable for externally-referenced tables)", prefix),
					prefix+".sourceColumnName",
				)
			}
			if rel.TargetTableID != nil && rel.TargetColumnName != nil {
				return validationError(
					fmt.Sprintf("%s: when targetTableId is set, targetColumnId is required (targetColumnName is unavailable for externally-referenced tables)", prefix),
					prefix+".targetColumnName",
				)
			}

			// Validate any UUIDs that are explicitly provided.
			if rel.SourceTableID != nil {
				if e := validateUUID(prefix+".sourceTableId", *rel.SourceTableID); e != nil {
					return fail(e)
				}
			}
			if rel.TargetTableID != nil {
				if e := validateUUID(prefix+".targetTableId", *rel.TargetTableID); e != nil {
					return fail(e)
				}
			}
			if rel.SourceColumnID != nil {
				if e := validateUUID(prefix+".sourceColumnId", *rel.SourceColumnID); e != nil {
					return fail(e)
				}
			}
			if rel.TargetColumnID != nil {
				if e := validateUUID(prefix+".targetColumnId", *rel.TargetColumnID); e != nil {
					return fail(e)
				}
			}
		}

		// --- Step 8: single auth check covering the entire batch ---
		userID := auth.UserID(ctx)
		projectID, err := data.GetWhiteboardProjectID(ctx, in.WhiteboardID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", in.WhiteboardID))
		}
		if err := auth.AssertProjectAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		// --- Step 9: bind emit to this request's whiteboard and user ---
		emit := func(event string, payload any) (socket.AckResult, error) {
			return socket.SocketEmitWithAck(ctx, in.WhiteboardID, userID, event, payload)
		}

		// --- Step 10: execute and return ---
		result := executeBatchSchema(in, in.WhiteboardID, emit)
		return success(result)
	})
}

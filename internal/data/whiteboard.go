package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/positioning"
)

// inPlaceholders builds an `IN (?, ?, …)` clause body and the matching []any
// args for a set of string ids. SQLite has no Postgres-style `= ANY($1)`.
func inPlaceholders(ids []string) (string, []any) {
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id
	}
	return strings.Join(marks, ","), args
}

// FindWhiteboardsByProjectID returns all whiteboards in a project, newest first.
func FindWhiteboardsByProjectID(ctx context.Context, projectID string) ([]Whiteboard, error) {
	rows, err := db.Pool().Query(ctx,
		`SELECT id, name, "projectId", "folderId", "canvasState", "textSource", "createdAt", "updatedAt"
		   FROM "Whiteboard"
		  WHERE "projectId" = $1
		  ORDER BY "updatedAt" DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Whiteboard
	for rows.Next() {
		var w Whiteboard
		if err := rows.Scan(&w.ID, &w.Name, &w.ProjectID, &w.FolderID,
			&w.CanvasState, &w.TextSource, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListTableRectsByWhiteboardID returns the bounding box of every table in a
// whiteboard. NULL width/height default to 280/220 to match the grid constants.
func ListTableRectsByWhiteboardID(ctx context.Context, whiteboardID string) ([]positioning.Rect, error) {
	// COALESCE defaults must match colW/rowH in internal/positioning/positioning.go.
	rows, err := db.Pool().Query(ctx,
		`SELECT "positionX", "positionY",
                COALESCE(width,  280.0),
                COALESCE(height, 220.0)
           FROM "DiagramTable"
          WHERE "whiteboardId" = $1`, whiteboardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []positioning.Rect
	for rows.Next() {
		var r positioning.Rect
		if err := rows.Scan(&r.X, &r.Y, &r.W, &r.H); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListWhiteboards returns the whiteboards in a project with a table count for
// each. Uses a single grouped query for the counts (avoids N+1).
// Mirrors listWhiteboards in src/mcp/read-data.ts.
func ListWhiteboards(ctx context.Context, projectID string) ([]WhiteboardSummary, error) {
	whiteboards, err := FindWhiteboardsByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(whiteboards) == 0 {
		return []WhiteboardSummary{}, nil
	}

	ids := make([]string, len(whiteboards))
	for i, wb := range whiteboards {
		ids[i] = wb.ID
	}

	// Single grouped COUNT for all whiteboards in one round-trip.
	marks, args := inPlaceholders(ids)
	countRows, err := db.Pool().Query(ctx,
		`SELECT "whiteboardId", COUNT(*) FROM "DiagramTable" WHERE "whiteboardId" IN (`+marks+`) GROUP BY "whiteboardId"`,
		args...)
	if err != nil {
		return nil, err
	}
	defer countRows.Close()

	counts := make(map[string]int, len(whiteboards))
	for countRows.Next() {
		var wbID string
		var n int
		if err := countRows.Scan(&wbID, &n); err != nil {
			return nil, err
		}
		counts[wbID] = n
	}
	if err := countRows.Err(); err != nil {
		return nil, err
	}

	out := make([]WhiteboardSummary, 0, len(whiteboards))
	for _, wb := range whiteboards {
		out = append(out, WhiteboardSummary{
			ID:         wb.ID,
			Name:       wb.Name,
			UpdatedAt:  wb.UpdatedAt,
			TableCount: counts[wb.ID], // 0 for boards with no tables (map zero value)
		})
	}
	return out, nil
}

// FindWhiteboardByIDWithDiagram loads a whiteboard with its full diagram data:
// tables, columns (ordered by "order" ASC), and outgoing/incoming relationships.
// Returns nil if the whiteboard does not exist.
func FindWhiteboardByIDWithDiagram(ctx context.Context, id string) (*WhiteboardWithDiagram, error) {
	pool := db.Pool()

	// 1. Whiteboard
	var wb Whiteboard
	err := pool.QueryRow(ctx,
		`SELECT id, name, "projectId", "folderId", "canvasState", "textSource", "createdAt", "updatedAt"
		   FROM "Whiteboard" WHERE id = $1`, id).
		Scan(&wb.ID, &wb.Name, &wb.ProjectID, &wb.FolderID,
			&wb.CanvasState, &wb.TextSource, &wb.CreatedAt, &wb.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	// 2. Tables (deterministic order by creation time)
	tableRows, err := pool.Query(ctx,
		`SELECT id, "whiteboardId", name, description, "positionX", "positionY", width, height, "createdAt", "updatedAt"
		   FROM "DiagramTable"
		  WHERE "whiteboardId" = $1
		  ORDER BY "createdAt" ASC`, id)
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()

	tables := make([]TableWithRelations, 0)
	tableIndex := make(map[string]int)
	tableIDs := make([]string, 0)
	for tableRows.Next() {
		var t TableWithRelations
		if err := tableRows.Scan(&t.ID, &t.WhiteboardID, &t.Name, &t.Description,
			&t.PositionX, &t.PositionY, &t.Width, &t.Height, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Columns = make([]Column, 0)
		t.OutgoingRelationships = make([]Relationship, 0)
		t.IncomingRelationships = make([]Relationship, 0)
		tableIndex[t.ID] = len(tables)
		tableIDs = append(tableIDs, t.ID)
		tables = append(tables, t)
	}
	if err := tableRows.Err(); err != nil {
		return nil, err
	}

	result := &WhiteboardWithDiagram{Whiteboard: wb, Tables: tables}
	if len(tableIDs) == 0 {
		return result, nil
	}

	// 3. Columns for all tables, ordered by "order" ASC
	colMarks, colArgs := inPlaceholders(tableIDs)
	colRows, err := pool.Query(ctx,
		`SELECT id, "tableId", name, "dataType", "isPrimaryKey", "isForeignKey", "isUnique", "isNullable", description, "order", "createdAt", "updatedAt"
		   FROM "Column"
		  WHERE "tableId" IN (`+colMarks+`)
		  ORDER BY "order" ASC`, colArgs...)
	if err != nil {
		return nil, err
	}
	defer colRows.Close()
	for colRows.Next() {
		var c Column
		if err := colRows.Scan(&c.ID, &c.TableID, &c.Name, &c.DataType,
			&c.IsPrimaryKey, &c.IsForeignKey, &c.IsUnique, &c.IsNullable,
			&c.Description, &c.Order, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if idx, ok := tableIndex[c.TableID]; ok {
			result.Tables[idx].Columns = append(result.Tables[idx].Columns, c)
		}
	}
	if err := colRows.Err(); err != nil {
		return nil, err
	}

	// 4. Relationships touching any table in this whiteboard
	relRows, err := pool.Query(ctx,
		`SELECT id, "whiteboardId", "sourceTableId", "targetTableId", "sourceColumnId", "targetColumnId", cardinality, label, "routingPoints", "createdAt", "updatedAt"
		   FROM "Relationship"
		  WHERE "whiteboardId" = $1
		  ORDER BY "createdAt" ASC`, id)
	if err != nil {
		return nil, err
	}
	defer relRows.Close()
	for relRows.Next() {
		var r Relationship
		if err := relRows.Scan(&r.ID, &r.WhiteboardID, &r.SourceTableID, &r.TargetTableID,
			&r.SourceColumnID, &r.TargetColumnID, &r.Cardinality, &r.Label,
			&r.RoutingPoints, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if idx, ok := tableIndex[r.SourceTableID]; ok {
			result.Tables[idx].OutgoingRelationships = append(result.Tables[idx].OutgoingRelationships, r)
		}
		if idx, ok := tableIndex[r.TargetTableID]; ok {
			result.Tables[idx].IncomingRelationships = append(result.Tables[idx].IncomingRelationships, r)
		}
	}
	return result, relRows.Err()
}

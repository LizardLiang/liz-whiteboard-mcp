// Package summary formats a WhiteboardWithDiagram into a compact text summary
// for the get_schema_summary tool. Output: one line per table header, one line
// per column, one line per relationship. No UUIDs, no positions.
//
// Ported from src/mcp/schema-summary.ts.
package summary

import (
	"strings"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

// columnFlags renders the [PK,FK,U,N] flag suffix for a column.
func columnFlags(col data.Column) string {
	var flags []string
	if col.IsPrimaryKey {
		flags = append(flags, "PK")
	}
	if col.IsForeignKey {
		flags = append(flags, "FK")
	}
	if col.IsUnique {
		flags = append(flags, "U")
	}
	if col.IsNullable {
		flags = append(flags, "N")
	}
	if len(flags) == 0 {
		return ""
	}
	return " [" + strings.Join(flags, ",") + "]"
}

// FormatSchemaSummary formats a board into a compact multi-line schema summary.
//
// Format:
//
//	TABLE <name>
//	  <colName> <dataType>[flags]
//	  ...
//	RELATIONSHIP <srcTable>.<srcCol> →(<cardinality>) <tgtTable>.<tgtCol>
func FormatSchemaSummary(board *data.WhiteboardWithDiagram) string {
	var lines []string

	// Lookup maps.
	tableByID := make(map[string]*data.TableWithRelations)
	columnByID := make(map[string]*data.Column)
	for i := range board.Tables {
		t := &board.Tables[i]
		tableByID[t.ID] = t
		for j := range t.Columns {
			c := &t.Columns[j]
			columnByID[c.ID] = c
		}
	}

	// Collect all relationships, deduped by id, preserving first-seen order.
	var relOrder []*data.Relationship
	seen := make(map[string]struct{})
	addRel := func(r *data.Relationship) {
		if _, ok := seen[r.ID]; ok {
			return
		}
		seen[r.ID] = struct{}{}
		relOrder = append(relOrder, r)
	}
	for i := range board.Tables {
		t := &board.Tables[i]
		for j := range t.OutgoingRelationships {
			addRel(&t.OutgoingRelationships[j])
		}
		for j := range t.IncomingRelationships {
			addRel(&t.IncomingRelationships[j])
		}
	}

	// Tables.
	for i := range board.Tables {
		t := &board.Tables[i]
		lines = append(lines, "TABLE "+t.Name)
		for j := range t.Columns {
			c := t.Columns[j]
			lines = append(lines, "  "+c.Name+" "+c.DataType+columnFlags(c))
		}
	}

	// Relationships.
	if len(relOrder) > 0 {
		lines = append(lines, "")
		for _, rel := range relOrder {
			srcName := rel.SourceTableID
			if t, ok := tableByID[rel.SourceTableID]; ok {
				srcName = t.Name
			}
			tgtName := rel.TargetTableID
			if t, ok := tableByID[rel.TargetTableID]; ok {
				tgtName = t.Name
			}
			srcColName := rel.SourceColumnID
			if c, ok := columnByID[rel.SourceColumnID]; ok {
				srcColName = c.Name
			}
			tgtColName := rel.TargetColumnID
			if c, ok := columnByID[rel.TargetColumnID]; ok {
				tgtColName = c.Name
			}
			label := ""
			if rel.Label != nil && *rel.Label != "" {
				label = ` "` + *rel.Label + `"`
			}
			lines = append(lines,
				"RELATIONSHIP "+srcName+"."+srcColName+" →("+rel.Cardinality+") "+tgtName+"."+tgtColName+label)
		}
	}

	return strings.Join(lines, "\n")
}

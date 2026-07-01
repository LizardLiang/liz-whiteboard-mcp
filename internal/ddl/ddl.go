package ddl

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

// GenerateTableDDL renders a single-table CREATE TABLE statement for the
// table identified by tableID within board, in the given dialect.
//
// dialect defaults to DialectPostgres when empty; an unrecognized non-empty
// dialect is an error. Returns an error if tableID is not found in board —
// callers should map that to mcperr.NotFound. Returns an error if the table
// has no columns — callers should map that to a client-request error, not
// NotFound. Columns are sorted by their Order field before rendering, so
// callers do not need to pre-sort target.Columns.
func GenerateTableDDL(board *data.WhiteboardWithDiagram, tableID, dialect string) (string, error) {
	if board == nil {
		return "", fmt.Errorf("board is nil")
	}
	if dialect == "" {
		dialect = DialectPostgres
	}
	if !IsValidDialect(dialect) {
		return "", fmt.Errorf("unsupported dialect %q", dialect)
	}

	tableByID := make(map[string]*data.TableWithRelations, len(board.Tables))
	columnByID := make(map[string]*data.Column)
	for i := range board.Tables {
		t := &board.Tables[i]
		tableByID[t.ID] = t
		for j := range t.Columns {
			columnByID[t.Columns[j].ID] = &t.Columns[j]
		}
	}

	target, ok := tableByID[tableID]
	if !ok {
		return "", fmt.Errorf("table %s not found", tableID)
	}

	// Render columns in their defined display order, independent of the
	// order they arrive in target.Columns.
	columns := make([]data.Column, len(target.Columns))
	copy(columns, target.Columns)
	sort.SliceStable(columns, func(i, j int) bool {
		return columns[i].Order < columns[j].Order
	})

	// Determine primary-key columns to decide inline vs. composite rendering.
	var pkCols []data.Column
	for _, c := range columns {
		if c.IsPrimaryKey {
			pkCols = append(pkCols, c)
		}
	}
	singlePK := len(pkCols) == 1

	var lines []string

	// Column lines.
	for _, c := range columns {
		var b strings.Builder
		b.WriteString(QuoteIdent(dialect, c.Name))
		b.WriteString(" ")
		b.WriteString(mapDataType(dialect, c.DataType))
		if !c.IsNullable {
			b.WriteString(" NOT NULL")
		}
		if c.IsUnique {
			b.WriteString(" UNIQUE")
		}
		if singlePK && c.IsPrimaryKey {
			b.WriteString(" PRIMARY KEY")
		}
		lines = append(lines, "  "+b.String())
	}

	if len(lines) == 0 {
		return "", fmt.Errorf("table %s has no columns", tableID)
	}

	// Composite primary key constraint line.
	if len(pkCols) > 1 {
		names := make([]string, len(pkCols))
		for i, c := range pkCols {
			names[i] = QuoteIdent(dialect, c.Name)
		}
		lines = append(lines, "  PRIMARY KEY ("+strings.Join(names, ", ")+")")
	}

	// Foreign-key constraint lines, one per outgoing relationship.
	for _, rel := range target.OutgoingRelationships {
		srcCol, ok := columnByID[rel.SourceColumnID]
		if !ok {
			continue
		}
		tgtTable, ok := tableByID[rel.TargetTableID]
		if !ok {
			continue
		}
		tgtCol, ok := columnByID[rel.TargetColumnID]
		if !ok {
			continue
		}
		fk := "  FOREIGN KEY (" + QuoteIdent(dialect, srcCol.Name) + ") REFERENCES " +
			QuoteIdent(dialect, tgtTable.Name) + "(" + QuoteIdent(dialect, tgtCol.Name) + ")"
		lines = append(lines, fk)
	}

	return "CREATE TABLE " + QuoteIdent(dialect, target.Name) + " (\n" +
		strings.Join(lines, ",\n") + "\n);", nil
}

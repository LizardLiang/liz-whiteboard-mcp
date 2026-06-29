package summary

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
)

func strptr(s string) *string { return &s }

func f64ptr(v float64) *float64 { return &v }

func makeColumn(id, tableID, name, dataType string, pk, fk, uniq, null bool) data.Column {
	return data.Column{
		ID: id, TableID: tableID, Name: name, DataType: dataType,
		IsPrimaryKey: pk, IsForeignKey: fk, IsUnique: uniq, IsNullable: null,
	}
}

// TC-UNIT-SUMMARY-01: compact format omits UUIDs and positions, renders flags.
func TestFormatSchemaSummary_CompactFormat(t *testing.T) {
	rel := data.Relationship{
		ID: "rel-id-1", WhiteboardID: "wb-id",
		SourceTableID: "table-id-1", TargetTableID: "table-id-2",
		SourceColumnID: "col-id-1", TargetColumnID: "col-id-4",
		Cardinality: "ONE_TO_MANY",
	}
	t1 := data.TableWithRelations{
		DiagramTable: data.DiagramTable{ID: "table-id-1", WhiteboardID: "wb-id", Name: "users", PositionX: f64ptr(100), PositionY: f64ptr(100)},
		Columns: []data.Column{
			makeColumn("col-id-1", "table-id-1", "id", "uuid", true, false, false, false),
			makeColumn("col-id-2", "table-id-1", "email", "varchar", false, false, false, false),
			makeColumn("col-id-3", "table-id-1", "is_active", "boolean", false, false, false, true),
		},
		OutgoingRelationships: []data.Relationship{rel},
		IncomingRelationships: []data.Relationship{},
	}
	t2 := data.TableWithRelations{
		DiagramTable: data.DiagramTable{ID: "table-id-2", WhiteboardID: "wb-id", Name: "orders"},
		Columns: []data.Column{
			makeColumn("col-id-4", "table-id-2", "user_id", "uuid", false, true, false, false),
		},
		OutgoingRelationships: []data.Relationship{},
		IncomingRelationships: []data.Relationship{rel},
	}
	board := &data.WhiteboardWithDiagram{
		Whiteboard: data.Whiteboard{ID: "wb-id", Name: "Test Board", ProjectID: "proj-id"},
		Tables:     []data.TableWithRelations{t1, t2},
	}

	out := FormatSchemaSummary(board)

	assert.Contains(t, out, "TABLE users")
	assert.Contains(t, out, "TABLE orders")
	assert.Contains(t, out, "id uuid")
	assert.Contains(t, out, "[PK]")
	assert.Contains(t, out, "[FK]")
	assert.Contains(t, out, "[N]")
	assert.Contains(t, out, "RELATIONSHIP")
	assert.Contains(t, out, "→(ONE_TO_MANY)")

	// Must NOT contain UUIDs or position keywords.
	assert.NotContains(t, out, "table-id-1")
	assert.NotContains(t, out, "col-id-1")
	assert.NotContains(t, out, "wb-id")
	assert.NotContains(t, out, "positionX")
	assert.NotContains(t, out, "positionY")
}

// Relationship label is rendered when present.
func TestFormatSchemaSummary_Label(t *testing.T) {
	rel := data.Relationship{
		ID: "r1", SourceTableID: "t1", TargetTableID: "t1",
		SourceColumnID: "c1", TargetColumnID: "c1",
		Cardinality: "ONE_TO_ONE", Label: strptr("owns"),
	}
	tbl := data.TableWithRelations{
		DiagramTable:          data.DiagramTable{ID: "t1", Name: "self"},
		Columns:               []data.Column{makeColumn("c1", "t1", "id", "uuid", true, false, false, false)},
		OutgoingRelationships: []data.Relationship{rel},
	}
	board := &data.WhiteboardWithDiagram{Tables: []data.TableWithRelations{tbl}}
	out := FormatSchemaSummary(board)
	assert.Contains(t, out, `"owns"`)
}

// TC-UNIT-SUMMARY-02: 50-table board stays under 16000 characters.
func TestFormatSchemaSummary_Under16000(t *testing.T) {
	var tables []data.TableWithRelations
	for i := 0; i < 50; i++ {
		var cols []data.Column
		for j := 0; j < 10; j++ {
			cols = append(cols, makeColumn(
				"col-"+itoa(i)+"-"+itoa(j), "table-"+itoa(i), "column_"+itoa(j),
				"int", j == 0, j == 1, j == 2, j > 2))
		}
		tables = append(tables, data.TableWithRelations{
			DiagramTable: data.DiagramTable{ID: "table-" + itoa(i), Name: "table_" + itoa(i)},
			Columns:      cols,
		})
	}
	board := &data.WhiteboardWithDiagram{Tables: tables}
	out := FormatSchemaSummary(board)
	assert.Less(t, len(out), 16000)
	assert.True(t, strings.HasPrefix(out, "TABLE table_0"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

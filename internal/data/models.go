// Package data is the data-access layer for the liz-whiteboard MCP server.
// All reads go directly to the main app's SQLite database (data/app.db) using
// raw SQL via database/sql + modernc.org/sqlite.
//
// Column names are camelCase quoted identifiers and table names are PascalCase
// quoted identifiers, matching the Prisma default mapping (no @map/@@map in
// prisma/schema.prisma; verified against the raw SQL in src/data/project.ts).
package data

import (
	"fmt"
	"strconv"
	"time"
)

// Timestamp scans the unix-millisecond INTEGER columns the main app stores for
// createdAt/updatedAt/expiresAt (SQLite has no native timestamp type) and
// marshals back to RFC3339 for parity with the previous Postgres output.
type Timestamp struct{ time.Time }

// Scan implements sql.Scanner for unix-ms integers, with text/time fallbacks.
func (t *Timestamp) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}
	case int64:
		t.Time = time.UnixMilli(v).UTC()
	case float64:
		t.Time = time.UnixMilli(int64(v)).UTC()
	case time.Time:
		t.Time = v
	case []byte:
		return t.Scan(string(v))
	case string:
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			t.Time = time.UnixMilli(ms).UTC()
			return nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return fmt.Errorf("Timestamp.Scan: cannot parse %q", v)
		}
		t.Time = parsed
	default:
		return fmt.Errorf("Timestamp.Scan: unsupported type %T", src)
	}
	return nil
}

// JSONText holds a raw JSON document stored as SQLite TEXT (canvasState,
// routingPoints). It scans both string and []byte and marshals back as raw
// JSON — matching the previous json.RawMessage behaviour. database/sql will not
// scan a string into json.RawMessage directly, hence this wrapper.
type JSONText []byte

// Scan implements sql.Scanner.
func (j *JSONText) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = []byte(v)
	default:
		return fmt.Errorf("JSONText.Scan: unsupported type %T", src)
	}
	return nil
}

// MarshalJSON emits the raw JSON, or null when empty.
func (j JSONText) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// Project mirrors the Prisma Project model.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	CreatedAt   Timestamp `json:"createdAt"`
	UpdatedAt   Timestamp `json:"updatedAt"`
	OwnerID     *string   `json:"ownerId"`
}

// Whiteboard mirrors the Prisma Whiteboard model.
type Whiteboard struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ProjectID   string    `json:"projectId"`
	FolderID    *string   `json:"folderId"`
	CanvasState JSONText  `json:"canvasState"`
	TextSource  *string   `json:"textSource"`
	CreatedAt   Timestamp `json:"createdAt"`
	UpdatedAt   Timestamp `json:"updatedAt"`
}

// DiagramTable mirrors the Prisma DiagramTable model.
type DiagramTable struct {
	ID           string    `json:"id"`
	WhiteboardID string    `json:"whiteboardId"`
	Name         string    `json:"name"`
	Description  *string   `json:"description"`
	PositionX    float64   `json:"positionX"`
	PositionY    float64   `json:"positionY"`
	Width        *float64  `json:"width"`
	Height       *float64  `json:"height"`
	CreatedAt    Timestamp `json:"createdAt"`
	UpdatedAt    Timestamp `json:"updatedAt"`
}

// Column mirrors the Prisma Column model.
type Column struct {
	ID           string    `json:"id"`
	TableID      string    `json:"tableId"`
	Name         string    `json:"name"`
	DataType     string    `json:"dataType"`
	IsPrimaryKey bool      `json:"isPrimaryKey"`
	IsForeignKey bool      `json:"isForeignKey"`
	IsUnique     bool      `json:"isUnique"`
	IsNullable   bool      `json:"isNullable"`
	Description  *string   `json:"description"`
	Order        int       `json:"order"`
	CreatedAt    Timestamp `json:"createdAt"`
	UpdatedAt    Timestamp `json:"updatedAt"`
}

// Relationship mirrors the Prisma Relationship model.
type Relationship struct {
	ID             string    `json:"id"`
	WhiteboardID   string    `json:"whiteboardId"`
	SourceTableID  string    `json:"sourceTableId"`
	TargetTableID  string    `json:"targetTableId"`
	SourceColumnID string    `json:"sourceColumnId"`
	TargetColumnID string    `json:"targetColumnId"`
	Cardinality    string    `json:"cardinality"`
	Label          *string   `json:"label"`
	RoutingPoints  JSONText  `json:"routingPoints"`
	CreatedAt      Timestamp `json:"createdAt"`
	UpdatedAt      Timestamp `json:"updatedAt"`
}

// TableWithRelations is a DiagramTable with its columns and relationships.
// JSON keys match the Prisma include shape used by findWhiteboardByIdWithDiagram.
type TableWithRelations struct {
	DiagramTable
	Columns               []Column       `json:"columns"`
	OutgoingRelationships []Relationship `json:"outgoingRelationships"`
	IncomingRelationships []Relationship `json:"incomingRelationships"`
}

// WhiteboardWithDiagram is a Whiteboard with its full diagram data.
type WhiteboardWithDiagram struct {
	Whiteboard
	Tables []TableWithRelations `json:"tables"`
}

// WhiteboardSummary is a list entry for list_whiteboards.
type WhiteboardSummary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	UpdatedAt  Timestamp `json:"updatedAt"`
	TableCount int       `json:"tableCount"`
}

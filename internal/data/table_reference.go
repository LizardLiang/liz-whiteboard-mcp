package data

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// ReferenceConfirmName is what delete_table_reference must match a caller's
// confirmName against: the referenced table's name EXACTLY as
// list_table_references reports it in `sourceTableName`.
type ReferenceConfirmName struct {
	// Name is the value to compare confirmName with.
	Name string
	// IsReference is false when the id names an ordinary table rather than a
	// reference node (no sourceTableId). The caller should refuse and say so.
	IsReference bool
}

// FindReferenceConfirmName resolves the confirmation name for a reference node,
// or nil when no table carries that id.
//
// It MIRRORS resolveTableReferences in liz-whiteboard's src/data/table-reference.ts:
//
//	sourceTableName = sourceIsLive ? sourceTable.name : table.name
//	sourceIsLive    = the source whiteboard still exists
//	                  AND the source table still exists
//	                  AND that table still lives on the declared source whiteboard
//
// The last clause matters: a source table that survived but MOVED to another
// file is as broken as a deleted one, because the reference no longer describes
// what it claims to. In that state the app falls back to the local row's name,
// so the guard must too — otherwise it would demand a name the agent was never
// shown.
//
// One query rather than three round trips, because the guard sits directly in
// front of a cascading delete and must not read a half-changed picture.
func FindReferenceConfirmName(ctx context.Context, tableID string) (*ReferenceConfirmName, error) {
	var (
		localName     string
		sourceTableID *string
		sourceName    *string
		sourceBoardID *string
		declaredBoard *string
		liveBoardID   *string
	)

	err := db.Pool().QueryRow(ctx,
		`SELECT ref."name",
		        ref."sourceTableId",
		        ref."sourceWhiteboardId",
		        src."name",
		        src."whiteboardId",
		        w."id"
		   FROM "DiagramTable" ref
		   LEFT JOIN "DiagramTable" src ON src."id" = ref."sourceTableId"
		   LEFT JOIN "Whiteboard"   w   ON w."id"  = ref."sourceWhiteboardId"
		  WHERE ref."id" = $1`, tableID).
		Scan(&localName, &sourceTableID, &declaredBoard, &sourceName, &sourceBoardID, &liveBoardID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if sourceTableID == nil {
		return &ReferenceConfirmName{Name: localName, IsReference: false}, nil
	}

	sourceIsLive := liveBoardID != nil &&
		sourceName != nil &&
		sourceBoardID != nil &&
		declaredBoard != nil &&
		*sourceBoardID == *declaredBoard

	name := localName
	if sourceIsLive {
		name = *sourceName
	}
	return &ReferenceConfirmName{Name: name, IsReference: true}, nil
}

package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// scanner is implemented by both *sql.Row and *sql.Rows, letting scanArea
// serve single-row and multi-row queries with the same scan logic.
type scanner interface {
	Scan(dest ...any) error
}

// areaSelectColumns is shared by FindAreasByWhiteboardID and FindAreaByID so
// the column list/order can never drift between the two queries.
const areaSelectColumns = `id, "whiteboardId", name, color, "positionX", "positionY", width, height, "memberTableIds", "createdAt", "updatedAt"`

// parseMemberTableIDs unmarshals the memberTableIds JSON TEXT column into a
// non-nil []string. A NULL/empty column or invalid JSON both map to an empty
// slice — mirrors mapArea's `Array.isArray(members) ? members : []` fallback
// in the app repo (src/db.ts).
func parseMemberTableIDs(j JSONText) []string {
	if len(j) == 0 {
		return []string{}
	}
	var ids []string
	if err := json.Unmarshal(j, &ids); err != nil {
		return []string{}
	}
	if ids == nil {
		return []string{}
	}
	return ids
}

// scanArea scans one Area row (column order must match areaSelectColumns).
func scanArea(s scanner) (*Area, error) {
	var a Area
	var members JSONText
	if err := s.Scan(&a.ID, &a.WhiteboardID, &a.Name, &a.Color,
		&a.PositionX, &a.PositionY, &a.Width, &a.Height,
		&members, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.MemberTableIDs = parseMemberTableIDs(members)
	return &a, nil
}

// FindAreasByWhiteboardID returns all areas in a whiteboard, creation order.
// Always returns a non-nil (possibly empty) slice.
func FindAreasByWhiteboardID(ctx context.Context, whiteboardID string) ([]Area, error) {
	rows, err := db.Pool().Query(ctx,
		`SELECT `+areaSelectColumns+`
		   FROM "Area"
		  WHERE "whiteboardId" = $1
		  ORDER BY "createdAt" ASC`, whiteboardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Area, 0)
	for rows.Next() {
		a, err := scanArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// FindAreaByID returns an area by ID, or nil if it does not exist.
func FindAreaByID(ctx context.Context, id string) (*Area, error) {
	a, err := scanArea(db.Pool().QueryRow(ctx,
		`SELECT `+areaSelectColumns+`
		   FROM "Area" WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

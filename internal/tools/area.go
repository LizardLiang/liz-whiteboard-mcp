// Area (subject area / table grouping, GH #106) tools. Adds MCP write access
// to the app's existing area:create/area:update/area:move Socket.IO events
// (src/routes/api/collaboration.ts) plus areas[] read exposure via get_board
// (see internal/data/whiteboard.go). update_area (rename/recolor/resize) and
// delete_area are intentionally out of scope (tactical plan mcp-area-tools).
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/area"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
)

type createAreaInput struct {
	WhiteboardID   string   `json:"whiteboardId"`
	Name           string   `json:"name"`
	Color          string   `json:"color" jsonschema:"Palette id: slate, red, orange, amber, green, teal, blue, violet"`
	PositionX      float64  `json:"positionX"`
	PositionY      float64  `json:"positionY"`
	Width          float64  `json:"width"`
	Height         float64  `json:"height"`
	MemberTableIDs []string `json:"memberTableIds,omitempty"`
}

type areaTableInput struct {
	AreaID  string `json:"areaId"`
	TableID string `json:"tableId"`
}

type moveAreaInput struct {
	AreaID    string  `json:"areaId"`
	PositionX float64 `json:"positionX"`
	PositionY float64 `json:"positionY"`
}

// areaMoveMember is one entry of the area:move payload's members array —
// mirrors areaMoveBroadcastSchema in src/data/schema.ts.
type areaMoveMember struct {
	TableID   string  `json:"tableId"`
	PositionX float64 `json:"positionX"`
	PositionY float64 `json:"positionY"`
}

// areaMoveMemberFromTable shifts a loaded table's position by (deltaX, deltaY)
// for the area:move atomic-drag payload. Returns ok=false when the table is
// nil or has a NULL position (R2) — such members are left in place, not moved.
func areaMoveMemberFromTable(tableID string, table *data.DiagramTable, deltaX, deltaY float64) (member areaMoveMember, ok bool) {
	if table == nil || table.PositionX == nil || table.PositionY == nil {
		return areaMoveMember{}, false
	}
	return areaMoveMember{
		TableID:   tableID,
		PositionX: *table.PositionX + deltaX,
		PositionY: *table.PositionY + deltaY,
	}, true
}

// RegisterAreaTools registers create_area, add_table_to_area,
// remove_table_from_area, and move_area.
func RegisterAreaTools(s *mcp.Server) {
	registerCreateArea(s)
	registerAddTableToArea(s)
	registerRemoveTableFromArea(s)
	registerMoveArea(s)
}

func registerCreateArea(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_area",
		Description: "Create a subject area: a colored region that groups tables. memberTableIds tables keep their positions; ones with no position are not moved.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createAreaInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("whiteboardId", in.WhiteboardID); e != nil {
			return fail(e)
		}
		if e := checkLen("name", in.Name, 1, 255); e != nil {
			return fail(e)
		}
		if !schema.IsValidAreaColor(in.Color) {
			return validationError("color must be one of: "+strings.Join(schema.AreaColors, ", ")+".", "color")
		}
		if e := checkFinite("positionX", in.PositionX); e != nil {
			return fail(e)
		}
		if e := checkFinite("positionY", in.PositionY); e != nil {
			return fail(e)
		}
		if e := checkPositive("width", in.Width); e != nil {
			return fail(e)
		}
		if e := checkPositive("height", in.Height); e != nil {
			return fail(e)
		}
		if len(in.MemberTableIDs) > 1000 {
			return validationError("memberTableIds must contain at most 1000 entries.", "memberTableIds")
		}
		for _, id := range in.MemberTableIDs {
			if e := validateUUID("memberTableIds", id); e != nil {
				return fail(e)
			}
		}

		userID := auth.UserID(ctx)
		projectID, err := data.GetWhiteboardProjectID(ctx, in.WhiteboardID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", in.WhiteboardID))
		}
		if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		memberIDs := in.MemberTableIDs
		if memberIDs == nil {
			memberIDs = []string{}
		}
		payload := map[string]any{
			"name":           in.Name,
			"color":          in.Color,
			"positionX":      in.PositionX,
			"positionY":      in.PositionY,
			"width":          in.Width,
			"height":         in.Height,
			"memberTableIds": memberIDs,
		}

		entity, e := createERRow(prodERWriteFns(ctx, userID), in.WhiteboardID, erAreaKind, in.WhiteboardID, payload)
		if e != nil {
			return fail(e)
		}
		return success(entity)
	})
}

func registerAddTableToArea(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_table_to_area",
		Description: "Add a table to an area; the area bounds grow to enclose it and the table keeps its position. Idempotent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in areaTableInput) (*mcp.CallToolResult, any, error) {
		return mutateAreaMembership(ctx, in, true)
	})
}

func registerRemoveTableFromArea(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_table_from_area",
		Description: "Remove a table from an area; the area bounds are recomputed. Idempotent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in areaTableInput) (*mcp.CallToolResult, any, error) {
		return mutateAreaMembership(ctx, in, false)
	})
}

// mutateAreaMembership implements FR-A2/FR-A3: add or remove a table from an
// area's memberTableIds, recompute bounds from the resulting membership, and
// emit ONE area:update carrying both memberTableIds and (when computable) the
// new positionX/Y/width/height. add=true appends tableId (if absent); add=false
// removes it (if present). Both directions are idempotent: re-running the same
// operation is a no-op on membership, and bounds are still (harmlessly)
// recomputed from the unchanged member set — Ares' choice, since a short-circuit
// would need an extra membership-comparison branch for no functional gain.
func mutateAreaMembership(ctx context.Context, in areaTableInput, add bool) (*mcp.CallToolResult, any, error) {
	if e := validateUUID("areaId", in.AreaID); e != nil {
		return fail(e)
	}
	if e := validateUUID("tableId", in.TableID); e != nil {
		return fail(e)
	}

	userID := auth.UserID(ctx)
	areaRecord, err := data.FindAreaByID(ctx, in.AreaID)
	if err != nil {
		return fail(err)
	}
	if areaRecord == nil {
		return mcpError(mcperr.NotFound, fmt.Sprintf("Area %s not found.", in.AreaID))
	}
	projectID, err := data.GetWhiteboardProjectID(ctx, areaRecord.WhiteboardID)
	if err != nil {
		return fail(err)
	}
	if projectID == "" {
		return mcpError(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", areaRecord.WhiteboardID))
	}
	if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
		return fail(err)
	}

	table, err := data.FindDiagramTableByID(ctx, in.TableID)
	if err != nil {
		return fail(err)
	}
	if table == nil {
		return mcpError(mcperr.NotFound, fmt.Sprintf("Table %s not found.", in.TableID))
	}
	// IDOR: area and table must belong to the same whiteboard.
	if !tableBelongsToArea(table, areaRecord) {
		return mcpError(mcperr.Forbidden, "Table does not belong to the area's whiteboard.")
	}

	nextMembers := nextMemberTableIDs(areaRecord.MemberTableIDs, in.TableID, add)

	payload := map[string]any{
		"areaId":         in.AreaID,
		"memberTableIds": nextMembers,
	}
	bounds, err := computeAreaBoundsForMembers(ctx, nextMembers)
	if err != nil {
		return fail(err)
	}
	if bounds != nil {
		payload["positionX"] = bounds.PositionX
		payload["positionY"] = bounds.PositionY
		payload["width"] = bounds.Width
		payload["height"] = bounds.Height
	}

	ack, err := emitAreaMembership(prodERWriteFns(ctx, userID), areaRecord, payload, nextMembers, bounds)
	if err != nil {
		return fail(err)
	}
	if !ack.OK() {
		return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected area update."))
	}
	return success(ack.Entity())
}

// nextMemberTableIDs computes the post-mutation membership list. add=true
// appends tableID if absent; add=false filters it out. Both are idempotent:
// applying the same operation twice yields the same result. Always returns a
// non-nil slice (an empty area:update memberTableIds is valid — see
// createAreaSchema's `.default([])`).
func nextMemberTableIDs(current []string, tableID string, add bool) []string {
	out := make([]string, 0, len(current)+1)
	found := false
	for _, id := range current {
		if id == tableID {
			found = true
			if !add {
				continue // remove: drop this entry
			}
		}
		out = append(out, id)
	}
	if add && !found {
		out = append(out, tableID)
	}
	return out
}

// tableBelongsToArea is the IDOR guard for add_table_to_area/
// remove_table_from_area: a table can only be added to/removed from an area
// on the same whiteboard.
func tableBelongsToArea(table *data.DiagramTable, areaRecord *data.Area) bool {
	return table.WhiteboardID == areaRecord.WhiteboardID
}

// areaBoundsMemberFromTable converts a loaded table + its column count into an
// area.AreaBoundsMember. Returns ok=false when the table is nil or has a NULL
// position (R2, tactical plan — not yet resolved by a browser client); callers
// must skip such members rather than including a zero-value entry.
func areaBoundsMemberFromTable(table *data.DiagramTable, columnCount int) (member area.AreaBoundsMember, ok bool) {
	if table == nil || table.PositionX == nil || table.PositionY == nil {
		return area.AreaBoundsMember{}, false
	}
	width := 0.0
	if table.Width != nil {
		width = *table.Width
	}
	return area.AreaBoundsMember{
		PositionX:   *table.PositionX,
		PositionY:   *table.PositionY,
		Width:       width,
		ColumnCount: columnCount,
	}, true
}

// computeAreaBoundsForMembers loads each member table's position/width/column
// count and computes the enclosing bounds. Members with a NULL position are
// skipped (R2). Returns nil when no member has a resolved position, mirroring
// ComputeAreaBounds's empty-input nil (caller must then leave the area's
// bounds unchanged).
func computeAreaBoundsForMembers(ctx context.Context, memberTableIDs []string) (*area.AreaBounds, error) {
	members := make([]area.AreaBoundsMember, 0, len(memberTableIDs))
	for _, id := range memberTableIDs {
		table, err := data.FindDiagramTableByID(ctx, id)
		if err != nil {
			return nil, err
		}
		colCount := 0
		if table != nil {
			colCount, err = data.CountColumnsByTableID(ctx, id)
			if err != nil {
				return nil, err
			}
		}
		if m, ok := areaBoundsMemberFromTable(table, colCount); ok {
			members = append(members, m)
		}
	}
	return area.ComputeAreaBounds(members), nil
}

func registerMoveArea(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_area",
		Description: "Move an area; every member table shifts by the same delta. The area bounds stay rigid. A member with no position stays put.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in moveAreaInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("areaId", in.AreaID); e != nil {
			return fail(e)
		}
		if e := checkFinite("positionX", in.PositionX); e != nil {
			return fail(e)
		}
		if e := checkFinite("positionY", in.PositionY); e != nil {
			return fail(e)
		}

		userID := auth.UserID(ctx)
		areaRecord, err := data.FindAreaByID(ctx, in.AreaID)
		if err != nil {
			return fail(err)
		}
		if areaRecord == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Area %s not found.", in.AreaID))
		}
		projectID, err := data.GetWhiteboardProjectID(ctx, areaRecord.WhiteboardID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Whiteboard %s not found.", areaRecord.WhiteboardID))
		}
		if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		deltaX := in.PositionX - areaRecord.PositionX
		deltaY := in.PositionY - areaRecord.PositionY

		members := make([]areaMoveMember, 0, len(areaRecord.MemberTableIDs))
		for _, tableID := range areaRecord.MemberTableIDs {
			table, err := data.FindDiagramTableByID(ctx, tableID)
			if err != nil {
				return fail(err)
			}
			// R2: skip members with an unresolved (NULL) position — they are
			// not moved, mirroring the app's client-side position resolution gap.
			if m, ok := areaMoveMemberFromTable(tableID, table, deltaX, deltaY); ok {
				members = append(members, m)
			}
		}

		// areaMoveBroadcastSchema caps members at 500 entries (matches
		// tableMoveBulkBroadcastSchema's cap) — check against the filtered
		// (emitted) member list, not the raw membership, since null-position
		// members are dropped above and never reach the payload.
		if len(members) > 500 {
			return validationError("Area has more than 500 members; move_area cannot emit an atomic move.", "areaId")
		}

		payload := map[string]any{
			"areaId":    in.AreaID,
			"positionX": in.PositionX,
			"positionY": in.PositionY,
			"members":   members,
		}

		ack, err := emitAreaMove(prodERWriteFns(ctx, userID), areaRecord, payload, in.PositionX, in.PositionY)
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected area move."))
		}
		// R5: a successful area:move ack carries no entity ({ok:true} only) —
		// assemble the result locally, mirroring update_table's move handling.
		return success(map[string]any{
			"id":        in.AreaID,
			"positionX": in.PositionX,
			"positionY": in.PositionY,
		})
	})
}

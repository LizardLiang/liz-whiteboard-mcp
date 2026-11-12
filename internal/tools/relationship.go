package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
)

type createRelationshipInput struct {
	WhiteboardID   string  `json:"whiteboardId"`
	SourceTableID  string  `json:"sourceTableId"`
	TargetTableID  string  `json:"targetTableId"`
	SourceColumnID string  `json:"sourceColumnId"`
	TargetColumnID string  `json:"targetColumnId"`
	Cardinality    string  `json:"cardinality"`
	Label          *string `json:"label,omitempty"`
}

type updateRelationshipInput struct {
	RelationshipID string  `json:"relationshipId"`
	SourceTableID  *string `json:"sourceTableId,omitempty"`
	TargetTableID  *string `json:"targetTableId,omitempty"`
	SourceColumnID *string `json:"sourceColumnId,omitempty"`
	TargetColumnID *string `json:"targetColumnId,omitempty"`
	Cardinality    *string `json:"cardinality,omitempty"`
	Label          *string `json:"label,omitempty"`
}

type relationshipIDInput struct {
	RelationshipID string `json:"relationshipId"`
}

// RegisterRelationshipTools registers create_relationship, update_relationship,
// delete_relationship.
func RegisterRelationshipTools(s *mcp.Server) {
	registerCreateRelationship(s)
	registerUpdateRelationship(s)
	registerDeleteRelationship(s)
}

func registerCreateRelationship(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_relationship",
		Description: "Create a relationship between two tables' columns.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createRelationshipInput) (*mcp.CallToolResult, any, error) {
		for field, val := range map[string]string{
			"whiteboardId":   in.WhiteboardID,
			"sourceTableId":  in.SourceTableID,
			"targetTableId":  in.TargetTableID,
			"sourceColumnId": in.SourceColumnID,
			"targetColumnId": in.TargetColumnID,
		} {
			if e := validateUUID(field, val); e != nil {
				return fail(e)
			}
		}
		if !schema.IsValidCardinality(in.Cardinality) {
			return validationError("Invalid enum value.", "cardinality")
		}
		// W6: label z.string().max(255)
		if in.Label != nil {
			if e := checkMaxLen("label", *in.Label, 255); e != nil {
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

		payload := map[string]any{
			"sourceTableId":  in.SourceTableID,
			"targetTableId":  in.TargetTableID,
			"sourceColumnId": in.SourceColumnID,
			"targetColumnId": in.TargetColumnID,
			"cardinality":    in.Cardinality,
		}
		if in.Label != nil {
			payload["label"] = *in.Label
		}

		entity, e := createERRow(prodERWriteFns(ctx, userID), in.WhiteboardID, erRelationshipKind, in.WhiteboardID, payload)
		if e != nil {
			return fail(e)
		}
		return success(entity)
	})
}

func registerUpdateRelationship(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_relationship",
		Description: "Update a relationship's cardinality, label, or endpoints.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateRelationshipInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("relationshipId", in.RelationshipID); e != nil {
			return fail(e)
		}
		for field, val := range map[string]*string{
			"sourceTableId":  in.SourceTableID,
			"targetTableId":  in.TargetTableID,
			"sourceColumnId": in.SourceColumnID,
			"targetColumnId": in.TargetColumnID,
		} {
			if val != nil {
				if e := validateUUID(field, *val); e != nil {
					return fail(e)
				}
			}
		}
		if in.Cardinality != nil && !schema.IsValidCardinality(*in.Cardinality) {
			return validationError("Invalid enum value.", "cardinality")
		}
		// W6: label z.string().max(255)
		if in.Label != nil {
			if e := checkMaxLen("label", *in.Label, 255); e != nil {
				return fail(e)
			}
		}

		userID := auth.UserID(ctx)
		projectID, err := data.GetRelationshipProjectID(ctx, in.RelationshipID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Relationship %s not found.", in.RelationshipID))
		}
		if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		existing, err := data.FindRelationshipByID(ctx, in.RelationshipID)
		if err != nil {
			return fail(err)
		}
		if existing == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Relationship %s not found.", in.RelationshipID))
		}

		payload := map[string]any{"relationshipId": in.RelationshipID}
		if in.SourceTableID != nil {
			payload["sourceTableId"] = *in.SourceTableID
		}
		if in.TargetTableID != nil {
			payload["targetTableId"] = *in.TargetTableID
		}
		if in.SourceColumnID != nil {
			payload["sourceColumnId"] = *in.SourceColumnID
		}
		if in.TargetColumnID != nil {
			payload["targetColumnId"] = *in.TargetColumnID
		}
		if in.Cardinality != nil {
			payload["cardinality"] = *in.Cardinality
		}
		if in.Label != nil {
			payload["label"] = *in.Label
		}

		ack, err := emitOp(ctx, existing.WhiteboardID, userID, "relationship:update", payload,
			"update of relationship "+in.RelationshipID,
			rowCheck(prodERReaders(ctx).relationship, in.RelationshipID, func(r *data.Relationship) bool {
				return relationshipShowsUpdate(r, in)
			}))
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected relationship update."))
		}
		return success(ack.Entity())
	})
}

func registerDeleteRelationship(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_relationship",
		Description: "Delete a relationship by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in relationshipIDInput) (*mcp.CallToolResult, any, error) {
		if e := validateUUID("relationshipId", in.RelationshipID); e != nil {
			return fail(e)
		}
		userID := auth.UserID(ctx)
		projectID, err := data.GetRelationshipProjectID(ctx, in.RelationshipID)
		if err != nil {
			return fail(err)
		}
		if projectID == "" {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Relationship %s not found.", in.RelationshipID))
		}
		if err := auth.AssertSchemaEditAccess(ctx, userID, projectID); err != nil {
			return fail(err)
		}

		existing, err := data.FindRelationshipByID(ctx, in.RelationshipID)
		if err != nil {
			return fail(err)
		}
		if existing == nil {
			return mcpError(mcperr.NotFound, fmt.Sprintf("Relationship %s not found.", in.RelationshipID))
		}

		ack, err := emitOp(ctx, existing.WhiteboardID, userID, "relationship:delete", map[string]any{"relationshipId": in.RelationshipID},
			"delete of relationship "+in.RelationshipID, deletedCheck(prodERReaders(ctx).relationship, in.RelationshipID))
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected relationship deletion."))
		}
		return success(map[string]any{"id": in.RelationshipID})
	})
}

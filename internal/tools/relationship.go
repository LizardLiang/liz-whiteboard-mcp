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

type createRelationshipInput struct {
	WhiteboardID   string  `json:"whiteboardId" jsonschema:"The whiteboard UUID"`
	SourceTableID  string  `json:"sourceTableId" jsonschema:"Source table UUID"`
	TargetTableID  string  `json:"targetTableId" jsonschema:"Target table UUID"`
	SourceColumnID string  `json:"sourceColumnId" jsonschema:"Source column UUID"`
	TargetColumnID string  `json:"targetColumnId" jsonschema:"Target column UUID"`
	Cardinality    string  `json:"cardinality" jsonschema:"Relationship cardinality"`
	Label          *string `json:"label,omitempty" jsonschema:"Optional label"`
}

type updateRelationshipInput struct {
	RelationshipID string  `json:"relationshipId" jsonschema:"The relationship UUID"`
	SourceTableID  *string `json:"sourceTableId,omitempty" jsonschema:"New source table UUID"`
	TargetTableID  *string `json:"targetTableId,omitempty" jsonschema:"New target table UUID"`
	SourceColumnID *string `json:"sourceColumnId,omitempty" jsonschema:"New source column UUID"`
	TargetColumnID *string `json:"targetColumnId,omitempty" jsonschema:"New target column UUID"`
	Cardinality    *string `json:"cardinality,omitempty" jsonschema:"New cardinality"`
	Label          *string `json:"label,omitempty" jsonschema:"New label"`
}

type relationshipIDInput struct {
	RelationshipID string `json:"relationshipId" jsonschema:"The relationship UUID"`
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
		Description: "Create a relationship between two tables.",
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

		ack, err := socket.SocketEmitWithAck(ctx, in.WhiteboardID, userID, "relationship:create", payload)
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected relationship creation."))
		}
		return success(ack.Entity())
	})
}

func registerUpdateRelationship(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "update_relationship",
		Description: "Update a relationship's cardinality, label, or endpoints. " +
			"The server validates referential integrity on all endpoint changes.",
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

		ack, err := socket.SocketEmitWithAck(ctx, existing.WhiteboardID, userID, "relationship:update", payload)
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
		Description: "Delete a relationship by ID.",
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

		ack, err := socket.SocketEmitWithAck(ctx, existing.WhiteboardID, userID, "relationship:delete", map[string]any{"relationshipId": in.RelationshipID})
		if err != nil {
			return fail(err)
		}
		if !ack.OK() {
			return mcpError(mcperr.AckCodeToMcpCode(ack.Code()), msgOr(ack.Message(), "Server rejected relationship deletion."))
		}
		return success(map[string]any{"id": in.RelationshipID})
	})
}

package data

import (
	"context"
	"fmt"
)

// RelationshipEndpoints holds the UUIDs needed to validate that relationship
// endpoint columns belong to their declared tables.
type RelationshipEndpoints struct {
	SourceTableID  string
	TargetTableID  string
	SourceColumnID string
	TargetColumnID string
	WhiteboardID   string
}

// columnLookupFunc is the injectable column finder. Production code passes
// FindColumnByID; tests pass a mock. Defined as an unexported type so tests
// in the same package can use it directly.
type columnLookupFunc func(ctx context.Context, id string) (*Column, error)

// assertRelationshipEndpointsValidWith is the injectable core.
// It verifies that sourceColumnId belongs to sourceTableId and targetColumnId
// belongs to targetTableId. Mirrors assertRelationshipEndpointsValid in
// src/data/relationship.ts.
func assertRelationshipEndpointsValidWith(
	ctx context.Context,
	lookup columnLookupFunc,
	ep RelationshipEndpoints,
) error {
	srcCol, err := lookup(ctx, ep.SourceColumnID)
	if err != nil {
		return err
	}
	if srcCol == nil || srcCol.TableID != ep.SourceTableID {
		return fmt.Errorf("sourceColumnId %s does not belong to sourceTableId %s",
			ep.SourceColumnID, ep.SourceTableID)
	}

	tgtCol, err := lookup(ctx, ep.TargetColumnID)
	if err != nil {
		return err
	}
	if tgtCol == nil || tgtCol.TableID != ep.TargetTableID {
		return fmt.Errorf("targetColumnId %s does not belong to targetTableId %s",
			ep.TargetColumnID, ep.TargetTableID)
	}
	return nil
}

// AssertRelationshipEndpointsValid validates that the source and target column
// IDs belong to their declared table IDs. Call this before emitting
// relationship:create or relationship:update to catch mismatches client-side
// and return a specific VALIDATION_ERROR instead of waiting for a server ack.
func AssertRelationshipEndpointsValid(ctx context.Context, ep RelationshipEndpoints) error {
	return assertRelationshipEndpointsValidWith(ctx, FindColumnByID, ep)
}

// Package data — relationship referential integrity tests.
// Suite H: TC-UNIT-REFINT-01 through TC-UNIT-REFINT-06
//
// Strategy: unit-mockable — inject a columnLookupFunc mock so no real DB is needed.
// Mirrors relationship-integrity.test.ts exactly, using the same RFC-4122 UUIDs.
package data

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UUIDs — identical to the TS relationship-integrity.test.ts fixtures.
const (
	riT1  = "11111111-1111-4111-8111-111111111111"
	riT2  = "22222222-2222-4222-8222-222222222222"
	riT3  = "33333333-3333-4333-8333-333333333333"
	riT99 = "99999999-9999-4999-8999-999999999999"
	riC1  = "a1111111-1111-4111-8111-111111111111"
	riC2  = "a2222222-2222-4222-8222-222222222222"
	riC3  = "a3333333-3333-4333-8333-333333333333"
	riWB  = "ab111111-1111-4111-8111-111111111111"
)

// mockColLookup returns a Column whose TableID is determined by the colToTable map.
// Returns (nil, nil) for unknown column IDs.
func mockColLookup(colToTable map[string]string) columnLookupFunc {
	return func(_ context.Context, id string) (*Column, error) {
		if tableID, ok := colToTable[id]; ok {
			return &Column{ID: id, TableID: tableID}, nil
		}
		return nil, nil
	}
}

// TC-UNIT-REFINT-01: sourceColumnId belongs to wrong table → exact error message.
func TestRefInt_01_SourceColumnWrongTable(t *testing.T) {
	lookup := mockColLookup(map[string]string{
		riC1: riT99, // C1 belongs to T99, not T1
		riC2: riT2,
	})
	err := assertRelationshipEndpointsValidWith(context.Background(), lookup,
		RelationshipEndpoints{
			SourceTableID:  riT1,
			TargetTableID:  riT2,
			SourceColumnID: riC1,
			TargetColumnID: riC2,
			WhiteboardID:   riWB,
		})
	require.Error(t, err)
	assert.Equal(t,
		fmt.Sprintf("sourceColumnId %s does not belong to sourceTableId %s", riC1, riT1),
		err.Error())
}

// TC-UNIT-REFINT-02: targetColumnId belongs to wrong table → exact error message.
func TestRefInt_02_TargetColumnWrongTable(t *testing.T) {
	lookup := mockColLookup(map[string]string{
		riC1: riT1,
		riC2: riT99, // C2 belongs to T99, not T2
	})
	err := assertRelationshipEndpointsValidWith(context.Background(), lookup,
		RelationshipEndpoints{
			SourceTableID:  riT1,
			TargetTableID:  riT2,
			SourceColumnID: riC1,
			TargetColumnID: riC2,
			WhiteboardID:   riWB,
		})
	require.Error(t, err)
	assert.Equal(t,
		fmt.Sprintf("targetColumnId %s does not belong to targetTableId %s", riC2, riT2),
		err.Error())
}

// TC-UNIT-REFINT-03: patching sourceColumnId to wrong table fails merged validation.
func TestRefInt_03_PatchedSourceColumnWrongTable(t *testing.T) {
	// C3 belongs to T99, not T1 — simulates update_relationship patching the source column.
	lookup := mockColLookup(map[string]string{
		riC3: riT99, // patched — wrong table
		riC2: riT2,
	})
	err := assertRelationshipEndpointsValidWith(context.Background(), lookup,
		RelationshipEndpoints{
			SourceTableID:  riT1,
			TargetTableID:  riT2,
			SourceColumnID: riC3, // patched
			TargetColumnID: riC2,
			WhiteboardID:   riWB,
		})
	require.Error(t, err)
	assert.Equal(t,
		fmt.Sprintf("sourceColumnId %s does not belong to sourceTableId %s", riC3, riT1),
		err.Error())
}

// TC-UNIT-REFINT-04: patching targetTableId while keeping old targetColumnId fails.
// C2 belongs to T2, but the relationship is being updated to claim T3 as target.
func TestRefInt_04_PatchedTargetTable_OldColumnFails(t *testing.T) {
	lookup := mockColLookup(map[string]string{
		riC1: riT1,
		riC2: riT2, // C2 belongs to T2, not T3
	})
	err := assertRelationshipEndpointsValidWith(context.Background(), lookup,
		RelationshipEndpoints{
			SourceTableID:  riT1,
			TargetTableID:  riT3, // patched to T3
			SourceColumnID: riC1,
			TargetColumnID: riC2, // still old column from T2
			WhiteboardID:   riWB,
		})
	require.Error(t, err)
	assert.Equal(t,
		fmt.Sprintf("targetColumnId %s does not belong to targetTableId %s", riC2, riT3),
		err.Error())
}

// TC-UNIT-REFINT-05: valid endpoints pass without error.
func TestRefInt_05_ValidEndpoints_NoError(t *testing.T) {
	lookup := mockColLookup(map[string]string{
		riC1: riT1,
		riC2: riT2,
	})
	err := assertRelationshipEndpointsValidWith(context.Background(), lookup,
		RelationshipEndpoints{
			SourceTableID:  riT1,
			TargetTableID:  riT2,
			SourceColumnID: riC1,
			TargetColumnID: riC2,
			WhiteboardID:   riWB,
		})
	assert.NoError(t, err)
}

// TC-UNIT-REFINT-06: regression — wrong source column is rejected on the create path.
// In Go, AssertRelationshipEndpointsValid is the validation gate before socket emit.
func TestRefInt_06_CreatePath_WrongSourceColumnRejected(t *testing.T) {
	lookup := mockColLookup(map[string]string{
		riC1: riT99, // wrong — simulates the bug that REFINT-06 guards against
		riC2: riT2,
	})
	err := assertRelationshipEndpointsValidWith(context.Background(), lookup,
		RelationshipEndpoints{
			SourceTableID:  riT1,
			TargetTableID:  riT2,
			SourceColumnID: riC1,
			TargetColumnID: riC2,
			WhiteboardID:   riWB,
		})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sourceColumnId")
	assert.Contains(t, err.Error(), "does not belong to sourceTableId")
}

// TestRefInt_NilSourceColumn_ReturnsError: column not found → treat as wrong table.
func TestRefInt_NilSourceColumn_ReturnsError(t *testing.T) {
	// Column ID not in map → lookup returns nil
	lookup := mockColLookup(map[string]string{
		riC2: riT2, // only C2 is found; C1 is missing
	})
	err := assertRelationshipEndpointsValidWith(context.Background(), lookup,
		RelationshipEndpoints{
			SourceTableID:  riT1,
			TargetTableID:  riT2,
			SourceColumnID: riC1, // not in DB
			TargetColumnID: riC2,
			WhiteboardID:   riWB,
		})
	require.Error(t, err)
	assert.Contains(t, err.Error(), riC1)
	assert.Contains(t, err.Error(), riT1)
}

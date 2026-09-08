package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The enum lists must match the Zod schemas in src/data/schema.ts exactly.
func TestEnumCounts(t *testing.T) {
	assert.Len(t, DataTypes, 25, "dataTypeSchema.options has 25 entries")
	assert.Len(t, Cardinalities, 17, "cardinalitySchema.options has 17 entries")
	assert.Len(t, AreaColors, 8, "AREA_COLOR_IDS (src/lib/area-colors.ts) has 8 entries")
}

func TestIsValidDataType(t *testing.T) {
	assert.True(t, IsValidDataType("uuid"))
	assert.True(t, IsValidDataType("varchar"))
	assert.True(t, IsValidDataType("json"))
	assert.False(t, IsValidDataType("NOTATYPE"))
	assert.False(t, IsValidDataType(""))
	assert.False(t, IsValidDataType("UUID")) // case-sensitive
}

func TestIsValidCardinality(t *testing.T) {
	assert.True(t, IsValidCardinality("ONE_TO_MANY"))
	assert.True(t, IsValidCardinality("ZERO_OR_MANY_TO_ZERO_OR_MANY"))
	assert.False(t, IsValidCardinality("MANY"))
	assert.False(t, IsValidCardinality(""))
}

func TestIsValidAreaColor(t *testing.T) {
	assert.True(t, IsValidAreaColor("slate"))
	assert.True(t, IsValidAreaColor("violet"))
	assert.False(t, IsValidAreaColor("NOTACOLOR"))
	assert.False(t, IsValidAreaColor(""))
	assert.False(t, IsValidAreaColor("Slate")) // case-sensitive
}

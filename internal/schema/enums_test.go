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

// CanvasElementKinds is the create_canvas_element surface, deliberately NARROWER
// than the app's canvasElementKindSchema (7 members): connector gets its own tool
// because its props arm is a filled union, and group is out of scope entirely.
func TestCanvasElementKindCount(t *testing.T) {
	assert.Len(t, CanvasElementKinds, 5,
		"create_canvas_element accepts the 5 shape/text kinds, not connector or group")
}

func TestIsValidCanvasElementKind(t *testing.T) {
	for _, kind := range []string{"rectangle", "ellipse", "diamond", "triangle", "text"} {
		assert.True(t, IsValidCanvasElementKind(kind), kind+" is a supported kind")
	}
	// Both exist in the app's enum and both must still be refused here.
	assert.False(t, IsValidCanvasElementKind("connector"), "connector has its own tool")
	assert.False(t, IsValidCanvasElementKind("group"), "group is out of scope")
	assert.False(t, IsValidCanvasElementKind(""))
	assert.False(t, IsValidCanvasElementKind("Rectangle")) // case-sensitive
}

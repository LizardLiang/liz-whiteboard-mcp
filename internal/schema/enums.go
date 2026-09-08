// Package schema defines the column data types and relationship cardinalities
// supported by liz-whiteboard, plus validation helpers.
//
// These lists MUST match dataTypeSchema.options and cardinalitySchema.options
// in src/data/schema.ts exactly. They are validated at the Go layer (the DB
// stores them as plain strings).
package schema

// DataTypes is the ordered list of valid column data types.
// Mirrors dataTypeSchema.options in src/data/schema.ts.
var DataTypes = []string{
	// Numeric
	"int", "bigint", "smallint", "float", "double", "decimal", "serial", "money",
	// String
	"string", "char", "varchar", "text",
	// Boolean
	"boolean", "bit",
	// Date/Time
	"date", "datetime", "timestamp", "time",
	// Binary
	"binary", "blob",
	// Structured
	"json", "xml", "array", "enum",
	// Identity
	"uuid",
}

// Cardinalities is the ordered list of valid relationship cardinalities.
// Mirrors cardinalitySchema.options in src/data/schema.ts.
var Cardinalities = []string{
	"ONE_TO_ONE",
	"ONE_TO_MANY",
	"MANY_TO_ONE",
	"MANY_TO_MANY",
	"ZERO_TO_ONE",
	"ZERO_TO_MANY",
	"SELF_REFERENCING",
	"MANY_TO_ZERO_OR_ONE",
	"MANY_TO_ZERO_OR_MANY",
	"ZERO_OR_ONE_TO_ONE",
	"ZERO_OR_ONE_TO_MANY",
	"ZERO_OR_ONE_TO_ZERO_OR_ONE",
	"ZERO_OR_ONE_TO_ZERO_OR_MANY",
	"ZERO_OR_MANY_TO_ONE",
	"ZERO_OR_MANY_TO_MANY",
	"ZERO_OR_MANY_TO_ZERO_OR_ONE",
	"ZERO_OR_MANY_TO_ZERO_OR_MANY",
}

// AreaColors is the fixed curated palette of subject-area (GH #106) color ids.
// Mirrors AREA_COLOR_IDS in src/lib/area-colors.ts exactly (ids and order;
// the visual values — solid/fill/border — are resolved server-side and are
// not needed here).
var AreaColors = []string{
	"slate", "red", "orange", "amber", "green", "teal", "blue", "violet",
}

// CanvasElementKinds is the ordered list of canvas element kinds that
// create_canvas_element accepts.
//
// This list is DELIBERATELY narrower than the app's canvasElementKindSchema
// (src/data/schema.ts), which also carries "connector" and "group":
//   - connector has its own MCP tool, because it is the only props arm with real
//     content and three cross-field invariants; folding it into the generic
//     create would produce a union an LLM cannot fill reliably.
//   - group is out of scope: its cascade and cycle integrity is a scene-level
//     invariant the client repairs on load, and the MCP has no scene to check
//     against.
//
// Every kind here has an empty props arm, so props is exactly {"kind": <kind>}.
var CanvasElementKinds = []string{
	"rectangle", "ellipse", "diamond", "triangle", "text",
}

var dataTypeSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(DataTypes))
	for _, dt := range DataTypes {
		m[dt] = struct{}{}
	}
	return m
}()

var cardinalitySet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(Cardinalities))
	for _, c := range Cardinalities {
		m[c] = struct{}{}
	}
	return m
}()

var areaColorSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(AreaColors))
	for _, c := range AreaColors {
		m[c] = struct{}{}
	}
	return m
}()

var canvasElementKindSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(CanvasElementKinds))
	for _, k := range CanvasElementKinds {
		m[k] = struct{}{}
	}
	return m
}()

// IsValidDataType reports whether s is a recognized column data type.
func IsValidDataType(s string) bool {
	_, ok := dataTypeSet[s]
	return ok
}

// IsValidCardinality reports whether s is a recognized relationship cardinality.
func IsValidCardinality(s string) bool {
	_, ok := cardinalitySet[s]
	return ok
}

// IsValidAreaColor reports whether s is a recognized area palette color id.
func IsValidAreaColor(s string) bool {
	_, ok := areaColorSet[s]
	return ok
}

// IsValidCanvasElementKind reports whether s is a canvas element kind that
// create_canvas_element accepts. "connector" and "group" are valid kinds in the
// app but are refused here on purpose — see CanvasElementKinds.
func IsValidCanvasElementKind(s string) bool {
	_, ok := canvasElementKindSet[s]
	return ok
}

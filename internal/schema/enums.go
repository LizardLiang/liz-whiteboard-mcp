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

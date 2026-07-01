// Package ddl generates single-table CREATE TABLE DDL statements for the
// get_table_ddl MCP tool. It operates entirely on already-loaded
// data.WhiteboardWithDiagram values (the same shape internal/summary consumes)
// so generation is DB-free and fully unit-testable.
package ddl

import "strings"

// Supported SQL dialects for DDL generation.
const (
	DialectPostgres = "postgres"
	DialectMySQL    = "mysql"
	DialectMSSQL    = "mssql"
)

// Dialects is the ordered list of valid dialect identifiers. Postgres is the
// default (the generic data types in internal/schema/enums.go already mirror
// Postgres naming: serial, money, uuid, json).
var Dialects = []string{DialectPostgres, DialectMySQL, DialectMSSQL}

var dialectSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(Dialects))
	for _, d := range Dialects {
		m[d] = struct{}{}
	}
	return m
}()

// IsValidDialect reports whether s is a recognized DDL dialect.
func IsValidDialect(s string) bool {
	_, ok := dialectSet[s]
	return ok
}

// QuoteIdent quotes an identifier (table or column name) per dialect
// convention, escaping any embedded quote character by doubling it.
func QuoteIdent(dialect, name string) string {
	switch dialect {
	case DialectMySQL:
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case DialectMSSQL:
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default: // DialectPostgres and any unrecognized value fall back to postgres quoting.
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
}

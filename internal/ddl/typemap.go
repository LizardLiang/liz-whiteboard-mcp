package ddl

// Type maps: one map[string]string per dialect, keyed by every value in
// schema.DataTypes (internal/schema/enums.go), mapping the generic type to
// that dialect's native SQL type. Every map below MUST contain exactly the
// same key set as schema.DataTypes — enforced by TestTypeMapCompleteness in
// ddl_test.go so a new data type can't silently fall through.
//
// Types with no clean 1:1 dialect equivalent (enum, array, xml, bit, char,
// etc.) get a documented best-effort mapping, flagged "// approximate mapping".

// postgresTypes maps generic data types to Postgres native types.
var postgresTypes = map[string]string{
	"int":       "INTEGER",
	"bigint":    "BIGINT",
	"smallint":  "SMALLINT",
	"float":     "REAL",
	"double":    "DOUBLE PRECISION",
	"decimal":   "DECIMAL",
	"serial":    "SERIAL",
	"money":     "MONEY",
	"string":    "VARCHAR",
	"char":      "CHAR", // approximate mapping (generic char with no length on the Column model)
	"varchar":   "VARCHAR",
	"text":      "TEXT",
	"boolean":   "BOOLEAN",
	"bit":       "BIT", // approximate mapping (generic bit with no length defaults to BIT(1))
	"date":      "DATE",
	"datetime":  "TIMESTAMP", // approximate mapping (Postgres has no native DATETIME type)
	"timestamp": "TIMESTAMP",
	"time":      "TIME",
	"binary":    "BYTEA", // approximate mapping (Postgres has no fixed-length BINARY type)
	"blob":      "BYTEA",
	"json":      "JSON",
	"xml":       "XML",
	"array":     "TEXT[]",       // approximate mapping (Column model carries no element type)
	"enum":      "VARCHAR(255)", // approximate mapping (Column model carries no enum-values list)
	"uuid":      "UUID",
}

// mysqlTypes maps generic data types to MySQL native types.
var mysqlTypes = map[string]string{
	"int":       "INT",
	"bigint":    "BIGINT",
	"smallint":  "SMALLINT",
	"float":     "FLOAT",
	"double":    "DOUBLE",
	"decimal":   "DECIMAL",
	"serial":    "INT AUTO_INCREMENT",
	"money":     "DECIMAL(19,4)", // approximate mapping (MySQL has no native MONEY type)
	"string":    "VARCHAR(255)",
	"char":      "CHAR(1)", // approximate mapping (generic char with no length on the Column model)
	"varchar":   "VARCHAR(255)",
	"text":      "TEXT",
	"boolean":   "TINYINT(1)",
	"bit":       "BIT(1)",
	"date":      "DATE",
	"datetime":  "DATETIME",
	"timestamp": "TIMESTAMP",
	"time":      "TIME",
	"binary":    "VARBINARY(255)", // approximate mapping (generic binary with no length on the Column model)
	"blob":      "BLOB",
	"json":      "JSON",
	"xml":       "TEXT",         // approximate mapping (MySQL has no native XML type)
	"array":     "JSON",         // approximate mapping (MySQL has no native ARRAY type)
	"enum":      "VARCHAR(255)", // approximate mapping (Column model carries no enum-values list)
	"uuid":      "CHAR(36)",
}

// mssqlTypes maps generic data types to Microsoft SQL Server native types.
var mssqlTypes = map[string]string{
	"int":       "INT",
	"bigint":    "BIGINT",
	"smallint":  "SMALLINT",
	"float":     "FLOAT",
	"double":    "FLOAT(53)", // approximate mapping (MSSQL has no native DOUBLE type)
	"decimal":   "DECIMAL",
	"serial":    "INT IDENTITY(1,1)",
	"money":     "MONEY",
	"string":    "NVARCHAR(255)",
	"char":      "NCHAR(1)", // approximate mapping (generic char with no length on the Column model)
	"varchar":   "NVARCHAR(255)",
	"text":      "NVARCHAR(MAX)",
	"boolean":   "BIT",
	"bit":       "BIT",
	"date":      "DATE",
	"datetime":  "DATETIME2", // approximate mapping (modern MSSQL equivalent)
	"timestamp": "DATETIME2", // approximate mapping (MSSQL TIMESTAMP is a rowversion type, not a datetime)
	"time":      "TIME",
	"binary":    "VARBINARY(MAX)",
	"blob":      "VARBINARY(MAX)",
	"json":      "NVARCHAR(MAX)",
	"xml":       "XML",
	"array":     "NVARCHAR(MAX)", // approximate mapping (MSSQL has no native ARRAY type)
	"enum":      "NVARCHAR(255)", // approximate mapping (Column model carries no enum-values list)
	"uuid":      "UNIQUEIDENTIFIER",
}

// typeMaps indexes the per-dialect maps by dialect constant, for lookup
// keyed by a runtime dialect string.
var typeMaps = map[string]map[string]string{
	DialectPostgres: postgresTypes,
	DialectMySQL:    mysqlTypes,
	DialectMSSQL:    mssqlTypes,
}

// mapDataType resolves dataType to its native SQL type for dialect. Falls
// back to the raw dataType string unchanged if somehow absent from the
// map — this should never happen given the completeness test, but keeps
// generation total rather than panicking on drift.
func mapDataType(dialect, dataType string) string {
	if m, ok := typeMaps[dialect]; ok {
		if t, ok := m[dataType]; ok {
			return t
		}
	}
	return dataType
}

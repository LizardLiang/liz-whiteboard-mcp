package ddl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/schema"
)

// ---------------------------------------------------------------------------
// TestTypeMapCompleteness
// Every dialect type map must have exactly the same key set as
// schema.DataTypes, so a newly added data type can't silently fall through
// to the raw-string fallback in mapDataType.
// ---------------------------------------------------------------------------

func TestTypeMapCompleteness(t *testing.T) {
	want := make(map[string]struct{}, len(schema.DataTypes))
	for _, dt := range schema.DataTypes {
		want[dt] = struct{}{}
	}

	for _, dialect := range Dialects {
		m := typeMaps[dialect]
		require.NotNilf(t, m, "no type map registered for dialect %q", dialect)
		assert.Lenf(t, m, len(want), "dialect %q: type map has %d entries, want %d (schema.DataTypes)", dialect, len(m), len(want))
		for dt := range want {
			_, ok := m[dt]
			assert.Truef(t, ok, "dialect %q: missing mapping for data type %q", dialect, dt)
		}
		for dt := range m {
			_, ok := want[dt]
			assert.Truef(t, ok, "dialect %q: type map has stray key %q not present in schema.DataTypes", dialect, dt)
		}
	}
}

// ---------------------------------------------------------------------------
// TestQuoteIdent — identifier quoting/escaping per dialect.
// ---------------------------------------------------------------------------

func TestQuoteIdent(t *testing.T) {
	cases := []struct {
		dialect string
		name    string
		want    string
	}{
		{DialectPostgres, "users", `"users"`},
		{DialectPostgres, `w"eird`, `"w""eird"`},
		{DialectMySQL, "users", "`users`"},
		{DialectMySQL, "w`eird", "`w``eird`"},
		{DialectMSSQL, "users", "[users]"},
		{DialectMSSQL, "w]eird", "[w]]eird]"},
	}
	for _, c := range cases {
		got := QuoteIdent(c.dialect, c.name)
		assert.Equalf(t, c.want, got, "QuoteIdent(%q, %q)", c.dialect, c.name)
	}
}

func TestIsValidDialect(t *testing.T) {
	assert.True(t, IsValidDialect(DialectPostgres))
	assert.True(t, IsValidDialect(DialectMySQL))
	assert.True(t, IsValidDialect(DialectMSSQL))
	assert.False(t, IsValidDialect("sqlite"))
	assert.False(t, IsValidDialect(""))
}

// ---------------------------------------------------------------------------
// Test fixture builders
// ---------------------------------------------------------------------------

// buildBoard constructs a two-table board: "users" (referenced target) and
// "posts" (the table under test), with one outgoing FK from posts.authorId
// to users.id. posts has a single-column identity PK ("id", dataType
// "serial"), a NOT NULL UNIQUE string column ("slug"), and the FK column
// ("authorId").
func buildBoard() *data.WhiteboardWithDiagram {
	usersID := "tbl-users"
	postsID := "tbl-posts"

	usersIDCol := data.Column{ID: "col-users-id", TableID: usersID, Name: "id", DataType: "uuid", IsPrimaryKey: true, IsNullable: false}

	postsIDCol := data.Column{ID: "col-posts-id", TableID: postsID, Name: "id", DataType: "serial", IsPrimaryKey: true, IsNullable: false}
	authorIDCol := data.Column{ID: "col-posts-authorId", TableID: postsID, Name: "authorId", DataType: "uuid", IsForeignKey: true, IsNullable: false}
	slugCol := data.Column{ID: "col-posts-slug", TableID: postsID, Name: "slug", DataType: "varchar", IsNullable: false, IsUnique: true}

	rel := data.Relationship{
		ID:             "rel-1",
		SourceTableID:  postsID,
		TargetTableID:  usersID,
		SourceColumnID: authorIDCol.ID,
		TargetColumnID: usersIDCol.ID,
		Cardinality:    "MANY_TO_ONE",
	}

	users := data.TableWithRelations{
		DiagramTable: data.DiagramTable{ID: usersID, Name: "users"},
		Columns:      []data.Column{usersIDCol},
	}
	posts := data.TableWithRelations{
		DiagramTable:          data.DiagramTable{ID: postsID, Name: "posts"},
		Columns:               []data.Column{postsIDCol, authorIDCol, slugCol},
		OutgoingRelationships: []data.Relationship{rel},
	}

	return &data.WhiteboardWithDiagram{
		Tables: []data.TableWithRelations{users, posts},
	}
}

// ---------------------------------------------------------------------------
// TestGenerateTableDDL_Golden — one exact-text assertion per dialect.
// ---------------------------------------------------------------------------

func TestGenerateTableDDL_Golden(t *testing.T) {
	board := buildBoard()

	cases := []struct {
		dialect string
		want    string
	}{
		{
			DialectPostgres,
			"CREATE TABLE \"posts\" (\n" +
				"  \"id\" SERIAL NOT NULL PRIMARY KEY,\n" +
				"  \"authorId\" UUID NOT NULL,\n" +
				"  \"slug\" VARCHAR NOT NULL UNIQUE,\n" +
				"  FOREIGN KEY (\"authorId\") REFERENCES \"users\"(\"id\")\n" +
				");",
		},
		{
			DialectMySQL,
			"CREATE TABLE `posts` (\n" +
				"  `id` INT AUTO_INCREMENT NOT NULL PRIMARY KEY,\n" +
				"  `authorId` CHAR(36) NOT NULL,\n" +
				"  `slug` VARCHAR(255) NOT NULL UNIQUE,\n" +
				"  FOREIGN KEY (`authorId`) REFERENCES `users`(`id`)\n" +
				");",
		},
		{
			DialectMSSQL,
			"CREATE TABLE [posts] (\n" +
				"  [id] INT IDENTITY(1,1) NOT NULL PRIMARY KEY,\n" +
				"  [authorId] UNIQUEIDENTIFIER NOT NULL,\n" +
				"  [slug] NVARCHAR(255) NOT NULL UNIQUE,\n" +
				"  FOREIGN KEY ([authorId]) REFERENCES [users]([id])\n" +
				");",
		},
	}

	for _, c := range cases {
		got, err := GenerateTableDDL(board, "tbl-posts", c.dialect)
		require.NoErrorf(t, err, "dialect %q", c.dialect)
		assert.Equalf(t, c.want, got, "dialect %q", c.dialect)
	}
}

// TestGenerateTableDDL_DefaultDialect confirms an empty dialect defaults to
// Postgres (same output as the explicit "postgres" golden case).
func TestGenerateTableDDL_DefaultDialect(t *testing.T) {
	board := buildBoard()
	got, err := GenerateTableDDL(board, "tbl-posts", "")
	require.NoError(t, err)
	assert.Contains(t, got, `CREATE TABLE "posts"`)
	assert.Contains(t, got, `"id" SERIAL NOT NULL PRIMARY KEY`)
}

func TestGenerateTableDDL_InvalidDialect(t *testing.T) {
	board := buildBoard()
	_, err := GenerateTableDDL(board, "tbl-posts", "sqlite")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// TestGenerateTableDDL_CompositePK — two IsPrimaryKey columns render a
// table-level PRIMARY KEY (...) constraint line instead of an inline PK.
// ---------------------------------------------------------------------------

func TestGenerateTableDDL_CompositePK(t *testing.T) {
	tblID := "tbl-membership"
	col1 := data.Column{ID: "col-1", TableID: tblID, Name: "userId", DataType: "uuid", IsPrimaryKey: true, IsNullable: false}
	col2 := data.Column{ID: "col-2", TableID: tblID, Name: "groupId", DataType: "uuid", IsPrimaryKey: true, IsNullable: false}

	board := &data.WhiteboardWithDiagram{
		Tables: []data.TableWithRelations{
			{
				DiagramTable: data.DiagramTable{ID: tblID, Name: "membership"},
				Columns:      []data.Column{col1, col2},
			},
		},
	}

	got, err := GenerateTableDDL(board, tblID, DialectPostgres)
	require.NoError(t, err)

	want := "CREATE TABLE \"membership\" (\n" +
		"  \"userId\" UUID NOT NULL,\n" +
		"  \"groupId\" UUID NOT NULL,\n" +
		"  PRIMARY KEY (\"userId\", \"groupId\")\n" +
		");"
	assert.Equal(t, want, got)
	assert.NotContains(t, got, "PRIMARY KEY\",\n") // no inline PK on either column
}

// ---------------------------------------------------------------------------
// TestGenerateTableDDL_NotFound — unknown tableID surfaces an error.
// ---------------------------------------------------------------------------

func TestGenerateTableDDL_NotFound(t *testing.T) {
	board := buildBoard()
	_, err := GenerateTableDDL(board, "does-not-exist", DialectPostgres)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// TestGenerateTableDDL_ZeroColumns — a table with no columns must error
// rather than silently emit invalid "CREATE TABLE x (\n\n);" SQL.
// ---------------------------------------------------------------------------

func TestGenerateTableDDL_ZeroColumns(t *testing.T) {
	tblID := "tbl-empty"
	board := &data.WhiteboardWithDiagram{
		Tables: []data.TableWithRelations{
			{
				DiagramTable: data.DiagramTable{ID: tblID, Name: "empty"},
				Columns:      nil,
			},
		},
	}

	got, err := GenerateTableDDL(board, tblID, DialectPostgres)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), tblID)
	assert.Contains(t, err.Error(), "no columns")
}

// ---------------------------------------------------------------------------
// TestGenerateTableDDL_NilBoard — a nil board must error rather than panic.
// ---------------------------------------------------------------------------

func TestGenerateTableDDL_NilBoard(t *testing.T) {
	_, err := GenerateTableDDL(nil, "tbl-posts", DialectPostgres)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// TestGenerateTableDDL_ColumnOrderIndependent — GenerateTableDDL must sort
// columns by their Order field before rendering, regardless of the order
// they appear in target.Columns. This makes the generator self-contained
// instead of relying on callers (e.g. internal/data's ORDER BY "order" ASC)
// to pre-sort.
// ---------------------------------------------------------------------------

func TestGenerateTableDDL_ColumnOrderIndependent(t *testing.T) {
	tblID := "tbl-scrambled"
	colA := data.Column{ID: "col-a", TableID: tblID, Name: "a_first", DataType: "uuid", Order: 0, IsNullable: false}
	colB := data.Column{ID: "col-b", TableID: tblID, Name: "b_second", DataType: "uuid", Order: 1, IsNullable: false}
	colC := data.Column{ID: "col-c", TableID: tblID, Name: "c_third", DataType: "uuid", Order: 2, IsNullable: false}

	// Columns arrive scrambled: c, a, b — Order fields still say a, b, c.
	board := &data.WhiteboardWithDiagram{
		Tables: []data.TableWithRelations{
			{
				DiagramTable: data.DiagramTable{ID: tblID, Name: "scrambled"},
				Columns:      []data.Column{colC, colA, colB},
			},
		},
	}

	got, err := GenerateTableDDL(board, tblID, DialectPostgres)
	require.NoError(t, err)

	want := "CREATE TABLE \"scrambled\" (\n" +
		"  \"a_first\" UUID NOT NULL,\n" +
		"  \"b_second\" UUID NOT NULL,\n" +
		"  \"c_third\" UUID NOT NULL\n" +
		");"
	assert.Equal(t, want, got)
}

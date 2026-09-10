package data

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// FindReferenceConfirmName decides what delete_table_reference compares a
// caller's confirmName against, in front of a cascading delete. It mirrors a
// rule that lives in the OTHER repo (resolveTableReferences in
// src/data/table-reference.ts), so these tests run against a real temporary
// SQLite built from the app's own column names: a drift between the two repos
// fails here rather than silently changing which name the guard demands.
var refTestSchema = []string{
	`CREATE TABLE "Whiteboard" (
		"id"        TEXT NOT NULL PRIMARY KEY,
		"name"      TEXT NOT NULL,
		"projectId" TEXT NOT NULL,
		"createdAt" DATETIME NOT NULL,
		"updatedAt" DATETIME NOT NULL)`,
	`CREATE TABLE "DiagramTable" (
		"id"                 TEXT NOT NULL PRIMARY KEY,
		"whiteboardId"       TEXT NOT NULL,
		"name"               TEXT NOT NULL,
		"description"        TEXT,
		"positionX"          REAL,
		"positionY"          REAL,
		"width"              REAL,
		"height"             REAL,
		"sourceWhiteboardId" TEXT,
		"sourceTableId"      TEXT,
		"createdAt"          DATETIME NOT NULL,
		"updatedAt"          DATETIME NOT NULL)`,
}

const (
	trLocalBoard  = "f0000000-0000-4000-8000-00000000000a"
	trSourceBoard = "f0000000-0000-4000-8000-00000000000b"
	trOtherBoard  = "f0000000-0000-4000-8000-00000000000c"
	trSourceTable = "f1111111-1111-4111-8111-111111111111"
	trRefNode     = "f2222222-2222-4222-8222-222222222222"
	trPlainTable  = "f3333333-3333-4333-8333-333333333333"
	trNow         = int64(1_757_000_000_000)
)

func newReferenceTestDB(t *testing.T) context.Context {
	t.Helper()
	db.Close()
	path := filepath.Join(t.TempDir(), "table_reference_test.db")
	t.Setenv("DATABASE_URL", "file:"+path)

	ctx := context.Background()
	conn, err := db.Connect(ctx)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	for _, stmt := range refTestSchema {
		_, err := conn.Exec(ctx, stmt)
		require.NoError(t, err, "schema: %s", stmt)
	}
	return ctx
}

func trExec(t *testing.T, ctx context.Context, q string, args ...any) {
	t.Helper()
	_, err := db.Pool().Exec(ctx, q, args...)
	require.NoError(t, err)
}

func trInsertBoard(t *testing.T, ctx context.Context, id, name string) {
	t.Helper()
	trExec(t, ctx, `INSERT INTO "Whiteboard" ("id","name","projectId","createdAt","updatedAt")
		VALUES (?,?,?,?,?)`, id, name, "p1", trNow, trNow)
}

func trInsertTable(t *testing.T, ctx context.Context, id, boardID, name string, srcBoard, srcTable any) {
	t.Helper()
	trExec(t, ctx, `INSERT INTO "DiagramTable"
		("id","whiteboardId","name","sourceWhiteboardId","sourceTableId","createdAt","updatedAt")
		VALUES (?,?,?,?,?,?,?)`, id, boardID, name, srcBoard, srcTable, trNow, trNow)
}

// The ordinary case: the source is intact, so the guard demands the SOURCE
// table's live name — which is what list_table_references shows the agent.
func TestFindReferenceConfirmNameUsesLiveSourceName(t *testing.T) {
	ctx := newReferenceTestDB(t)
	trInsertBoard(t, ctx, trLocalBoard, "Billing")
	trInsertBoard(t, ctx, trSourceBoard, "Accounts")
	trInsertTable(t, ctx, trSourceTable, trSourceBoard, "users", nil, nil)
	// The local row was named at create time and may since be stale.
	trInsertTable(t, ctx, trRefNode, trLocalBoard, "users (stale)", trSourceBoard, trSourceTable)

	got, err := FindReferenceConfirmName(ctx, trRefNode)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.IsReference)
	assert.Equal(t, "users", got.Name, "the live source name wins over the stale local one")
}

// A renamed source repaints the reference, so the guard must follow the rename
// rather than keep demanding the name the node was created with.
func TestFindReferenceConfirmNameFollowsASourceRename(t *testing.T) {
	ctx := newReferenceTestDB(t)
	trInsertBoard(t, ctx, trLocalBoard, "Billing")
	trInsertBoard(t, ctx, trSourceBoard, "Accounts")
	trInsertTable(t, ctx, trSourceTable, trSourceBoard, "users", nil, nil)
	trInsertTable(t, ctx, trRefNode, trLocalBoard, "users", trSourceBoard, trSourceTable)

	trExec(t, ctx, `UPDATE "DiagramTable" SET "name" = ? WHERE "id" = ?`, "accounts", trSourceTable)

	got, err := FindReferenceConfirmName(ctx, trRefNode)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "accounts", got.Name)
}

// Source table deleted: the app falls back to the local row's name, so the
// guard must accept exactly that. Demanding an unobtainable name would make a
// dangling reference impossible to clean up.
func TestFindReferenceConfirmNameFallsBackWhenSourceTableGone(t *testing.T) {
	ctx := newReferenceTestDB(t)
	trInsertBoard(t, ctx, trLocalBoard, "Billing")
	trInsertBoard(t, ctx, trSourceBoard, "Accounts")
	trInsertTable(t, ctx, trRefNode, trLocalBoard, "users", trSourceBoard, trSourceTable)

	got, err := FindReferenceConfirmName(ctx, trRefNode)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.IsReference)
	assert.Equal(t, "users", got.Name)
}

// Source whiteboard deleted — the state the live demo produced.
func TestFindReferenceConfirmNameFallsBackWhenSourceBoardGone(t *testing.T) {
	ctx := newReferenceTestDB(t)
	trInsertBoard(t, ctx, trLocalBoard, "Billing")
	trInsertTable(t, ctx, trRefNode, trLocalBoard, "users", trSourceBoard, trSourceTable)

	got, err := FindReferenceConfirmName(ctx, trRefNode)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "users", got.Name)
}

// The clause that is easy to drop: a source table that still EXISTS but has
// moved to a different file is as broken as a deleted one, because the
// reference no longer describes what it claims to. The app falls back in that
// state, so the guard must too.
func TestFindReferenceConfirmNameFallsBackWhenSourceMovedFiles(t *testing.T) {
	ctx := newReferenceTestDB(t)
	trInsertBoard(t, ctx, trLocalBoard, "Billing")
	trInsertBoard(t, ctx, trSourceBoard, "Accounts")
	trInsertBoard(t, ctx, trOtherBoard, "Elsewhere")
	// The table lives on a board OTHER than the one the reference declares.
	trInsertTable(t, ctx, trSourceTable, trOtherBoard, "moved", nil, nil)
	trInsertTable(t, ctx, trRefNode, trLocalBoard, "users", trSourceBoard, trSourceTable)

	got, err := FindReferenceConfirmName(ctx, trRefNode)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "users", got.Name, "a moved source must not lend its name to the guard")
}

// An ordinary table carries no sourceTableId. The caller must refuse rather
// than cascade a real table's relationships under a reference-only tool.
func TestFindReferenceConfirmNameFlagsAnOrdinaryTable(t *testing.T) {
	ctx := newReferenceTestDB(t)
	trInsertBoard(t, ctx, trLocalBoard, "Billing")
	trInsertTable(t, ctx, trPlainTable, trLocalBoard, "invoices", nil, nil)

	got, err := FindReferenceConfirmName(ctx, trPlainTable)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.IsReference)
}

func TestFindReferenceConfirmNameReturnsNilForUnknownID(t *testing.T) {
	ctx := newReferenceTestDB(t)

	got, err := FindReferenceConfirmName(ctx, trRefNode)

	require.NoError(t, err)
	assert.Nil(t, got)
}

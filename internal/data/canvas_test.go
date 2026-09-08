package data

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// Canvas read-path tests run against a real (temporary) SQLite file built from
// the main app's own DDL, so a column-name drift between the two repos fails
// here rather than at runtime. The DDL below is copied verbatim from
// liz-whiteboard/src/data/schema-sql.ts (canvas engine, milestone 1); "Project",
// "Folder" and "Whiteboard" are reduced to the columns these tests touch,
// because foreign_keys(1) is on and the canvas rows reference them.
var canvasTestSchema = []string{
	`CREATE TABLE "Project" ("id" TEXT NOT NULL PRIMARY KEY, "name" TEXT NOT NULL)`,
	`CREATE TABLE "Folder" ("id" TEXT NOT NULL PRIMARY KEY, "name" TEXT NOT NULL)`,
	`CREATE TABLE "Whiteboard" (
		"id" TEXT NOT NULL PRIMARY KEY,
		"projectId" TEXT NOT NULL)`,
	`CREATE TABLE "CanvasBoard" (
		"id"        TEXT NOT NULL PRIMARY KEY,
		"name"      TEXT NOT NULL,
		"projectId" TEXT NOT NULL,
		"folderId"  TEXT,
		"createdAt" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		"updatedAt" DATETIME NOT NULL,
		CONSTRAINT "CanvasBoard_projectId_fkey" FOREIGN KEY ("projectId")
			REFERENCES "Project" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
		CONSTRAINT "CanvasBoard_folderId_fkey" FOREIGN KEY ("folderId")
			REFERENCES "Folder" ("id") ON DELETE CASCADE ON UPDATE CASCADE)`,
	`CREATE TABLE "CanvasElement" (
		"id"        TEXT NOT NULL PRIMARY KEY,
		"boardId"   TEXT NOT NULL,
		"kind"      TEXT NOT NULL,
		"positionX" REAL NOT NULL,
		"positionY" REAL NOT NULL,
		"width"     REAL NOT NULL,
		"height"    REAL NOT NULL,
		"rotation"  REAL NOT NULL DEFAULT 0,
		"zIndex"    INTEGER NOT NULL DEFAULT 0,
		"text"      TEXT,
		"style"     JSONB,
		"props"     JSONB,
		"createdAt" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		"updatedAt" DATETIME NOT NULL,
		"revision"  INTEGER NOT NULL DEFAULT 0,
		CONSTRAINT "CanvasElement_boardId_fkey" FOREIGN KEY ("boardId")
			REFERENCES "CanvasBoard" ("id") ON DELETE CASCADE ON UPDATE CASCADE)`,
}

// Fixture ids. Canvas board ids are randomUUID() values in the app, exactly
// like whiteboard ids — that shared id space is why the namespace and the
// resolver must be canvas-specific.
const (
	cvProjectA = "c0000000-0000-4000-8000-00000000000a"
	cvProjectB = "c0000000-0000-4000-8000-00000000000b"
	cvBoard1   = "c1111111-1111-4111-8111-111111111111"
	cvBoard2   = "c2222222-2222-4222-8222-222222222222"
	cvBoardWB  = "c3333333-3333-4333-8333-333333333333" // a Whiteboard id, not a CanvasBoard id
	cvUnknown  = "c9999999-9999-4999-8999-999999999999"
)

// cvNow is the fixed unix-ms base the fixtures use. The app writes createdAt /
// updatedAt through nowMs(), i.e. unix milliseconds in a DATETIME-declared
// column, so the existing Timestamp scanner applies unchanged.
const cvNow int64 = 1_757_000_000_000

// newCanvasTestDB opens a fresh temporary SQLite database, applies the canvas
// DDL, and points the db singleton at it for the duration of the test.
func newCanvasTestDB(t *testing.T) context.Context {
	t.Helper()
	db.Close() // drop any singleton a previous test left behind
	path := filepath.Join(t.TempDir(), "canvas_test.db")
	t.Setenv("DATABASE_URL", "file:"+path)

	ctx := context.Background()
	conn, err := db.Connect(ctx)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	for _, stmt := range canvasTestSchema {
		_, err := conn.Exec(ctx, stmt)
		require.NoError(t, err, "schema: %s", stmt)
	}
	return ctx
}

// seedCanvas inserts two projects, two canvas boards in project A, one
// whiteboard, and the elements used by the paint-order tests.
func seedCanvas(t *testing.T, ctx context.Context) {
	t.Helper()
	conn := db.Pool()
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := conn.Exec(ctx, q, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO "Project" ("id","name") VALUES (?,?)`, cvProjectA, "Project A")
	exec(`INSERT INTO "Project" ("id","name") VALUES (?,?)`, cvProjectB, "Project B")
	exec(`INSERT INTO "Whiteboard" ("id","projectId") VALUES (?,?)`, cvBoardWB, cvProjectA)

	// board2 is the more recently updated board, so it must sort first.
	exec(`INSERT INTO "CanvasBoard" ("id","name","projectId","folderId","createdAt","updatedAt")
	      VALUES (?,?,?,?,?,?)`, cvBoard1, "Board One", cvProjectA, nil, cvNow, cvNow)
	exec(`INSERT INTO "CanvasBoard" ("id","name","projectId","folderId","createdAt","updatedAt")
	      VALUES (?,?,?,?,?,?)`, cvBoard2, "Board Two", cvProjectA, nil, cvNow, cvNow+5000)

	insertEl := func(id, kind string, zIndex int, createdAt int64, text any, style, props any) {
		t.Helper()
		exec(`INSERT INTO "CanvasElement"
		        ("id","boardId","kind","positionX","positionY","width","height","rotation",
		         "zIndex","text","style","props","createdAt","updatedAt","revision")
		      VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, cvBoard1, kind, 10.5, 20.25, 120.0, 60.0, 0.0,
			zIndex, text, style, props, createdAt, createdAt, 3)
	}

	// Deliberately inserted out of paint order, and with a zIndex tie that only
	// createdAt can break.
	insertEl("e0000000-0000-4000-8000-000000000003", "ellipse", 2, cvNow+300, nil, nil, `{"kind":"ellipse"}`)
	insertEl("e0000000-0000-4000-8000-000000000002", "text", 1, cvNow+200, "hello", `{"fill":"#fff"}`, `{"kind":"text"}`)
	insertEl("e0000000-0000-4000-8000-000000000001", "rectangle", 1, cvNow+100, nil, nil, `{"kind":"rectangle"}`)
}

// ---------------------------------------------------------------------------
// GetCanvasBoardProjectID
// ---------------------------------------------------------------------------

func TestGetCanvasBoardProjectID_ResolvesOwningProject(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	projectID, err := GetCanvasBoardProjectID(ctx, cvBoard1)
	require.NoError(t, err)
	assert.Equal(t, cvProjectA, projectID)
}

func TestGetCanvasBoardProjectID_UnknownIDReturnsEmpty(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	projectID, err := GetCanvasBoardProjectID(ctx, cvUnknown)
	require.NoError(t, err, "a missing board is not an error, it is an empty project id")
	assert.Equal(t, "", projectID)
}

// The landmine this resolver exists for: the whiteboard resolver silently
// returns "" for a canvas board id, which every caller reads as NOT_FOUND.
func TestGetWhiteboardProjectID_DoesNotResolveCanvasBoards(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	viaWhiteboard, err := GetWhiteboardProjectID(ctx, cvBoard1)
	require.NoError(t, err)
	assert.Equal(t, "", viaWhiteboard, "a canvas board id must not resolve through the Whiteboard table")

	viaCanvas, err := GetCanvasBoardProjectID(ctx, cvBoardWB)
	require.NoError(t, err)
	assert.Equal(t, "", viaCanvas, "a whiteboard id must not resolve through the CanvasBoard table")
}

// ---------------------------------------------------------------------------
// ListCanvasBoards
// ---------------------------------------------------------------------------

func TestListCanvasBoards_OrdersByUpdatedAtDescWithElementCounts(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	boards, err := ListCanvasBoards(ctx, cvProjectA)
	require.NoError(t, err)
	require.Len(t, boards, 2)

	assert.Equal(t, cvBoard2, boards[0].ID, "most recently updated board sorts first")
	assert.Equal(t, "Board Two", boards[0].Name)
	assert.Equal(t, 0, boards[0].ElementCount, "a board with no elements counts zero")

	assert.Equal(t, cvBoard1, boards[1].ID)
	assert.Equal(t, 3, boards[1].ElementCount)
	assert.Equal(t, time.UnixMilli(cvNow).UTC(), boards[1].UpdatedAt.Time,
		"unix-ms timestamps scan through the existing Timestamp scanner")
}

func TestListCanvasBoards_EmptyProjectReturnsNonNilSlice(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	boards, err := ListCanvasBoards(ctx, cvProjectB)
	require.NoError(t, err)
	require.NotNil(t, boards, "an empty result must be an empty slice, not nil")
	assert.Len(t, boards, 0)
}

// ---------------------------------------------------------------------------
// FindCanvasBoardByIDWithElements
// ---------------------------------------------------------------------------

func TestFindCanvasBoardByIDWithElements_PaintOrder(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	board, err := FindCanvasBoardByIDWithElements(ctx, cvBoard1)
	require.NoError(t, err)
	require.NotNil(t, board)

	assert.Equal(t, "Board One", board.Name)
	assert.Equal(t, cvProjectA, board.ProjectID)
	assert.Nil(t, board.FolderID)

	require.Len(t, board.Elements, 3)
	// zIndex ASC, then createdAt ASC — the app's paint order.
	assert.Equal(t, []string{"rectangle", "text", "ellipse"},
		[]string{board.Elements[0].Kind, board.Elements[1].Kind, board.Elements[2].Kind})
	assert.Equal(t, 1, board.Elements[0].ZIndex)
	assert.Equal(t, 1, board.Elements[1].ZIndex)
	assert.Equal(t, 2, board.Elements[2].ZIndex)
}

func TestFindCanvasBoardByIDWithElements_ScansEveryElementField(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	board, err := FindCanvasBoardByIDWithElements(ctx, cvBoard1)
	require.NoError(t, err)
	require.NotNil(t, board)
	require.Len(t, board.Elements, 3)

	rect := board.Elements[0]
	assert.Equal(t, cvBoard1, rect.BoardID)
	assert.Equal(t, 10.5, rect.PositionX)
	assert.Equal(t, 20.25, rect.PositionY)
	assert.Equal(t, 120.0, rect.Width)
	assert.Equal(t, 60.0, rect.Height)
	assert.Equal(t, 0.0, rect.Rotation)
	assert.Equal(t, 3, rect.Revision)
	assert.Nil(t, rect.Text, "a null text column scans to nil, not an empty string")
	assert.Nil(t, rect.Style, "a null style column scans to nil JSON")
	assert.JSONEq(t, `{"kind":"rectangle"}`, string(rect.Props))
	assert.Equal(t, time.UnixMilli(cvNow+100).UTC(), rect.CreatedAt.Time)

	textEl := board.Elements[1]
	require.NotNil(t, textEl.Text)
	assert.Equal(t, "hello", *textEl.Text)
	assert.JSONEq(t, `{"fill":"#fff"}`, string(textEl.Style))
}

func TestFindCanvasBoardByIDWithElements_EmptyBoardReturnsNonNilElements(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	board, err := FindCanvasBoardByIDWithElements(ctx, cvBoard2)
	require.NoError(t, err)
	require.NotNil(t, board)
	require.NotNil(t, board.Elements, "an element-free board must return [] rather than null")
	assert.Len(t, board.Elements, 0)
}

func TestFindCanvasBoardByIDWithElements_UnknownIDReturnsNilNil(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	board, err := FindCanvasBoardByIDWithElements(ctx, cvUnknown)
	require.NoError(t, err, "a missing board is not an error, matching FindWhiteboardByIDWithDiagram")
	assert.Nil(t, board)
}

// A whiteboard id must never load as a canvas board.
func TestFindCanvasBoardByIDWithElements_WhiteboardIDIsNotFound(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	board, err := FindCanvasBoardByIDWithElements(ctx, cvBoardWB)
	require.NoError(t, err)
	assert.Nil(t, board)
}

// ---------------------------------------------------------------------------
// FindCanvasBoardByID
// ---------------------------------------------------------------------------

func TestFindCanvasBoardByID_FoundAndMissing(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	board, err := FindCanvasBoardByID(ctx, cvBoard2)
	require.NoError(t, err)
	require.NotNil(t, board)
	assert.Equal(t, "Board Two", board.Name)
	assert.Equal(t, time.UnixMilli(cvNow+5000).UTC(), board.UpdatedAt.Time)

	missing, err := FindCanvasBoardByID(ctx, cvUnknown)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

// ---------------------------------------------------------------------------
// FindCanvasElementByID (Wave 3 — update_canvas_connector's read-and-merge)
// ---------------------------------------------------------------------------

// The connector update path must read the whole stored props blob before it
// writes a replacement, so this query has to return every column, including the
// board id the caller's ownership check compares against.
func TestFindCanvasElementByID_ReturnsWholeRow(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	el, err := FindCanvasElementByID(ctx, "e0000000-0000-4000-8000-000000000002")
	require.NoError(t, err)
	require.NotNil(t, el)

	assert.Equal(t, cvBoard1, el.BoardID, "the board id is what scopes an element to a board")
	assert.Equal(t, "text", el.Kind)
	require.NotNil(t, el.Text)
	assert.Equal(t, "hello", *el.Text)
	assert.JSONEq(t, `{"kind":"text"}`, string(el.Props))
	assert.JSONEq(t, `{"fill":"#fff"}`, string(el.Style))
	assert.Equal(t, 1, el.ZIndex)
	assert.Equal(t, 3, el.Revision)
	assert.Equal(t, time.UnixMilli(cvNow+200).UTC(), el.CreatedAt.Time)
}

func TestFindCanvasElementByID_UnknownIDReturnsNilNil(t *testing.T) {
	ctx := newCanvasTestDB(t)
	seedCanvas(t, ctx)

	el, err := FindCanvasElementByID(ctx, cvUnknown)
	require.NoError(t, err, "a missing element is not an error, matching FindCanvasBoardByID")
	assert.Nil(t, el)
}

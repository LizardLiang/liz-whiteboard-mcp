package data

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// Folder read-path tests run against a real (temporary) SQLite file built from
// the app's own DDL, so a column-name drift between the two repos fails here
// rather than at runtime. The tables carry the columns these tests touch plus
// the foreign keys that make the cascade real, because foreign_keys(1) is on.
//
// Folder.parentFolderId is a self-reference the app does not constrain to a
// DAG, which is why the tree tests below include a cycle and a self-parent.
var folderTestSchema = []string{
	`CREATE TABLE "Project" ("id" TEXT NOT NULL PRIMARY KEY, "name" TEXT NOT NULL)`,
	`CREATE TABLE "Folder" (
		"id"             TEXT NOT NULL PRIMARY KEY,
		"name"           TEXT NOT NULL,
		"projectId"      TEXT NOT NULL,
		"parentFolderId" TEXT,
		"createdAt"      DATETIME NOT NULL,
		"updatedAt"      DATETIME NOT NULL,
		CONSTRAINT "Folder_projectId_fkey" FOREIGN KEY ("projectId")
			REFERENCES "Project" ("id") ON DELETE CASCADE ON UPDATE CASCADE)`,
	`CREATE TABLE "Whiteboard" (
		"id"          TEXT NOT NULL PRIMARY KEY,
		"name"        TEXT NOT NULL,
		"projectId"   TEXT NOT NULL,
		"folderId"    TEXT,
		"canvasState" JSONB,
		"textSource"  TEXT,
		"createdAt"   DATETIME NOT NULL,
		"updatedAt"   DATETIME NOT NULL)`,
	`CREATE TABLE "CanvasBoard" (
		"id"        TEXT NOT NULL PRIMARY KEY,
		"name"      TEXT NOT NULL,
		"projectId" TEXT NOT NULL,
		"folderId"  TEXT,
		"createdAt" DATETIME NOT NULL,
		"updatedAt" DATETIME NOT NULL)`,
}

const (
	fdProject  = "d0000000-0000-4000-8000-00000000000a"
	fdProjectB = "d0000000-0000-4000-8000-00000000000b"
	fdRoot     = "d1111111-1111-4111-8111-111111111111"
	fdChild    = "d2222222-2222-4222-8222-222222222222"
	fdGrand    = "d3333333-3333-4333-8333-333333333333"
	fdOrphan   = "d4444444-4444-4444-8444-444444444444"
	fdNow      = int64(1_757_000_000_000)
)

func newFolderTestDB(t *testing.T) context.Context {
	t.Helper()
	db.Close()
	path := filepath.Join(t.TempDir(), "folder_test.db")
	t.Setenv("DATABASE_URL", "file:"+path)

	ctx := context.Background()
	conn, err := db.Connect(ctx)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	for _, stmt := range folderTestSchema {
		_, err := conn.Exec(ctx, stmt)
		require.NoError(t, err, "schema: %s", stmt)
	}
	return ctx
}

func fdExec(t *testing.T, ctx context.Context, q string, args ...any) {
	t.Helper()
	_, err := db.Pool().Exec(ctx, q, args...)
	require.NoError(t, err)
}

func fdInsertFolder(t *testing.T, ctx context.Context, id, name, projectID string, parent any) {
	t.Helper()
	fdExec(t, ctx, `INSERT INTO "Folder"
		("id","name","projectId","parentFolderId","createdAt","updatedAt")
		VALUES (?,?,?,?,?,?)`, id, name, projectID, parent, fdNow, fdNow)
}

func fdInsertWhiteboard(t *testing.T, ctx context.Context, id, name, projectID string, folder any) {
	t.Helper()
	fdExec(t, ctx, `INSERT INTO "Whiteboard"
		("id","name","projectId","folderId","canvasState","textSource","createdAt","updatedAt")
		VALUES (?,?,?,?,?,?,?,?)`, id, name, projectID, folder, nil, nil, fdNow, fdNow)
}

func fdInsertCanvasBoard(t *testing.T, ctx context.Context, id, name, projectID string, folder any) {
	t.Helper()
	fdExec(t, ctx, `INSERT INTO "CanvasBoard"
		("id","name","projectId","folderId","createdAt","updatedAt")
		VALUES (?,?,?,?,?,?)`, id, name, projectID, folder, fdNow, fdNow)
}

// seedFolders builds: root > child > grand, one loose folder, and boards at the
// project root and inside `child`.
func seedFolders(t *testing.T, ctx context.Context) {
	t.Helper()
	fdExec(t, ctx, `INSERT INTO "Project" ("id","name") VALUES (?,?)`, fdProject, "Project A")
	fdExec(t, ctx, `INSERT INTO "Project" ("id","name") VALUES (?,?)`, fdProjectB, "Project B")

	fdInsertFolder(t, ctx, fdRoot, "A root", fdProject, nil)
	fdInsertFolder(t, ctx, fdChild, "B child", fdProject, fdRoot)
	fdInsertFolder(t, ctx, fdGrand, "C grand", fdProject, fdChild)

	fdInsertWhiteboard(t, ctx, "w0000000-0000-4000-8000-000000000001", "Root board", fdProject, nil)
	fdInsertWhiteboard(t, ctx, "w0000000-0000-4000-8000-000000000002", "Filed board", fdProject, fdChild)
	fdInsertCanvasBoard(t, ctx, "b0000000-0000-4000-8000-000000000001", "Canvas in child", fdProject, fdChild)
}

// ---------------------------------------------------------------------------
// FindFolderByID
// ---------------------------------------------------------------------------

func TestFindFolderByID_ReturnsTheRow(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)

	f, err := FindFolderByID(ctx, fdChild)
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, "B child", f.Name)
	assert.Equal(t, fdProject, f.ProjectID)
	require.NotNil(t, f.ParentFolderID)
	assert.Equal(t, fdRoot, *f.ParentFolderID)
}

// A missing folder is (nil, nil), not an error — the tool layer turns that into
// its own NOT_FOUND so the message names the id.
func TestFindFolderByID_UnknownIsNilWithoutError(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)

	f, err := FindFolderByID(ctx, "d9999999-9999-4999-8999-999999999999")
	require.NoError(t, err)
	assert.Nil(t, f)
}

// ---------------------------------------------------------------------------
// ListFolders
// ---------------------------------------------------------------------------

func TestListFolders_ScopesToTheProject(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)
	fdInsertFolder(t, ctx, fdOrphan, "Elsewhere", fdProjectB, nil)

	got, err := ListFolders(ctx, fdProject)
	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, f := range got {
		assert.NotEqual(t, fdOrphan, f.ID)
	}
}

// The counts are what an agent reads before deciding a delete is safe, so each
// one must be attributed to the right folder.
func TestListFolders_CountsChildrenAndBothBoardKinds(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)

	got, err := ListFolders(ctx, fdProject)
	require.NoError(t, err)

	byID := make(map[string]FolderSummary, len(got))
	for _, f := range got {
		byID[f.ID] = f
	}

	assert.Equal(t, 1, byID[fdRoot].ChildFolderCount)
	assert.Equal(t, 0, byID[fdRoot].WhiteboardCount)
	assert.Equal(t, 0, byID[fdRoot].CanvasBoardCount)

	assert.Equal(t, 1, byID[fdChild].ChildFolderCount)
	assert.Equal(t, 1, byID[fdChild].WhiteboardCount)
	assert.Equal(t, 1, byID[fdChild].CanvasBoardCount)

	assert.Equal(t, 0, byID[fdGrand].ChildFolderCount)
	assert.Equal(t, 0, byID[fdGrand].WhiteboardCount)
}

// An empty project returns an empty slice, never nil — the tool serialises it
// as [] rather than null.
func TestListFolders_EmptyProjectReturnsEmptySlice(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)

	got, err := ListFolders(ctx, fdProjectB)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// GetProjectTree
// ---------------------------------------------------------------------------

func TestGetProjectTree_NestsFoldersAndBoards(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)

	tree, err := GetProjectTree(ctx, fdProject)
	require.NoError(t, err)
	require.NotNil(t, tree)

	// One root folder, with the loose whiteboard at the project root.
	require.Len(t, tree.Folders, 1)
	assert.Equal(t, fdRoot, tree.Folders[0].ID)
	require.Len(t, tree.Boards, 1)
	assert.Equal(t, "Root board", tree.Boards[0].Name)
	assert.Equal(t, "whiteboard", tree.Boards[0].Kind)

	// The grandchild must survive the nesting — this is what a
	// link-pointers-then-copy assembly silently loses.
	child := tree.Folders[0].Folders
	require.Len(t, child, 1)
	assert.Equal(t, fdChild, child[0].ID)
	require.Len(t, child[0].Folders, 1)
	assert.Equal(t, fdGrand, child[0].Folders[0].ID)

	// Both board kinds are filed in `child` and labelled distinctly.
	require.Len(t, child[0].Boards, 2)
	kinds := map[string]string{}
	for _, b := range child[0].Boards {
		kinds[b.Kind] = b.Name
	}
	assert.Equal(t, "Filed board", kinds["whiteboard"])
	assert.Equal(t, "Canvas in child", kinds["canvasBoard"])
}

// A folder whose parent lives in another project must appear at the root rather
// than vanish from the tree.
func TestGetProjectTree_OrphanedFolderSurfacesAtRoot(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)
	fdInsertFolder(t, ctx, fdOrphan, "Z orphan", fdProject, "d0000000-0000-4000-8000-0000000000ff")

	tree, err := GetProjectTree(ctx, fdProject)
	require.NoError(t, err)

	ids := make([]string, 0, len(tree.Folders))
	for _, f := range tree.Folders {
		ids = append(ids, f.ID)
	}
	assert.Contains(t, ids, fdOrphan)
}

// A self-parenting row would nest a node inside itself. It belongs at the root.
func TestGetProjectTree_SelfParentIsTreatedAsRoot(t *testing.T) {
	ctx := newFolderTestDB(t)
	fdExec(t, ctx, `INSERT INTO "Project" ("id","name") VALUES (?,?)`, fdProject, "P")
	fdInsertFolder(t, ctx, fdRoot, "Self", fdProject, fdRoot)

	tree, err := GetProjectTree(ctx, fdProject)
	require.NoError(t, err)
	require.Len(t, tree.Folders, 1)
	assert.Equal(t, fdRoot, tree.Folders[0].ID)
	assert.Empty(t, tree.Folders[0].Folders)
}

// A parentFolderId cycle has no root at all. The walk must terminate and must
// still surface both folders rather than dropping them.
func TestGetProjectTree_CycleTerminatesAndKeepsBothFolders(t *testing.T) {
	ctx := newFolderTestDB(t)
	fdExec(t, ctx, `INSERT INTO "Project" ("id","name") VALUES (?,?)`, fdProject, "P")
	fdInsertFolder(t, ctx, fdRoot, "A", fdProject, nil)
	fdInsertFolder(t, ctx, fdChild, "B", fdProject, fdRoot)
	// Close the loop: A's parent becomes B.
	fdExec(t, ctx, `UPDATE "Folder" SET "parentFolderId" = ? WHERE "id" = ?`, fdChild, fdRoot)

	tree, err := GetProjectTree(ctx, fdProject)
	require.NoError(t, err)

	seen := map[string]bool{}
	var walk func([]TreeFolder)
	walk = func(fs []TreeFolder) {
		for _, f := range fs {
			seen[f.ID] = true
			walk(f.Folders)
		}
	}
	walk(tree.Folders)
	assert.True(t, seen[fdRoot], "folder A missing from a cyclic tree")
	assert.True(t, seen[fdChild], "folder B missing from a cyclic tree")
}

// An empty project still returns a usable shape: empty slices, not nils.
func TestGetProjectTree_EmptyProjectHasEmptySlices(t *testing.T) {
	ctx := newFolderTestDB(t)
	seedFolders(t, ctx)

	tree, err := GetProjectTree(ctx, fdProjectB)
	require.NoError(t, err)
	require.NotNil(t, tree)
	assert.Equal(t, fdProjectB, tree.ProjectID)
	require.NotNil(t, tree.Folders)
	require.NotNil(t, tree.Boards)
	assert.Empty(t, tree.Folders)
	assert.Empty(t, tree.Boards)
}

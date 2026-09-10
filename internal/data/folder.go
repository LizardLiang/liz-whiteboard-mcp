// Folder reads for the MCP tool surface.
//
// Folders had no read path in this server at all before this file: they existed
// only as a `folderId` parameter on the board tools. That made every folder
// argument unusable — an agent had no way to learn an id — and it made the
// confirmName guard on delete_folder unreachable, because the guard needs the
// folder's stored name.
//
// Both queries below are read-only, like the rest of this package. Folder
// writes go through the app's /api/mcp-lifecycle route (internal/appapi).
package data

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/db"
)

// Folder mirrors the Prisma Folder model.
type Folder struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ProjectID      string    `json:"projectId"`
	ParentFolderID *string   `json:"parentFolderId"`
	CreatedAt      Timestamp `json:"createdAt"`
	UpdatedAt      Timestamp `json:"updatedAt"`
}

// FolderSummary is a list entry for list_folders. The two counts are what an
// agent needs before deciding whether a delete is safe: a folder delete
// cascades child folders AND every whiteboard filed in them.
type FolderSummary struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	ParentFolderID   *string `json:"parentFolderId"`
	ChildFolderCount int     `json:"childFolderCount"`
	WhiteboardCount  int     `json:"whiteboardCount"`
	CanvasBoardCount int     `json:"canvasBoardCount"`
}

// FindFolderByID returns a folder by ID, or nil if it does not exist.
func FindFolderByID(ctx context.Context, id string) (*Folder, error) {
	var f Folder
	err := db.Pool().QueryRow(ctx,
		`SELECT id, name, "projectId", "parentFolderId", "createdAt", "updatedAt"
		   FROM "Folder" WHERE id = $1`, id).
		Scan(&f.ID, &f.Name, &f.ProjectID, &f.ParentFolderID, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

// FindFoldersByProjectID returns every folder in a project, parents before
// children is NOT guaranteed — order is by name so a listing reads predictably.
func FindFoldersByProjectID(ctx context.Context, projectID string) ([]Folder, error) {
	rows, err := db.Pool().Query(ctx,
		`SELECT id, name, "projectId", "parentFolderId", "createdAt", "updatedAt"
		   FROM "Folder"
		  WHERE "projectId" = $1
		  ORDER BY name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Folder, 0)
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ProjectID, &f.ParentFolderID,
			&f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListFolders returns the folders in a project with the three counts that
// describe what a delete would destroy. Uses one grouped COUNT per relation
// rather than a per-folder query, mirroring ListWhiteboards.
func ListFolders(ctx context.Context, projectID string) ([]FolderSummary, error) {
	folders, err := FindFoldersByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(folders) == 0 {
		return []FolderSummary{}, nil
	}

	ids := make([]string, len(folders))
	for i, f := range folders {
		ids[i] = f.ID
	}

	children, err := countByFolder(ctx, `"Folder"`, `"parentFolderId"`, ids)
	if err != nil {
		return nil, err
	}
	whiteboards, err := countByFolder(ctx, `"Whiteboard"`, `"folderId"`, ids)
	if err != nil {
		return nil, err
	}
	canvasBoards, err := countByFolder(ctx, `"CanvasBoard"`, `"folderId"`, ids)
	if err != nil {
		return nil, err
	}

	out := make([]FolderSummary, 0, len(folders))
	for _, f := range folders {
		out = append(out, FolderSummary{
			ID:               f.ID,
			Name:             f.Name,
			ParentFolderID:   f.ParentFolderID,
			ChildFolderCount: children[f.ID],
			WhiteboardCount:  whiteboards[f.ID],
			CanvasBoardCount: canvasBoards[f.ID],
		})
	}
	return out, nil
}

// countByFolder runs one grouped COUNT over a child table keyed by a folder
// column. Table and column names are package constants at every call site, not
// caller input.
func countByFolder(ctx context.Context, table, column string, folderIDs []string) (map[string]int, error) {
	marks, args := inPlaceholders(folderIDs)
	rows, err := db.Pool().Query(ctx,
		`SELECT `+column+`, COUNT(*) FROM `+table+
			` WHERE `+column+` IN (`+marks+`) GROUP BY `+column, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int, len(folderIDs))
	for rows.Next() {
		var folderID string
		var n int
		if err := rows.Scan(&folderID, &n); err != nil {
			return nil, err
		}
		counts[folderID] = n
	}
	return counts, rows.Err()
}

// ── Project tree ─────────────────────────────────────────────────────────────

// TreeBoard is one board of either kind in a project tree.
type TreeBoard struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is "whiteboard" (ER diagram) or "canvasBoard" (freeform). The two
	// board kinds have separate tool sets and separate tables, so a tree that
	// did not label them would be actively misleading.
	Kind string `json:"kind"`
}

// TreeFolder is one folder in a project tree, with its children nested.
type TreeFolder struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Folders []TreeFolder `json:"folders"`
	Boards  []TreeBoard  `json:"boards"`
}

// ProjectTree is the whole navigable shape of one project: folders nested, with
// the boards filed in each, plus the boards sitting at the project root.
type ProjectTree struct {
	ProjectID string       `json:"projectId"`
	Folders   []TreeFolder `json:"folders"`
	// Boards are the ones with no folderId — the project root.
	Boards []TreeBoard `json:"boards"`
}

// GetProjectTree assembles a project's folders, ER whiteboards and canvas
// boards into one nested structure.
//
// Three queries, then assembly in Go. The app builds the same shape in
// TypeScript (findAllProjectsWithTreeForUser); that code is not reachable from
// here, so this is a second implementation of the same idea rather than a
// reuse. Assembly is a post-order walk carrying a re-entry guard: a cycle in
// parentFolderId — which the schema does not prevent — would otherwise hang it,
// and orphaned folders (a parent outside this project, which a cross-project
// bug could produce) are surfaced at the root rather than dropped.
func GetProjectTree(ctx context.Context, projectID string) (*ProjectTree, error) {
	folders, err := FindFoldersByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	whiteboards, err := FindWhiteboardsByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	canvasBoards, err := FindCanvasBoardsByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Boards grouped by the folder holding them; "" is the project root.
	boardsByFolder := make(map[string][]TreeBoard)
	addBoard := func(folderID *string, b TreeBoard) {
		key := ""
		if folderID != nil {
			key = *folderID
		}
		boardsByFolder[key] = append(boardsByFolder[key], b)
	}
	for _, w := range whiteboards {
		addBoard(w.FolderID, TreeBoard{ID: w.ID, Name: w.Name, Kind: "whiteboard"})
	}
	for _, b := range canvasBoards {
		addBoard(b.FolderID, TreeBoard{ID: b.ID, Name: b.Name, Kind: "canvasBoard"})
	}

	// Index the rows, then build the value tree in one post-order walk. Linking
	// pointers first and copying afterwards is what loses grandchildren, so the
	// walk builds each node only once its children are already built.
	byID := make(map[string]Folder, len(folders))
	order := make([]string, 0, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
		order = append(order, f.ID)
	}

	children := make(map[string][]string, len(folders))
	isChild := make(map[string]bool, len(folders))
	for _, id := range order {
		f := byID[id]
		if f.ParentFolderID == nil || *f.ParentFolderID == f.ID {
			// No parent, or self-parenting — a corrupt row that would nest a
			// node inside itself. Both belong at the root.
			continue
		}
		if _, ok := byID[*f.ParentFolderID]; !ok {
			// Parent lives outside this project. Surface the folder at the root
			// rather than silently dropping it from the tree.
			continue
		}
		children[*f.ParentFolderID] = append(children[*f.ParentFolderID], id)
		isChild[id] = true
	}

	// `building` guards against a parentFolderId cycle, which the schema does
	// not prevent. A node reached twice on one path is emitted without its
	// children instead of recursing forever.
	building := make(map[string]bool, len(folders))
	var build func(id string) TreeFolder
	build = func(id string) TreeFolder {
		f := byID[id]
		node := TreeFolder{
			ID:      f.ID,
			Name:    f.Name,
			Folders: []TreeFolder{},
			Boards:  boardsOrEmpty(boardsByFolder[id]),
		}
		if building[id] {
			return node
		}
		building[id] = true
		for _, childID := range children[id] {
			node.Folders = append(node.Folders, build(childID))
		}
		building[id] = false
		return node
	}

	roots := make([]TreeFolder, 0)
	emitted := make(map[string]bool, len(folders))
	for _, id := range order {
		if isChild[id] {
			continue
		}
		roots = append(roots, build(id))
		markEmitted(id, children, emitted)
	}
	// Anything still unemitted sits in a parentFolderId cycle: every member has
	// a parent, so none of them is a root, and without this they would vanish.
	for _, id := range order {
		if !emitted[id] {
			roots = append(roots, build(id))
			markEmitted(id, children, emitted)
		}
	}

	return &ProjectTree{
		ProjectID: projectID,
		Folders:   roots,
		Boards:    boardsOrEmpty(boardsByFolder[""]),
	}, nil
}

// markEmitted records a subtree as placed, so the cycle sweep above does not
// re-emit a folder that already appeared under a root. It carries its own
// re-entry guard for the same reason build does.
func markEmitted(id string, children map[string][]string, emitted map[string]bool) {
	if emitted[id] {
		return
	}
	emitted[id] = true
	for _, childID := range children[id] {
		markEmitted(childID, children, emitted)
	}
}

func boardsOrEmpty(b []TreeBoard) []TreeBoard {
	if b == nil {
		return []TreeBoard{}
	}
	return b
}

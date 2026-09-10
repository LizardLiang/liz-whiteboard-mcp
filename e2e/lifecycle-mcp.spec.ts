// e2e/lifecycle-mcp.spec.ts
// The project / folder / ER whiteboard lifecycle gate: the cold-start chain,
// over the real wire, against a running stack, with a real user's OAuth bearer.
//
// THE ASSERTION THIS SUITE EXISTS FOR is `cold start`: an agent holding nothing
// but a bearer token reaches a populated ER whiteboard using MCP tools alone.
// Before this feature that was impossible — every write tool took a projectId or
// whiteboardId the server could not mint, so a human had to click "new project"
// in the web app first. A unit test cannot show that: the tools call the app's
// /api/mcp-lifecycle route over HTTP, so only a running app proves the route
// exists, accepts this server's collaboration credential, and writes real rows.
//
// The negative cases matter as much. A VIEWER must be refused at each entity,
// and a confirmName mismatch must leave the row alive — a delete guard that
// only looks right in a unit test is exactly the kind that ships broken.
//
// Prerequisites: the stack is up (see playwright.config.ts) and its app image
// carries /api/mcp-lifecycle. A stack built from an app commit before that route
// existed fails every test here with NOT_FOUND from the route, which is checked
// once, loudly, before any assertion runs.
import { expect, test } from '@playwright/test'
import { EDITOR_USER, VIEWER_USER } from './fixtures'
import { McpClient } from './mcp-client'
import type { McpErrorPayload } from './mcp-client'
import { mintAccessToken } from './oauth'

// ─────────────────────────────────────────────────────────────────────────────
// Types mirroring what the tools return. Narrow on purpose — asserting the
// whole row would make every unrelated schema change break this suite.
// ─────────────────────────────────────────────────────────────────────────────

interface ProjectRow {
  id: string
  name: string
  description: string | null
}

interface FolderRow {
  id: string
  name: string
  projectId: string
}

interface WhiteboardRow {
  id: string
  name: string
  projectId: string
  folderId: string | null
}

interface FolderSummary {
  id: string
  name: string
  parentFolderId: string | null
  childFolderCount: number
  whiteboardCount: number
  canvasBoardCount: number
}

interface TreeBoard {
  id: string
  name: string
  kind: 'whiteboard' | 'canvasBoard'
}

interface TreeFolder {
  id: string
  name: string
  folders: Array<TreeFolder>
  boards: Array<TreeBoard>
}

interface ProjectTree {
  projectId: string
  folders: Array<TreeFolder>
  boards: Array<TreeBoard>
}

// A run-unique suffix keeps repeated local runs from colliding on names. The
// suite creates its own projects rather than reusing a seeded one — cold start
// is the thing under test, so starting from a seeded project would assume away
// the entire feature.
const RUN = `${Date.now()}`

let editor: McpClient
let viewer: McpClient

// Projects this suite creates, torn down at the end so repeated local runs do
// not accumulate them. Deletion is best-effort: a failed teardown must not
// turn a green suite red.
const createdProjects: Array<{ id: string; name: string }> = []

async function newProject(name: string): Promise<ProjectRow> {
  const project = await editor.expectOk<ProjectRow>('create_project', { name })
  createdProjects.push({ id: project.id, name: project.name })
  return project
}

test.beforeAll(async () => {
  editor = new McpClient(await mintAccessToken(EDITOR_USER))
  await editor.connect()
  viewer = new McpClient(await mintAccessToken(VIEWER_USER))
  await viewer.connect()

  // Fail loudly and once if the stack predates the tools, rather than leaving
  // 15 assertions to report the same missing surface.
  const names = await editor.listToolNames()
  for (const required of [
    'create_project',
    'update_project',
    'delete_project',
    'create_folder',
    'update_folder',
    'delete_folder',
    'create_whiteboard',
    'update_whiteboard',
    'delete_whiteboard',
    'list_folders',
    'get_project_tree',
  ]) {
    expect(names, `the running server does not advertise ${required}`).toContain(required)
  }
})

test.afterAll(async () => {
  for (const project of createdProjects) {
    try {
      await editor.callTool('delete_project', {
        projectId: project.id,
        confirmName: project.name,
      })
    } catch {
      // Teardown is best-effort; a leftover project fails nothing.
    }
  }
})

// ─────────────────────────────────────────────────────────────────────────────
// The gate: cold start
// ─────────────────────────────────────────────────────────────────────────────

test('cold start: an agent builds a project, folder, whiteboard and table with MCP tools alone', async () => {
  // 1. A project, from nothing. No projectId existed before this call.
  const project = await newProject(`E2E cold start ${RUN}`)
  expect(project.id).toBeTruthy()
  expect(project.name).toBe(`E2E cold start ${RUN}`)

  // The new project is immediately visible to its creator, who is its owner.
  const projects = await editor.expectOk<Array<ProjectRow>>('list_projects', {})
  expect(projects.map((p) => p.id)).toContain(project.id)

  // 2. A folder in it.
  const folder = await editor.expectOk<FolderRow>('create_folder', {
    projectId: project.id,
    name: 'Schemas',
  })
  expect(folder.projectId).toBe(project.id)

  // 3. An ER whiteboard filed in that folder — the step that was impossible.
  const board = await editor.expectOk<WhiteboardRow>('create_whiteboard', {
    projectId: project.id,
    name: 'Orders',
    folderId: folder.id,
  })
  expect(board.projectId).toBe(project.id)
  expect(board.folderId).toBe(folder.id)

  // 4. A table on it, through the pre-existing schema tool. This is the join
  //    between the new lifecycle surface and the surface that already worked:
  //    create_table takes a whiteboardId no tool could previously produce.
  const table = await editor.expectOk<{ id: string; name: string }>('create_table', {
    whiteboardId: board.id,
    name: 'orders',
  })
  expect(table.name).toBe('orders')

  // 5. The whole shape reads back through the new tree tool.
  const tree = await editor.expectOk<ProjectTree>('get_project_tree', {
    projectId: project.id,
  })
  expect(tree.projectId).toBe(project.id)
  expect(tree.folders).toHaveLength(1)
  expect(tree.folders[0].id).toBe(folder.id)
  expect(tree.folders[0].boards).toHaveLength(1)
  expect(tree.folders[0].boards[0]).toMatchObject({
    id: board.id,
    name: 'Orders',
    kind: 'whiteboard',
  })

  // And the board carries the table, read over the wire, not out of SQL.
  const loaded = await editor.expectOk<{ tables: Array<{ name: string }> }>('get_board', {
    whiteboardId: board.id,
  })
  expect(loaded.tables.map((t) => t.name)).toContain('orders')
})

// ─────────────────────────────────────────────────────────────────────────────
// Reads
// ─────────────────────────────────────────────────────────────────────────────

test('list_folders reports the counts an agent needs before deleting', async () => {
  const project = await newProject(`E2E counts ${RUN}`)
  const parent = await editor.expectOk<FolderRow>('create_folder', {
    projectId: project.id,
    name: 'Parent',
  })
  await editor.expectOk('create_folder', {
    projectId: project.id,
    name: 'Child',
    parentFolderId: parent.id,
  })
  await editor.expectOk('create_whiteboard', {
    projectId: project.id,
    name: 'Filed board',
    folderId: parent.id,
  })

  const folders = await editor.expectOk<Array<FolderSummary>>('list_folders', {
    projectId: project.id,
  })
  const summary = folders.find((f) => f.id === parent.id)
  expect(summary).toBeDefined()
  expect(summary!.childFolderCount).toBe(1)
  expect(summary!.whiteboardCount).toBe(1)
})

test('get_project_tree nests a grandchild folder', async () => {
  const project = await newProject(`E2E nesting ${RUN}`)
  const a = await editor.expectOk<FolderRow>('create_folder', {
    projectId: project.id,
    name: 'A',
  })
  const b = await editor.expectOk<FolderRow>('create_folder', {
    projectId: project.id,
    name: 'B',
    parentFolderId: a.id,
  })
  await editor.expectOk('create_folder', {
    projectId: project.id,
    name: 'C',
    parentFolderId: b.id,
  })

  const tree = await editor.expectOk<ProjectTree>('get_project_tree', {
    projectId: project.id,
  })
  expect(tree.folders).toHaveLength(1)
  expect(tree.folders[0].name).toBe('A')
  expect(tree.folders[0].folders[0].name).toBe('B')
  // The grandchild is what a link-then-copy assembly silently loses.
  expect(tree.folders[0].folders[0].folders[0].name).toBe('C')
})

// ─────────────────────────────────────────────────────────────────────────────
// Updates
// ─────────────────────────────────────────────────────────────────────────────

test('update_project renames, and refuses an update naming no field', async () => {
  const project = await newProject(`E2E rename ${RUN}`)

  const renamed = await editor.expectOk<ProjectRow>('update_project', {
    projectId: project.id,
    name: `E2E renamed ${RUN}`,
  })
  expect(renamed.name).toBe(`E2E renamed ${RUN}`)
  // Keep teardown able to confirm the current name.
  createdProjects[createdProjects.length - 1].name = renamed.name

  const empty = await editor.callTool<McpErrorPayload>('update_project', {
    projectId: project.id,
  })
  expect(empty.isError).toBe(true)
})

test('update_whiteboard moves a board into a folder', async () => {
  const project = await newProject(`E2E refile ${RUN}`)
  const folder = await editor.expectOk<FolderRow>('create_folder', {
    projectId: project.id,
    name: 'Target',
  })
  const board = await editor.expectOk<WhiteboardRow>('create_whiteboard', {
    projectId: project.id,
    name: 'Loose board',
  })
  expect(board.folderId).toBeNull()

  const moved = await editor.expectOk<WhiteboardRow>('update_whiteboard', {
    whiteboardId: board.id,
    folderId: folder.id,
  })
  expect(moved.folderId).toBe(folder.id)
})

// ─────────────────────────────────────────────────────────────────────────────
// Delete guards — a mismatch must leave the row alive
// ─────────────────────────────────────────────────────────────────────────────

test('delete_whiteboard refuses a mismatched confirmName and deletes nothing', async () => {
  const project = await newProject(`E2E wb guard ${RUN}`)
  const board = await editor.expectOk<WhiteboardRow>('create_whiteboard', {
    projectId: project.id,
    name: 'Precious',
  })

  const refused = await editor.callTool<McpErrorPayload>('delete_whiteboard', {
    whiteboardId: board.id,
    confirmName: 'Wrong name',
  })
  expect(refused.isError).toBe(true)
  expect(refused.data.field).toBe('confirmName')
  // The guard must not disclose the stored name, or a blind retry defeats it.
  expect(JSON.stringify(refused.data)).not.toContain('Precious')

  // Still there, read over the wire.
  const boards = await editor.expectOk<Array<{ id: string }>>('list_whiteboards', {
    projectId: project.id,
  })
  expect(boards.map((b) => b.id)).toContain(board.id)

  // The exact name deletes it.
  await editor.expectOk('delete_whiteboard', {
    whiteboardId: board.id,
    confirmName: 'Precious',
  })
  const after = await editor.expectOk<Array<{ id: string }>>('list_whiteboards', {
    projectId: project.id,
  })
  expect(after.map((b) => b.id)).not.toContain(board.id)
})

test('delete_folder refuses a mismatch, reports the cascade, then cascades on match', async () => {
  const project = await newProject(`E2E folder guard ${RUN}`)
  const folder = await editor.expectOk<FolderRow>('create_folder', {
    projectId: project.id,
    name: 'Doomed',
  })
  const board = await editor.expectOk<WhiteboardRow>('create_whiteboard', {
    projectId: project.id,
    name: 'Inside doomed',
    folderId: folder.id,
  })

  const refused = await editor.callTool<McpErrorPayload>('delete_folder', {
    folderId: folder.id,
    confirmName: 'Wrong',
  })
  expect(refused.isError).toBe(true)
  // The cascade warning is the reason this guard is louder than the others: a
  // folder delete destroys boards the caller never named.
  expect(refused.data.message).toContain('1 board(s)')

  // The board inside survived the refusal.
  let boards = await editor.expectOk<Array<{ id: string }>>('list_whiteboards', {
    projectId: project.id,
  })
  expect(boards.map((b) => b.id)).toContain(board.id)

  // On a match the cascade really does take the board with it.
  await editor.expectOk('delete_folder', {
    folderId: folder.id,
    confirmName: 'Doomed',
  })
  boards = await editor.expectOk<Array<{ id: string }>>('list_whiteboards', {
    projectId: project.id,
  })
  expect(boards.map((b) => b.id)).not.toContain(board.id)
})

test('delete_project refuses a mismatched confirmName and deletes nothing', async () => {
  const project = await newProject(`E2E project guard ${RUN}`)

  const refused = await editor.callTool<McpErrorPayload>('delete_project', {
    projectId: project.id,
    confirmName: 'Definitely not the name',
  })
  expect(refused.isError).toBe(true)
  expect(refused.data.field).toBe('confirmName')

  const projects = await editor.expectOk<Array<ProjectRow>>('list_projects', {})
  expect(projects.map((p) => p.id)).toContain(project.id)
})

// ─────────────────────────────────────────────────────────────────────────────
// Authorization — a VIEWER is a real member with a real role, so a refusal here
// proves the role gate, not project scoping.
// ─────────────────────────────────────────────────────────────────────────────

test('a VIEWER cannot create a folder or a whiteboard in a project they can read', async () => {
  // The VIEWER's own project is not the subject: the seeded canvas project is
  // where they hold VIEWER, and that is where the gate must bite.
  const seen = await viewer.expectOk<Array<ProjectRow>>('list_projects', {})
  expect(seen.length, 'the viewer must be a member of at least one project').toBeGreaterThan(0)
  const projectId = seen[0].id

  const folder = await viewer.callTool<McpErrorPayload>('create_folder', {
    projectId,
    name: 'Should not exist',
  })
  expect(folder.isError).toBe(true)
  expect(folder.data.code).toBe('FORBIDDEN')

  const board = await viewer.callTool<McpErrorPayload>('create_whiteboard', {
    projectId,
    name: 'Should not exist',
  })
  expect(board.isError).toBe(true)
  expect(board.data.code).toBe('FORBIDDEN')
})

test('a non-owner cannot delete a project they can read', async () => {
  const seen = await viewer.expectOk<Array<ProjectRow>>('list_projects', {})
  expect(seen.length).toBeGreaterThan(0)

  const refused = await viewer.callTool<McpErrorPayload>('delete_project', {
    projectId: seen[0].id,
    confirmName: seen[0].name,
  })
  expect(refused.isError).toBe(true)
  expect(refused.data.code).toBe('FORBIDDEN')

  // Still readable afterwards — the refusal destroyed nothing.
  const after = await viewer.expectOk<Array<ProjectRow>>('list_projects', {})
  expect(after.map((p) => p.id)).toContain(seen[0].id)
})

test('a stranger cannot read a project tree they hold no role on', async () => {
  const project = await newProject(`E2E scoping ${RUN}`)

  const refused = await viewer.callTool<McpErrorPayload>('get_project_tree', {
    projectId: project.id,
  })
  expect(refused.isError).toBe(true)
  // Project scoping reports NOT_FOUND or FORBIDDEN depending on the tier; both
  // are correct, and neither may leak a folder or board name.
  expect(['NOT_FOUND', 'FORBIDDEN']).toContain(refused.data.code)
})

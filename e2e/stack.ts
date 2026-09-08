// e2e/stack.ts
// Helpers that reach into the running compose stack.
//
// WHY docker exec AND NOT A LOCAL SCRIPT: the app container owns its OWN SQLite
// database on the `appdata` volume (DATABASE_URL=file:/app/data/app.db),
// isolated from anything on the host. Seeding from the host would write a file
// nothing in the stack reads. The seeds also need `bun:sqlite`, which exists in
// the app image and not in Playwright's Node runner.
//
// The seed scripts belong to ../liz-whiteboard and ship inside its image. This
// repo does not copy them: a copy would drift from the fixture ids the app's
// own suite maintains, and this suite asserts against those same ids.
import { execFileSync } from 'node:child_process'
import { COMPOSE_PROJECT } from './fixtures'

const APP_CONTAINER = `${COMPOSE_PROJECT}-app-1`

function exec(args: Array<string>): string {
  return execFileSync('docker', args, {
    encoding: 'utf8',
    maxBuffer: 32 * 1024 * 1024,
  })
}

/** Run one of the app image's seed scripts inside the app container. */
export function runAppSeed(script: string): string {
  return exec(['exec', APP_CONTAINER, 'bun', 'run', `e2e/${script}`])
}

/** Run a Bun snippet inside the app container (has bun:sqlite and the volume). */
export function runInAppContainer(source: string): string {
  return exec(['exec', APP_CONTAINER, 'bun', '-e', source])
}

/**
 * Full seed: users, projects and every canvas board. Runs ONCE, in global
 * setup, BEFORE any login.
 *
 * Order matters: seed-canvas.ts reuses the users seed.ts and seed-stress.ts
 * create, and documents that both must run first.
 *
 * Never call this mid-suite. seed.ts DELETEs the user row and recreates it,
 * which cascades away that user's sessions — every logged-in page bounces to
 * /login on its next navigation, and the live-render test fails looking like a
 * broken canvas. Use `resetCanvasBoards` between tests instead.
 */
export function seedCanvasFixtures(): void {
  runAppSeed('seed.ts')
  runAppSeed('seed-stress.ts')
  runAppSeed('seed-canvas.ts')
}

/**
 * Reset only the canvas boards, leaving users and their sessions alone.
 *
 * seed-canvas.ts wipes and rebuilds IDS.canvasProject and its boards but only
 * REFERENCES the two users, so a saved storageState stays valid across it.
 * This is the reset every test in this suite wants.
 */
export function resetCanvasBoards(): void {
  runAppSeed('seed-canvas.ts')
}

/**
 * Read one canvas element's stored props straight from the database.
 *
 * Used only where a DB read is the point (proving a refused write left no row).
 * Behavioural assertions read through `get_canvas_board` over the wire instead —
 * asserting on SQL is what this whole gate exists to avoid.
 */
export function readElementProps(elementId: string): Record<string, unknown> {
  const output = runInAppContainer(`
    const { Database } = require('bun:sqlite')
    const db = new Database('/app/data/app.db')
    db.exec('PRAGMA busy_timeout = 5000')
    const row = db.query('SELECT props FROM "CanvasElement" WHERE id = ?').get(${JSON.stringify(elementId)})
    console.log(row ? row.props : 'null')
  `)
  return JSON.parse(output.trim())
}

/**
 * Give the seeded connector the endpoint state a HUMAN produces in the app, so
 * the merge test has something real to preserve.
 *
 * This writes PRIOR STATE, it does not stand in for the code under test. The
 * app writes `sourceAttach` / `targetAttach` when a user drags a connector
 * endpoint onto a shape (src/lib/canvas-element-adapter.ts:231) and `curvature`
 * when a user bows the line; `sourceAnchor` / `targetAnchor` are the legacy
 * form the adapter still reads (canvas-element-adapter.ts:104). None of them
 * are fields `update_canvas_connector` accepts, which is exactly why they are
 * the trap: a typed round trip through the tool would silently delete every one
 * of them. The tool call and every assertion below still run over the wire.
 */
export function seedConnectorEndpointState(
  elementId: string,
  extra: Record<string, unknown>,
): void {
  runInAppContainer(`
    const { Database } = require('bun:sqlite')
    const db = new Database('/app/data/app.db')
    db.exec('PRAGMA busy_timeout = 5000')
    const id = ${JSON.stringify(elementId)}
    const row = db.query('SELECT props FROM "CanvasElement" WHERE id = ?').get(id)
    if (!row) throw new Error('connector ' + id + ' not found — seed first')
    const props = { ...JSON.parse(row.props), ...${JSON.stringify(extra)} }
    db.query('UPDATE "CanvasElement" SET props = ? WHERE id = ?').run(JSON.stringify(props), id)
  `)
}

/**
 * Insert a canvas element with a DIRECT SQL WRITE, bypassing Socket.IO.
 *
 * This is the NEGATIVE CONTROL for the live-render assertion, and it is the
 * only place in this suite that writes application data behind the app's back.
 * A direct-SQL implementation of the MCP write tools is the specific failure
 * this whole gate exists to catch: the row lands, every database assertion
 * passes, and no connected client ever repaints. Proving the live assertion
 * FAILS for such a write is what proves it would have caught one.
 */
export function insertElementBypassingSocket(element: {
  id: string
  boardId: string
  kind: string
  positionX: number
  positionY: number
  width: number
  height: number
  text: string
}): void {
  runInAppContainer(`
    const { Database } = require('bun:sqlite')
    const db = new Database('/app/data/app.db')
    db.exec('PRAGMA busy_timeout = 5000')
    const e = ${JSON.stringify(element)}
    const now = Date.now()
    db.query(\`INSERT INTO "CanvasElement"
        ("id","boardId","kind","positionX","positionY","width","height",
         "rotation","zIndex","text","style","props","createdAt","updatedAt","revision")
        VALUES (?,?,?,?,?,?,?,0,900,?,?,?,?,?,1)\`).run(
      e.id, e.boardId, e.kind, e.positionX, e.positionY, e.width, e.height, e.text,
      JSON.stringify({ fill: 'rgba(59, 130, 246, 0.10)', stroke: '#3b82f6', strokeWidth: 2,
                       fontSize: 16, color: '#0f172a', cornerRadius: 8,
                       textAlign: 'left', verticalAlign: 'top' }),
      JSON.stringify({ kind: e.kind }), now, now)
  `)
}

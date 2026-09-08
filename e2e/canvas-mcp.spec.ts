// e2e/canvas-mcp.spec.ts
// The canvas MCP hard gate: every canvas tool, over the real wire, against a
// running stack, with a real user's OAuth bearer.
//
// THE ASSERTION THIS SUITE EXISTS FOR is `live broadcast`: a browser sitting on
// a canvas board must render an element an agent created over MCP WITHOUT
// reloading. A database-row assertion alone would pass a direct-SQL
// implementation, and a direct-SQL implementation is the exact failure this
// feature was designed to avoid — every write in this server rides Socket.IO so
// co-viewing clients re-render. Nothing else here substitutes for it.
//
// Canvas content paints to one <canvas>; there are no DOM nodes to query. The
// app publishes its live scene at `window.__canvasEngine` for e2e use, gated
// out of user builds — docker-compose.e2e.yml builds the app with
// VITE_E2E_HOOKS=1 and explains why. Where the scene alone could in principle
// be a store that updated without repainting, the test also diffs the painted
// canvas pixels.
//
// Prerequisites: the stack is up (see playwright.config.ts) and the app image
// carries the e2e hook. Both are checked, loudly, before any assertion runs.
import { expect, test } from '@playwright/test'
import type { Browser, Page } from '@playwright/test'
import { EDITOR_USER, IDS, ORIGIN, VIEWER_USER } from './fixtures'
import { McpClient } from './mcp-client'
import type { McpErrorPayload } from './mcp-client'
import { mintAccessToken } from './oauth'
import {
  insertElementBypassingSocket,
  readElementProps,
  resetCanvasBoards,
  seedConnectorEndpointState,
} from './stack'
import { EDITOR_STORAGE_STATE } from './global-setup'

// ─────────────────────────────────────────────────────────────────────────────
// Types mirroring what the tools return. Narrow on purpose — asserting the
// whole row would make every unrelated schema change break this suite.
// ─────────────────────────────────────────────────────────────────────────────

interface CanvasBoardRow {
  id: string
  name: string
  projectId: string
  folderId: string | null
}

interface CanvasElementRow {
  id: string
  boardId: string
  kind: string
  positionX: number
  positionY: number
  width: number
  height: number
  zIndex: number
  text: string | null
  props: Record<string, unknown>
}

interface CanvasBoardWithElements extends CanvasBoardRow {
  elements: Array<CanvasElementRow>
}

interface CanvasBoardSummaryRow {
  id: string
  name: string
  elementCount: number
}

/** The subset of `window.__canvasEngine` this suite reads. */
interface EngineScene {
  boardId: string
  elements: Array<{
    id: string
    kind: string
    x: number
    y: number
    width: number
    height: number
    text?: string | null
  }>
}

declare global {
  interface Window {
    __canvasEngine?: unknown
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared clients. Minting a bearer drives a full browser login, so it happens
// once per role rather than once per test.
// ─────────────────────────────────────────────────────────────────────────────

let editor: McpClient
let viewer: McpClient

test.beforeAll(async () => {
  editor = new McpClient(await mintAccessToken(EDITOR_USER))
  await editor.connect()
  viewer = new McpClient(await mintAccessToken(VIEWER_USER))
  await viewer.connect()
})

/** Open the canvas board as a logged-in editor and wait for the live scene. */
async function openBoardAsEditor(
  browser: Browser,
  boardId: string,
): Promise<{ page: Page; close: () => Promise<void> }> {
  const context = await browser.newContext({
    storageState: EDITOR_STORAGE_STATE,
    baseURL: ORIGIN,
    viewport: { width: 1600, height: 1000 },
  })
  const page = await context.newPage()
  await page.goto(`/canvas/${boardId}`)
  await page.waitForSelector('canvas')
  await page
    .waitForFunction(() => window.__canvasEngine !== undefined, null, {
      timeout: 20_000,
    })
    .catch(() => {
      throw new Error(
        'window.__canvasEngine was never published. The app image was built ' +
          'without VITE_E2E_HOOKS=1 — rebuild with docker-compose.e2e.yml.',
      )
    })
  return { page, close: () => context.close() }
}

async function scene(page: Page): Promise<EngineScene> {
  return page.evaluate(() => window.__canvasEngine as unknown) as Promise<EngineScene>
}

// ─────────────────────────────────────────────────────────────────────────────
// Read path
// ─────────────────────────────────────────────────────────────────────────────

test.describe('canvas read tools over the wire', () => {
  test.beforeAll(() => resetCanvasBoards())

  test('the server advertises the full canvas tool surface', async () => {
    const names = await editor.listToolNames()
    expect(names).toEqual(
      expect.arrayContaining([
        'list_canvas_boards',
        'get_canvas_board',
        'get_canvas_summary',
        'create_canvas_element',
        'update_canvas_element',
        'delete_canvas_element',
        'create_canvas_connector',
        'update_canvas_connector',
        'create_canvas_board',
        'update_canvas_board',
        'delete_canvas_board',
      ]),
    )
  })

  test('list_canvas_boards returns the project’s boards with element counts', async () => {
    const boards = await editor.expectOk<Array<CanvasBoardSummaryRow>>(
      'list_canvas_boards',
      { projectId: IDS.canvasProject },
    )
    const board = boards.find((b) => b.id === IDS.canvasBoard)
    expect(board, 'the seeded board must be listed').toBeDefined()
    expect(board!.name).toBe('E2E Canvas')
    expect(board!.elementCount).toBe(2)
  })

  test('get_canvas_board returns elements in paint order with their props', async () => {
    const board = await editor.expectOk<CanvasBoardWithElements>(
      'get_canvas_board',
      { canvasBoardId: IDS.canvasBoard },
    )
    expect(board.projectId).toBe(IDS.canvasProject)
    expect(board.elements.map((e) => e.id)).toEqual([
      IDS.canvasRect,
      IDS.canvasText,
    ])
    expect(board.elements[0].kind).toBe('rectangle')
    expect(board.elements[1].text).toBe('seeded label')
    // zIndex ascending is what "paint order" means; the read must not resort it.
    expect(board.elements[0].zIndex).toBeLessThanOrEqual(board.elements[1].zIndex)
  })

  test('get_canvas_summary renders a compact board digest', async () => {
    const summary = await editor.expectOk<string>('get_canvas_summary', {
      canvasBoardId: IDS.canvasBoard,
    })
    expect(summary).toContain('CANVAS E2E Canvas')
    expect(summary).toContain('ELEMENTS 2 (rectangle=1, text=1)')
    expect(summary).toContain('seeded label')
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// THE CENTRAL ASSERTION
// ─────────────────────────────────────────────────────────────────────────────

test.describe('a connected browser follows MCP element writes live', () => {
  test.beforeAll(() => resetCanvasBoards())

  test('create, update and delete all reach the open board without a reload', async ({
    browser,
  }) => {
    const { page, close } = await openBoardAsEditor(browser, IDS.canvasBoard)
    try {
      const before = await scene(page)
      expect(before.boardId).toBe(IDS.canvasBoard)
      expect(before.elements).toHaveLength(2)

      // The <canvas> as painted right now. Compared against later so the test
      // proves a REPAINT, not merely a store that changed.
      const canvas = page.locator('canvas').first()
      const paintedBefore = await canvas.screenshot()

      // ── create ────────────────────────────────────────────────────────────
      const created = await editor.expectOk<CanvasElementRow>(
        'create_canvas_element',
        {
          canvasBoardId: IDS.canvasBoard,
          kind: 'rectangle',
          positionX: 320,
          positionY: 320,
          width: 160,
          height: 100,
          text: 'made-by-agent',
        },
      )
      expect(created.boardId).toBe(IDS.canvasBoard)

      // No reload anywhere in this test. If this wait times out, the write did
      // not ride the socket — which is precisely the defect this gate hunts.
      await page.waitForFunction(
        (id) =>
          ((window.__canvasEngine as { elements: Array<{ id: string }> })
            ?.elements ?? []).some((e) => e.id === id),
        created.id,
        { timeout: 20_000 },
      )

      const afterCreate = await scene(page)
      const live = afterCreate.elements.find((e) => e.id === created.id)!
      expect(live.kind).toBe('rectangle')
      expect(live.x).toBe(320)
      expect(live.y).toBe(320)
      expect(live.width).toBe(160)
      expect(live.height).toBe(100)
      expect(live.text).toBe('made-by-agent')

      const paintedAfterCreate = await canvas.screenshot()
      expect(
        Buffer.compare(paintedBefore, paintedAfterCreate),
        'the canvas must actually repaint, not just update a store',
      ).not.toBe(0)

      // ── update ────────────────────────────────────────────────────────────
      await editor.expectOk('update_canvas_element', {
        canvasBoardId: IDS.canvasBoard,
        elementId: created.id,
        positionX: 900,
        positionY: 200,
        text: 'moved-by-agent',
      })
      await page.waitForFunction(
        (id) => {
          const el = (
            window.__canvasEngine as {
              elements: Array<{ id: string; x: number; text?: string | null }>
            }
          )?.elements?.find((e) => e.id === id)
          return el?.x === 900 && el?.text === 'moved-by-agent'
        },
        created.id,
        { timeout: 20_000 },
      )

      // ── delete ────────────────────────────────────────────────────────────
      await editor.expectOk('delete_canvas_element', {
        canvasBoardId: IDS.canvasBoard,
        elementId: created.id,
      })
      await page.waitForFunction(
        (id) =>
          !(
            (window.__canvasEngine as { elements: Array<{ id: string }> })
              ?.elements ?? []
          ).some((e) => e.id === id),
        created.id,
        { timeout: 20_000 },
      )

      const afterDelete = await scene(page)
      expect(afterDelete.elements).toHaveLength(2)
    } finally {
      await close()
    }
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// NEGATIVE CONTROL for the test above
// ─────────────────────────────────────────────────────────────────────────────

test.describe('the live assertion has teeth', () => {
  test.beforeAll(() => resetCanvasBoards())

  test('a direct-SQL write does NOT reach the open board, but survives a reload', async ({
    browser,
  }) => {
    // Without this test, "the browser followed the MCP write" is unfalsifiable
    // from the suite's own evidence: a test that can never fail proves nothing.
    // Here the row is inserted straight into SQLite, bypassing Socket.IO
    // entirely — exactly what a direct-SQL implementation of
    // create_canvas_element would do. The connected page must NOT show it.
    const { page, close } = await openBoardAsEditor(browser, IDS.canvasBoard)
    try {
      const smuggled = {
        id: '70000000-0000-4000-8000-0000000000ff',
        boardId: IDS.canvasBoard,
        kind: 'rectangle',
        positionX: 640,
        positionY: 640,
        width: 140,
        height: 90,
        text: 'smuggled-past-the-socket',
      }
      insertElementBypassingSocket(smuggled)

      // Generous on purpose: the point is that no amount of waiting helps.
      // The MCP path above needed well under a second.
      await page.waitForTimeout(5_000)

      const live = await scene(page)
      expect(
        live.elements.map((e) => e.id),
        'a write that skipped the socket must never reach a connected client',
      ).not.toContain(smuggled.id)

      // ...and the row really was written, so the assertion above failed for
      // the RIGHT reason (no broadcast) rather than because the insert no-oped.
      await page.reload()
      await page.waitForSelector('canvas')
      await page.waitForFunction(
        (id) =>
          ((window.__canvasEngine as { elements: Array<{ id: string }> })
            ?.elements ?? []).some((e) => e.id === id),
        smuggled.id,
        { timeout: 20_000 },
      )
    } finally {
      await close()
    }
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// Element writes, shape and text, asserted through the read path
// ─────────────────────────────────────────────────────────────────────────────

test.describe('element writes', () => {
  test.beforeAll(() => resetCanvasBoards())

  for (const kind of ['rectangle', 'text'] as const) {
    test(`create, update and delete a ${kind}`, async () => {
      const created = await editor.expectOk<CanvasElementRow>(
        'create_canvas_element',
        {
          canvasBoardId: IDS.canvasBoard,
          kind,
          positionX: 1200,
          positionY: 100,
          width: 200,
          height: 80,
          text: `${kind}-from-mcp`,
        },
      )
      expect(created.kind).toBe(kind)
      expect(created.props).toMatchObject({ kind })
      // The server owns zIndex; the tool must not send one, and the row must
      // still land on top of the two seeded elements.
      expect(created.zIndex).toBeGreaterThan(1)
      expect(created.text).toBe(`${kind}-from-mcp`)

      const updated = await editor.expectOk<CanvasElementRow>(
        'update_canvas_element',
        {
          canvasBoardId: IDS.canvasBoard,
          elementId: created.id,
          positionX: 1400,
          width: 260,
          text: `${kind}-edited`,
        },
      )
      expect(updated.positionX).toBe(1400)
      expect(updated.width).toBe(260)
      expect(updated.text).toBe(`${kind}-edited`)

      await editor.expectOk('delete_canvas_element', {
        canvasBoardId: IDS.canvasBoard,
        elementId: created.id,
      })

      const board = await editor.expectOk<CanvasBoardWithElements>(
        'get_canvas_board',
        { canvasBoardId: IDS.canvasBoard },
      )
      expect(board.elements.map((e) => e.id)).not.toContain(created.id)
    })
  }
})

// ─────────────────────────────────────────────────────────────────────────────
// Connectors — including the merge trap
// ─────────────────────────────────────────────────────────────────────────────

test.describe('connectors', () => {
  test.beforeEach(() => resetCanvasBoards())

  test('create_canvas_connector attaches both ends and survives the call', async () => {
    // REGRESSION GUARD: this exact call used to panic the Go server with a nil
    // dereference and kill the process, because createCanvasConnector validates
    // the request BEFORE loading the source element and the placeholder helper
    // assumed a free source point. An attached source is the ORDINARY case, so
    // the whole tool was unusable. `expectOk` throwing on an empty 502 body is
    // what that failure looks like from here.
    const connector = await editor.expectOk<CanvasElementRow>(
      'create_canvas_connector',
      {
        canvasBoardId: IDS.canvasConnectorBoard,
        sourceElementId: IDS.canvasConnSource,
        targetElementId: IDS.canvasConnTarget,
        routing: 'elbow',
        curvature: 0.5,
      },
    )
    expect(connector.kind).toBe('connector')
    // The placeholder is a 1x1 box at the source element's centre — never 0x0,
    // which the server's own schema rejects.
    expect(connector.width).toBe(1)
    expect(connector.height).toBe(1)
    expect(connector.props).toMatchObject({
      kind: 'connector',
      routing: 'elbow',
      curvature: 0.5,
      sourceElementId: IDS.canvasConnSource,
      targetElementId: IDS.canvasConnTarget,
    })

    // The server is still alive after the call. A panic would have taken the
    // process down and this second call would fail outright.
    const stillUp = await editor.expectOk<CanvasBoardWithElements>(
      'get_canvas_board',
      { canvasBoardId: IDS.canvasConnectorBoard },
    )
    expect(stillUp.elements.map((e) => e.id)).toContain(connector.id)
  })

  test('update_canvas_connector preserves endpoint state it does not own', async () => {
    // The trap: sourceAttach / targetAttach / sourceAnchor / targetAnchor /
    // curvature are written by the APP when a human drags or bows a connector.
    // `update_canvas_connector` accepts none of them as arguments. A typed
    // round trip through the tool would delete every one, silently
    // un-anchoring and un-bowing a connector the caller only meant to re-route.
    const endpointState = {
      sourceAttach: { x: 1, y: 0.5 },
      targetAttach: { x: 0, y: 0.5 },
      curvature: 0.75,
    }
    seedConnectorEndpointState(IDS.canvasConnector, endpointState)

    const before = readElementProps(IDS.canvasConnector)
    expect(before).toMatchObject({
      ...endpointState,
      sourceAnchor: 'right', // seeded by the app's own seed-canvas.ts
      targetAnchor: 'left',
      routing: 'straight',
    })

    // Change ONLY the routing.
    const updated = await editor.expectOk<CanvasElementRow>(
      'update_canvas_connector',
      {
        canvasBoardId: IDS.canvasConnectorBoard,
        elementId: IDS.canvasConnector,
        routing: 'elbow',
      },
    )
    expect(updated.props.routing).toBe('elbow')

    // Read the stored row back over the wire, not out of the tool's own reply:
    // the reply is what the server was SENT, the row is what it KEPT.
    const board = await editor.expectOk<CanvasBoardWithElements>(
      'get_canvas_board',
      { canvasBoardId: IDS.canvasConnectorBoard },
    )
    const stored = board.elements.find((e) => e.id === IDS.canvasConnector)!
    expect(stored.props).toMatchObject({
      kind: 'connector',
      routing: 'elbow',
      sourceElementId: IDS.canvasConnSource,
      targetElementId: IDS.canvasConnTarget,
      sourceAttach: { x: 1, y: 0.5 },
      targetAttach: { x: 0, y: 0.5 },
      sourceAnchor: 'right',
      targetAnchor: 'left',
      curvature: 0.75,
    })
  })

  test('update_canvas_connector rejects an end that is both attached and free', async () => {
    const result = await editor.callTool<McpErrorPayload>(
      'update_canvas_connector',
      {
        canvasBoardId: IDS.canvasConnectorBoard,
        elementId: IDS.canvasConnector,
        sourceElementId: IDS.canvasConnSource,
        sourcePoint: { x: 10, y: 10 },
      },
    )
    expect(result.isError).toBe(true)
    expect(result.data.code).toBe('VALIDATION_ERROR')
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// Wave 5 — board lifecycle through the app's JWT-authenticated HTTP route
// ─────────────────────────────────────────────────────────────────────────────

test.describe('canvas board lifecycle', () => {
  test.beforeAll(() => resetCanvasBoards())

  test('an agent creates a board, works on it, and deletes it by name', async () => {
    const name = `Agent Board ${Date.now()}`

    const board = await editor.expectOk<CanvasBoardRow>('create_canvas_board', {
      projectId: IDS.canvasProject,
      name,
    })
    expect(board.projectId).toBe(IDS.canvasProject)
    expect(board.name).toBe(name)

    // It must appear in the board list the app itself reads.
    const listed = await editor.expectOk<Array<CanvasBoardSummaryRow>>(
      'list_canvas_boards',
      { projectId: IDS.canvasProject },
    )
    expect(listed.map((b) => b.id)).toContain(board.id)

    // An agent must be able to go from an empty project to a finished diagram
    // with no human clicking "new canvas board" — so element writes have to
    // work on the board it just made.
    const element = await editor.expectOk<CanvasElementRow>(
      'create_canvas_element',
      {
        canvasBoardId: board.id,
        kind: 'ellipse',
        positionX: 40,
        positionY: 40,
        width: 120,
        height: 120,
        text: 'on the agent board',
      },
    )

    const renamed = await editor.expectOk<CanvasBoardRow>('update_canvas_board', {
      canvasBoardId: board.id,
      name: `${name} renamed`,
    })
    expect(renamed.name).toBe(`${name} renamed`)

    // ── the guard: a WRONG confirmName must delete nothing ─────────────────
    // This exists because a cascade delete here takes every element and share
    // link with it, and this project has lost production data to a cascade
    // once already.
    const refused = await editor.callTool<McpErrorPayload>(
      'delete_canvas_board',
      { canvasBoardId: board.id, confirmName: 'not the right name' },
    )
    expect(refused.isError).toBe(true)
    expect(refused.data.code).toBe('VALIDATION_ERROR')
    expect(refused.data.field).toBe('confirmName')

    const survived = await editor.expectOk<CanvasBoardWithElements>(
      'get_canvas_board',
      { canvasBoardId: board.id },
    )
    expect(survived.name).toBe(`${name} renamed`)
    expect(survived.elements.map((e) => e.id)).toContain(element.id)

    // ── the correct name deletes the board and cascades its elements ───────
    await editor.expectOk('delete_canvas_board', {
      canvasBoardId: board.id,
      confirmName: `${name} renamed`,
    })

    const afterDelete = await editor.callTool<McpErrorPayload>(
      'get_canvas_board',
      { canvasBoardId: board.id },
    )
    expect(afterDelete.isError).toBe(true)
    expect(afterDelete.data.code).toBe('NOT_FOUND')

    const finalList = await editor.expectOk<Array<CanvasBoardSummaryRow>>(
      'list_canvas_boards',
      { projectId: IDS.canvasProject },
    )
    expect(finalList.map((b) => b.id)).not.toContain(board.id)
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// Authorization — the EDITOR+ gate has to bite
// ─────────────────────────────────────────────────────────────────────────────

test.describe('the EDITOR+ gate refuses a VIEWER', () => {
  test.beforeAll(() => resetCanvasBoards())

  test('a VIEWER may read the board', async () => {
    const summary = await viewer.expectOk<string>('get_canvas_summary', {
      canvasBoardId: IDS.canvasBoard,
    })
    expect(summary).toContain('CANVAS E2E Canvas')
  })

  const refusedWrites: Array<[string, Record<string, unknown>]> = [
    [
      'create_canvas_element',
      {
        canvasBoardId: IDS.canvasBoard,
        kind: 'rectangle',
        positionX: 0,
        positionY: 0,
        width: 50,
        height: 50,
      },
    ],
    [
      'update_canvas_element',
      {
        canvasBoardId: IDS.canvasBoard,
        elementId: IDS.canvasRect,
        positionX: 999,
      },
    ],
    [
      'delete_canvas_element',
      { canvasBoardId: IDS.canvasBoard, elementId: IDS.canvasRect },
    ],
    [
      'create_canvas_connector',
      {
        canvasBoardId: IDS.canvasConnectorBoard,
        sourceElementId: IDS.canvasConnSource,
        targetElementId: IDS.canvasConnTarget,
        routing: 'straight',
      },
    ],
    [
      'update_canvas_connector',
      {
        canvasBoardId: IDS.canvasConnectorBoard,
        elementId: IDS.canvasConnector,
        routing: 'elbow',
      },
    ],
    ['create_canvas_board', { projectId: IDS.canvasProject, name: 'nope' }],
    [
      'update_canvas_board',
      { canvasBoardId: IDS.canvasBoard, name: 'renamed by a viewer' },
    ],
    [
      'delete_canvas_board',
      { canvasBoardId: IDS.canvasBoard, confirmName: 'E2E Canvas' },
    ],
  ]

  for (const [tool, args] of refusedWrites) {
    test(`${tool} is FORBIDDEN for a VIEWER`, async () => {
      const result = await viewer.callTool<McpErrorPayload>(tool, args)
      expect(result.isError, `${tool} must refuse a VIEWER`).toBe(true)
      expect(result.data.code).toBe('FORBIDDEN')
    })
  }

  test('nothing a VIEWER attempted changed the board', async () => {
    const board = await editor.expectOk<CanvasBoardWithElements>(
      'get_canvas_board',
      { canvasBoardId: IDS.canvasBoard },
    )
    expect(board.name).toBe('E2E Canvas')
    expect(board.elements).toHaveLength(2)
    const rect = board.elements.find((e) => e.id === IDS.canvasRect)!
    expect(rect.positionX).toBe(300)
  })
})

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
// Canvas tables: create_canvas_table / edit_canvas_table
// ─────────────────────────────────────────────────────────────────────────────

interface TableProps {
  kind: 'table'
  table: {
    rows: Array<{ id: string; height: number }>
    cols: Array<{ id: string; width: number }>
    cells: Record<string, { text: string; fill?: string; textAlign?: string }>
    headerRow: boolean
  }
}

interface CreatedTable {
  element: CanvasElementRow
  rowIds: Array<string>
  columnIds: Array<string>
}

interface EditResult {
  applied: number
  failed?: { index: number; type: string; code: string; message: string }
  skipped: number
  rows: number
  columns: number
}

interface SceneTable {
  id: string
  kind: string
  table?: TableProps['table']
}

async function sceneTable(page: Page, id: string): Promise<SceneTable | undefined> {
  const s = (await scene(page)) as unknown as { elements: Array<SceneTable> }
  return s.elements.find((e) => e.id === id)
}

/**
 * The row height the BROWSER needs for `text` in a column `colWidth` wide: the
 * app's own wrap rule (text-layout.ts wrapParagraph, with the todo-marker
 * advance) over canvas measureText in the app's font stack (render-target.ts
 * FONT_FAMILY), at 14 px and line height 1.4 plus 8 px padding per side, floored
 * at the default row height of 40. Measured in the page, not compared to a literal.
 */
async function browserFit(
  page: Page,
  text: string,
  colWidth: number,
  bold: boolean,
): Promise<number> {
  return page.evaluate(
    ({ text, colWidth, bold }) => {
      const fontSize = 14
      const ctx = new OffscreenCanvas(1, 1).getContext('2d')!
      ctx.font = `${bold ? 'bold ' : ''}${fontSize}px system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans", "Noto Sans CJK TC", sans-serif`
      const measure = (s: string) => ctx.measureText(s).width
      const maxWidth = colWidth - 16
      const advance = fontSize * 1.25
      let lines = 0
      for (const paragraph of text.split('\n')) {
        const todo = paragraph[0] === '☐' || paragraph[0] === '☑'
        const markerWidth = todo ? measure(paragraph[0]) : 0
        const widthOf = (from: number, to: number) => {
          const raw = measure(paragraph.slice(from, to))
          if (!todo) return raw
          return from === 0 ? raw - markerWidth + advance : raw + advance
        }
        if (paragraph.length === 0 || widthOf(0, paragraph.length) <= maxWidth) {
          lines += 1
          continue
        }
        let lineStart = 0
        let lastBreak = -1
        let i = 0
        while (i < paragraph.length) {
          if (widthOf(lineStart, i + 1) > maxWidth && i > lineStart) {
            lines += 1
            const breakAt = lastBreak > lineStart ? lastBreak : i
            lineStart = lastBreak > lineStart ? breakAt + 1 : breakAt
            lastBreak = -1
            i = lineStart
            continue
          }
          if (paragraph[i] === ' ') lastBreak = i
          i += 1
        }
        if (lineStart < paragraph.length) lines += 1
      }
      return Math.max(40, Math.ceil(lines * fontSize * 1.4 + 16))
    },
    { text, colWidth, bold },
  )
}

async function storedTable(boardId: string, id: string): Promise<TableProps['table']> {
  const board = await editor.expectOk<CanvasBoardWithElements>('get_canvas_board', {
    canvasBoardId: boardId,
  })
  const el = board.elements.find((e) => e.id === id)
  expect(el, 'the table must be on the board').toBeDefined()
  return (el!.props as unknown as TableProps).table
}

test.describe('canvas tables over MCP', () => {
  test.beforeAll(() => resetCanvasBoards())

  test('the server advertises both table tools', async () => {
    expect(await editor.listToolNames()).toEqual(
      expect.arrayContaining(['create_canvas_table', 'edit_canvas_table']),
    )
  })

  test('a created table, and each edit, reach an open browser live and survive a reload', async ({
    browser,
  }) => {
    const { page, close } = await openBoardAsEditor(browser, IDS.canvasBoard)
    try {
      // (a) create
      const created = await editor.expectOk<CreatedTable>('create_canvas_table', {
        canvasBoardId: IDS.canvasBoard,
        positionX: 80,
        positionY: 400,
        rows: 3,
        columns: 3,
        cells: [['Name', 'Role', 'Team'], ['Ada', 'Engineer']],
      })
      expect(created.rowIds).toHaveLength(3)
      expect(created.columnIds).toHaveLength(3)
      const id = created.element.id
      await page.waitForFunction(
        (elId) =>
          ((window.__canvasEngine as { elements: Array<{ id: string }> })?.elements ?? []).some(
            (e) => e.id === elId,
          ),
        id,
        { timeout: 20_000 },
      )
      const live = (await sceneTable(page, id))!
      expect(live.kind).toBe('table')
      expect(live.table!.rows).toHaveLength(3)
      expect(live.table!.headerRow).toBe(true)
      const texts = Object.values(live.table!.cells).map((c) => c.text)
      expect(texts).toEqual(expect.arrayContaining(['Name', 'Role', 'Team', 'Ada', 'Engineer']))

      // (b) setText + insertRow + setFill in one ordered call
      const edit = await editor.expectOk<EditResult>('edit_canvas_table', {
        canvasBoardId: IDS.canvasBoard,
        elementId: id,
        changes: [
          { type: 'setText', row: 2, column: 0, text: 'Grace' },
          { type: 'insertRow', at: 3 },
          { type: 'setText', row: 3, column: 1, text: 'appended' },
          { type: 'setFill', fromRow: 0, toRow: 0, fromColumn: 0, toColumn: 2, fill: '#fde68a' },
        ],
      })
      expect(edit).toMatchObject({ applied: 4, skipped: 0, rows: 4, columns: 3 })
      expect(edit.failed).toBeUndefined()

      await page.waitForFunction(
        (elId) => {
          const el = (
            window.__canvasEngine as {
              elements: Array<{
                id: string
                table?: { rows: Array<unknown>; cells: Record<string, { text: string; fill?: string }> }
              }>
            }
          )?.elements?.find((e) => e.id === elId)
          const cells = Object.values(el?.table?.cells ?? {})
          return (
            el?.table?.rows.length === 4 &&
            cells.some((c) => c.text === 'appended') &&
            cells.some((c) => c.text === 'Grace') &&
            cells.filter((c) => c.fill === '#fde68a').length === 3
          )
        },
        id,
        { timeout: 20_000 },
      )

      // The write is persisted, not just broadcast.
      await page.reload()
      await page.waitForFunction(() => window.__canvasEngine !== undefined)
      await page.waitForFunction(
        (elId) =>
          (
            window.__canvasEngine as { elements: Array<{ id: string; table?: { rows: Array<unknown> } }> }
          )?.elements?.find((e) => e.id === elId)?.table?.rows.length === 4,
        id,
        { timeout: 20_000 },
      )
    } finally {
      await close()
    }
  })

  test('row heights are never below the browser’s own fit: bold header cell, body cells, todo marker, em dash', async ({
    browser,
  }) => {
    const { page, close } = await openBoardAsEditor(browser, IDS.canvasBoard)
    try {
      const header = 'Total revenue' // 97 px regular, 109 px bold: only the bold header wraps in 104 px
      const long = 'a long sentence that has to wrap over several lines inside one cell '.repeat(2)
      const todo = '☐ Review the quarterly figures'
      const dash = 'Q1 — Q2 — Q3'
      const texts = [header, long, todo, dash, header]
      // Row 0 is the header row, drawn bold; the last row repeats the header
      // text as a BODY cell, so the test fails if bold leaks into body rows.
      const bold = [true, false, false, false, false]

      const fits: Array<number> = []
      for (let r = 0; r < texts.length; r += 1) {
        fits.push(await browserFit(page, texts[r], 120, bold[r]))
      }
      expect(fits[0], 'the bold header cell must wrap in the browser').toBeGreaterThan(40)
      expect(fits[4], 'the same text as a body cell fits one line').toBe(40)

      const assertRows = (rows: Array<{ height: number }>, how: string) => {
        for (let r = 0; r < texts.length; r += 1) {
          expect(
            rows[r].height,
            `${how}: row ${r} (${JSON.stringify(texts[r])}) must be at least the browser fit ${fits[r]}`,
          ).toBeGreaterThanOrEqual(fits[r])
          expect(
            rows[r].height,
            `${how}: row ${r} must not be absurdly tall`,
          ).toBeLessThanOrEqual(fits[r] + 80)
        }
      }

      // Estimated at create time.
      const created = await editor.expectOk<CreatedTable>('create_canvas_table', {
        canvasBoardId: IDS.canvasBoard,
        positionX: 80,
        positionY: 700,
        rows: texts.length,
        columns: 1,
        cells: texts.map((t) => [t]),
      })
      assertRows((await storedTable(IDS.canvasBoard, created.element.id)).rows, 'create')

      // Estimated at edit time.
      const blank = await editor.expectOk<CreatedTable>('create_canvas_table', {
        canvasBoardId: IDS.canvasBoard,
        positionX: 500,
        positionY: 700,
        rows: texts.length,
        columns: 1,
      })
      const edit = await editor.expectOk<EditResult>('edit_canvas_table', {
        canvasBoardId: IDS.canvasBoard,
        elementId: blank.element.id,
        changes: texts.map((text, row) => ({ type: 'setText', row, column: 0, text })),
      })
      expect(edit.applied).toBe(texts.length)
      const edited = await storedTable(IDS.canvasBoard, blank.element.id)
      assertRows(edited.rows, 'edit')

      // Grow only: a shorter text never shrinks the row.
      const grownHeight = edited.rows[1].height
      await editor.expectOk('edit_canvas_table', {
        canvasBoardId: IDS.canvasBoard,
        elementId: blank.element.id,
        changes: [{ type: 'setText', row: 1, column: 0, text: 'short' }],
      })
      const shorter = await storedTable(IDS.canvasBoard, blank.element.id)
      expect(shorter.rows[1].height, 'the MCP never shrinks a row').toBe(grownHeight)
    } finally {
      await close()
    }
  })

  test('removing the only column reports the app refusal; an unapplied refusal is a tool error', async () => {
    const created = await editor.expectOk<CreatedTable>('create_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      positionX: 600,
      positionY: 400,
      rows: 1,
      columns: 1,
    })
    const id = created.element.id

    const partial = await editor.expectOk<EditResult>('edit_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      elementId: id,
      changes: [
        { type: 'setText', row: 0, column: 0, text: 'kept' },
        { type: 'removeColumn', column: 0 },
        { type: 'setText', row: 0, column: 0, text: 'never reached' },
      ],
    })
    expect(partial.applied).toBe(1)
    expect(partial.skipped).toBe(1)
    expect(partial.failed).toMatchObject({ index: 1, type: 'removeColumn', code: 'VALIDATION_ERROR' })
    const stored = await storedTable(IDS.canvasBoard, id)
    expect(Object.values(stored.cells).map((c) => c.text)).toEqual(['kept'])

    const alone = await editor.callTool<McpErrorPayload>('edit_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      elementId: id,
      changes: [{ type: 'removeColumn', column: 0 }],
    })
    expect(alone.isError).toBe(true)
    expect(alone.data.code).toBe('VALIDATION_ERROR')

    const outOfRange = await editor.callTool<McpErrorPayload>('edit_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      elementId: id,
      changes: [{ type: 'removeRow', row: 5 }],
    })
    expect(outOfRange.isError).toBe(true)
    expect(JSON.stringify(outOfRange.data)).toContain('changes[0].row: 5 is out of range')
  })

  test('a create that is refused before the gate does not kill the server', async () => {
    // Commit 3640069: a pre-load validation path once crashed the process.
    const tooBig = await editor.callTool<McpErrorPayload>('create_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      positionX: 0,
      positionY: 0,
      rows: 30,
      columns: 30,
    })
    expect(tooBig.isError).toBe(true)
    expect(tooBig.data.code).toBe('VALIDATION_ERROR')
    expect(await editor.listToolNames()).toContain('create_canvas_table')
  })

  test('edit_canvas_table refuses a non-table element', async () => {
    const result = await editor.callTool<McpErrorPayload>('edit_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      elementId: IDS.canvasRect,
      changes: [{ type: 'setHeaderRow', on: false }],
    })
    expect(result.isError).toBe(true)
    expect(result.data.code).toBe('VALIDATION_ERROR')
  })

  test('a VIEWER is refused both table tools', async () => {
    const create = await viewer.callTool<McpErrorPayload>('create_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      positionX: 0,
      positionY: 0,
      rows: 2,
      columns: 2,
    })
    expect(create.isError).toBe(true)
    expect(create.data.code).toBe('FORBIDDEN')
    const edit = await viewer.callTool<McpErrorPayload>('edit_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      elementId: IDS.canvasRect,
      changes: [{ type: 'setHeaderRow', on: false }],
    })
    expect(edit.isError).toBe(true)
    expect(edit.data.code).toBe('FORBIDDEN')
  })

  test('get_canvas_summary prints a table’s cell text', async () => {
    const created = await editor.expectOk<CreatedTable>('create_canvas_table', {
      canvasBoardId: IDS.canvasBoard,
      positionX: 900,
      positionY: 900,
      rows: 2,
      columns: 2,
      cells: [['Fruit', 'Colour'], ['Plum', 'Purple']],
    })
    expect(created.element.id).toBeTruthy()
    const summary = await editor.expectOk<string>('get_canvas_summary', {
      canvasBoardId: IDS.canvasBoard,
    })
    expect(summary).toContain('2×2 header')
    expect(summary).toContain('Fruit | Colour')
    expect(summary).toContain('Plum | Purple')
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

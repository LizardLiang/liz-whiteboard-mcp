// e2e/board-image-mcp.spec.ts
// get_board_image and get_canvas_board_image, over the real MCP wire, against
// the compose stack: MCP server -> app /api/render-token -> renderer sidecar
// (headless Chromium) -> app /render/... pages -> PNG back through the tool.
//
// What a passing run proves that unit tests cannot: the sidecar is reachable by
// service name (the DevTools Host-header workaround works), it can reach the
// app, the CJK font is installed, and the bytes that come back decode as PNG
// images of the expected size.
//
// Set E2E_SHOTS_DIR to also write every returned image to disk for inspection.
//
// Prerequisites: the stack is up with the renderer service (see
// playwright.config.ts for the command). The app image must carry
// e2e/seed-render.ts.
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { expect, test } from '@playwright/test'
import { EDITOR_USER, RENDER_IDS, VIEWER_USER } from './fixtures'
import { McpClient } from './mcp-client'
import type { ContentBlock } from './mcp-client'
import { mintAccessToken } from './oauth'
import { runAppSeed } from './stack'

const PNG_SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
const SHOTS_DIR = process.env.E2E_SHOTS_DIR
const MISSING_ID = 'e1000000-0000-4000-8000-0000000000ff'

let editor: McpClient
let viewer: McpClient

test.beforeAll(async () => {
  runAppSeed('seed-render.ts')
  editor = new McpClient(await mintAccessToken(EDITOR_USER))
  await editor.connect()
  viewer = new McpClient(await mintAccessToken(VIEWER_USER))
  await viewer.connect()
})

/** Fail with the tool's own error text, not just "expected false". */
function expectOk(r: { content: Array<ContentBlock>; isError: boolean }) {
  expect(r.isError, JSON.stringify(r.content.map((b) => b.text ?? b.type))).toBe(false)
}

function images(content: Array<ContentBlock>): Array<Buffer> {
  return content
    .filter((b) => b.type === 'image')
    .map((b) => {
      expect(b.mimeType).toBe('image/png')
      return Buffer.from(b.data ?? '', 'base64')
    })
}

function texts(content: Array<ContentBlock>): Array<string> {
  return content.filter((b) => b.type === 'text').map((b) => b.text ?? '')
}

/** PNG width and height from the IHDR chunk. */
function pngSize(png: Buffer): { width: number; height: number } {
  return { width: png.readUInt32BE(16), height: png.readUInt32BE(20) }
}

function expectPng(png: Buffer) {
  expect(png.subarray(0, 8).equals(PNG_SIGNATURE)).toBe(true)
  expect(pngSize(png)).toEqual({ width: 1568, height: 1568 })
}

function save(name: string, pngs: Array<Buffer>) {
  if (!SHOTS_DIR) return
  mkdirSync(SHOTS_DIR, { recursive: true })
  pngs.forEach((png, i) => writeFileSync(join(SHOTS_DIR, `${name}-${i}.png`), png))
}

test.describe('get_board_image', () => {
  test('fit returns one 1568px PNG plus its world rect', async () => {
    const r = await editor.callToolRaw('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
    })
    expectOk(r)
    const pngs = images(r.content)
    expect(pngs).toHaveLength(1)
    expectPng(pngs[0])
    expect(texts(r.content).join(' ')).toMatch(/World rect: x=-?\d+ y=-?\d+ width=\d+ height=\d+/)
    save('er-fit', pngs)
  })

  test('focusIds frames only the chosen tables', async () => {
    const r = await editor.callToolRaw('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
      focusIds: [RENDER_IDS.usersTable, RENDER_IDS.ordersTable],
    })
    expectOk(r)
    const pngs = images(r.content)
    expect(pngs).toHaveLength(1)
    expectPng(pngs[0])
    save('er-focus', pngs)
  })

  test('tiles returns more than one image on a wide board', async () => {
    const r = await editor.callToolRaw('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
      mode: 'tiles',
    })
    expectOk(r)
    const pngs = images(r.content)
    expect(pngs.length).toBeGreaterThan(1)
    expect(pngs.length).toBeLessThanOrEqual(12)
    pngs.forEach(expectPng)
    expect(texts(r.content).some((t) => /Tile row \d+, column \d+/.test(t))).toBe(true)
    save('er-tile', pngs)
  })

  test('dark theme returns a different picture from light', async () => {
    const light = await editor.callToolRaw('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
      focusIds: [RENDER_IDS.usersTable],
    })
    const dark = await editor.callToolRaw('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
      focusIds: [RENDER_IDS.usersTable],
      theme: 'dark',
    })
    const [l, d] = [images(light.content)[0], images(dark.content)[0]]
    expectPng(d)
    expect(l.equals(d)).toBe(false)
    save('er-dark', [d])
  })

  test('an unknown focus id is a validation error naming focusIds and the id', async () => {
    const r = await editor.callTool('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
      focusIds: [MISSING_ID],
    })
    expect(r.isError).toBe(true)
    expect(r.data).toMatchObject({ code: 'VALIDATION_ERROR', field: 'focusIds' })
    expect(JSON.stringify(r.data)).toContain(MISSING_ID)
  })

  test('focusIds with tiles is refused before any render', async () => {
    const r = await editor.callTool('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
      mode: 'tiles',
      focusIds: [RENDER_IDS.usersTable],
    })
    expect(r.isError).toBe(true)
    expect(r.data).toMatchObject({ code: 'VALIDATION_ERROR', field: 'focusIds' })
  })

  test('a board the caller cannot read returns not-found, with no image', async () => {
    const r = await viewer.callToolRaw('get_board_image', {
      whiteboardId: RENDER_IDS.whiteboard,
    })
    expect(r.isError).toBe(true)
    expect(images(r.content)).toHaveLength(0)
    const { code } = JSON.parse(texts(r.content)[0]) as { code: string }
    expect(['NOT_FOUND', 'FORBIDDEN']).toContain(code)
  })

  test('an unknown board returns not-found', async () => {
    const r = await editor.callTool('get_board_image', { whiteboardId: MISSING_ID })
    expect(r.isError).toBe(true)
    expect(r.data).toMatchObject({ code: 'NOT_FOUND' })
  })
})

test.describe('get_canvas_board_image', () => {
  test('fit returns one 1568px PNG', async () => {
    const r = await editor.callToolRaw('get_canvas_board_image', {
      canvasBoardId: RENDER_IDS.canvasBoard,
    })
    expectOk(r)
    const pngs = images(r.content)
    expect(pngs).toHaveLength(1)
    expectPng(pngs[0])
    save('canvas-fit', pngs)
  })

  test('focusIds frames the chosen elements', async () => {
    const r = await editor.callToolRaw('get_canvas_board_image', {
      canvasBoardId: RENDER_IDS.canvasBoard,
      focusIds: [RENDER_IDS.canvasRect, RENDER_IDS.canvasText],
    })
    expectOk(r)
    const pngs = images(r.content)
    expect(pngs).toHaveLength(1)
    expectPng(pngs[0])
    save('canvas-focus', pngs)
  })

  test('tiles returns more than one image on a wide board', async () => {
    const r = await editor.callToolRaw('get_canvas_board_image', {
      canvasBoardId: RENDER_IDS.canvasBoard,
      mode: 'tiles',
    })
    expectOk(r)
    const pngs = images(r.content)
    expect(pngs.length).toBeGreaterThan(1)
    pngs.forEach(expectPng)
    save('canvas-tile', pngs)
  })

  test('a board the caller cannot read returns not-found, with no image', async () => {
    const r = await viewer.callToolRaw('get_canvas_board_image', {
      canvasBoardId: RENDER_IDS.canvasBoard,
    })
    expect(r.isError).toBe(true)
    expect(images(r.content)).toHaveLength(0)
  })

  test('an empty board returns the text "board is empty" and no image', async () => {
    const created = await editor.expectOk<{ id: string }>('create_canvas_board', {
      projectId: RENDER_IDS.project,
      name: `empty ${Date.now()}`,
    })
    const r = await editor.callToolRaw('get_canvas_board_image', {
      canvasBoardId: created.id,
    })
    expectOk(r)
    expect(images(r.content)).toHaveLength(0)
    expect(texts(r.content)).toEqual(['board is empty'])
  })
})

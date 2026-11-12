// e2e/toolsets-mcp.spec.ts
// Progressive disclosure over the real wire: the X-Liz-Toolsets header picks the
// tools a session receives, a bad value is refused at session start, and
// get_tool_guide serves the long how-to text the short descriptions leave out.
//
// Header handling lives in the Go server (cmd/mcp/toolsets.go), but only a running
// stack proves Caddy forwards the header and the bearer middleware still runs first.
import { expect, test } from '@playwright/test'
import { IDS, MCP_URL } from './fixtures'
import { EDITOR_STORAGE_STATE } from './global-setup'
import { McpClient } from './mcp-client'
import { mintAccessToken } from './oauth'

let token: string

test.beforeAll(async () => {
  token = await mintAccessToken(EDITOR_STORAGE_STATE)
})

async function connected(headers: Record<string, string> = {}): Promise<McpClient> {
  const client = new McpClient(token, headers)
  await client.connect()
  return client
}

test('no header lists all tools: a superset of, and longer than, the canvas list', async () => {
  const names = await (await connected()).listToolNames()
  const canvas = await (await connected({ 'X-Liz-Toolsets': 'canvas' })).listToolNames()
  expect(names).toContain('get_tool_guide')
  expect(names).toContain('create_table')
  expect(names).toContain('create_project')
  for (const name of canvas) expect(names).toContain(name)
  expect(names.length).toBeGreaterThan(canvas.length)
})

test('a request without a token gets 401', async () => {
  const response = await fetch(MCP_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list', params: {} }),
  })
  expect(response.status).toBe(401)
})

test('a later request in the same session with a different header keeps the session toolsets', async () => {
  const client = await connected({ 'X-Liz-Toolsets': 'canvas' })
  client.setHeader('X-Liz-Toolsets', 'er')
  const names = await client.listToolNames()
  expect(names).toContain('edit_canvas_table')
  expect(names).not.toContain('create_table')
})

test('X-Liz-Toolsets: canvas lists only core and canvas tools, and a canvas create works', async () => {
  const client = await connected({ 'X-Liz-Toolsets': 'canvas' })
  const names = await client.listToolNames()
  expect(names).toContain('list_projects')
  expect(names).toContain('get_tool_guide')
  expect(names).toContain('edit_canvas_table')
  expect(names).not.toContain('create_table')
  expect(names).not.toContain('create_project')

  const element = await client.expectOk<{ id: string }>('create_canvas_element', {
    canvasBoardId: IDS.canvasBoard,
    kind: 'rectangle',
    positionX: 10,
    positionY: 10,
    width: 80,
    height: 40,
  })
  expect(element.id).toBeTruthy()
})

test('an unknown toolset is refused with HTTP 400 naming the valid toolsets', async () => {
  const response = await fetch(MCP_URL, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${token}`,
      'X-Liz-Toolsets': 'nope',
    },
    body: JSON.stringify({
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } },
    }),
  })
  expect(response.status).toBe(400)
  const body = await response.text()
  expect(body).toContain('all, core, er, canvas, manage')
})

test('get_tool_guide edit_canvas_table returns the guide', async () => {
  const client = await connected({ 'X-Liz-Toolsets': 'core' })
  const guide = await client.expectOk<string>('get_tool_guide', { tool: 'edit_canvas_table' })
  expect(String(guide)).toContain('setFill')
  expect(String(guide)).toContain('CONNECTION_ERROR')
  const listing = await client.expectOk<string>('get_tool_guide', {})
  expect(String(listing)).toContain('batch_schema_update')
})

// e2e/mcp-client.ts
// A minimal streamable-HTTP MCP client, deliberately hand-written.
//
// WHY NOT THE OFFICIAL SDK CLIENT: this suite exists to prove the wire works.
// An SDK client would hide the transport details that have already broken here
// once (see the repo's memory notes on the transport and connect-readiness
// bugs) and would make a protocol regression look like a library upgrade. The
// three things that actually matter are visible below: the Accept header must
// name BOTH application/json and text/event-stream, `initialize` returns the
// session id in the Mcp-Session-Id RESPONSE header, and every later request
// must echo it back.
//
// Tool results arrive as MCP content blocks. `callTool` returns the decoded
// payload plus the isError flag, because half this suite's assertions are about
// a call being REFUSED, and a thrown exception would lose the error code.
import { MCP_URL } from './fixtures'

export interface ToolResult<T = unknown> {
  /** The tool's JSON payload, or the raw text when it is not JSON. */
  data: T
  /** MCP-level error flag. A refused tool call is a normal 200 response. */
  isError: boolean
}

export class McpClient {
  private sessionId: string | null = null
  private nextId = 1

  private readonly extraHeaders: Record<string, string>

  constructor(
    private readonly accessToken: string,
    extraHeaders: Record<string, string> = {},
  ) {
    this.extraHeaders = { ...extraHeaders }
  }

  /** Change a request header for the rest of this client's requests (session unchanged). */
  setHeader(name: string, value: string): void {
    this.extraHeaders[name] = value
  }

  private headers(): Record<string, string> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      // Both types, always. The server streams some responses as SSE and
      // answers 406 if text/event-stream is missing.
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${this.accessToken}`,
      ...this.extraHeaders,
    }
    if (this.sessionId) headers['Mcp-Session-Id'] = this.sessionId
    return headers
  }

  private async post(body: unknown): Promise<Response> {
    try {
      return await fetch(MCP_URL, {
        method: 'POST',
        headers: this.headers(),
        body: JSON.stringify(body),
      })
    } catch (error) {
      throw transportFailure(`POST ${MCP_URL}`, body, error)
    }
  }

  /** Decode a JSON-RPC response that may arrive as JSON or as one SSE event. */
  private static decode(text: string, contentType: string | null): any {
    if (contentType?.includes('text/event-stream')) {
      for (const line of text.split('\n')) {
        if (line.startsWith('data:')) return JSON.parse(line.slice(5).trim())
      }
      throw new Error(`no data line in SSE response: ${text.slice(0, 500)}`)
    }
    if (!text) {
      // An empty body with a 5xx is what a server-side panic looks like from
      // out here. Say so plainly — this exact shape was a real defect.
      throw new Error('empty response body from the MCP endpoint')
    }
    return JSON.parse(text)
  }

  private async rpc(method: string, params: unknown): Promise<any> {
    const id = this.nextId++
    const response = await this.post({ jsonrpc: '2.0', id, method, params })
    const returnedSession = response.headers.get('mcp-session-id')
    if (returnedSession) this.sessionId = returnedSession
    let text: string
    try {
      text = await response.text()
    } catch (error) {
      // An SSE response can abort after its headers arrived.
      throw transportFailure(
        `read body of HTTP ${response.status} from ${MCP_URL}`,
        { method, id },
        error,
      )
    }
    if (!response.ok) {
      throw new Error(
        `MCP ${method} → HTTP ${response.status}: ${text.slice(0, 500) || '<empty body>'}`,
      )
    }
    const decoded = McpClient.decode(text, response.headers.get('content-type'))
    if (decoded.error) {
      throw new Error(`MCP ${method} → JSON-RPC error: ${JSON.stringify(decoded.error)}`)
    }
    return decoded.result
  }

  /** Perform the initialize handshake and the initialized notification. */
  async connect(): Promise<{ name: string; version: string }> {
    const result = await this.rpc('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'canvas-mcp-e2e', version: '1' },
    })
    // A notification carries no id and expects no response body.
    await this.post({ jsonrpc: '2.0', method: 'notifications/initialized' })
    return result.serverInfo
  }

  /** Full tools/list result, descriptions included. */
  async listTools(): Promise<Array<{ name: string; description?: string }>> {
    const result = await this.rpc('tools/list', {})
    return result.tools
  }

  async listToolNames(): Promise<Array<string>> {
    return (await this.listTools()).map((tool) => tool.name)
  }

  async callTool<T = any>(
    name: string,
    args: Record<string, unknown>,
  ): Promise<ToolResult<T>> {
    const result = await this.rpc('tools/call', { name, arguments: args })
    const block = result?.content?.[0]
    let data: unknown = result
    if (block && typeof block.text === 'string') {
      try {
        data = JSON.parse(block.text)
      } catch {
        data = block.text
      }
    }
    return { data: data as T, isError: result?.isError === true }
  }

  /**
   * callTool that keeps every content block. The image tools return image
   * blocks (base64 `data`) followed by text blocks, which `callTool` would drop.
   */
  async callToolRaw(
    name: string,
    args: Record<string, unknown>,
  ): Promise<{ content: Array<ContentBlock>; isError: boolean }> {
    const result = await this.rpc('tools/call', { name, arguments: args })
    return {
      content: (result?.content ?? []) as Array<ContentBlock>,
      isError: result?.isError === true,
    }
  }

  /** callTool for the happy path: throws if the tool refused. */
  async expectOk<T = any>(
    name: string,
    args: Record<string, unknown>,
  ): Promise<T> {
    const result = await this.callTool<T>(name, args)
    if (result.isError) {
      throw new Error(`${name} unexpectedly failed: ${JSON.stringify(result.data)}`)
    }
    return result.data
  }
}

/**
 * Describe a failed request with its whole error chain and log it.
 *
 * Node's fetch (undici) throws `TypeError: fetch failed` and puts the real
 * reason in `cause`, often several levels down. A socket-level cause (e.g.
 * "other side closed") carries a `socket` record with the byte counts, which
 * tell a connection the peer closed before any request byte arrived apart
 * from one closed mid-response.
 */
function transportFailure(
  what: string,
  request: unknown,
  error: unknown,
): Error {
  const lines = [`MCP transport failure: ${what}`]
  const rpc = request as { method?: unknown; id?: unknown } | null
  if (rpc && typeof rpc === 'object') {
    lines.push(`  rpc: method=${String(rpc.method)} id=${String(rpc.id)}`)
  }
  let current: unknown = error
  for (let depth = 0; current !== undefined && current !== null; depth++) {
    const indent = '  '.repeat(depth + 1)
    if (depth > 8) {
      lines.push(`${indent}<cause chain truncated>`)
      break
    }
    if (!(current instanceof Error)) {
      lines.push(`${indent}${depth === 0 ? 'error' : 'cause'}: ${String(current)}`)
      break
    }
    const detail = current as Error & {
      code?: unknown
      errno?: unknown
      syscall?: unknown
      socket?: Record<string, unknown>
    }
    const code = detail.code === undefined ? '' : ` code=${String(detail.code)}`
    const errno = detail.errno === undefined ? '' : ` errno=${String(detail.errno)}`
    const syscall =
      detail.syscall === undefined ? '' : ` syscall=${String(detail.syscall)}`
    lines.push(
      `${indent}${depth === 0 ? 'error' : 'cause'}: ${detail.name}: ${detail.message}${code}${errno}${syscall}`,
    )
    if (detail.socket && typeof detail.socket === 'object') {
      lines.push(`${indent}  socket: ${JSON.stringify(detail.socket)}`)
    }
    current = detail.cause
  }
  lines.push(`  at: ${new Date().toISOString()}`)
  const message = lines.join('\n')
  console.error(message)
  return new Error(message, { cause: error })
}

/** One MCP content block as it arrives on the wire. */
export interface ContentBlock {
  type: string
  text?: string
  data?: string
  mimeType?: string
}

/** Shape of the error payload a refused tool call returns. */
export interface McpErrorPayload {
  code: string
  message: string
  field?: string
}

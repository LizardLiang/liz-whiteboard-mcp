// src/mcp/errors.ts
// Error taxonomy and MCP response mapping for the liz-whiteboard MCP server.
// All errors return { isError: true, content: [{ type: 'text', text: JSON.stringify(...) }] }
// Tokens are redacted before output.

import { ZodError } from 'zod'

// ---------------------------------------------------------------------------
// Error codes
// ---------------------------------------------------------------------------

export type McpErrorCode =
  | 'VALIDATION_ERROR'
  | 'NOT_FOUND'
  | 'FORBIDDEN'
  | 'CONNECTION_ERROR'
  | 'SESSION_EXPIRED'
  | 'INTERNAL_ERROR'

// ---------------------------------------------------------------------------
// Typed error class
// ---------------------------------------------------------------------------

export class McpError extends Error {
  constructor(
    public readonly code: McpErrorCode,
    message: string,
    public readonly field?: string,
  ) {
    super(message)
    this.name = 'McpError'
  }
}

// ---------------------------------------------------------------------------
// Well-known error messages
// ---------------------------------------------------------------------------

export const ERROR_MESSAGES = {
  CONNECTION_ERROR:
    "Cannot connect to liz-whiteboard collaboration server at localhost:3010. Start the app with 'bun run dev' before using write tools.",
  SESSION_EXPIRED:
    'Session token has expired. Update LIZ_SESSION_TOKEN with a fresh token from the session_token cookie, then retry.',
} as const

// ---------------------------------------------------------------------------
// Token redaction
// ---------------------------------------------------------------------------

function redactToken(text: string): string {
  const token = process.env.LIZ_SESSION_TOKEN
  if (!token || token.length < 8) return text
  // Replace the token value with a placeholder.
  return text.split(token).join('[REDACTED]')
}

// ---------------------------------------------------------------------------
// MCP response type
// ---------------------------------------------------------------------------

export interface McpErrorResponse {
  isError: true
  content: Array<{ type: 'text'; text: string }>
  [key: string]: unknown
}

// ---------------------------------------------------------------------------
// Mapping function
// ---------------------------------------------------------------------------

/**
 * Convert any thrown value into an MCP error response object.
 * Never surfaces raw Prisma errors, Zod issue paths, or session tokens.
 */
export function toMcpErrorResponse(err: unknown): McpErrorResponse {
  let code: McpErrorCode = 'INTERNAL_ERROR'
  let message = 'An internal error occurred.'
  let field: string | undefined

  if (err instanceof McpError) {
    code = err.code
    message = err.message
    field = err.field
  } else if (err instanceof ZodError) {
    code = 'VALIDATION_ERROR'
    const first = err.issues.at(0)
    const rawField = first?.path.join('.') ?? ''
    field = rawField.length > 0 ? rawField : undefined
    message = first?.message ?? 'Validation failed.'
  } else if (err instanceof Error) {
    // Generic error: mask the raw message for non-specific errors.
    // We intentionally do NOT propagate Prisma internals.
    message = 'An internal error occurred.'
  }

  // Redact any accidental token leakage.
  message = redactToken(message)

  const payload: { code: McpErrorCode; message: string; field?: string } = {
    code,
    message,
  }
  if (field !== undefined) payload.field = field

  return {
    isError: true,
    content: [{ type: 'text', text: JSON.stringify(payload) }],
  }
}

/**
 * Create an MCP error response for a known code with a custom message.
 */
export function makeMcpError(
  code: McpErrorCode,
  message: string,
  field?: string,
): McpErrorResponse {
  const payload: { code: McpErrorCode; message: string; field?: string } = {
    code,
    message: redactToken(message),
  }
  if (field !== undefined) payload.field = field
  return {
    isError: true,
    content: [{ type: 'text', text: JSON.stringify(payload) }],
  }
}

/**
 * Map a raw ack error code string to a typed McpErrorCode.
 * Used by all write-tool handlers that call socketEmitWithAck.
 */
export function ackCodeToMcpCode(
  code: string,
): 'FORBIDDEN' | 'NOT_FOUND' | 'SESSION_EXPIRED' | 'VALIDATION_ERROR' {
  if (code === 'FORBIDDEN') return 'FORBIDDEN'
  if (code === 'NOT_FOUND') return 'NOT_FOUND'
  if (code === 'SESSION_EXPIRED') return 'SESSION_EXPIRED'
  return 'VALIDATION_ERROR'
}

/**
 * Create a success MCP response with JSON-stringified data.
 */
export function makeMcpSuccess(data: unknown): {
  content: Array<{ type: 'text'; text: string }>
} {
  return {
    content: [{ type: 'text', text: JSON.stringify(data) }],
  }
}

// src/mcp/__tests__/unit/errors.test.ts
// Suite B: Unit Tests — Error Taxonomy
// TC-UNIT-ERR-01 through TC-UNIT-ERR-05

import { describe, expect, it } from 'vitest'
import { z } from 'zod'
import {
  ERROR_MESSAGES,
  McpError,
  makeMcpError,
  toMcpErrorResponse,
} from '../../errors'
import type { ZodError} from 'zod';

describe('errors.ts — toMcpErrorResponse', () => {
  // TC-UNIT-ERR-01
  it('Zod parse error maps to VALIDATION_ERROR with field info', () => {
    const schema = z.object({ dataType: z.enum(['int', 'varchar']) })
    const result = schema.safeParse({ dataType: 'NOTATYPE' })
    expect(result.success).toBe(false)
    const zodError = (result as { success: false; error: ZodError }).error

    const response = toMcpErrorResponse(zodError)

    expect(response.isError).toBe(true)
    const parsed = JSON.parse(response.content[0].text)
    expect(parsed.code).toBe('VALIDATION_ERROR')
    expect(typeof parsed.message).toBe('string')
    expect(parsed.field).toBe('dataType')
  })

  // TC-UNIT-ERR-02
  it('NOT_FOUND McpError maps correctly', () => {
    const err = new McpError('NOT_FOUND', 'Table abc not found.')

    const response = toMcpErrorResponse(err)

    expect(response.isError).toBe(true)
    const parsed = JSON.parse(response.content[0].text)
    expect(parsed.code).toBe('NOT_FOUND')
    expect(parsed.message).toContain('not found')
  })

  // TC-UNIT-ERR-03
  it('token value is redacted from error output', () => {
    const originalToken = process.env.LIZ_SESSION_TOKEN
    process.env.LIZ_SESSION_TOKEN = 'secret-token-abc123'

    const err = new McpError(
      'INTERNAL_ERROR',
      'Something went wrong with secret-token-abc123 in it.',
    )
    const response = toMcpErrorResponse(err)

    const text = response.content[0].text
    expect(text).not.toContain('secret-token-abc123')

    if (originalToken !== undefined) {
      process.env.LIZ_SESSION_TOKEN = originalToken
    } else {
      delete process.env.LIZ_SESSION_TOKEN
    }
  })

  // TC-UNIT-ERR-04
  it('CONNECTION_ERROR message includes guidance text', () => {
    const response = makeMcpError(
      'CONNECTION_ERROR',
      ERROR_MESSAGES.CONNECTION_ERROR,
    )

    const parsed = JSON.parse(response.content[0].text)
    expect(parsed.message).toContain('localhost:3010')
    expect(parsed.message).toContain('bun run dev')
  })

  // TC-UNIT-ERR-05
  it('SESSION_EXPIRED message includes recovery instruction', () => {
    const response = makeMcpError(
      'SESSION_EXPIRED',
      ERROR_MESSAGES.SESSION_EXPIRED,
    )

    const parsed = JSON.parse(response.content[0].text)
    expect(parsed.message).toContain('LIZ_SESSION_TOKEN')
    expect(parsed.message).toContain('session_token cookie')
  })
})

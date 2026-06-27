// src/mcp/tools/read.ts
// MCP tools: get_board, get_schema_summary

import { z } from 'zod'
import { assertProjectAccess, getAuthedUserId, isSessionTokenValid } from '../auth'
import { getBoard } from '../read-data'
import { formatSchemaSummary } from '../schema-summary'
import { McpError, makeMcpSuccess, toMcpErrorResponse } from '../errors'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { getWhiteboardProjectId } from '@/data/resolve-project'

export function registerReadTools(server: McpServer): void {
  // -------------------------------------------------------------------------
  // get_board
  // -------------------------------------------------------------------------
  server.tool(
    'get_board',
    'Get the full ER diagram state for a whiteboard (tables, columns, relationships).',
    {
      whiteboardId: z.string().uuid().describe('The whiteboard UUID'),
    },
    async ({ whiteboardId }) => {
      try {
        // FR-021: validate token on every read call to detect mid-session expiry
        if (!(await isSessionTokenValid())) {
          throw new McpError('SESSION_EXPIRED', 'Session expired. Update LIZ_SESSION_TOKEN.')
        }
        const userId = getAuthedUserId()
        const projectId = await getWhiteboardProjectId(whiteboardId)
        if (!projectId) {
          throw new McpError(
            'NOT_FOUND',
            `Whiteboard ${whiteboardId} not found.`,
          )
        }
        await assertProjectAccess(userId, projectId)
        const board = await getBoard(whiteboardId)
        return makeMcpSuccess(board)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // get_schema_summary
  // -------------------------------------------------------------------------
  server.tool(
    'get_schema_summary',
    'Get a compact text summary of the ER schema for a whiteboard. ' +
      'Omits UUIDs and positions. Suitable for feeding into AI prompts.',
    {
      whiteboardId: z.string().uuid().describe('The whiteboard UUID'),
    },
    async ({ whiteboardId }) => {
      try {
        // FR-021: validate token on every read call to detect mid-session expiry
        if (!(await isSessionTokenValid())) {
          throw new McpError('SESSION_EXPIRED', 'Session expired. Update LIZ_SESSION_TOKEN.')
        }
        const userId = getAuthedUserId()
        const projectId = await getWhiteboardProjectId(whiteboardId)
        if (!projectId) {
          throw new McpError(
            'NOT_FOUND',
            `Whiteboard ${whiteboardId} not found.`,
          )
        }
        await assertProjectAccess(userId, projectId)
        const board = await getBoard(whiteboardId)
        const summary = formatSchemaSummary(board)
        return {
          content: [{ type: 'text' as const, text: summary }],
        }
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )
}

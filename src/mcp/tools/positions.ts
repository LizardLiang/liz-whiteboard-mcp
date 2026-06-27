// src/mcp/tools/positions.ts
// MCP tool: bulk_update_positions
// Implemented as a loop of individual table:move emits (Apollo SA-1).
// Non-atomic; partial results reported via {updated, failed}.

import { z } from 'zod'
import { assertProjectAccess, getAuthedUserId } from '../auth'
import { socketEmitWithAck } from '../socket-manager'
import { McpError, makeMcpSuccess, toMcpErrorResponse } from '../errors'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { getWhiteboardProjectId } from '@/data/resolve-project'

export function registerPositionsTools(server: McpServer): void {
  server.tool(
    'bulk_update_positions',
    'Update positions of multiple tables at once. ' +
      'Non-atomic: failures on individual tables are reported in the "failed" array ' +
      'without rolling back already-persisted updates.',
    {
      whiteboardId: z.string().uuid().describe('The whiteboard UUID'),
      positions: z
        .array(
          z.object({
            id: z.string().uuid().describe('Table UUID'),
            positionX: z.number().finite().describe('New X position'),
            positionY: z.number().finite().describe('New Y position'),
          }),
        )
        .min(1)
        .max(500)
        .describe('Array of table position updates'),
    },
    async ({ whiteboardId, positions }) => {
      try {
        const userId = getAuthedUserId()
        const projectId = await getWhiteboardProjectId(whiteboardId)
        if (!projectId)
          throw new McpError(
            'NOT_FOUND',
            `Whiteboard ${whiteboardId} not found.`,
          )
        await assertProjectAccess(userId, projectId)

        const updated: Array<{ id: string }> = []
        const failed: Array<{ id: string; code: string; message: string }> = []

        for (const pos of positions) {
          try {
            const ack = await socketEmitWithAck(whiteboardId, 'table:move', {
              tableId: pos.id,
              positionX: pos.positionX,
              positionY: pos.positionY,
            })
            if (ack.ok === false) {
              failed.push({
                id: pos.id,
                code: (ack.code as string | undefined) ?? 'VALIDATION_ERROR',
                message:
                  (ack.message as string | undefined) ?? 'Server rejected position update.',
              })
            } else {
              updated.push({ id: pos.id })
            }
          } catch (err: unknown) {
            failed.push({
              id: pos.id,
              code: err instanceof McpError ? err.code : 'INTERNAL_ERROR',
              message: err instanceof Error ? err.message : 'Unknown error',
            })
          }
        }

        return makeMcpSuccess({ updated, failed })
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )
}

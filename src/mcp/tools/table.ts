// src/mcp/tools/table.ts
// MCP tools: create_table, update_table, delete_table

import { z } from 'zod'
import { assertProjectAccess, getAuthedUserId } from '../auth'
import { computeDefaultPosition } from '../positioning'
import { socketEmitWithAck } from '../socket-manager'
import {
  McpError,
  ackCodeToMcpCode,
  makeMcpError,
  makeMcpSuccess,
  toMcpErrorResponse,
} from '../errors'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import {
  getTableProjectId,
  getWhiteboardProjectId,
} from '@/data/resolve-project'
import { findDiagramTableById } from '@/data/diagram-table'

// ---------------------------------------------------------------------------
// create_table MCP input schema (positions optional; defaults filled by MCP)
// ---------------------------------------------------------------------------

const createTableMcpSchema = z.object({
  whiteboardId: z.string().uuid(),
  name: z.string().min(1).max(255),
  description: z.string().optional(),
  positionX: z.number().finite().optional(),
  positionY: z.number().finite().optional(),
  width: z.number().positive().optional(),
  height: z.number().positive().optional(),
})

export function registerTableTools(server: McpServer): void {
  // -------------------------------------------------------------------------
  // create_table
  // -------------------------------------------------------------------------
  server.tool(
    'create_table',
    'Create a new table in a whiteboard. If positionX/positionY are omitted, ' +
      'the server assigns a non-overlapping grid position.',
    {
      whiteboardId: z.string().uuid().describe('The whiteboard UUID'),
      name: z.string().min(1).max(255).describe('Table name'),
      description: z.string().optional().describe('Optional description'),
      positionX: z
        .number()
        .finite()
        .optional()
        .describe('X position (auto-assigned if omitted)'),
      positionY: z
        .number()
        .finite()
        .optional()
        .describe('Y position (auto-assigned if omitted)'),
      width: z.number().positive().optional().describe('Table width'),
      height: z.number().positive().optional().describe('Table height'),
    },
    async (input) => {
      try {
        // Step 1: Validate with MCP schema
        const parsed = createTableMcpSchema.safeParse(input)
        if (!parsed.success) {
          return makeMcpError(
            'VALIDATION_ERROR',
            parsed.error.issues[0]?.message ?? 'Invalid input',
            parsed.error.issues[0]?.path.join('.'),
          )
        }
        const data = parsed.data

        // Step 2: Scope check
        const userId = getAuthedUserId()
        const projectId = await getWhiteboardProjectId(data.whiteboardId)
        if (!projectId) {
          throw new McpError(
            'NOT_FOUND',
            `Whiteboard ${data.whiteboardId} not found.`,
          )
        }
        await assertProjectAccess(userId, projectId)

        // Step 3: Fill default position if omitted
        let positionX = data.positionX
        let positionY = data.positionY
        if (positionX === undefined || positionY === undefined) {
          const pos = await computeDefaultPosition(data.whiteboardId)
          positionX = positionX ?? pos.positionX
          positionY = positionY ?? pos.positionY
        }

        // Step 4: Emit table:create with ack
        const payload: Record<string, unknown> = {
          name: data.name,
          positionX,
          positionY,
        }
        if (data.description !== undefined)
          payload.description = data.description
        if (data.width !== undefined) payload.width = data.width
        if (data.height !== undefined) payload.height = data.height

        const ack = await socketEmitWithAck(
          data.whiteboardId,
          'table:create',
          payload,
        )

        // Step 5: Map ack to response
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected table creation.',
          )
        }

        return makeMcpSuccess(ack.entity)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // update_table
  // -------------------------------------------------------------------------
  server.tool(
    'update_table',
    "Update a table's name, description, size, and/or position. " +
      'Internally routes name/description/size → table:update and position → table:move. ' +
      'Emits are sequential; a failure on the first aborts the second.',
    {
      tableId: z.string().uuid().describe('The table UUID'),
      name: z.string().min(1).max(255).optional().describe('New name'),
      description: z.string().optional().describe('New description'),
      positionX: z.number().finite().optional().describe('New X position'),
      positionY: z.number().finite().optional().describe('New Y position'),
      width: z.number().positive().optional().describe('New width'),
      height: z.number().positive().optional().describe('New height'),
    },
    async ({
      tableId,
      name,
      description,
      positionX,
      positionY,
      width,
      height,
    }) => {
      try {
        const metaChanged =
          name !== undefined ||
          description !== undefined ||
          width !== undefined ||
          height !== undefined
        const posChanged = positionX !== undefined || positionY !== undefined

        if (!metaChanged && !posChanged) {
          return makeMcpError(
            'VALIDATION_ERROR',
            'update_table requires at least one field.',
          )
        }

        // Scope check
        const userId = getAuthedUserId()
        const projectId = await getTableProjectId(tableId)
        if (!projectId) {
          throw new McpError('NOT_FOUND', `Table ${tableId} not found.`)
        }
        await assertProjectAccess(userId, projectId)

        // Determine the whiteboard ID (needed for socket connection)
        const table = await findDiagramTableById(tableId)
        if (!table) {
          throw new McpError('NOT_FOUND', `Table ${tableId} not found.`)
        }
        const whiteboardId = table.whiteboardId

        let resultEntity: unknown = table

        // Emit table:update for meta fields
        if (metaChanged) {
          const metaPayload: Record<string, unknown> = { tableId }
          if (name !== undefined) metaPayload.name = name
          if (description !== undefined) metaPayload.description = description
          if (width !== undefined) metaPayload.width = width
          if (height !== undefined) metaPayload.height = height

          const ackMeta = await socketEmitWithAck(
            whiteboardId,
            'table:update',
            metaPayload,
          )
          if (ackMeta.ok === false) {
            return makeMcpError(
              ackCodeToMcpCode(ackMeta.code as string),
              (ackMeta.message as string | undefined) ?? 'Server rejected table update.',
            )
          }
          resultEntity = ackMeta.entity ?? resultEntity
        }

        // Emit table:move for position (requires BOTH axes)
        if (posChanged) {
          // Fill missing axis from current table position
          const finalX = positionX ?? table.positionX
          const finalY = positionY ?? table.positionY

          const movePayload = { tableId, positionX: finalX, positionY: finalY }
          const ackMove = await socketEmitWithAck(
            whiteboardId,
            'table:move',
            movePayload,
          )
          if (ackMove.ok === false) {
            return makeMcpError(
              ackCodeToMcpCode(ackMove.code as string),
              (ackMove.message as string | undefined) ?? 'Server rejected table move.',
            )
          }

          // Merge position into result
          const moveEntity = ackMove.entity as
            | Record<string, unknown>
            | undefined
          if (moveEntity) {
            resultEntity = {
              ...(resultEntity as Record<string, unknown>),
              positionX: moveEntity.positionX ?? finalX,
              positionY: moveEntity.positionY ?? finalY,
            }
          }
        }

        return makeMcpSuccess(resultEntity)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // delete_table
  // -------------------------------------------------------------------------
  server.tool(
    'delete_table',
    'Delete a table and all its columns and relationships (cascade). ' +
      'Returns the deleted table ID and cascade counts.',
    {
      tableId: z.string().uuid().describe('The table UUID'),
    },
    async ({ tableId }) => {
      try {
        const userId = getAuthedUserId()
        const projectId = await getTableProjectId(tableId)
        if (!projectId) {
          throw new McpError('NOT_FOUND', `Table ${tableId} not found.`)
        }
        await assertProjectAccess(userId, projectId)

        const table = await findDiagramTableById(tableId)
        if (!table) {
          throw new McpError('NOT_FOUND', `Table ${tableId} not found.`)
        }

        const ack = await socketEmitWithAck(
          table.whiteboardId,
          'table:delete',
          { tableId },
        )

        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected table deletion.',
          )
        }

        const cascade = ack.cascade as
          | { relationships?: number; columns?: number }
          | undefined
        const warnings: Array<string> = []
        if (cascade?.columns && cascade.columns > 0) {
          warnings.push(`${cascade.columns} column(s) deleted`)
        }
        if (cascade?.relationships && cascade.relationships > 0) {
          warnings.push(`${cascade.relationships} relationship(s) deleted`)
        }

        return makeMcpSuccess({
          id: tableId,
          cascade,
          warning:
            warnings.length > 0
              ? `Cascade deleted: ${warnings.join(', ')}.`
              : undefined,
        })
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )
}

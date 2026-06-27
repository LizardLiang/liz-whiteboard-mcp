// src/mcp/tools/column.ts
// MCP tools: create_column, update_column, delete_column, reorder_columns

import { z } from 'zod'
import { assertProjectAccess, getAuthedUserId } from '../auth'
import { socketEmitWithAck } from '../socket-manager'
import {
  McpError,
  ackCodeToMcpCode,
  makeMcpError,
  makeMcpSuccess,
  toMcpErrorResponse,
} from '../errors'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { dataTypeSchema } from '@/data/schema'
import { getTableProjectId } from '@/data/resolve-project'
import { findDiagramTableById } from '@/data/diagram-table'
import { findColumnById } from '@/data/column'

/**
 * Get the whiteboard ID for a table (needed to get the socket connection namespace).
 */
async function getWhiteboardIdForTable(tableId: string): Promise<string> {
  const table = await findDiagramTableById(tableId)
  if (!table) throw new McpError('NOT_FOUND', `Table ${tableId} not found.`)
  return table.whiteboardId
}


export function registerColumnTools(server: McpServer): void {
  // -------------------------------------------------------------------------
  // create_column
  // -------------------------------------------------------------------------
  server.tool(
    'create_column',
    'Create a new column in a table.',
    {
      tableId: z.string().uuid().describe('The table UUID'),
      name: z.string().min(1).max(255).describe('Column name'),
      dataType: dataTypeSchema.describe('Column data type'),
      isPrimaryKey: z.boolean().optional().describe('Is primary key'),
      isForeignKey: z.boolean().optional().describe('Is foreign key'),
      isUnique: z.boolean().optional().describe('Is unique'),
      isNullable: z.boolean().optional().describe('Is nullable'),
      description: z.string().optional().describe('Optional description'),
      order: z
        .number()
        .int()
        .min(0)
        .optional()
        .describe('Column order position'),
    },
    async ({
      tableId,
      name,
      dataType,
      isPrimaryKey,
      isForeignKey,
      isUnique,
      isNullable,
      description,
      order,
    }) => {
      try {
        const userId = getAuthedUserId()
        const projectId = await getTableProjectId(tableId)
        if (!projectId)
          throw new McpError('NOT_FOUND', `Table ${tableId} not found.`)
        await assertProjectAccess(userId, projectId)

        const whiteboardId = await getWhiteboardIdForTable(tableId)
        const payload: Record<string, unknown> = { tableId, name, dataType }
        if (isPrimaryKey !== undefined) payload.isPrimaryKey = isPrimaryKey
        if (isForeignKey !== undefined) payload.isForeignKey = isForeignKey
        if (isUnique !== undefined) payload.isUnique = isUnique
        if (isNullable !== undefined) payload.isNullable = isNullable
        if (description !== undefined) payload.description = description
        if (order !== undefined) payload.order = order

        const ack = await socketEmitWithAck(
          whiteboardId,
          'column:create',
          payload,
        )
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected column creation.',
          )
        }
        return makeMcpSuccess(ack.entity)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // update_column
  // -------------------------------------------------------------------------
  server.tool(
    'update_column',
    "Update a column's properties.",
    {
      columnId: z.string().uuid().describe('The column UUID'),
      name: z.string().min(1).max(255).optional().describe('New name'),
      dataType: dataTypeSchema.optional().describe('New data type'),
      isPrimaryKey: z.boolean().optional().describe('Is primary key'),
      isForeignKey: z.boolean().optional().describe('Is foreign key'),
      isUnique: z.boolean().optional().describe('Is unique'),
      isNullable: z.boolean().optional().describe('Is nullable'),
      description: z.string().optional().describe('New description'),
    },
    async ({
      columnId,
      name,
      dataType,
      isPrimaryKey,
      isForeignKey,
      isUnique,
      isNullable,
      description,
    }) => {
      try {
        const userId = getAuthedUserId()
        const column = await findColumnById(columnId)
        if (!column)
          throw new McpError('NOT_FOUND', `Column ${columnId} not found.`)

        const projectId = await getTableProjectId(column.tableId)
        if (!projectId)
          throw new McpError('NOT_FOUND', `Column ${columnId} not found.`)
        await assertProjectAccess(userId, projectId)

        const whiteboardId = await getWhiteboardIdForTable(column.tableId)
        const payload: Record<string, unknown> = { columnId }
        if (name !== undefined) payload.name = name
        if (dataType !== undefined) payload.dataType = dataType
        if (isPrimaryKey !== undefined) payload.isPrimaryKey = isPrimaryKey
        if (isForeignKey !== undefined) payload.isForeignKey = isForeignKey
        if (isUnique !== undefined) payload.isUnique = isUnique
        if (isNullable !== undefined) payload.isNullable = isNullable
        if (description !== undefined) payload.description = description

        const ack = await socketEmitWithAck(
          whiteboardId,
          'column:update',
          payload,
        )
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected column update.',
          )
        }
        return makeMcpSuccess(ack.entity)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // delete_column
  // -------------------------------------------------------------------------
  server.tool(
    'delete_column',
    'Delete a column. Returns the deleted column ID and cascade relationship count.',
    {
      columnId: z.string().uuid().describe('The column UUID'),
    },
    async ({ columnId }) => {
      try {
        const userId = getAuthedUserId()
        const column = await findColumnById(columnId)
        if (!column)
          throw new McpError('NOT_FOUND', `Column ${columnId} not found.`)

        const projectId = await getTableProjectId(column.tableId)
        if (!projectId)
          throw new McpError('NOT_FOUND', `Column ${columnId} not found.`)
        await assertProjectAccess(userId, projectId)

        const whiteboardId = await getWhiteboardIdForTable(column.tableId)
        const ack = await socketEmitWithAck(whiteboardId, 'column:delete', {
          columnId,
        })
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected column deletion.',
          )
        }

        const cascade = ack.cascade as { relationships?: number } | undefined
        const warnings: Array<string> = []
        if (cascade?.relationships && cascade.relationships > 0) {
          warnings.push(`${cascade.relationships} relationship(s) deleted`)
        }

        return makeMcpSuccess({
          id: columnId,
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

  // -------------------------------------------------------------------------
  // reorder_columns
  // -------------------------------------------------------------------------
  server.tool(
    'reorder_columns',
    'Reorder columns within a table by providing the desired column ID order.',
    {
      tableId: z.string().uuid().describe('The table UUID'),
      orderedColumnIds: z
        .array(z.string().uuid())
        .min(1)
        .max(500)
        .describe('Column IDs in the desired order'),
    },
    async ({ tableId, orderedColumnIds }) => {
      try {
        const userId = getAuthedUserId()
        const projectId = await getTableProjectId(tableId)
        if (!projectId)
          throw new McpError('NOT_FOUND', `Table ${tableId} not found.`)
        await assertProjectAccess(userId, projectId)

        const whiteboardId = await getWhiteboardIdForTable(tableId)
        const ack = await socketEmitWithAck(whiteboardId, 'column:reorder', {
          tableId,
          orderedColumnIds,
        })
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected column reorder.',
          )
        }
        return makeMcpSuccess(ack.entity ?? { tableId, orderedColumnIds })
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )
}


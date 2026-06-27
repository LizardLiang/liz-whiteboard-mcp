// src/mcp/tools/relationship.ts
// MCP tools: create_relationship, update_relationship, delete_relationship

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
import { cardinalitySchema } from '@/data/schema'
import {
  getRelationshipProjectId,
  getWhiteboardProjectId,
} from '@/data/resolve-project'
import { findRelationshipById } from '@/data/relationship'


export function registerRelationshipTools(server: McpServer): void {
  // -------------------------------------------------------------------------
  // create_relationship
  // -------------------------------------------------------------------------
  server.tool(
    'create_relationship',
    'Create a relationship between two tables.',
    {
      whiteboardId: z.string().uuid().describe('The whiteboard UUID'),
      sourceTableId: z.string().uuid().describe('Source table UUID'),
      targetTableId: z.string().uuid().describe('Target table UUID'),
      sourceColumnId: z.string().uuid().describe('Source column UUID'),
      targetColumnId: z.string().uuid().describe('Target column UUID'),
      cardinality: cardinalitySchema.describe('Relationship cardinality'),
      label: z.string().max(255).optional().describe('Optional label'),
    },
    async ({
      whiteboardId,
      sourceTableId,
      targetTableId,
      sourceColumnId,
      targetColumnId,
      cardinality,
      label,
    }) => {
      try {
        const userId = getAuthedUserId()
        const projectId = await getWhiteboardProjectId(whiteboardId)
        if (!projectId)
          throw new McpError(
            'NOT_FOUND',
            `Whiteboard ${whiteboardId} not found.`,
          )
        await assertProjectAccess(userId, projectId)

        const payload: Record<string, unknown> = {
          sourceTableId,
          targetTableId,
          sourceColumnId,
          targetColumnId,
          cardinality,
        }
        if (label !== undefined) payload.label = label

        const ack = await socketEmitWithAck(
          whiteboardId,
          'relationship:create',
          payload,
        )
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected relationship creation.',
          )
        }
        return makeMcpSuccess(ack.entity)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // update_relationship
  // -------------------------------------------------------------------------
  server.tool(
    'update_relationship',
    "Update a relationship's cardinality, label, or endpoints. " +
      'The server validates referential integrity on all endpoint changes.',
    {
      relationshipId: z.string().uuid().describe('The relationship UUID'),
      sourceTableId: z
        .string()
        .uuid()
        .optional()
        .describe('New source table UUID'),
      targetTableId: z
        .string()
        .uuid()
        .optional()
        .describe('New target table UUID'),
      sourceColumnId: z
        .string()
        .uuid()
        .optional()
        .describe('New source column UUID'),
      targetColumnId: z
        .string()
        .uuid()
        .optional()
        .describe('New target column UUID'),
      cardinality: cardinalitySchema.optional().describe('New cardinality'),
      label: z.string().max(255).optional().describe('New label'),
    },
    async ({
      relationshipId,
      sourceTableId,
      targetTableId,
      sourceColumnId,
      targetColumnId,
      cardinality,
      label,
    }) => {
      try {
        const userId = getAuthedUserId()
        const projectId = await getRelationshipProjectId(relationshipId)
        if (!projectId)
          throw new McpError(
            'NOT_FOUND',
            `Relationship ${relationshipId} not found.`,
          )
        await assertProjectAccess(userId, projectId)

        const existing = await findRelationshipById(relationshipId)
        if (!existing)
          throw new McpError(
            'NOT_FOUND',
            `Relationship ${relationshipId} not found.`,
          )

        const payload: Record<string, unknown> = { relationshipId }
        if (sourceTableId !== undefined) payload.sourceTableId = sourceTableId
        if (targetTableId !== undefined) payload.targetTableId = targetTableId
        if (sourceColumnId !== undefined)
          payload.sourceColumnId = sourceColumnId
        if (targetColumnId !== undefined)
          payload.targetColumnId = targetColumnId
        if (cardinality !== undefined) payload.cardinality = cardinality
        if (label !== undefined) payload.label = label

        const ack = await socketEmitWithAck(
          existing.whiteboardId,
          'relationship:update',
          payload,
        )
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected relationship update.',
          )
        }
        return makeMcpSuccess(ack.entity)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // delete_relationship
  // -------------------------------------------------------------------------
  server.tool(
    'delete_relationship',
    'Delete a relationship by ID.',
    {
      relationshipId: z.string().uuid().describe('The relationship UUID'),
    },
    async ({ relationshipId }) => {
      try {
        const userId = getAuthedUserId()
        const projectId = await getRelationshipProjectId(relationshipId)
        if (!projectId)
          throw new McpError(
            'NOT_FOUND',
            `Relationship ${relationshipId} not found.`,
          )
        await assertProjectAccess(userId, projectId)

        const existing = await findRelationshipById(relationshipId)
        if (!existing)
          throw new McpError(
            'NOT_FOUND',
            `Relationship ${relationshipId} not found.`,
          )

        const ack = await socketEmitWithAck(
          existing.whiteboardId,
          'relationship:delete',
          { relationshipId },
        )
        if (ack.ok === false) {
          return makeMcpError(
            ackCodeToMcpCode(ack.code as string),
            (ack.message as string | undefined) ?? 'Server rejected relationship deletion.',
          )
        }
        return makeMcpSuccess({ id: relationshipId })
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )
}

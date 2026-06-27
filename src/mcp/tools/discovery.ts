// src/mcp/tools/discovery.ts
// MCP tools: list_projects, list_whiteboards

import { z } from 'zod'
import {
  assertProjectAccess,
  getAuthedUserId,
  isSessionTokenValid,
  listAccessibleProjects,
} from '../auth'
import { listWhiteboards } from '../read-data'
import { McpError, makeMcpSuccess, toMcpErrorResponse } from '../errors'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'

export function registerDiscoveryTools(server: McpServer): void {
  // -------------------------------------------------------------------------
  // list_projects
  // -------------------------------------------------------------------------
  server.tool(
    'list_projects',
    'List all ER diagram projects accessible to the authenticated user.',
    {},
    async () => {
      try {
        // FR-021: validate token on every read call to detect mid-session expiry
        if (!(await isSessionTokenValid())) {
          throw new McpError('SESSION_EXPIRED', 'Session expired. Update LIZ_SESSION_TOKEN.')
        }
        const userId = getAuthedUserId()
        const projects = await listAccessibleProjects(userId)
        return makeMcpSuccess(
          projects.map((p) => ({
            id: p.id,
            name: p.name,
            description: p.description ?? null,
          })),
        )
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )

  // -------------------------------------------------------------------------
  // list_whiteboards
  // -------------------------------------------------------------------------
  server.tool(
    'list_whiteboards',
    'List all whiteboards in a project.',
    {
      projectId: z.string().uuid().describe('The project UUID'),
    },
    async ({ projectId }) => {
      try {
        // FR-021: validate token on every read call to detect mid-session expiry
        if (!(await isSessionTokenValid())) {
          throw new McpError('SESSION_EXPIRED', 'Session expired. Update LIZ_SESSION_TOKEN.')
        }
        const userId = getAuthedUserId()
        await assertProjectAccess(userId, projectId)
        const boards = await listWhiteboards(projectId)
        return makeMcpSuccess(boards)
      } catch (err) {
        return toMcpErrorResponse(err)
      }
    },
  )
}

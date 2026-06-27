// src/mcp/tools/static.ts
// MCP tools: list_data_types, list_cardinalities
// Returns live enum values from Zod schemas — no hardcoded arrays.

import { makeMcpSuccess } from '../errors'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { cardinalitySchema, dataTypeSchema } from '@/data/schema'

export function registerStaticTools(server: McpServer): void {
  // -------------------------------------------------------------------------
  // list_data_types
  // -------------------------------------------------------------------------
  server.tool(
    'list_data_types',
    'List all valid column data types supported by liz-whiteboard.',
    {},
    async () => {
      return makeMcpSuccess(dataTypeSchema.options)
    },
  )

  // -------------------------------------------------------------------------
  // list_cardinalities
  // -------------------------------------------------------------------------
  server.tool(
    'list_cardinalities',
    'List all valid relationship cardinalities supported by liz-whiteboard.',
    {},
    async () => {
      return makeMcpSuccess(cardinalitySchema.options)
    },
  )
}

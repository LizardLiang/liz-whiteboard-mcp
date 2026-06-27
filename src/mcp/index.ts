// src/mcp/index.ts
// Entrypoint for the liz-whiteboard MCP stdio server.
// Run with: bun run src/mcp/index.ts
// Required env: LIZ_SESSION_TOKEN, DATABASE_URL

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { validateStartupToken } from './auth'
import { closeAll } from './socket-manager'
import { registerDiscoveryTools } from './tools/discovery'
import { registerReadTools } from './tools/read'
import { registerTableTools } from './tools/table'
import { registerColumnTools } from './tools/column'
import { registerRelationshipTools } from './tools/relationship'
import { registerPositionsTools } from './tools/positions'
import { registerStaticTools } from './tools/static'

async function main(): Promise<void> {
  // Step 1: Validate session token (exits with code 1 on failure)
  await validateStartupToken()

  // Step 2: Create the MCP server
  const server = new McpServer({
    name: 'liz-whiteboard',
    version: '1.0.0',
  })

  // Step 3: Register all 17 tools
  registerDiscoveryTools(server) // list_projects, list_whiteboards
  registerReadTools(server) // get_board, get_schema_summary
  registerTableTools(server) // create_table, update_table, delete_table
  registerColumnTools(server) // create_column, update_column, delete_column, reorder_columns
  registerRelationshipTools(server) // create_relationship, update_relationship, delete_relationship
  registerPositionsTools(server) // bulk_update_positions
  registerStaticTools(server) // list_data_types, list_cardinalities

  // Step 4: Connect stdio transport
  const transport = new StdioServerTransport()
  await server.connect(transport)

  process.stderr.write(
    '[liz-whiteboard MCP] Server ready. Listening on stdio.\n',
  )
}

main().catch((err) => {
  process.stderr.write(
    `[liz-whiteboard MCP] Fatal error: ${err instanceof Error ? err.message : String(err)}\n`,
  )
  closeAll()
  process.exit(1)
})

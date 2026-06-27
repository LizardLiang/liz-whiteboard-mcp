# liz-whiteboard-mcp

MCP (Model Context Protocol) stdio server for the [liz-whiteboard](https://github.com/LizardLiang/liz-whiteboard) ER diagram tool.

Extracted from the main app on 2026-06-27.

## What it does

Exposes an MCP server that AI agents (e.g. Claude Desktop) can use to:
- Read ER diagram data (tables, columns, relationships, positions)
- Write changes back to the live whiteboard via Socket.IO

## Setup

1. Copy `.env.local.example` to `.env.local` and fill in the values.
2. Install dependencies: `bun install`
3. Generate the Prisma client: `bun run db:generate`
4. Start the server: `bun run start`

## Environment variables

| Variable             | Description                                                      |
|----------------------|------------------------------------------------------------------|
| `DATABASE_URL`       | PostgreSQL connection string (same as main app)                  |
| `DIRECT_DATABASE_URL`| Direct PostgreSQL URL (for Prisma pooling bypass if needed)      |
| `LIZ_SESSION_TOKEN`  | Value of `session_token` cookie from a logged-in browser session |
| `LIZ_SOCKET_URL`     | Base URL of the running main app's Socket.IO server (default: `ws://localhost:3010`) |

## MCP client config (Claude Desktop)

```json
{
  "mcpServers": {
    "liz-whiteboard": {
      "command": "bun",
      "args": ["run", "/path/to/liz-whiteboard-mcp/src/mcp/index.ts"],
      "env": {
        "DATABASE_URL": "...",
        "DIRECT_DATABASE_URL": "...",
        "LIZ_SESSION_TOKEN": "...",
        "LIZ_SOCKET_URL": "ws://localhost:3010"
      }
    }
  }
}
```

## Shared modules

The following files are copied (not symlinked) from the main `liz-whiteboard` repo:

- `src/db.ts` — Prisma singleton
- `src/data/*.ts` — Data-access layer
- `src/lib/auth/session.ts` — Session validation
- `prisma/schema.prisma` — Schema (read-only mirror; never run migrations here)

When the main repo's schema or shared modules change, update the copies here and re-run `bun run db:generate`.

## Testing

```bash
bun run test
```

Requires `.env.local` with valid `DATABASE_URL` for integration tests.

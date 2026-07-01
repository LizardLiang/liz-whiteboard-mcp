# liz-whiteboard-mcp — MCP Server for ER Diagrams & Database Schema Editing (Go, OAuth 2.1)

> **A Model Context Protocol (MCP) server that lets AI agents — Claude, Cursor, VS Code, Claude Code — read and edit entity-relationship (ER) diagrams and SQL database schemas** in [liz-whiteboard](https://github.com/LizardLiang/liz-whiteboard). Written in Go, it serves the **Streamable HTTP** transport and authenticates clients with **OAuth 2.1** (PKCE + JWKS).

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)
![MCP](https://img.shields.io/badge/Model_Context_Protocol-server-6E56CF)
![OAuth 2.1](https://img.shields.io/badge/OAuth-2.1-EB5424)
![SQLite](https://img.shields.io/badge/SQLite-modernc%20(no%20cgo)-003B57?logo=sqlite&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-distroless-2496ED?logo=docker&logoColor=white)
![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)

This is the **AI integration layer** for [liz-whiteboard](https://github.com/LizardLiang/liz-whiteboard), the open-source collaborative ER diagram and database schema designer. Connect any MCP-compatible AI client and design databases conversationally — "add a `users` table with a one-to-many relationship to `orders`" — and watch the changes appear live on the whiteboard. Compiles to a single self-contained binary (pure Go, no cgo, no Node/Bun runtime).

---

## Table of contents

- [What it does](#what-it-does)
- [The 19 MCP tools](#the-19-mcp-tools)
- [How it works](#how-it-works)
- [Quick start (local, dev token)](#quick-start-local-dev-token)
- [Deploy with Docker (single domain)](#deploy-with-docker-single-domain)
- [Connect an MCP client (OAuth)](#connect-an-mcp-client-oauth)
- [Configuration](#configuration)
- [Project layout](#project-layout)
- [Testing](#testing)
- [License](#license)

---

## What it does

Exposes the liz-whiteboard ER diagram as **MCP tools** so an LLM agent can:

- **Discover** — list the user's projects and whiteboards.
- **Read** — load a whiteboard's full diagram (tables, columns, relationships, positions) or a compact text schema summary.
- **Write** — create / update / delete tables, columns, and relationships; reorder columns; bulk-move tables.

Reads go straight to the app's SQLite database; writes are sent to the live collaboration server over Socket.IO and broadcast to every connected user in real time. Every request is scoped to the authenticated user (project-membership checks).

## The 19 MCP tools

| Group | Tools |
|---|---|
| Discovery | `list_projects`, `list_whiteboards` |
| Read | `get_board`, `get_schema_summary`, `get_table_ddl` |
| Tables | `create_table`, `update_table`, `delete_table` |
| Columns | `create_column`, `update_column`, `delete_column`, `reorder_columns` |
| Relationships | `create_relationship`, `update_relationship`, `delete_relationship` |
| Positions | `bulk_update_positions` |
| Batch | `batch_schema_update` |
| Static | `list_data_types` (25), `list_cardinalities` (17) |

## How it works

```
AI client (Claude / Cursor)
   │  OAuth 2.1 (PKCE) → access token (RS256 JWT)
   ▼
liz-whiteboard-mcp  ── OAuth 2.0 Resource Server (RFC 9728 + RFC 8707) ──
   │  • validates the JWT via the AS's JWKS (iss / aud / exp / signature)
   │  • resolves identity per request (sub = User.id), checks project access
   ├── reads  → SQLite (data/app.db)
   └── writes → Socket.IO collaboration server
                (authenticated with a separate collab-audience JWT —
                 the client's token is never passed through)
```

- **Transport:** MCP Streamable HTTP (`POST /mcp`).
- **AuthN/Z:** OAuth 2.1 Resource Server. Serves Protected Resource Metadata at `/.well-known/oauth-protected-resource`, returns `401` + `WWW-Authenticate` for unauthenticated requests, and validates audience-bound RS256 tokens issued by the liz-whiteboard Authorization Server.
- **No token passthrough:** writes use a distinct collaboration token (avoids the OAuth "confused deputy" problem).

## Quick start (local, dev token)

Requirements: Go `1.25+`.

```bash
make build        # → ./liz-whiteboard-mcp   (or: go build ./cmd/mcp/)

# Run with the DEV-ONLY stub verifier (skips the full OAuth flow for local testing).
# NEVER set MCP_DEV_AUTH in production.
DATABASE_URL="file:/absolute/path/to/liz-whiteboard/data/app.db" \
MCP_DEV_AUTH=stub \
MCP_DEV_STUB_TOKEN="dev-token" \
MCP_DEV_USER_ID="<a-real-user-uuid>" \
LIZ_SOCKET_URL="ws://localhost:3010" \
./liz-whiteboard-mcp
# → serves http://127.0.0.1:3011/mcp
```

Then call it with `Authorization: Bearer dev-token`. Without `MCP_DEV_AUTH=stub`, the server runs in production mode and requires real OAuth (see below).

## Deploy with Docker (single domain)

The repo ships a Docker Compose stack that runs the **app + Authorization Server + this MCP server behind one reverse proxy (Caddy)** — so clients use a single origin, no separate ports:

```
http://localhost:8080/        → liz-whiteboard app + OAuth (/authorize, /token, JWKS)
http://localhost:8080/mcp     → this MCP server
```

```bash
bash deploy/run.sh            # provisions a persistent signing key + secret, then docker compose up
```

See [`docker-compose.yml`](docker-compose.yml) and [`deploy/Caddyfile`](deploy/Caddyfile). The Go server itself builds to a tiny distroless image via the [`Dockerfile`](Dockerfile).

## Connect an MCP client (OAuth)

Point an MCP client (Claude Desktop, Claude Code, Cursor, VS Code) at the server URL (e.g. `https://your-domain/mcp`). The client performs the standard MCP OAuth flow automatically:

1. Calls `/mcp`, gets `401` + the Protected Resource Metadata URL.
2. Discovers the Authorization Server, runs the browser **authorize → consent → token** flow (PKCE).
3. Retries `/mcp` with the bearer token.

No API keys or copied cookies required.

## Configuration

| Variable | Description |
|---|---|
| `DATABASE_URL` | SQLite file — the same `data/app.db` the app uses (e.g. `file:/abs/path/data/app.db`). |
| `MCP_LISTEN_ADDR` | Listen address (default `127.0.0.1:3011`). |
| `OAUTH_ISSUER` | Public issuer URL of the Authorization Server; validated in the token `iss` claim. |
| `MCP_RESOURCE_URI` | Canonical public URI of this server (e.g. `https://your-domain/mcp`); the expected token `aud`. |
| `OAUTH_JWKS_URL` | Optional — fetch JWKS from an internal address while `OAUTH_ISSUER` stays public (reverse-proxy / split-horizon). Defaults to `{issuer}/.well-known/jwks.json`. |
| `LIZ_SOCKET_URL` | Collaboration Socket.IO server URL (write path), e.g. `ws://localhost:3010`. |
| `MCP_CLIENT_SECRET` | Confidential-client secret used to mint collaboration tokens from the AS. |
| `COLLAB_TOKEN_URL` / `COLLAB_RESOURCE_URI` | AS collab-token endpoint and the collaboration token audience. |
| `MCP_DEV_AUTH`, `MCP_DEV_STUB_TOKEN`, `MCP_DEV_USER_ID` | **Dev only** — enable the stub verifier. Never set in production. |

## Project layout

```
cmd/mcp/main.go        # entrypoint: HTTP transport, OAuth wiring, tool registration
internal/auth          # OAuth Resource Server: JWKS verifier, per-request identity, project scoping
internal/db            # SQLite connection (database/sql + modernc.org/sqlite, no cgo)
internal/data          # raw-SQL read layer
internal/socket        # Socket.IO write path + collab-token client
internal/tools         # the 19 MCP tool handlers
internal/errors        # error taxonomy + token redaction
internal/{positioning,schema,summary}  # helpers
```

## Testing

```bash
make test              # unit tests (no database required)

# Integration tests against a real SQLite database:
make test-integration DATABASE_URL=file:/abs/path/to/liz-whiteboard/data/app.db
```

## License

[MIT](LICENSE) © LizardLiang

---

**Keywords:** Model Context Protocol server, MCP server Go, MCP server example, OAuth 2.1 resource server, JWKS, PKCE, RFC 9728, RFC 8707, AI database design, ER diagram MCP, SQL schema MCP tools, Claude MCP server, Cursor MCP, Claude Code, Socket.IO, SQLite, modernc, self-hosted MCP, streamable HTTP MCP.

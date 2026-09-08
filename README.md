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
- [The 34 MCP tools](#the-34-mcp-tools)
- [How it works](#how-it-works)
- [How to install & run](#how-to-install--run)
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
- **Read** — load a whiteboard's full diagram (tables, columns, relationships, positions, subject areas) or a compact text schema summary.
- **Write** — create / update / delete tables, columns, and relationships; reorder columns; bulk-move tables; create subject areas and manage their membership/position.
- **Draw on canvas boards** — create canvas boards, and read, create, update, and delete the shapes, text, and connectors on them (see [Canvas boards vs ER whiteboards](#canvas-boards-vs-er-whiteboards)).

Reads go straight to the app's SQLite database; writes are sent to the live collaboration server over Socket.IO and broadcast to every connected user in real time. Every request is scoped to the authenticated user (project-membership checks).

## The 34 MCP tools

| Group | Tools |
|---|---|
| Discovery | `list_projects`, `list_whiteboards` |
| Read | `get_board`, `get_schema_summary`, `get_table_ddl` |
| Tables | `create_table`, `update_table`, `delete_table` |
| Columns | `create_column`, `update_column`, `delete_column`, `reorder_columns` |
| Relationships | `create_relationship`, `update_relationship`, `delete_relationship` |
| Positions | `bulk_update_positions` |
| Areas | `create_area`, `add_table_to_area`, `remove_table_from_area`, `move_area` |
| Batch | `batch_schema_update` |
| Static | `list_data_types` (25), `list_cardinalities` (17) |
| Canvas read | `list_canvas_boards`, `get_canvas_board`, `get_canvas_summary` |
| Canvas elements | `create_canvas_element`, `update_canvas_element`, `delete_canvas_element` |
| Canvas connectors | `create_canvas_connector`, `update_canvas_connector` |
| Canvas boards | `create_canvas_board`, `update_canvas_board`, `delete_canvas_board` |

### Canvas boards vs ER whiteboards

A project holds two independent board types, and the tools do not cross between them. An **ER whiteboard** holds tables, columns, and relationships; every tool above the Canvas rows works on it. A **canvas board** is a freeform FigJam-style surface of shapes, text, and connectors; only the Canvas tools work on it. `list_whiteboards` never returns canvas boards, and `list_canvas_boards` never returns ER whiteboards.

Canvas writes take the same path as ER writes: the tool emits over Socket.IO to the collaboration server, so every open browser client re-renders without a reload. Reads come straight from SQLite.

Six behaviours are non-obvious and an agent calling these tools must know them.

1. **`style` is written whole, not merged.** The app's style schema is a `z.strictObject` with a default for every key, so a `style` argument that names only `fill` resets `stroke`, `strokeWidth`, `fontSize`, `color`, `cornerRadius`, `textAlign`, and `verticalAlign` to the engine defaults. To change one key and keep the rest, read the element first with `get_canvas_board` and resend the full style.
2. **`maxElements` is rejected above 500, never clamped.** `get_canvas_board` and `get_canvas_summary` default to 500 elements; a value below 1 or above 500 fails with a validation error naming `maxElements`. A truncated read reports `truncated: true` and `totalElements`, and returns the first `maxElements` in paint order (`zIndex` ascending, then `createdAt`).
3. **`text` takes no null; an empty string clears the label.** Omit `text` to leave the current label untouched. Pass `""` to remove it. The maximum length is 10,000 characters.
4. **`update_canvas_connector` preserves the endpoint keys it does not manage.** It reads the stored element, merges your fields into a full replacement `props` object, and carries `sourceAttach`, `targetAttach`, the legacy `sourceAnchor` / `targetAnchor`, and `curvature` across unchanged. A connector the user attached to a specific side in the UI keeps that attachment after an MCP re-route.
5. **`delete_canvas_board` requires `confirmName`, and it must match exactly.** Pass the board's current name; read it first with `get_canvas_board`. A mismatch deletes nothing and returns a validation error naming `confirmName`, and the error does not disclose the stored name — the guard exists to catch a wrong `canvasBoardId`, so a blind retry must not be able to defeat it. The delete is permanent and cascades to every element and every share link on the board.
6. **`connector` and `group` are not valid kinds for `create_canvas_element`.** It accepts `rectangle`, `ellipse`, `diamond`, `triangle`, and `text` only. Connectors have their own pair of tools, because their `props` carry cross-field invariants a single generic union would make unreliable to fill. Groups are unsupported by design: their cascade and cycle integrity is a scene-level invariant the browser client repairs on load, and this server has no scene to check against.

Connectors carry two further rules, both checked before anything is written. Each end takes exactly one form: either an element id (`sourceElementId` / `targetElementId`) or a free point (`sourcePoint` / `targetPoint`). Giving both forms for one end, or neither, is a validation error. A connector also cannot join an element to itself, so `sourceElementId` and `targetElementId` must differ. `create_canvas_connector` takes no `style`; set a connector's colour or stroke afterwards with `update_canvas_element`.

Board lifecycle takes a different transport from element writes. `create_canvas_board`, `update_canvas_board`, and `delete_canvas_board` call the app's `POST /api/canvas-boards` route over HTTP (set `LIZ_CANVAS_BOARD_API_URL`), authenticated with the same collaboration JWT the socket path uses. Element writes stay on Socket.IO because open clients must re-render live; board creation has no co-viewing client to broadcast to. One consequence: `update_canvas_board` can move a board into a folder but cannot move it back to the project root, because an omitted `folderId` means "leave it where it is".

Server-owned fields are never accepted from a caller. The collaboration server assigns each element's `id`, computes `zIndex` as `MAX(zIndex) + 1` on create, and forces `rotation` to 0. Use `update_canvas_element`'s `zIndex` argument (range -1,000,000 to 1,000,000) to restack an element; there is no separate bring-to-front tool. Every canvas write requires the EDITOR role or higher, checked here and again by the collaboration server.

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

## How to install & run

Pick one of three ways to get the binary, then point an MCP client at it.

### 1. Install the binary

**Option A — `go install`** (needs Go `1.25+`; produces a binary named `mcp`):

```bash
go install github.com/LizardLiang/liz-whiteboard-mcp/cmd/mcp@latest
# → $(go env GOPATH)/bin/mcp   (add that dir to your PATH)
```

**Option B — build from source:**

```bash
git clone https://github.com/LizardLiang/liz-whiteboard-mcp
cd liz-whiteboard-mcp
make build        # → ./liz-whiteboard-mcp
```

**Option C — Docker** (runs the whole stack; skip to [Deploy with Docker](#deploy-with-docker-single-domain)).

### 2. Run it

Set the environment (see [Configuration](#configuration)) and start the server. It listens on `127.0.0.1:3011` by default and serves the MCP endpoint at `/mcp`:

```bash
DATABASE_URL="file:/absolute/path/to/liz-whiteboard/data/app.db" \
OAUTH_ISSUER="https://your-domain" \
MCP_RESOURCE_URI="https://your-domain/mcp" \
LIZ_SOCKET_URL="ws://localhost:3010" \
MCP_CLIENT_SECRET="<shared-with-the-app>" \
COLLAB_TOKEN_URL="https://your-domain/api/collab-token" \
LIZ_CANVAS_BOARD_API_URL="https://your-domain/api/canvas-boards" \
./liz-whiteboard-mcp
```

> For local testing without a full OAuth setup, use the dev-token path in [Quick start](#quick-start-local-dev-token) instead.

### 3. Connect your AI client

Register the running server with any MCP client. For Claude Code:

```bash
claude mcp add --transport http liz-whiteboard https://your-domain/mcp
```

On first use the client runs the browser OAuth flow automatically (see [Connect an MCP client](#connect-an-mcp-client-oauth)) — no API keys to copy. For Claude Desktop / Cursor / VS Code, add the same URL in the client's MCP server settings.

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
| `MCP_CLIENT_ID` / `MCP_CLIENT_SECRET` | Confidential-client credentials used to mint collaboration tokens from the AS (`MCP_CLIENT_ID` defaults to `mcp-server`). |
| `COLLAB_TOKEN_URL` / `COLLAB_RESOURCE_URI` | AS collab-token endpoint and the collaboration token audience. |
| `LIZ_CANVAS_BOARD_API_URL` | App route for canvas board create / rename / delete (default `http://localhost:3000/api/canvas-boards`). Only the three `*_canvas_board` tools use it. |
| `MCP_DEV_AUTH`, `MCP_DEV_STUB_TOKEN`, `MCP_DEV_USER_ID` | **Dev only** — enable the stub verifier. Never set in production. |

## Project layout

```
cmd/mcp/main.go        # entrypoint: HTTP transport, OAuth wiring, tool registration
internal/auth          # OAuth Resource Server: JWKS verifier, per-request identity, project scoping
internal/db            # SQLite connection (database/sql + modernc.org/sqlite, no cgo)
internal/data          # raw-SQL read layer
internal/socket        # Socket.IO write path (canvas + ER element writes)
internal/collabtoken   # collab-audience JWT client, shared by the socket and HTTP paths
internal/appapi        # HTTP client for the app's canvas-board route (board lifecycle)
internal/tools         # MCP tool handlers (ER + canvas); tools.ToolCount is the tool-surface size
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

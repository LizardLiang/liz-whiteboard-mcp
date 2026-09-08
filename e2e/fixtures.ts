// e2e/fixtures.ts
// Constants shared by the canvas MCP end-to-end suite.
//
// Every id here is SEEDED BY THE APP REPO, not by this one. The stack's app
// container ships ../liz-whiteboard's own e2e seed scripts, and
// e2e/global-setup.ts runs them inside that container (see its header for why).
// The ids must therefore match ../liz-whiteboard/e2e/fixtures.ts exactly. They
// are deliberately fixed, not random, so this suite and the seed agree without
// passing state between two runtimes and two containers.

/** The single public origin Caddy fronts. Host 8080 is unbindable under WSL2;
 *  docker-compose.e2e.yml publishes 18080 instead and explains why. */
export const ORIGIN = process.env.E2E_ORIGIN ?? 'http://localhost:18080'

/** The MCP streamable-HTTP endpoint behind that origin. Clients MUST use this
 *  canonical public URI as the OAuth `resource`; an internal host (app:3000)
 *  fails audience validation and 401s every call. */
export const MCP_URL = `${ORIGIN}/mcp`

/** Compose project name — global-setup.ts execs the seeds in its app container. */
export const COMPOSE_PROJECT = process.env.E2E_COMPOSE_PROJECT ?? 'lizverify'

/** ADMIN of CANVAS_PROJECT. Seeded by the app's e2e/seed.ts. */
export const EDITOR_USER = {
  email: 'e2e_dogfood@example.com',
  password: 'E2eDogfood123!',
}

/** VIEWER member of CANVAS_PROJECT. Seeded by the app's e2e/seed-stress.ts.
 *  The negative authorization case depends on this user being a real member
 *  with a real role, not a stranger: a stranger would be refused by project
 *  scoping and prove nothing about the EDITOR+ gate. */
export const VIEWER_USER = {
  email: 'e2e_viewer@example.com',
  password: 'E2eViewer123!',
}

export const IDS = {
  canvasProject: '60000000-0000-4000-8000-000000000001',
  /** Board the element tests mutate. */
  canvasBoard: '60000000-0000-4000-8000-000000000002',
  canvasRect: '60000000-0000-4000-8000-000000000003',
  canvasText: '60000000-0000-4000-8000-000000000004',
  /** Board carrying the pre-attached connector the merge test reads. */
  canvasConnectorBoard: '60000000-0000-4000-8000-000000000007',
  canvasConnSource: '60000000-0000-4000-8000-000000000008',
  canvasConnTarget: '60000000-0000-4000-8000-000000000009',
  canvasConnector: '60000000-0000-4000-8000-00000000000a',
} as const

/** OAuth first-party client id. This client auto-approves, so the authorize
 *  step needs no consent click — only a live session cookie. */
export const OAUTH_CLIENT_ID = 'mcp-claude'

/** Registered loopback redirect for OAUTH_CLIENT_ID (RFC 8252). Nothing ever
 *  listens on it: the suite reads the code out of the 302 Location header
 *  instead of running a loopback server, which is equivalent and has no port
 *  to collide with. */
export const OAUTH_REDIRECT_URI = 'http://localhost:10000/callback'

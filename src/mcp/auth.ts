// src/mcp/auth.ts
// Startup token validation and per-tool project-membership scoping.
// This module is the SOLE V1 security boundary for the MCP server.
// It must NOT delegate to findAllProjectsForUser (a no-op that returns all projects).

import { McpError } from './errors'
import { validateSessionToken } from '@/lib/auth/session'
import { prisma } from '@/db'

// ---------------------------------------------------------------------------
// Module-level authenticated userId cache (set once at startup)
// ---------------------------------------------------------------------------

let _authedUserId: string | null = null

/**
 * Validate the LIZ_SESSION_TOKEN env var at startup.
 * On failure: prints guidance to stderr and calls process.exit(1).
 * On success: caches the resolved userId for the process lifetime.
 */
export async function validateStartupToken(): Promise<void> {
  const token = process.env.LIZ_SESSION_TOKEN

  if (!token) {
    process.stderr.write(
      '[liz-whiteboard MCP] Error: LIZ_SESSION_TOKEN is not set.\n' +
        'Copy the session_token cookie value from your browser DevTools → ' +
        'Application → Cookies after logging in to liz-whiteboard, then set ' +
        'LIZ_SESSION_TOKEN=<value> in your MCP client environment config.\n',
    )
    process.exit(1)
  }

  const authResult = await validateSessionToken(token)

  if (!authResult) {
    process.stderr.write(
      '[liz-whiteboard MCP] Error: The provided LIZ_SESSION_TOKEN is invalid or expired.\n' +
        'Copy a fresh session_token cookie value from your browser DevTools → ' +
        'Application → Cookies after logging in to liz-whiteboard, then update ' +
        'LIZ_SESSION_TOKEN in your MCP client environment config.\n',
    )
    process.exit(1)
  }

  _authedUserId = authResult.user.id
  process.stderr.write(
    `[liz-whiteboard MCP] Authenticated as user ${authResult.user.id} (${authResult.user.email})\n`,
  )
}

/**
 * Returns the cached authenticated user ID.
 * Must be called after validateStartupToken() succeeds.
 */
export function getAuthedUserId(): string {
  if (!_authedUserId) {
    throw new McpError(
      'INTERNAL_ERROR',
      'MCP server not yet authenticated. Call validateStartupToken() first.',
    )
  }
  return _authedUserId
}

/**
 * Re-validate the session token (used by read tools to detect mid-session expiry).
 * Returns true if valid, false if expired/invalid.
 */
export async function isSessionTokenValid(): Promise<boolean> {
  const token = process.env.LIZ_SESSION_TOKEN
  if (!token) return false
  const result = await validateSessionToken(token)
  return result !== null
}

// ---------------------------------------------------------------------------
// Project-membership scoping (MCP-enforced; sole V1 access control gate)
// ---------------------------------------------------------------------------

/**
 * List only projects accessible to the given user.
 * Uses a direct Prisma query with OR:[{ownerId},{members:{some:{userId}}}].
 * Does NOT call findAllProjectsForUser (which is a no-op returning all projects).
 */
export async function listAccessibleProjects(userId: string) {
  return prisma.project.findMany({
    where: {
      OR: [{ ownerId: userId }, { members: { some: { userId } } }],
    },
    orderBy: { createdAt: 'desc' },
  })
}

/**
 * Assert that a user has access to a project (owner or ProjectMember row).
 * Throws McpError FORBIDDEN if not; McpError NOT_FOUND if projectId is null.
 */
export async function assertProjectAccess(
  userId: string,
  projectId: string | null,
): Promise<void> {
  if (!projectId) {
    throw new McpError('NOT_FOUND', `Resource not found.`)
  }

  // Single query: project exists AND (user is owner OR user has a member row).
  const accessible = await prisma.project.findFirst({
    where: {
      id: projectId,
      OR: [{ ownerId: userId }, { members: { some: { userId } } }],
    },
    select: { id: true },
  })

  if (!accessible) {
    throw new McpError(
      'FORBIDDEN',
      `User ${userId} has no access to project ${projectId}.`,
    )
  }
}

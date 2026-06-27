// src/data/session.ts
// Data access layer for auth Session entity (not CollaborationSession)

import type { Session } from '@prisma/client'
import { prisma } from '@/db'

/**
 * Create an auth session
 * @param data - Session data
 * @returns Created session
 */
export async function createAuthSession(data: {
  tokenHash: string
  userId: string
  expiresAt: Date
}): Promise<Session> {
  const session = await prisma.session.create({
    data,
  })
  return session
}

/**
 * Find an auth session by token hash
 * @param tokenHash - SHA-256 hash of the raw session token
 * @returns Session with user or null if not found
 */
export async function findAuthSessionByTokenHash(tokenHash: string): Promise<
  | (Session & {
      user: { id: string; username: string; email: string }
    })
  | null
> {
  const session = await prisma.session.findUnique({
    where: { tokenHash },
    include: { user: { select: { id: true, username: true, email: true } } },
  })
  return session
}

/**
 * Delete an auth session by ID
 * @param id - Session UUID
 */
export async function deleteAuthSession(id: string): Promise<void> {
  await prisma.session
    .delete({
      where: { id },
    })
    .catch(() => {})
}

/**
 * Delete all expired auth sessions
 * @returns Count of deleted sessions
 */
export async function deleteExpiredAuthSessions(): Promise<number> {
  const result = await prisma.session.deleteMany({
    where: { expiresAt: { lt: new Date() } },
  })
  return result.count
}

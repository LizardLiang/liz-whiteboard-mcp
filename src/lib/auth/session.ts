// src/lib/auth/session.ts
// Session token generation, validation, and management

import { createHash, randomBytes } from 'node:crypto'
import {
  createAuthSession,
  deleteAuthSession,
  deleteExpiredAuthSessions,
  findAuthSessionByTokenHash,
} from '@/data/session'

const SESSION_EXPIRY_DEFAULT = 24 * 60 * 60 * 1000 // 24 hours
const SESSION_EXPIRY_REMEMBER = 30 * 24 * 60 * 60 * 1000 // 30 days

export interface AuthUser {
  id: string
  username: string
  email: string
}

export interface AuthSession {
  id: string
  expiresAt: Date
}

/**
 * Generate a secure session token.
 * Uses Node's crypto.randomBytes (no secure context required — works over HTTP).
 * Never uses crypto.randomUUID() which requires HTTPS/localhost.
 *
 * @returns 64-character hex string (32 bytes)
 */
export function generateSessionToken(): string {
  return randomBytes(32).toString('hex')
}

/**
 * Hash a session token with SHA-256.
 * The raw token goes to the cookie; the hash is stored in the DB.
 *
 * @param token - Raw session token
 * @returns SHA-256 hex digest (64 chars)
 */
export function hashToken(token: string): string {
  return createHash('sha256').update(token).digest('hex')
}

/**
 * Create a new auth session for a user.
 *
 * @param userId - User UUID
 * @param rememberMe - true = 30-day expiry; false = 24-hour expiry
 * @returns { session, token } — token for cookie, session saved to DB with hash
 */
export async function createUserSession(
  userId: string,
  rememberMe: boolean,
): Promise<{ session: AuthSession; token: string }> {
  const token = generateSessionToken()
  const tokenHash = hashToken(token)
  const expiresAt = new Date(
    Date.now() +
      (rememberMe ? SESSION_EXPIRY_REMEMBER : SESSION_EXPIRY_DEFAULT),
  )

  const session = await createAuthSession({ tokenHash, userId, expiresAt })

  return {
    session: { id: session.id, expiresAt: session.expiresAt },
    token,
  }
}

/**
 * Validate a session token and return the associated user and session.
 * Deletes expired sessions on access (lazy expiry).
 *
 * @param token - Raw session token from cookie
 * @returns { user, session } or null if invalid/expired
 */
export async function validateSessionToken(
  token: string,
): Promise<{ user: AuthUser; session: AuthSession } | null> {
  const tokenHash = hashToken(token)
  const record = await findAuthSessionByTokenHash(tokenHash)

  if (!record) return null

  if (record.expiresAt < new Date()) {
    // Expired: delete and return null
    await deleteAuthSession(record.id)
    return null
  }

  return {
    user: {
      id: record.user.id,
      username: record.user.username,
      email: record.user.email,
    },
    session: { id: record.id, expiresAt: record.expiresAt },
  }
}

/**
 * Invalidate (delete) a session by ID.
 *
 * @param sessionId - Session UUID
 */
export async function invalidateSession(sessionId: string): Promise<void> {
  await deleteAuthSession(sessionId)
}

/**
 * Delete all expired sessions (for periodic cleanup).
 *
 * @returns Count of deleted sessions
 */
export async function deleteExpiredSessions(): Promise<number> {
  return deleteExpiredAuthSessions()
}

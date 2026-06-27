// src/mcp/socket-manager.ts
// Lazy per-whiteboard Socket.IO client connection pool.
// Handles FR-021: mid-session token expiry with one reconnect attempt.

import {  io } from 'socket.io-client'
import { McpError } from './errors'
import type {Socket} from 'socket.io-client';

// ---------------------------------------------------------------------------
// Connection pool
// ---------------------------------------------------------------------------

const connections = new Map<string, Socket>()

/**
 * Wait for a socket to connect, with timeout.
 * Rejects with CONNECTION_ERROR if connection does not establish within timeoutMs.
 */
function waitForConnect(socket: Socket, timeoutMs: number): Promise<void> {
  return new Promise((resolve, reject) => {
    if (socket.connected) {
      resolve()
      return
    }

    const timer = setTimeout(() => {
      socket.removeAllListeners('connect')
      socket.removeAllListeners('connect_error')
      reject(
        new McpError(
          'CONNECTION_ERROR',
          'Cannot connect to liz-whiteboard collaboration server at localhost:3010. ' +
            "Start the app with 'bun run dev' before using write tools.",
        ),
      )
    }, timeoutMs)

    socket.once('connect', () => {
      clearTimeout(timer)
      resolve()
    })

    socket.once('connect_error', (_err) => {
      clearTimeout(timer)
      reject(
        new McpError(
          'CONNECTION_ERROR',
          'Cannot connect to liz-whiteboard collaboration server at localhost:3010. ' +
            "Start the app with 'bun run dev' before using write tools.",
        ),
      )
    })
  })
}

/**
 * Create a fresh Socket.IO client for a whiteboard namespace.
 */
function createSocket(whiteboardId: string): Socket {
  const token = process.env.LIZ_SESSION_TOKEN!
  const baseUrl = process.env.LIZ_SOCKET_URL ?? 'ws://localhost:3010'
  return io(`${baseUrl}/whiteboard/${whiteboardId}`, {
    transports: ['websocket'],
    extraHeaders: { Cookie: `session_token=${token}` },
    reconnection: false, // We manage reconnect explicitly (FR-021)
    timeout: 5000,
  })
}

/**
 * Get (or lazily create) the Socket.IO connection for a whiteboard.
 * On session_expired: drops the cached socket, attempts ONE reconnect.
 * On reconnect failure: throws SESSION_EXPIRED.
 */
export async function getConnection(whiteboardId: string): Promise<Socket> {
  const existing = connections.get(whiteboardId)
  if (existing?.connected) return existing

  // Remove stale socket if it exists but is not connected.
  if (existing) {
    existing.removeAllListeners()
    existing.disconnect()
    connections.delete(whiteboardId)
  }

  const socket = createSocket(whiteboardId)

  try {
    await waitForConnect(socket, 5000)
  } catch (err) {
    socket.removeAllListeners()
    socket.disconnect()
    throw err
  }

  // FR-021: when server signals session expiry, clean up and prepare for reconnect.
  socket.on('session_expired', () => {
    process.stderr.write(
      `[liz-whiteboard MCP] Session expired on whiteboard ${whiteboardId}. ` +
        'Will attempt one reconnect on next write.\n',
    )
    socket.disconnect()
    connections.delete(whiteboardId)
  })

  connections.set(whiteboardId, socket)
  return socket
}

// ---------------------------------------------------------------------------
// Emit with ack + 5-second timeout
// ---------------------------------------------------------------------------

/**
 * Emit a Socket.IO event and await an ack callback (FR-022).
 * Wraps socket.timeout(5000).emitWithAck; throws CONNECTION_ERROR on timeout.
 * On session_expired detected mid-emit: attempts ONE reconnect, then retries.
 */
export async function socketEmitWithAck(
  whiteboardId: string,
  event: string,
  payload: unknown,
): Promise<{ ok: boolean; [key: string]: unknown }> {
  let socket = await getConnection(whiteboardId)

  try {
    const ack = await socket.timeout(5000).emitWithAck(event, payload)
    return ack as { ok: boolean; [key: string]: unknown }
  } catch (err: unknown) {
    // socket.io-client timeout throws with a message containing 'timeout'
    if (
      err instanceof Error &&
      (err.message.toLowerCase().includes('timeout') ||
        err.message.toLowerCase().includes('operation'))
    ) {
      throw new McpError(
        'CONNECTION_ERROR',
        'Cannot connect to liz-whiteboard collaboration server at localhost:3010. ' +
          "Start the app with 'bun run dev' before using write tools.",
      )
    }

    // If disconnected (session_expired or network drop), attempt ONE reconnect.
    if (!socket.connected) {
      connections.delete(whiteboardId)
      process.stderr.write(
        `[liz-whiteboard MCP] Socket disconnected on ${event} emit. ` +
          'Attempting one reconnect (FR-021)...\n',
      )
      try {
        socket = await getConnection(whiteboardId)
        const ack = await socket.timeout(5000).emitWithAck(event, payload)
        return ack as { ok: boolean; [key: string]: unknown }
      } catch (_reconnectErr) {
        throw new McpError(
          'SESSION_EXPIRED',
          'Session token has expired. Update LIZ_SESSION_TOKEN with a fresh token from ' +
            'the session_token cookie, then retry.',
        )
      }
    }

    throw err
  }
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

export function closeAll(): void {
  for (const [, socket] of connections) {
    socket.removeAllListeners()
    socket.disconnect()
  }
  connections.clear()
}

// Register cleanup handlers
process.on('exit', closeAll)
process.on('SIGINT', () => {
  closeAll()
  process.exit(0)
})
process.on('SIGTERM', () => {
  closeAll()
  process.exit(0)
})

// src/mcp/positioning.ts
// Default non-overlapping table position computation (FR-006).
// Uses a deterministic slot-by-count grid algorithm.

import { prisma } from '@/db'

// Grid constants
const COL_W = 280
const ROW_H = 220
const COLS = 4
const ORIGIN = { x: 40, y: 40 }

/**
 * Compute a default non-overlapping position for a new table in a whiteboard.
 *
 * Algorithm: count existing tables, map count → (col, row) slot in a 4-column
 * grid, multiply by spacing. Guarantees auto-placed tables never overlap each
 * other regardless of manually-placed table positions.
 *
 * @param whiteboardId - Whiteboard UUID
 * @returns { positionX, positionY } for the new table
 */
export async function computeDefaultPosition(
  whiteboardId: string,
): Promise<{ positionX: number; positionY: number }> {
  const count = await prisma.diagramTable.count({ where: { whiteboardId } })
  const colIndex = count % COLS
  const rowIndex = Math.floor(count / COLS)
  return {
    positionX: ORIGIN.x + colIndex * COL_W,
    positionY: ORIGIN.y + rowIndex * ROW_H,
  }
}

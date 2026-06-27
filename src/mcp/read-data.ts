// src/mcp/read-data.ts
// Thin wrappers over src/data/* for use by MCP read tools.
// All reads go directly to PostgreSQL via Prisma (no collaboration server needed).

import { McpError } from './errors'
import type { Whiteboard } from '@prisma/client'
import type {WhiteboardWithDiagram} from '@/data/whiteboard';
import {
  
  findWhiteboardByIdWithDiagram,
  findWhiteboardsByProjectId
} from '@/data/whiteboard'
import { prisma } from '@/db'

// ---------------------------------------------------------------------------
// List whiteboards in a project
// ---------------------------------------------------------------------------

export async function listWhiteboards(
  projectId: string,
): Promise<
  Array<{ id: string; name: string; updatedAt: Date; tableCount: number }>
> {
  const whiteboards: Array<Whiteboard> = await findWhiteboardsByProjectId(projectId)

  if (whiteboards.length === 0) return []

  // Fetch table counts in a single query.
  const counts = await prisma.diagramTable.groupBy({
    by: ['whiteboardId'],
    where: { whiteboardId: { in: whiteboards.map((wb) => wb.id) } },
    _count: { id: true },
  })
  const countMap = new Map(counts.map((c) => [c.whiteboardId, c._count.id]))

  return whiteboards.map((wb) => ({
    id: wb.id,
    name: wb.name,
    updatedAt: wb.updatedAt,
    tableCount: countMap.get(wb.id) ?? 0,
  }))
}

// ---------------------------------------------------------------------------
// Get full board
// ---------------------------------------------------------------------------

export async function getBoard(
  whiteboardId: string,
): Promise<WhiteboardWithDiagram> {
  const board = await findWhiteboardByIdWithDiagram(whiteboardId)
  if (!board) {
    throw new McpError('NOT_FOUND', `Whiteboard ${whiteboardId} not found.`)
  }
  return board
}

// src/data/whiteboard.ts
// Data access layer for Whiteboard entity

import { createWhiteboardSchema, updateWhiteboardSchema } from './schema'
import type { CanvasState, CreateWhiteboard, UpdateWhiteboard } from './schema'
import type {
  Column,
  DiagramTable,
  Relationship,
  Whiteboard,
} from '@prisma/client'
import { prisma } from '@/db'

/**
 * Whiteboard with full diagram data (tables, columns, relationships)
 */
export type WhiteboardWithDiagram = Whiteboard & {
  tables: Array<
    DiagramTable & {
      columns: Array<Column>
      outgoingRelationships: Array<Relationship>
      incomingRelationships: Array<Relationship>
    }
  >
}

/**
 * Create a new whiteboard
 * @param data - Whiteboard creation data (validated with Zod)
 * @returns Created whiteboard
 * @throws Error if validation fails or database operation fails
 */
export async function createWhiteboard(
  data: CreateWhiteboard,
): Promise<Whiteboard> {
  // Validate input with Zod schema
  const validated = createWhiteboardSchema.parse(data)

  try {
    const whiteboard = await prisma.whiteboard.create({
      data: validated,
    })
    return whiteboard
  } catch (error) {
    throw new Error(
      `Failed to create whiteboard: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all whiteboards in a project
 * @param projectId - Project UUID
 * @returns Array of whiteboards in the project
 */
export async function findWhiteboardsByProjectId(
  projectId: string,
): Promise<Array<Whiteboard>> {
  try {
    const whiteboards = await prisma.whiteboard.findMany({
      where: { projectId },
      orderBy: { updatedAt: 'desc' },
    })
    return whiteboards
  } catch (error) {
    throw new Error(
      `Failed to fetch whiteboards: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all whiteboards in a folder
 * @param folderId - Folder UUID
 * @returns Array of whiteboards in the folder
 */
export async function findWhiteboardsByFolderId(
  folderId: string,
): Promise<Array<Whiteboard>> {
  try {
    const whiteboards = await prisma.whiteboard.findMany({
      where: { folderId },
      orderBy: { updatedAt: 'desc' },
    })
    return whiteboards
  } catch (error) {
    throw new Error(
      `Failed to fetch whiteboards: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find a whiteboard by ID with full diagram data
 * @param id - Whiteboard UUID
 * @returns Whiteboard with tables, columns, and relationships or null if not found
 */
export async function findWhiteboardByIdWithDiagram(
  id: string,
): Promise<WhiteboardWithDiagram | null> {
  try {
    const whiteboard = await prisma.whiteboard.findUnique({
      where: { id },
      include: {
        tables: {
          include: {
            columns: { orderBy: { order: 'asc' } },
            outgoingRelationships: true,
            incomingRelationships: true,
          },
        },
      },
    })
    return whiteboard
  } catch (error) {
    throw new Error(
      `Failed to fetch whiteboard: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find a whiteboard by ID
 * @param id - Whiteboard UUID
 * @returns Whiteboard or null if not found
 */
export async function findWhiteboardById(
  id: string,
): Promise<Whiteboard | null> {
  try {
    const whiteboard = await prisma.whiteboard.findUnique({
      where: { id },
    })
    return whiteboard
  } catch (error) {
    throw new Error(
      `Failed to fetch whiteboard: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update a whiteboard
 * @param id - Whiteboard UUID
 * @param data - Partial whiteboard data to update (validated with Zod)
 * @returns Updated whiteboard
 * @throws Error if whiteboard not found or validation fails
 */
export async function updateWhiteboard(
  id: string,
  data: UpdateWhiteboard,
): Promise<Whiteboard> {
  // Validate input with Zod schema
  const validated = updateWhiteboardSchema.parse(data)

  try {
    const whiteboard = await prisma.whiteboard.update({
      where: { id },
      data: validated,
    })
    return whiteboard
  } catch (error) {
    throw new Error(
      `Failed to update whiteboard: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update whiteboard canvas state
 * @param id - Whiteboard UUID
 * @param canvasState - Canvas state (zoom, offsetX, offsetY)
 * @returns Updated whiteboard
 */
export async function updateWhiteboardCanvasState(
  id: string,
  canvasState: CanvasState,
): Promise<Whiteboard> {
  try {
    const whiteboard = await prisma.whiteboard.update({
      where: { id },
      data: { canvasState },
    })
    return whiteboard
  } catch (error) {
    throw new Error(
      `Failed to update canvas state: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update whiteboard text source
 * @param id - Whiteboard UUID
 * @param textSource - Text-based diagram syntax
 * @returns Updated whiteboard
 */
export async function updateWhiteboardTextSource(
  id: string,
  textSource: string,
): Promise<Whiteboard> {
  try {
    const whiteboard = await prisma.whiteboard.update({
      where: { id },
      data: { textSource },
    })
    return whiteboard
  } catch (error) {
    throw new Error(
      `Failed to update text source: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Delete a whiteboard (cascade deletes all tables, columns, relationships)
 * @param id - Whiteboard UUID
 * @returns Deleted whiteboard
 * @throws Error if whiteboard not found
 */
export async function deleteWhiteboard(id: string): Promise<Whiteboard> {
  try {
    const whiteboard = await prisma.whiteboard.delete({
      where: { id },
    })
    return whiteboard
  } catch (error) {
    throw new Error(
      `Failed to delete whiteboard: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find recent whiteboards (ordered by last updated)
 * @param limit - Maximum number of whiteboards to return
 * @returns Array of recent whiteboards
 */
export async function findRecentWhiteboards(
  limit: number = 10,
): Promise<Array<Whiteboard>> {
  try {
    const whiteboards = await prisma.whiteboard.findMany({
      orderBy: { updatedAt: 'desc' },
      take: limit,
    })
    return whiteboards
  } catch (error) {
    throw new Error(
      `Failed to fetch recent whiteboards: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

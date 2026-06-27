// src/data/diagram-table.ts
// Data access layer for DiagramTable entity

import { createTableSchema, updateTableSchema } from './schema'
import type { CreateTable, UpdateTable } from './schema'
import type { Column, DiagramTable, Relationship } from '@prisma/client'
import { prisma } from '@/db'

/**
 * DiagramTable with columns and relationships
 */
export type DiagramTableWithRelations = DiagramTable & {
  columns: Array<Column>
  outgoingRelationships: Array<Relationship>
  incomingRelationships: Array<Relationship>
}

/**
 * Create a new table
 * @param data - Table creation data (validated with Zod)
 * @returns Created table
 * @throws Error if validation fails or database operation fails
 */
export async function createDiagramTable(
  data: CreateTable,
): Promise<DiagramTable> {
  // Validate input with Zod schema
  const validated = createTableSchema.parse(data)

  try {
    const table = await prisma.diagramTable.create({
      data: validated,
    })
    return table
  } catch (error) {
    throw new Error(
      `Failed to create table: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all tables in a whiteboard
 * @param whiteboardId - Whiteboard UUID
 * @returns Array of tables in the whiteboard
 */
export async function findDiagramTablesByWhiteboardId(
  whiteboardId: string,
): Promise<Array<DiagramTable>> {
  try {
    const tables = await prisma.diagramTable.findMany({
      where: { whiteboardId },
      orderBy: { createdAt: 'asc' },
    })
    return tables
  } catch (error) {
    throw new Error(
      `Failed to fetch tables: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all tables in a whiteboard with columns and relationships
 * @param whiteboardId - Whiteboard UUID
 * @returns Array of tables with columns and relationships
 */
export async function findDiagramTablesByWhiteboardIdWithRelations(
  whiteboardId: string,
): Promise<Array<DiagramTableWithRelations>> {
  try {
    const tables = await prisma.diagramTable.findMany({
      where: { whiteboardId },
      include: {
        columns: { orderBy: { order: 'asc' } },
        outgoingRelationships: true,
        incomingRelationships: true,
      },
      orderBy: { createdAt: 'asc' },
    })
    return tables
  } catch (error) {
    throw new Error(
      `Failed to fetch tables with relations: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find a table by ID
 * @param id - Table UUID
 * @returns Table or null if not found
 */
export async function findDiagramTableById(
  id: string,
): Promise<DiagramTable | null> {
  try {
    const table = await prisma.diagramTable.findUnique({
      where: { id },
    })
    return table
  } catch (error) {
    throw new Error(
      `Failed to fetch table: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find a table by ID with columns and relationships
 * @param id - Table UUID
 * @returns Table with columns and relationships or null if not found
 */
export async function findDiagramTableByIdWithRelations(
  id: string,
): Promise<DiagramTableWithRelations | null> {
  try {
    const table = await prisma.diagramTable.findUnique({
      where: { id },
      include: {
        columns: { orderBy: { order: 'asc' } },
        outgoingRelationships: true,
        incomingRelationships: true,
      },
    })
    return table
  } catch (error) {
    throw new Error(
      `Failed to fetch table with relations: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update a table
 * @param id - Table UUID
 * @param data - Partial table data to update (validated with Zod)
 * @returns Updated table
 * @throws Error if table not found or validation fails
 */
export async function updateDiagramTable(
  id: string,
  data: UpdateTable,
): Promise<DiagramTable> {
  // Validate input with Zod schema
  const validated = updateTableSchema.parse(data)

  try {
    const table = await prisma.diagramTable.update({
      where: { id },
      data: validated,
    })
    return table
  } catch (error) {
    throw new Error(
      `Failed to update table: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update table position (for drag-and-drop)
 * @param id - Table UUID
 * @param positionX - New X coordinate
 * @param positionY - New Y coordinate
 * @returns Updated table
 */
export async function updateDiagramTablePosition(
  id: string,
  positionX: number,
  positionY: number,
): Promise<DiagramTable> {
  try {
    const table = await prisma.diagramTable.update({
      where: { id },
      data: { positionX, positionY },
    })
    return table
  } catch (error) {
    throw new Error(
      `Failed to update table position: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Delete a table (cascade deletes all columns and relationships)
 * @param id - Table UUID
 * @returns Deleted table
 * @throws Error if table not found
 */
export async function deleteDiagramTable(id: string): Promise<DiagramTable> {
  try {
    const table = await prisma.diagramTable.delete({
      where: { id },
    })
    return table
  } catch (error) {
    throw new Error(
      `Failed to delete table: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

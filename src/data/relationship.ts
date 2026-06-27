// src/data/relationship.ts
// Data access layer for Relationship entity

import { createRelationshipSchema, updateRelationshipSchema } from './schema'
import type { CreateRelationship, UpdateRelationship } from './schema'
import type { Column, DiagramTable, Relationship } from '@prisma/client'
import { prisma } from '@/db'

// ---------------------------------------------------------------------------
// Shared referential-integrity validator (Apollo SA-2)
// ---------------------------------------------------------------------------

/**
 * Verify that source/target columns belong to the correct tables and that
 * both tables are inside the same whiteboard.
 *
 * Accepts the MERGED endpoint set (existing values overridden by patch values).
 * Called by both createRelationship and the relationship:update handler so
 * that the check is enforced at every write path.
 *
 * @throws Error with a structured message on any integrity violation.
 */
export async function assertRelationshipEndpointsValid(endpoints: {
  sourceTableId: string
  targetTableId: string
  sourceColumnId: string
  targetColumnId: string
  whiteboardId: string
}): Promise<void> {
  const {
    sourceTableId,
    targetTableId,
    sourceColumnId,
    targetColumnId,
    whiteboardId,
  } = endpoints

  // 1. Source column belongs to source table
  const sourceColumn = await prisma.column.findUnique({
    where: { id: sourceColumnId },
    select: { tableId: true },
  })
  if (!sourceColumn || sourceColumn.tableId !== sourceTableId) {
    throw new Error(
      `sourceColumnId ${sourceColumnId} does not belong to sourceTableId ${sourceTableId}.`,
    )
  }

  // 2. Target column belongs to target table
  const targetColumn = await prisma.column.findUnique({
    where: { id: targetColumnId },
    select: { tableId: true },
  })
  if (!targetColumn || targetColumn.tableId !== targetTableId) {
    throw new Error(
      `targetColumnId ${targetColumnId} does not belong to targetTableId ${targetTableId}.`,
    )
  }

  // 3. Both tables belong to the whiteboard
  const tables = await prisma.diagramTable.findMany({
    where: {
      id: { in: [sourceTableId, targetTableId] },
      whiteboardId,
    },
    select: { id: true },
  })
  const foundIds = new Set(tables.map((t) => t.id))
  if (!foundIds.has(sourceTableId)) {
    throw new Error(
      `sourceTableId ${sourceTableId} does not belong to whiteboard ${whiteboardId}.`,
    )
  }
  if (!foundIds.has(targetTableId)) {
    throw new Error(
      `targetTableId ${targetTableId} does not belong to whiteboard ${whiteboardId}.`,
    )
  }
}

/**
 * Relationship with source and target table/column details
 */
export type RelationshipWithDetails = Relationship & {
  sourceTable: DiagramTable
  targetTable: DiagramTable
  sourceColumn: Column
  targetColumn: Column
}

/**
 * Create a new relationship
 * @param data - Relationship creation data (validated with Zod)
 * @returns Created relationship
 * @throws Error if validation fails or database operation fails
 */
export async function createRelationship(
  data: CreateRelationship,
): Promise<Relationship> {
  // Validate input with Zod schema
  const validated = createRelationshipSchema.parse(data)

  try {
    // Verify referential integrity using the shared validator (Apollo SA-2)
    await assertRelationshipEndpointsValid({
      sourceTableId: validated.sourceTableId,
      targetTableId: validated.targetTableId,
      sourceColumnId: validated.sourceColumnId,
      targetColumnId: validated.targetColumnId,
      whiteboardId: validated.whiteboardId,
    })

    const relationship = await prisma.relationship.create({
      data: validated,
    })
    return relationship
  } catch (error) {
    throw new Error(
      `Failed to create relationship: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all relationships in a whiteboard
 * @param whiteboardId - Whiteboard UUID
 * @returns Array of relationships in the whiteboard
 */
export async function findRelationshipsByWhiteboardId(
  whiteboardId: string,
): Promise<Array<Relationship>> {
  try {
    const relationships = await prisma.relationship.findMany({
      where: { whiteboardId },
      orderBy: { createdAt: 'asc' },
    })
    return relationships
  } catch (error) {
    throw new Error(
      `Failed to fetch relationships: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all relationships in a whiteboard with table and column details
 * @param whiteboardId - Whiteboard UUID
 * @returns Array of relationships with source/target table/column details
 */
export async function findRelationshipsByWhiteboardIdWithDetails(
  whiteboardId: string,
): Promise<Array<RelationshipWithDetails>> {
  try {
    const relationships = await prisma.relationship.findMany({
      where: { whiteboardId },
      include: {
        sourceTable: true,
        targetTable: true,
        sourceColumn: true,
        targetColumn: true,
      },
      orderBy: { createdAt: 'asc' },
    })
    return relationships
  } catch (error) {
    throw new Error(
      `Failed to fetch relationships with details: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find a relationship by ID
 * @param id - Relationship UUID
 * @returns Relationship or null if not found
 */
export async function findRelationshipById(
  id: string,
): Promise<Relationship | null> {
  try {
    const relationship = await prisma.relationship.findUnique({
      where: { id },
    })
    return relationship
  } catch (error) {
    throw new Error(
      `Failed to fetch relationship: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find a relationship by ID with table and column details
 * @param id - Relationship UUID
 * @returns Relationship with source/target details or null if not found
 */
export async function findRelationshipByIdWithDetails(
  id: string,
): Promise<RelationshipWithDetails | null> {
  try {
    const relationship = await prisma.relationship.findUnique({
      where: { id },
      include: {
        sourceTable: true,
        targetTable: true,
        sourceColumn: true,
        targetColumn: true,
      },
    })
    return relationship
  } catch (error) {
    throw new Error(
      `Failed to fetch relationship with details: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all relationships connected to a table (incoming and outgoing)
 * @param tableId - Table UUID
 * @returns Array of relationships connected to the table
 */
export async function findRelationshipsByTableId(
  tableId: string,
): Promise<Array<Relationship>> {
  try {
    const relationships = await prisma.relationship.findMany({
      where: {
        OR: [{ sourceTableId: tableId }, { targetTableId: tableId }],
      },
      orderBy: { createdAt: 'asc' },
    })
    return relationships
  } catch (error) {
    throw new Error(
      `Failed to fetch table relationships: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update a relationship
 * @param id - Relationship UUID
 * @param data - Partial relationship data to update (validated with Zod)
 * @returns Updated relationship
 * @throws Error if relationship not found or validation fails
 */
export async function updateRelationship(
  id: string,
  data: UpdateRelationship,
): Promise<Relationship> {
  // Validate input with Zod schema
  const validated = updateRelationshipSchema.parse(data)

  try {
    // Load current relationship to compute merged endpoints (Apollo SA-2)
    const current = await prisma.relationship.findUnique({ where: { id } })
    if (!current) {
      throw new Error(`Relationship ${id} not found.`)
    }

    // If any endpoint field is being changed, validate the MERGED endpoints
    const endpointFields = [
      'sourceTableId',
      'targetTableId',
      'sourceColumnId',
      'targetColumnId',
    ] as const
    const hasEndpointChange = endpointFields.some(
      (f) => validated[f] !== undefined,
    )

    if (hasEndpointChange) {
      await assertRelationshipEndpointsValid({
        sourceTableId: validated.sourceTableId ?? current.sourceTableId,
        targetTableId: validated.targetTableId ?? current.targetTableId,
        sourceColumnId: validated.sourceColumnId ?? current.sourceColumnId,
        targetColumnId: validated.targetColumnId ?? current.targetColumnId,
        whiteboardId: current.whiteboardId,
      })
    }

    const relationship = await prisma.relationship.update({
      where: { id },
      data: validated,
    })
    return relationship
  } catch (error) {
    throw new Error(
      `Failed to update relationship: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Delete a relationship
 * @param id - Relationship UUID
 * @returns Deleted relationship
 * @throws Error if relationship not found
 */
export async function deleteRelationship(id: string): Promise<Relationship> {
  try {
    const relationship = await prisma.relationship.delete({
      where: { id },
    })
    return relationship
  } catch (error) {
    throw new Error(
      `Failed to delete relationship: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

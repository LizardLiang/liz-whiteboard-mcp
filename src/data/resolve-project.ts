// src/data/resolve-project.ts
// Shared helpers to resolve projectId from child resources.
// Used by server function files for permission checks.

import { prisma } from '@/db'

/**
 * Resolve projectId for a whiteboard by ID.
 * Returns null if the whiteboard does not exist.
 */
export async function getWhiteboardProjectId(
  whiteboardId: string,
): Promise<string | null> {
  const wb = await prisma.whiteboard.findUnique({
    where: { id: whiteboardId },
    select: { projectId: true },
  })
  return wb?.projectId ?? null
}

/**
 * Resolve projectId for a folder by ID.
 * Returns null if the folder does not exist.
 */
export async function getFolderProjectId(
  folderId: string,
): Promise<string | null> {
  const folder = await prisma.folder.findUnique({
    where: { id: folderId },
    select: { projectId: true },
  })
  return folder?.projectId ?? null
}

/**
 * Resolve projectId for a table by table ID (via its whiteboard).
 * Returns null if the table does not exist.
 */
export async function getTableProjectId(
  tableId: string,
): Promise<string | null> {
  const table = await prisma.diagramTable.findUnique({
    where: { id: tableId },
    select: { whiteboard: { select: { projectId: true } } },
  })
  return table?.whiteboard?.projectId ?? null
}

/**
 * Resolve projectId for a column by column ID (via its table's whiteboard).
 * Returns null if the column does not exist.
 */
export async function getColumnProjectId(
  columnId: string,
): Promise<string | null> {
  const column = await prisma.column.findUnique({
    where: { id: columnId },
    select: {
      table: { select: { whiteboard: { select: { projectId: true } } } },
    },
  })
  return column?.table?.whiteboard?.projectId ?? null
}

/**
 * Resolve projectId for a relationship by relationship ID (via its whiteboard).
 * Returns null if the relationship does not exist.
 */
export async function getRelationshipProjectId(
  relationshipId: string,
): Promise<string | null> {
  const rel = await prisma.relationship.findUnique({
    where: { id: relationshipId },
    select: { whiteboard: { select: { projectId: true } } },
  })
  return rel?.whiteboard?.projectId ?? null
}

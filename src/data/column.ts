// src/data/column.ts
// Data access layer for Column entity

import { createColumnSchema, updateColumnSchema } from './schema'
import type { CreateColumn, UpdateColumn } from './schema'
import type { Column } from '@prisma/client'
import { prisma } from '@/db'

/**
 * Create a new column
 * @param data - Column creation data (validated with Zod)
 * @returns Created column
 * @throws Error if validation fails or database operation fails
 */
export async function createColumn(data: CreateColumn): Promise<Column> {
  // Validate input with Zod schema
  const validated = createColumnSchema.parse(data)

  try {
    const column = await prisma.column.create({
      data: validated,
    })
    return column
  } catch (error) {
    throw new Error(
      `Failed to create column: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Create multiple columns in a single transaction
 * @param columns - Array of column creation data
 * @returns Array of created columns
 */
export async function createColumns(
  columns: Array<CreateColumn>,
): Promise<Array<Column>> {
  // Validate all inputs
  const validated = columns.map((col) => createColumnSchema.parse(col))

  try {
    const result = await prisma.$transaction(
      validated.map((data) => prisma.column.create({ data })),
    )
    return result
  } catch (error) {
    throw new Error(
      `Failed to create columns: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all columns in a table
 * @param tableId - Table UUID
 * @returns Array of columns in the table (ordered by order field)
 */
export async function findColumnsByTableId(
  tableId: string,
): Promise<Array<Column>> {
  try {
    const columns = await prisma.column.findMany({
      where: { tableId },
      orderBy: { order: 'asc' },
    })
    return columns
  } catch (error) {
    throw new Error(
      `Failed to fetch columns: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find a column by ID
 * @param id - Column UUID
 * @returns Column or null if not found
 */
export async function findColumnById(id: string): Promise<Column | null> {
  try {
    const column = await prisma.column.findUnique({
      where: { id },
    })
    return column
  } catch (error) {
    throw new Error(
      `Failed to fetch column: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update a column
 * @param id - Column UUID
 * @param data - Partial column data to update (validated with Zod)
 * @returns Updated column
 * @throws Error if column not found or validation fails
 */
export async function updateColumn(
  id: string,
  data: UpdateColumn,
): Promise<Column> {
  // Validate input with Zod schema
  const validated = updateColumnSchema.parse(data)

  try {
    const column = await prisma.column.update({
      where: { id },
      data: validated,
    })
    return column
  } catch (error) {
    throw new Error(
      `Failed to update column: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update column order (for reordering columns)
 * @param id - Column UUID
 * @param order - New order index
 * @returns Updated column
 */
export async function updateColumnOrder(
  id: string,
  order: number,
): Promise<Column> {
  try {
    const column = await prisma.column.update({
      where: { id },
      data: { order },
    })
    return column
  } catch (error) {
    throw new Error(
      `Failed to update column order: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Delete a column (cascade deletes relationships referencing this column)
 * @param id - Column UUID
 * @returns Deleted column
 * @throws Error if column not found
 */
export async function deleteColumn(id: string): Promise<Column> {
  try {
    const column = await prisma.column.delete({
      where: { id },
    })
    return column
  } catch (error) {
    throw new Error(
      `Failed to delete column: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find primary key columns in a table
 * @param tableId - Table UUID
 * @returns Array of primary key columns
 */
export async function findPrimaryKeyColumnsByTableId(
  tableId: string,
): Promise<Array<Column>> {
  try {
    const columns = await prisma.column.findMany({
      where: {
        tableId,
        isPrimaryKey: true,
      },
      orderBy: { order: 'asc' },
    })
    return columns
  } catch (error) {
    throw new Error(
      `Failed to fetch primary key columns: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Reorder all columns in a table in a single atomic transaction.
 * The orderedColumnIds array defines the desired order — each column is assigned
 * order = index (0-based) matching its position in the array.
 *
 * Validation: all IDs must belong to the given tableId. Any mismatch throws.
 * Persistence: wraps all updates in a single prisma.$transaction (REQ-03).
 *
 * @param tableId - Table UUID that owns all columns
 * @param orderedColumnIds - Desired column order (complete list)
 * @returns Array of updated columns in the new order
 * @throws Error if orderedColumnIds is empty, or if any ID does not belong to tableId
 */
export async function reorderColumns(
  tableId: string,
  orderedColumnIds: Array<string>,
): Promise<Array<Column>> {
  if (orderedColumnIds.length === 0) {
    throw new Error('orderedColumnIds must not be empty')
  }

  // Fetch current columns to validate ownership
  const currentColumns = await prisma.column.findMany({
    where: { tableId },
  })

  const ownedIds = new Set(currentColumns.map((c) => c.id))

  // Validate that every supplied ID belongs to this table
  for (const id of orderedColumnIds) {
    if (!ownedIds.has(id)) {
      throw new Error(`Column ${id} does not belong to table ${tableId}`)
    }
  }

  // Build transaction: update each column's order to its array index
  try {
    const updates = orderedColumnIds.map((id, index) =>
      prisma.column.update({
        where: { id },
        data: { order: index },
      }),
    )

    const updated = await prisma.$transaction(updates)
    return updated
  } catch (error) {
    throw new Error(
      `Failed to reorder columns: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Duplicate a column by ID, inserting it directly below the source.
 *
 * The new column receives:
 *  - name: `<original>_copy` (falls back to `<original>_copy2`, `_copy3`, … on conflict)
 *  - same dataType, isPrimaryKey=false, isForeignKey=false, isUnique, isNullable, description
 *  - order: source.order + 1 (all columns with order ≥ that value are shifted down first)
 *
 * @param columnId - Source column UUID
 * @returns The newly created column
 * @throws Error if the source column is not found
 */
export async function duplicateColumn(columnId: string): Promise<Column> {
  // Load source column
  const source = await prisma.column.findUnique({ where: { id: columnId } })
  if (!source) {
    throw new Error(`Column not found: ${columnId}`)
  }

  const newOrder = source.order + 1

  // Shift all sibling columns with order >= newOrder down by 1 to make room
  await prisma.column.updateMany({
    where: {
      tableId: source.tableId,
      order: { gte: newOrder },
    },
    data: { order: { increment: 1 } },
  })

  // Build a unique name (try _copy, then _copy2, _copy3, …)
  const baseName = `${source.name}_copy`
  let candidateName = baseName
  let suffix = 2

  while (true) {
    const conflict = await prisma.column.findUnique({
      where: { tableId_name: { tableId: source.tableId, name: candidateName } },
    })
    if (!conflict) break
    candidateName = `${baseName}${suffix}`
    suffix++
  }

  const newColumn = await prisma.column.create({
    data: {
      tableId: source.tableId,
      name: candidateName,
      dataType: source.dataType,
      isPrimaryKey: false,
      isForeignKey: false,
      isUnique: source.isUnique,
      isNullable: source.isNullable,
      description: source.description,
      order: newOrder,
    },
  })

  return newColumn
}

/**
 * Find foreign key columns in a table
 * @param tableId - Table UUID
 * @returns Array of foreign key columns
 */
export async function findForeignKeyColumnsByTableId(
  tableId: string,
): Promise<Array<Column>> {
  try {
    const columns = await prisma.column.findMany({
      where: {
        tableId,
        isForeignKey: true,
      },
      orderBy: { order: 'asc' },
    })
    return columns
  } catch (error) {
    throw new Error(
      `Failed to fetch foreign key columns: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

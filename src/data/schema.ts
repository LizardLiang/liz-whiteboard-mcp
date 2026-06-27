// src/data/schema.ts
// Zod validation schemas for all entities in the ER Diagram Whiteboard

import { z } from 'zod'

// ============================================================================
// JSON Sub-Schemas (for nested JSON fields)
// ============================================================================

/**
 * Canvas viewport state schema
 * Used in Whiteboard.canvasState
 */
export const canvasStateSchema = z.object({
  zoom: z.number().min(0.1).max(5),
  offsetX: z.number().finite(),
  offsetY: z.number().finite(),
})

/**
 * Routing points for relationship arrows
 * Used in Relationship.routingPoints
 */
export const routingPointsSchema = z.array(
  z.object({
    x: z.number().finite(),
    y: z.number().finite(),
  }),
)

/**
 * Cursor position for collaboration
 * Used in CollaborationSession.cursor
 */
export const cursorSchema = z.object({
  x: z.number().finite(),
  y: z.number().finite(),
})

// ============================================================================
// Enum Schemas
// ============================================================================

/**
 * Cardinality for relationships between tables
 */
export const cardinalitySchema = z.enum([
  'ONE_TO_ONE',
  'ONE_TO_MANY',
  'MANY_TO_ONE',
  'MANY_TO_MANY',
  'ZERO_TO_ONE',
  'ZERO_TO_MANY',
  'SELF_REFERENCING',
  'MANY_TO_ZERO_OR_ONE',
  'MANY_TO_ZERO_OR_MANY',
  'ZERO_OR_ONE_TO_ONE',
  'ZERO_OR_ONE_TO_MANY',
  'ZERO_OR_ONE_TO_ZERO_OR_ONE',
  'ZERO_OR_ONE_TO_ZERO_OR_MANY',
  'ZERO_OR_MANY_TO_ONE',
  'ZERO_OR_MANY_TO_MANY',
  'ZERO_OR_MANY_TO_ZERO_OR_ONE',
  'ZERO_OR_MANY_TO_ZERO_OR_MANY',
])

/**
 * Allowed data types for columns
 */
export const dataTypeSchema = z.enum([
  // Numeric
  'int',
  'bigint',
  'smallint',
  'float',
  'double',
  'decimal',
  'serial',
  'money',
  // String
  'string',
  'char',
  'varchar',
  'text',
  // Boolean
  'boolean',
  'bit',
  // Date/Time
  'date',
  'datetime',
  'timestamp',
  'time',
  // Binary
  'binary',
  'blob',
  // Structured
  'json',
  'xml',
  'array',
  'enum',
  // Identity
  'uuid',
])

// ============================================================================
// Project Schemas
// ============================================================================

/**
 * Schema for creating a new project
 */
export const createProjectSchema = z.object({
  name: z.string().min(1).max(255),
  description: z.string().max(1000).optional(),
})

/**
 * Schema for updating an existing project
 */
export const updateProjectSchema = createProjectSchema.partial()

// ============================================================================
// Folder Schemas
// ============================================================================

/**
 * Schema for creating a new folder
 */
export const createFolderSchema = z.object({
  name: z.string().min(1).max(255),
  projectId: z.string().uuid(),
  parentFolderId: z.string().uuid().optional(),
})

/**
 * Schema for updating an existing folder
 */
export const updateFolderSchema = createFolderSchema
  .pick({ name: true })
  .partial()

// ============================================================================
// Whiteboard Schemas
// ============================================================================

/**
 * Schema for creating a new whiteboard
 */
export const createWhiteboardSchema = z.object({
  name: z.string().min(1).max(255),
  projectId: z.string().uuid(),
  folderId: z.string().uuid().optional(),
  canvasState: canvasStateSchema.optional(),
  textSource: z.string().optional(),
})

/**
 * Schema for updating an existing whiteboard
 */
export const updateWhiteboardSchema = createWhiteboardSchema.partial()

// ============================================================================
// DiagramTable Schemas
// ============================================================================

/**
 * Schema for creating a new table
 */
export const createTableSchema = z.object({
  whiteboardId: z.string().uuid(),
  name: z.string().min(1).max(255),
  description: z.string().optional(),
  positionX: z.number().finite(),
  positionY: z.number().finite(),
  width: z.number().positive().optional(),
  height: z.number().positive().optional(),
})

/**
 * Schema for updating an existing table
 */
export const updateTableSchema = createTableSchema
  .omit({ whiteboardId: true })
  .partial()

// ============================================================================
// Column Schemas
// ============================================================================

/**
 * Schema for creating a new column
 */
export const createColumnSchema = z.object({
  tableId: z.string().uuid(),
  name: z.string().min(1).max(255),
  dataType: dataTypeSchema,
  isPrimaryKey: z.boolean().default(false),
  isForeignKey: z.boolean().default(false),
  isUnique: z.boolean().default(false),
  isNullable: z.boolean().default(false),
  description: z.string().optional(),
  order: z.number().int().min(0).default(0),
})

/**
 * Schema for batch-reordering columns within a table.
 * orderedColumnIds must contain at least 1 UUID (the complete desired order).
 * Max 500 as a sanity cap — tables with more columns are unsupported in V1.
 * All IDs must use .uuid() per project convention (never .cuid()).
 */
export const reorderColumnsSchema = z.object({
  tableId: z.string().uuid(),
  orderedColumnIds: z.array(z.string().uuid()).min(1).max(500),
})

/**
 * Schema for updating an existing column
 *
 * Defined independently (without basing on createColumnSchema) so that absent
 * fields parse as `undefined` rather than inheriting the `.default()` values
 * from createColumnSchema. This ensures only explicitly-provided fields are
 * passed to Prisma, preventing silent overwrites (e.g. resetting isPrimaryKey
 * to false when only isNullable was changed).
 */
export const updateColumnSchema = z.object({
  name: z.string().min(1).max(255).optional(),
  dataType: dataTypeSchema.optional(),
  isPrimaryKey: z.boolean().optional(),
  isForeignKey: z.boolean().optional(),
  isUnique: z.boolean().optional(),
  isNullable: z.boolean().optional(),
  description: z.string().optional(),
})

// ============================================================================
// Relationship Schemas
// ============================================================================

/**
 * Schema for creating a new relationship
 */
export const createRelationshipSchema = z.object({
  whiteboardId: z.string().uuid(),
  sourceTableId: z.string().uuid(),
  targetTableId: z.string().uuid(),
  sourceColumnId: z.string().uuid(),
  targetColumnId: z.string().uuid(),
  cardinality: cardinalitySchema,
  label: z.string().max(255).optional(),
  routingPoints: routingPointsSchema.optional(),
})

/**
 * Schema for updating an existing relationship
 */
export const updateRelationshipSchema = createRelationshipSchema
  .omit({ whiteboardId: true })
  .partial()

// ============================================================================
// CollaborationSession Schemas
// ============================================================================

/**
 * Schema for creating a new collaboration session
 */
export const createSessionSchema = z.object({
  whiteboardId: z.string().uuid(),
  userId: z.string().uuid(),
  socketId: z.string(),
  cursor: cursorSchema.optional(),
})

/**
 * Schema for updating an existing collaboration session
 */
export const updateSessionSchema = z.object({
  cursor: cursorSchema.optional(),
})

// ============================================================================
// Type Exports (inferred from schemas)
// ============================================================================

export type CanvasState = z.infer<typeof canvasStateSchema>
export type RoutingPoints = z.infer<typeof routingPointsSchema>
export type CursorPosition = z.infer<typeof cursorSchema>
export type Cardinality = z.infer<typeof cardinalitySchema>
export type DataType = z.infer<typeof dataTypeSchema>

export type CreateProject = z.infer<typeof createProjectSchema>
export type UpdateProject = z.infer<typeof updateProjectSchema>

export type CreateFolder = z.infer<typeof createFolderSchema>
export type UpdateFolder = z.infer<typeof updateFolderSchema>

export type CreateWhiteboard = z.infer<typeof createWhiteboardSchema>
export type UpdateWhiteboard = z.infer<typeof updateWhiteboardSchema>

export type CreateTable = z.infer<typeof createTableSchema>
export type UpdateTable = z.infer<typeof updateTableSchema>

export type CreateColumn = z.infer<typeof createColumnSchema>
export type UpdateColumn = z.infer<typeof updateColumnSchema>
export type ReorderColumns = z.infer<typeof reorderColumnsSchema>

export type CreateRelationship = z.infer<typeof createRelationshipSchema>
export type UpdateRelationship = z.infer<typeof updateRelationshipSchema>

export type CreateSession = z.infer<typeof createSessionSchema>
export type UpdateSession = z.infer<typeof updateSessionSchema>

// ============================================================================
// Auth Schemas
// ============================================================================

/**
 * Schema for user registration input
 */
export const registerInputSchema = z.object({
  username: z
    .string()
    .min(3, 'Username must be at least 3 characters')
    .max(50, 'Username must be at most 50 characters')
    .regex(
      /^[a-zA-Z0-9_]+$/,
      'Username must be alphanumeric with underscores only',
    ),
  email: z.string().email('Invalid email address').max(255),
  password: z
    .string()
    .min(8, 'Password must be at least 8 characters')
    .max(128, 'Password must be at most 128 characters'),
})

/**
 * Schema for user login input
 */
export const loginInputSchema = z.object({
  email: z.string().email('Invalid email address'),
  password: z.string().min(1, 'Password is required'),
  rememberMe: z.boolean().default(false),
})

// ============================================================================
// Permission Schemas
// ============================================================================

/**
 * Schema for ProjectRole enum
 */
export const projectRoleSchema = z.enum(['VIEWER', 'EDITOR', 'ADMIN'])

/**
 * Schema for granting a permission (by email)
 */
export const grantPermissionSchema = z.object({
  projectId: z.string().uuid(),
  email: z.string().email(),
  role: projectRoleSchema,
})

/**
 * Schema for updating a permission
 */
export const updatePermissionSchema = z.object({
  projectId: z.string().uuid(),
  userId: z.string().uuid(),
  role: projectRoleSchema,
})

/**
 * Schema for revoking a permission
 */
export const revokePermissionSchema = z.object({
  projectId: z.string().uuid(),
  userId: z.string().uuid(),
})

// Auth type exports
export type RegisterInput = z.infer<typeof registerInputSchema>
export type LoginInput = z.infer<typeof loginInputSchema>
export type ProjectRoleValue = z.infer<typeof projectRoleSchema>
export type GrantPermission = z.infer<typeof grantPermissionSchema>
export type UpdatePermission = z.infer<typeof updatePermissionSchema>
export type RevokePermission = z.infer<typeof revokePermissionSchema>

// ============================================================================
// Auto Layout Schemas
// ============================================================================

/**
 * Schema for bulk-updating table positions (used by Auto Layout).
 * - whiteboardId scopes the IDOR guard
 * - positions[] must contain ≥ 1 entry; each id must be a UUID
 * - 500-entry cap as a sanity bound (auto-layout supported size is ≤ 100;
 *   larger payloads suggest a bug or abuse and are rejected client-side)
 */
export const bulkUpdatePositionsSchema = z.object({
  whiteboardId: z.string().uuid(),
  positions: z
    .array(
      z.object({
        id: z.string().uuid(),
        positionX: z.number().finite(),
        positionY: z.number().finite(),
      }),
    )
    .min(1)
    .max(500),
})

export type BulkUpdatePositions = z.infer<typeof bulkUpdatePositionsSchema>

/**
 * Schema for the table:move:bulk socket broadcast payload.
 * Validated server-side before re-broadcasting to all collaborators.
 * - userId must be a UUID (wire format uses userId throughout)
 * - Each position entry must have finite numeric coordinates (rejects NaN / Infinity)
 * - tableId uses the wire-format field name (positionX/positionY), matching
 *   the existing table:moved event convention
 * - 500-entry cap matches bulkUpdatePositionsSchema
 */
export const tableMoveBulkBroadcastSchema = z.object({
  userId: z.string().uuid(),
  positions: z
    .array(
      z.object({
        tableId: z.string().uuid(),
        positionX: z.number().finite(),
        positionY: z.number().finite(),
      }),
    )
    .min(1)
    .max(500),
})

export type TableMoveBulkBroadcast = z.infer<
  typeof tableMoveBulkBroadcastSchema
>

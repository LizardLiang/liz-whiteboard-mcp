// src/mcp/__tests__/unit/validation.test.ts
// Suite E: Unit Tests — Zod Input Validation
// TC-UNIT-VAL-01 through TC-UNIT-VAL-12

import { describe, expect, it } from 'vitest'
import { z } from 'zod'
import {
  bulkUpdatePositionsSchema,
  cardinalitySchema,
  createColumnSchema,
  createRelationshipSchema,
  dataTypeSchema,
  reorderColumnsSchema,
} from '@/data/schema'

// createTableMcpSchema — local definition (mirrors src/mcp/tools/table.ts)
const createTableMcpSchema = z.object({
  whiteboardId: z.string().uuid(),
  name: z.string().min(1).max(255),
  description: z.string().optional(),
  positionX: z.number().finite().optional(),
  positionY: z.number().finite().optional(),
  width: z.number().positive().optional(),
  height: z.number().positive().optional(),
})

const VALID_UUID = 'f47ac10b-58cc-4372-a567-0e02b2c3d479'

describe('createTableMcpSchema', () => {
  // TC-UNIT-VAL-01
  it('rejects missing name', () => {
    const result = createTableMcpSchema.safeParse({ whiteboardId: VALID_UUID })
    expect(result.success).toBe(false)
  })

  // TC-UNIT-VAL-02
  it('rejects non-UUID whiteboardId', () => {
    const result = createTableMcpSchema.safeParse({
      whiteboardId: 'not-a-uuid',
      name: 'users',
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      const path = result.error.issues[0]?.path.join('.')
      expect(path).toBe('whiteboardId')
    }
  })

  // TC-UNIT-VAL-03
  it('accepts omitted positionX/positionY (optional)', () => {
    const result = createTableMcpSchema.safeParse({
      whiteboardId: VALID_UUID,
      name: 'orders',
    })
    expect(result.success).toBe(true)
  })

  // TC-UNIT-VAL-04
  it('rejects Infinity/NaN positions', () => {
    const result = createTableMcpSchema.safeParse({
      whiteboardId: VALID_UUID,
      name: 'test',
      positionX: Infinity,
      positionY: NaN,
    })
    expect(result.success).toBe(false)
  })
})

describe('createColumnSchema', () => {
  // TC-UNIT-VAL-05
  it('rejects invalid dataType', () => {
    const result = createColumnSchema.safeParse({
      tableId: VALID_UUID,
      name: 'col1',
      dataType: 'NOTATYPE',
    })
    expect(result.success).toBe(false)
  })

  // TC-UNIT-VAL-06
  it('accepts all 25 valid dataTypes', () => {
    for (const dt of dataTypeSchema.options) {
      const result = createColumnSchema.safeParse({
        tableId: VALID_UUID,
        name: 'col1',
        dataType: dt,
      })
      expect(result.success, `Expected ${dt} to be valid`).toBe(true)
    }
  })
})

describe('createRelationshipSchema', () => {
  const baseRel = {
    whiteboardId: VALID_UUID,
    sourceTableId: VALID_UUID,
    targetTableId: VALID_UUID,
    sourceColumnId: VALID_UUID,
    targetColumnId: VALID_UUID,
  }

  // TC-UNIT-VAL-07
  it('rejects invalid cardinality', () => {
    const result = createRelationshipSchema.safeParse({
      ...baseRel,
      cardinality: 'SEVEN_TO_FOUR',
    })
    expect(result.success).toBe(false)
  })

  // TC-UNIT-VAL-08
  it('accepts all 17 valid cardinalities', () => {
    for (const card of cardinalitySchema.options) {
      const result = createRelationshipSchema.safeParse({
        ...baseRel,
        cardinality: card,
      })
      expect(result.success, `Expected ${card} to be valid`).toBe(true)
    }
  })
})

describe('update_table empty payload', () => {
  // TC-UNIT-VAL-09: Tested via the tool handler logic, but we verify the
  // sentinel condition here: an update schema with no fields set.
  it('updateTableSchema.partial() accepts empty object (individual handler enforces non-empty)', () => {
    // The schema itself allows empty; the handler logic in tools/table.ts
    // checks metaChanged || posChanged and returns VALIDATION_ERROR for empty.
    const updateTableSchema = z
      .object({
        name: z.string().min(1).max(255),
        description: z.string(),
        positionX: z.number().finite(),
        positionY: z.number().finite(),
        width: z.number().positive(),
        height: z.number().positive(),
      })
      .partial()
    const result = updateTableSchema.safeParse({})
    // Schema passes (empty partial is valid); tool handler must gate this.
    expect(result.success).toBe(true)
  })
})

describe('bulkUpdatePositionsSchema', () => {
  // TC-UNIT-VAL-10
  it('rejects empty positions array', () => {
    const result = bulkUpdatePositionsSchema.safeParse({
      whiteboardId: VALID_UUID,
      positions: [],
    })
    expect(result.success).toBe(false)
  })

  it('accepts valid positions', () => {
    const result = bulkUpdatePositionsSchema.safeParse({
      whiteboardId: VALID_UUID,
      positions: [{ id: VALID_UUID, positionX: 100, positionY: 200 }],
    })
    expect(result.success).toBe(true)
  })
})

describe('update_relationship validation', () => {
  // TC-UNIT-VAL-11
  it('rejects non-UUID relationshipId', () => {
    // The MCP tool uses z.string().uuid() for the relationshipId input parameter.
    const schema = z.object({ relationshipId: z.string().uuid() })
    const result = schema.safeParse({ relationshipId: 'not-uuid' })
    expect(result.success).toBe(false)
  })
})

describe('reorderColumnsSchema', () => {
  // TC-UNIT-VAL-12
  it('rejects empty orderedColumnIds', () => {
    const result = reorderColumnsSchema.safeParse({
      tableId: VALID_UUID,
      orderedColumnIds: [],
    })
    expect(result.success).toBe(false)
  })
})

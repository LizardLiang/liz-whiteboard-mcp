// src/mcp/__tests__/unit/schema-summary.test.ts
// Suite D: Unit Tests — Schema Summary
// TC-UNIT-SUMMARY-01, TC-UNIT-SUMMARY-02

import { describe, expect, it } from 'vitest'
import { formatSchemaSummary } from '../../schema-summary'
import type { WhiteboardWithDiagram } from '@/data/whiteboard'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeColumn(overrides: Record<string, unknown> = {}) {
  return {
    id: 'col-id-1',
    tableId: 'table-id-1',
    name: 'id',
    dataType: 'uuid',
    isPrimaryKey: false,
    isForeignKey: false,
    isUnique: false,
    isNullable: false,
    description: null,
    order: 0,
    createdAt: new Date(),
    updatedAt: new Date(),
    ...overrides,
  }
}

function makeRelationship(overrides: Record<string, unknown> = {}) {
  return {
    id: 'rel-id-1',
    whiteboardId: 'wb-id',
    sourceTableId: 'table-id-1',
    targetTableId: 'table-id-2',
    sourceColumnId: 'col-id-1',
    targetColumnId: 'col-id-2',
    cardinality: 'ONE_TO_MANY',
    label: null,
    routingPoints: null,
    createdAt: new Date(),
    updatedAt: new Date(),
    ...overrides,
  }
}

function makeTable(
  id: string,
  name: string,
  columns: Array<ReturnType<typeof makeColumn>> = [],
  rels: Array<ReturnType<typeof makeRelationship>> = [],
) {
  return {
    id,
    whiteboardId: 'wb-id',
    name,
    description: null,
    positionX: 100,
    positionY: 100,
    width: null,
    height: null,
    createdAt: new Date(),
    updatedAt: new Date(),
    columns,
    outgoingRelationships: rels,
    incomingRelationships: [],
  }
}

function makeBoard(
  tables: Array<ReturnType<typeof makeTable>>,
): WhiteboardWithDiagram {
  return {
    id: 'wb-id',
    name: 'Test Board',
    projectId: 'proj-id',
    folderId: null,
    canvasState: null,
    textSource: null,
    createdAt: new Date(),
    updatedAt: new Date(),
    tables,
  } as unknown as WhiteboardWithDiagram
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('formatSchemaSummary', () => {
  // TC-UNIT-SUMMARY-01
  it('compact format omits UUIDs and positions', () => {
    const cols = [
      makeColumn({
        id: 'col-id-1',
        tableId: 'table-id-1',
        name: 'id',
        dataType: 'uuid',
        isPrimaryKey: true,
      }),
      makeColumn({
        id: 'col-id-2',
        tableId: 'table-id-1',
        name: 'email',
        dataType: 'varchar',
        isForeignKey: false,
      }),
      makeColumn({
        id: 'col-id-3',
        tableId: 'table-id-1',
        name: 'is_active',
        dataType: 'boolean',
        isNullable: true,
      }),
    ]
    const orderUserIdCol = makeColumn({
      id: 'col-id-4',
      tableId: 'table-id-2',
      name: 'user_id',
      dataType: 'uuid',
      isForeignKey: true,
    })
    const rel = makeRelationship({
      sourceColumnId: 'col-id-1',
      targetColumnId: 'col-id-4',
      targetTableId: 'table-id-2',
    })
    const t1 = makeTable('table-id-1', 'users', cols, [rel])
    const t2 = makeTable('table-id-2', 'orders', [orderUserIdCol])

    const summary = formatSchemaSummary(makeBoard([t1, t2]))

    // Should contain table and column names
    expect(summary).toContain('TABLE users')
    expect(summary).toContain('TABLE orders')
    expect(summary).toContain('id uuid')
    expect(summary).toContain('[PK]')
    expect(summary).toContain('[FK]')
    expect(summary).toContain('[N]')
    expect(summary).toContain('RELATIONSHIP')

    // Must NOT contain UUIDs
    expect(summary).not.toContain('table-id-1')
    expect(summary).not.toContain('col-id-1')
    expect(summary).not.toContain('wb-id')

    // Must NOT contain positionX/Y keywords
    expect(summary).not.toContain('positionX')
    expect(summary).not.toContain('positionY')
  })

  // TC-UNIT-SUMMARY-02
  it('50-table board produces output under 16000 characters', () => {
    const tables = []
    const relList: Array<ReturnType<typeof makeRelationship>> = []

    for (let i = 0; i < 50; i++) {
      const cols = []
      for (let j = 0; j < 10; j++) {
        cols.push(
          makeColumn({
            id: `col-${i}-${j}`,
            tableId: `table-${i}`,
            name: `column_${j}`,
            dataType: j === 0 ? 'uuid' : j === 1 ? 'varchar' : 'int',
            isPrimaryKey: j === 0,
            isForeignKey: j === 1,
            isUnique: j === 2,
            isNullable: j > 2,
          }),
        )
      }
      if (i > 0) {
        relList.push(
          makeRelationship({
            id: `rel-${i}`,
            sourceTableId: `table-${i - 1}`,
            targetTableId: `table-${i}`,
            sourceColumnId: `col-${i - 1}-0`,
            targetColumnId: `col-${i}-1`,
          }),
        )
      }
      tables.push(
        makeTable(
          `table-${i}`,
          `table_${i}`,
          cols,
          i > 0 ? [relList[i - 1]] : [],
        ),
      )
    }

    const board = makeBoard(tables)
    const summary = formatSchemaSummary(board)

    // Under 16000 chars (conservative 4 chars/token proxy for 4000 tokens)
    expect(summary.length).toBeLessThan(16000)
  })
})

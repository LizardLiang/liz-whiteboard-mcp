// src/mcp/__tests__/handlers/relationship-integrity.test.ts
// Suite H: Referential Integrity Tests
// TC-UNIT-REFINT-01 through TC-UNIT-REFINT-06

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { prisma } from '@/db'

// Mock Prisma BEFORE importing the module under test
vi.mock('@/db', () => ({
  prisma: {
    column: {
      findUnique: vi.fn(),
    },
    diagramTable: {
      findMany: vi.fn(),
    },
    relationship: {
      findUnique: vi.fn(),
      update: vi.fn(),
    },
  },
}))

// Use valid RFC 4122 v4 UUIDs throughout
const T1 = '11111111-1111-4111-8111-111111111111'
const T2 = '22222222-2222-4222-8222-222222222222'
const T3 = '33333333-3333-4333-8333-333333333333'
const T99 = '99999999-9999-4999-8999-999999999999'
const C1 = 'a1111111-1111-4111-8111-111111111111'
const C2 = 'a2222222-2222-4222-8222-222222222222'
const C3 = 'a3333333-3333-4333-8333-333333333333'
const WB = 'ab111111-1111-4111-8111-111111111111'

describe('assertRelationshipEndpointsValid', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // TC-UNIT-REFINT-01
  it('source column wrong table — throws with specific message', async () => {
    // sourceColumn belongs to T99, not T1
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(prisma.column.findUnique as any).mockImplementation(
      async ({ where }: { where: { id: string } }) => {
        if (where.id === C1) return { id: C1, tableId: T99 }
        if (where.id === C2) return { id: C2, tableId: T2 }
        return null
      },
    )
    vi.mocked(prisma.diagramTable.findMany).mockResolvedValue([
      { id: T1 },
      { id: T2 },
    ] as any)

    const { assertRelationshipEndpointsValid } = await import(
      '@/data/relationship'
    )

    await expect(
      assertRelationshipEndpointsValid({
        sourceTableId: T1,
        targetTableId: T2,
        sourceColumnId: C1,
        targetColumnId: C2,
        whiteboardId: WB,
      }),
    ).rejects.toThrow(
      `sourceColumnId ${C1} does not belong to sourceTableId ${T1}`,
    )
  })

  // TC-UNIT-REFINT-02
  it('target column wrong table — throws with specific message', async () => {
    // sourceColumn OK, targetColumn belongs to T99 not T2
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(prisma.column.findUnique as any).mockImplementation(
      async ({ where }: { where: { id: string } }) => {
        if (where.id === C1) return { id: C1, tableId: T1 }
        if (where.id === C2) return { id: C2, tableId: T99 } // wrong table
        return null
      },
    )
    vi.mocked(prisma.diagramTable.findMany).mockResolvedValue([
      { id: T1 },
      { id: T2 },
    ] as any)

    const { assertRelationshipEndpointsValid } = await import(
      '@/data/relationship'
    )

    await expect(
      assertRelationshipEndpointsValid({
        sourceTableId: T1,
        targetTableId: T2,
        sourceColumnId: C1,
        targetColumnId: C2,
        whiteboardId: WB,
      }),
    ).rejects.toThrow(
      `targetColumnId ${C2} does not belong to targetTableId ${T2}`,
    )
  })

  // TC-UNIT-REFINT-05: valid endpoint change succeeds
  it('valid endpoints pass validation', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(prisma.column.findUnique as any).mockImplementation(
      async ({ where }: { where: { id: string } }) => {
        if (where.id === C1) return { id: C1, tableId: T1 }
        if (where.id === C2) return { id: C2, tableId: T2 }
        return null
      },
    )
    vi.mocked(prisma.diagramTable.findMany).mockResolvedValue([
      { id: T1 },
      { id: T2 },
    ] as any)

    const { assertRelationshipEndpointsValid } = await import(
      '@/data/relationship'
    )

    await expect(
      assertRelationshipEndpointsValid({
        sourceTableId: T1,
        targetTableId: T2,
        sourceColumnId: C1,
        targetColumnId: C2,
        whiteboardId: WB,
      }),
    ).resolves.toBeUndefined()
  })

  // TC-UNIT-REFINT-06: createRelationship data layer regression
  it('validator is reused in create path — wrong source column is rejected', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(prisma.column.findUnique as any).mockImplementation(
      async ({ where }: { where: { id: string } }) => {
        // C1 belongs to T99, not T1
        if (where.id === C1) return { id: C1, tableId: T99 }
        if (where.id === C2) return { id: C2, tableId: T2 }
        return null
      },
    )
    vi.mocked(prisma.diagramTable.findMany).mockResolvedValue([
      { id: T1 },
      { id: T2 },
    ] as any)

    const { createRelationship } = await import('@/data/relationship')

    await expect(
      createRelationship({
        whiteboardId: WB,
        sourceTableId: T1,
        targetTableId: T2,
        sourceColumnId: C1,
        targetColumnId: C2,
        cardinality: 'ONE_TO_MANY',
      }),
    ).rejects.toThrow(
      /sourceColumnId.*does not belong to sourceTableId|Failed to create relationship.*sourceColumnId/,
    )
  })
})

describe('relationship:update handler merged-endpoint validation (TC-UNIT-REFINT-03, REFINT-04)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // TC-UNIT-REFINT-03: changing sourceColumnId to wrong table is rejected
  it('patch sourceColumnId to wrong table fails merged validation', async () => {
    // C3 belongs to T99, not T1
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(prisma.column.findUnique as any).mockImplementation(
      async ({ where }: { where: { id: string } }) => {
        if (where.id === C3) return { id: C3, tableId: T99 }
        if (where.id === C2) return { id: C2, tableId: T2 }
        return null
      },
    )
    vi.mocked(prisma.diagramTable.findMany).mockResolvedValue([
      { id: T1 },
      { id: T2 },
    ] as any)

    const { assertRelationshipEndpointsValid } = await import(
      '@/data/relationship'
    )

    // Merged: sourceTableId=T1, targetTableId=T2, sourceColumnId=C3 (patch), targetColumnId=C2 (existing)
    await expect(
      assertRelationshipEndpointsValid({
        sourceTableId: T1,
        targetTableId: T2,
        sourceColumnId: C3, // patched — wrong table
        targetColumnId: C2, // existing
        whiteboardId: WB,
      }),
    ).rejects.toThrow(
      `sourceColumnId ${C3} does not belong to sourceTableId ${T1}`,
    )
  })

  // TC-UNIT-REFINT-04: patch only targetTableId while keeping targetColumnId (wrong after merge)
  it('patch targetTableId but keep old targetColumnId fails merged validation', async () => {
    // C1 OK for T1; C2 belongs to T2 (not T3)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(prisma.column.findUnique as any).mockImplementation(
      async ({ where }: { where: { id: string } }) => {
        if (where.id === C1) return { id: C1, tableId: T1 }
        if (where.id === C2) return { id: C2, tableId: T2 } // C2 belongs to T2, not T3
        return null
      },
    )
    vi.mocked(prisma.diagramTable.findMany).mockResolvedValue([
      { id: T1 },
      { id: T3 },
    ] as any)

    const { assertRelationshipEndpointsValid } = await import(
      '@/data/relationship'
    )

    // Merged: sourceTableId=T1, targetTableId=T3 (patch), sourceColumnId=C1, targetColumnId=C2 (existing)
    await expect(
      assertRelationshipEndpointsValid({
        sourceTableId: T1,
        targetTableId: T3, // patched
        sourceColumnId: C1,
        targetColumnId: C2, // existing — wrong for T3
        whiteboardId: WB,
      }),
    ).rejects.toThrow(
      `targetColumnId ${C2} does not belong to targetTableId ${T3}`,
    )
  })
})

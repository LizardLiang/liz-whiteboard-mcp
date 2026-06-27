// src/mcp/__tests__/unit/positioning.test.ts
// Suite C: Unit Tests — Positioning
// TC-UNIT-POS-01 through TC-UNIT-POS-03

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { computeDefaultPosition } from '../../positioning'
import { prisma } from '@/db'

// Mock Prisma
vi.mock('@/db', () => ({
  prisma: {
    diagramTable: {
      count: vi.fn(),
    },
  },
}))

const COL_W = 280
const ROW_H = 220

describe('computeDefaultPosition', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // TC-UNIT-POS-01
  it('first table placed at grid origin (count=0)', async () => {
    vi.mocked(prisma.diagramTable.count).mockResolvedValue(0)

    const pos = await computeDefaultPosition('wb-uuid')

    expect(pos).toEqual({ positionX: 40, positionY: 40 })
  })

  // TC-UNIT-POS-02
  it('fifth table wraps to second row (count=4)', async () => {
    vi.mocked(prisma.diagramTable.count).mockResolvedValue(4)

    const pos = await computeDefaultPosition('wb-uuid')

    // col 0, row 1: x = 40 + 0*280 = 40, y = 40 + 1*220 = 260
    expect(pos).toEqual({ positionX: 40, positionY: 260 })
  })

  // TC-UNIT-POS-03
  it('no two auto-placed tables overlap for counts 0-15', async () => {
    const positions: Array<{ positionX: number; positionY: number }> = []

    for (let count = 0; count < 16; count++) {
      vi.mocked(prisma.diagramTable.count).mockResolvedValue(count)
      const pos = await computeDefaultPosition('wb-uuid')
      positions.push(pos)
    }

    // Check no two bounding boxes overlap.
    // Bounding box: [x, x+COL_W) × [y, y+ROW_H)
    for (let i = 0; i < positions.length; i++) {
      for (let j = i + 1; j < positions.length; j++) {
        const a = positions[i]
        const b = positions[j]
        // X overlap: a.x < b.x + W AND b.x < a.x + W
        // Y overlap: a.y < b.y + H AND b.y < a.y + H
        const xOverlap =
          a.positionX < b.positionX + COL_W && b.positionX < a.positionX + COL_W
        const yOverlap =
          a.positionY < b.positionY + ROW_H && b.positionY < a.positionY + ROW_H
        expect(xOverlap && yOverlap).toBe(false)
      }
    }
  })

  it('second table in first row at correct position (count=1)', async () => {
    vi.mocked(prisma.diagramTable.count).mockResolvedValue(1)
    const pos = await computeDefaultPosition('wb-uuid')
    // col 1, row 0: x = 40 + 1*280 = 320, y = 40
    expect(pos).toEqual({ positionX: 320, positionY: 40 })
  })

  it('fourth table (last in first row) at correct position (count=3)', async () => {
    vi.mocked(prisma.diagramTable.count).mockResolvedValue(3)
    const pos = await computeDefaultPosition('wb-uuid')
    // col 3, row 0: x = 40 + 3*280 = 880, y = 40
    expect(pos).toEqual({ positionX: 880, positionY: 40 })
  })
})

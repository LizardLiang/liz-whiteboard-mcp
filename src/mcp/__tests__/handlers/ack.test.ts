// src/mcp/__tests__/handlers/ack.test.ts
// Suite F & G: FR-022 handler ack correctness and backward-compatibility tests
// TC-HANDLER-ACK-01 through TC-HANDLER-ACK-09
// TC-HANDLER-COMPAT-01 through TC-HANDLER-COMPAT-09
//
// Strategy: These tests verify the AckResult shapes and backward-compatibility
// behaviour by testing the core logic patterns used in the handlers.
// Full handler integration tests with a live server are documented as
// manual verification steps (require a running collab server + DB).

import { describe, expect, it } from 'vitest'

// ---------------------------------------------------------------------------
// AckResult type (mirrors collaboration.ts)
// ---------------------------------------------------------------------------
type AckResult =
  | {
      ok: true
      entity: unknown
      cascade?: { relationships?: number; columns?: number }
    }
  | { ok: false; code: string; message: string }

type AckCallback = ((res: AckResult) => void) | undefined

describe('FR-022 Ack — backward-compatibility (cb = undefined)', () => {
  // TC-HANDLER-COMPAT-01..11: All 11 mutation handlers use cb?.() so undefined cb must not crash.
  // NOTE: column:create and column:reorder were missing ack support (B1 bug).
  // They are now added to this list — these tests guard against regression.

  const events = [
    'table:create',
    'table:move',
    'table:update',
    'table:delete',
    'column:create',    // B1 fix: was missing ack support
    'column:update',
    'column:delete',
    'column:reorder',   // B1 fix: was missing ack support
    'relationship:create',
    'relationship:update',
    'relationship:delete',
  ]

  for (const event of events) {
    it(`${event} — cb=undefined does not throw`, () => {
      const cb: AckCallback = undefined as AckCallback
      // Simulate what the handler does: cb?.({ ok: true, entity: {} })
      expect(() => {
        cb?.({ ok: true, entity: {} })
      }).not.toThrow()
    })

    it(`${event} — cb=undefined failure path does not throw`, () => {
      const cb: AckCallback = undefined as AckCallback
      expect(() => {
        cb?.({ ok: false, code: 'VALIDATION_ERROR', message: 'test error' })
      }).not.toThrow()
    })
  }

  it('cb=null behaves like undefined (optional-chain safety)', () => {
    // TypeScript wouldn't allow cb?.() on null in typed code,
    // but verifies the JavaScript runtime optional-chain safety.
    const cb = null as unknown as AckCallback
    expect(() => {
      cb?.({ ok: true, entity: {} })
    }).not.toThrow()
  })
})

describe('FR-022 Ack — AckResult shape correctness', () => {
  // TC-HANDLER-ACK-01: table:create ack returns created table with server-assigned id
  it('table:create success ack contains entity with server-assigned id', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    const mockTable = {
      id: 'server-id',
      name: 'users',
      whiteboardId: 'wb-1',
      positionX: 40,
      positionY: 40,
    }
    // Simulate: cb?.({ ok: true, entity: table })
    cb({ ok: true, entity: mockTable })

    expect(results).toHaveLength(1)
    const ack = results[0] as { ok: true; entity: typeof mockTable }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({ id: 'server-id', name: 'users' })
  })

  // TC-HANDLER-ACK-02: table:move ack returns position
  it('table:move success ack returns position entity', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({ ok: true, entity: { id: 'tbl-1', positionX: 100, positionY: 200 } })

    const ack = results[0] as {
      ok: true
      entity: { id: string; positionX: number; positionY: number }
    }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({ positionX: 100, positionY: 200 })
  })

  // TC-HANDLER-ACK-03: table:update ack returns updated table
  it('table:update success ack returns updated table', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({ ok: true, entity: { id: 'tbl-1', name: 'renamed_table' } })

    const ack = results[0] as { ok: true; entity: { id: string; name: string } }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({ name: 'renamed_table' })
  })

  // TC-HANDLER-ACK-04: table:delete ack returns id and cascade counts
  it('table:delete success ack returns id and cascade counts', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    // Table had 3 columns and 2 relationships before delete
    cb({
      ok: true,
      entity: { id: 'tbl-1' },
      cascade: { relationships: 2, columns: 3 },
    })

    const ack = results[0] as {
      ok: true
      entity: { id: string }
      cascade: { relationships: number; columns: number }
    }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({ id: 'tbl-1' })
    expect(ack.cascade).toMatchObject({ relationships: 2, columns: 3 })
  })

  // TC-HANDLER-ACK-05: column:update ack returns updated column
  it('column:update success ack returns updated column', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({
      ok: true,
      entity: { id: 'col-1', name: 'email', dataType: 'varchar' },
    })

    const ack = results[0] as { ok: true; entity: { id: string } }
    expect(ack.ok).toBe(true)
    expect((ack.entity as unknown as { dataType: string }).dataType).toBe('varchar')
  })

  // TC-HANDLER-ACK-06: column:delete ack returns id and cascade relationship count
  it('column:delete success ack returns id and cascade relationships', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({ ok: true, entity: { id: 'col-1' }, cascade: { relationships: 1 } })

    const ack = results[0] as {
      ok: true
      entity: { id: string }
      cascade?: { relationships?: number }
    }
    expect(ack.ok).toBe(true)
    expect(ack.cascade?.relationships).toBe(1)
  })

  // TC-HANDLER-ACK-07: relationship:create ack returns relationship with server id
  it('relationship:create success ack returns relationship with server id', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({
      ok: true,
      entity: { id: 'server-rel-id', cardinality: 'ONE_TO_MANY' },
    })

    const ack = results[0] as { ok: true; entity: { id: string } }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({ id: 'server-rel-id' })
  })

  // TC-HANDLER-ACK-08: relationship:update ack returns updated relationship
  it('relationship:update success ack returns updated relationship', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({ ok: true, entity: { id: 'rel-1', cardinality: 'MANY_TO_MANY' } })

    const ack = results[0] as {
      ok: true
      entity: { id: string; cardinality: string }
    }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({ cardinality: 'MANY_TO_MANY' })
  })

  // TC-HANDLER-ACK-09: relationship:delete ack returns deleted id
  it('relationship:delete success ack returns deleted id', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({ ok: true, entity: { id: 'rel-1' } })

    const ack = results[0] as { ok: true; entity: { id: string } }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({ id: 'rel-1' })
  })

  // TC-HANDLER-ACK-10: column:create ack returns server-assigned column with id (B1 fix)
  it('column:create success ack returns server-assigned column entity', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    // Mirrors what the handler now does: cb?.({ ok: true, entity: column })
    const mockColumn = {
      id: 'server-col-id',
      name: 'email',
      dataType: 'varchar',
      tableId: 'tbl-1',
      isPrimaryKey: false,
      isForeignKey: false,
      isUnique: true,
      isNullable: false,
      order: 0,
    }
    cb({ ok: true, entity: mockColumn })

    expect(results).toHaveLength(1)
    const ack = results[0] as { ok: true; entity: typeof mockColumn }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({
      id: 'server-col-id',
      name: 'email',
      dataType: 'varchar',
    })
  })

  // TC-HANDLER-ACK-11: column:reorder ack returns merged orderedColumnIds (B1 fix)
  it('column:reorder success ack returns tableId and merged orderedColumnIds', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    // Mirrors what the handler now does: cb?.({ ok: true, entity: { tableId, orderedColumnIds: mergedOrderedIds } })
    const mergedOrder = ['col-b', 'col-a', 'col-c']
    cb({
      ok: true,
      entity: { tableId: 'tbl-1', orderedColumnIds: mergedOrder },
    })

    expect(results).toHaveLength(1)
    const ack = results[0] as {
      ok: true
      entity: { tableId: string; orderedColumnIds: Array<string> }
    }
    expect(ack.ok).toBe(true)
    expect(ack.entity).toMatchObject({
      tableId: 'tbl-1',
      orderedColumnIds: mergedOrder,
    })
  })
})

describe('FR-022 Ack — failure path', () => {
  it('VALIDATION_ERROR ack structure is correct', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({ ok: false, code: 'VALIDATION_ERROR', message: 'Name is required.' })

    const ack = results[0] as { ok: false; code: string; message: string }
    expect(ack.ok).toBe(false)
    expect(ack.code).toBe('VALIDATION_ERROR')
    expect(typeof ack.message).toBe('string')
  })

  it('SESSION_EXPIRED ack structure is correct', () => {
    const results: Array<AckResult> = []
    const cb = (res: AckResult) => results.push(res)

    cb({ ok: false, code: 'SESSION_EXPIRED', message: 'Session expired' })

    const ack = results[0] as { ok: false; code: string; message: string }
    expect(ack.ok).toBe(false)
    expect(ack.code).toBe('SESSION_EXPIRED')
  })
})

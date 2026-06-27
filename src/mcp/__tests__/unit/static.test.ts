// src/mcp/__tests__/unit/static.test.ts
// Suite I: Static Enum Tools
// TC-UNIT-STATIC-01, TC-UNIT-STATIC-02

import { describe, expect, it } from 'vitest'
import { cardinalitySchema, dataTypeSchema } from '@/data/schema'

describe('list_data_types', () => {
  // TC-UNIT-STATIC-01
  it('returns exactly 25 values from dataTypeSchema.options', () => {
    const options = dataTypeSchema.options
    expect(options).toHaveLength(25)
  })

  it('each value is a non-empty string', () => {
    for (const opt of dataTypeSchema.options) {
      expect(typeof opt).toBe('string')
      expect(opt.length).toBeGreaterThan(0)
    }
  })
})

describe('list_cardinalities', () => {
  // TC-UNIT-STATIC-02
  it('returns exactly 17 values from cardinalitySchema.options', () => {
    const options = cardinalitySchema.options
    expect(options).toHaveLength(17)
  })

  it('each value is a non-empty string', () => {
    for (const opt of cardinalitySchema.options) {
      expect(typeof opt).toBe('string')
      expect(opt.length).toBeGreaterThan(0)
    }
  })
})

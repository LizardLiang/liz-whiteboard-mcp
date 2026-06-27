// src/mcp/schema-summary.ts
// Formats a WhiteboardWithDiagram into a compact text summary for get_schema_summary.
// Output: one line per table header + one line per column + one line per relationship.
// No UUIDs, no positions. Target: < 4000 tokens (≈ 16000 chars) for 50 tables.

import type { WhiteboardWithDiagram } from '@/data/whiteboard'
import type { Column, Relationship } from '@prisma/client'

// ---------------------------------------------------------------------------
// Column flags
// ---------------------------------------------------------------------------

function columnFlags(col: Column): string {
  const flags: Array<string> = []
  if (col.isPrimaryKey) flags.push('PK')
  if (col.isForeignKey) flags.push('FK')
  if (col.isUnique) flags.push('U')
  if (col.isNullable) flags.push('N')
  return flags.length > 0 ? ` [${flags.join(',')}]` : ''
}

// ---------------------------------------------------------------------------
// Main formatter
// ---------------------------------------------------------------------------

/**
 * Format a board into a compact multi-line schema summary.
 *
 * Format:
 *   TABLE <name>
 *     <colName> <dataType>[flags]
 *     ...
 *   RELATIONSHIP <srcTable>.<srcCol> →(<cardinality>) <tgtTable>.<tgtCol>
 */
export function formatSchemaSummary(board: WhiteboardWithDiagram): string {
  const lines: Array<string> = []

  // Build lookup maps.
  const tableById = new Map(board.tables.map((t) => [t.id, t]))
  const columnById = new Map(
    board.tables.flatMap((t) => t.columns.map((c) => [c.id, c])),
  )

  // Collect all relationships (dedup by id).
  const relById = new Map<string, Relationship>()
  for (const table of board.tables) {
    for (const rel of table.outgoingRelationships) {
      relById.set(rel.id, rel)
    }
    for (const rel of table.incomingRelationships) {
      relById.set(rel.id, rel)
    }
  }

  // Tables.
  for (const table of board.tables) {
    lines.push(`TABLE ${table.name}`)
    for (const col of table.columns) {
      lines.push(`  ${col.name} ${col.dataType}${columnFlags(col)}`)
    }
  }

  // Relationships.
  if (relById.size > 0) {
    lines.push('')
    for (const rel of relById.values()) {
      const srcTable = tableById.get(rel.sourceTableId)
      const tgtTable = tableById.get(rel.targetTableId)
      const srcCol = columnById.get(rel.sourceColumnId)
      const tgtCol = columnById.get(rel.targetColumnId)
      const srcName = srcTable ? srcTable.name : rel.sourceTableId
      const tgtName = tgtTable ? tgtTable.name : rel.targetTableId
      const srcColName = srcCol ? srcCol.name : rel.sourceColumnId
      const tgtColName = tgtCol ? tgtCol.name : rel.targetColumnId
      const label = rel.label ? ` "${rel.label}"` : ''
      lines.push(
        `RELATIONSHIP ${srcName}.${srcColName} →(${rel.cardinality}) ${tgtName}.${tgtColName}${label}`,
      )
    }
  }

  return lines.join('\n')
}

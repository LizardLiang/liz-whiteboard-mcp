// scripts/migrate-pg-to-sqlite.ts
//
// One-off migration: read the legacy production Postgres database (via Prisma,
// using the postgresql schema in this repo's prisma/schema.prisma) and write a
// fresh SQLite `app.db` matching the app's canonical SQLite schema.
//
// Usage:
//   bunx prisma generate
//   DATABASE_URL='postgres://USER:PASS@db.prisma.io:5432/postgres?sslmode=require' \
//   DIRECT_DATABASE_URL="$DATABASE_URL" \
//   bun scripts/migrate-pg-to-sqlite.ts [outfile]
//
// Default outfile: ./app.db (overwritten if present).
// Read-only against Postgres. Aborts non-zero if any table's written row count
// does not match the source.

import { Database } from 'bun:sqlite'
import { PrismaClient } from '@prisma/client'
import { rmSync, existsSync } from 'node:fs'
// Canonical SQLite DDL lives in the app repo (sibling checkout).
import { SCHEMA_SQL } from '../../liz-whiteboard/src/data/schema-sql.ts'

const outfile = process.argv[2] ?? './app.db'

// ── value coercers (Postgres → SQLite storage formats) ──────────────────────
const d = (v: unknown) => (v == null ? null : new Date(v as string | Date).getTime()) // DATETIME → unix-ms INTEGER
const b = (v: unknown) => (v ? 1 : 0) // BOOLEAN → 0/1
const j = (v: unknown) => (v == null ? null : JSON.stringify(v)) // JSON/JSONB → TEXT

// Each table: ordered SQLite columns + a mapper from a Prisma row to the value tuple.
// Order matters for FK integrity on insert.
const TABLES: Array<{
  name: string
  fetch: (p: PrismaClient) => Promise<any[]>
  cols: string[]
  row: (r: any) => unknown[]
}> = [
  {
    name: 'User',
    fetch: (p) => p.user.findMany(),
    cols: ['id', 'username', 'email', 'passwordHash', 'failedLoginAttempts', 'lockedUntil', 'createdAt', 'updatedAt'],
    row: (r) => [r.id, r.username, r.email, r.passwordHash, r.failedLoginAttempts, d(r.lockedUntil), d(r.createdAt), d(r.updatedAt)],
  },
  {
    name: 'Session',
    fetch: (p) => p.session.findMany(),
    cols: ['id', 'tokenHash', 'userId', 'expiresAt', 'createdAt'],
    row: (r) => [r.id, r.tokenHash, r.userId, d(r.expiresAt), d(r.createdAt)],
  },
  {
    name: 'Project',
    fetch: (p) => p.project.findMany(),
    cols: ['id', 'name', 'description', 'createdAt', 'updatedAt', 'ownerId'],
    row: (r) => [r.id, r.name, r.description, d(r.createdAt), d(r.updatedAt), r.ownerId],
  },
  {
    name: 'ProjectMember',
    fetch: (p) => p.projectMember.findMany(),
    cols: ['id', 'projectId', 'userId', 'role', 'createdAt', 'updatedAt'],
    row: (r) => [r.id, r.projectId, r.userId, r.role, d(r.createdAt), d(r.updatedAt)],
  },
  {
    name: 'Folder',
    fetch: (p) => p.folder.findMany(),
    cols: ['id', 'name', 'projectId', 'parentFolderId', 'createdAt', 'updatedAt'],
    row: (r) => [r.id, r.name, r.projectId, r.parentFolderId, d(r.createdAt), d(r.updatedAt)],
  },
  {
    name: 'Whiteboard',
    fetch: (p) => p.whiteboard.findMany(),
    cols: ['id', 'name', 'projectId', 'folderId', 'canvasState', 'textSource', 'createdAt', 'updatedAt'],
    row: (r) => [r.id, r.name, r.projectId, r.folderId, j(r.canvasState), r.textSource, d(r.createdAt), d(r.updatedAt)],
  },
  {
    name: 'DiagramTable',
    fetch: (p) => p.diagramTable.findMany(),
    cols: ['id', 'whiteboardId', 'name', 'description', 'positionX', 'positionY', 'width', 'height', 'createdAt', 'updatedAt'],
    row: (r) => [r.id, r.whiteboardId, r.name, r.description, r.positionX, r.positionY, r.width, r.height, d(r.createdAt), d(r.updatedAt)],
  },
  {
    name: 'Column',
    fetch: (p) => p.column.findMany(),
    cols: ['id', 'tableId', 'name', 'dataType', 'isPrimaryKey', 'isForeignKey', 'isUnique', 'isNullable', 'description', 'order', 'createdAt', 'updatedAt'],
    row: (r) => [r.id, r.tableId, r.name, r.dataType, b(r.isPrimaryKey), b(r.isForeignKey), b(r.isUnique), b(r.isNullable), r.description, r.order, d(r.createdAt), d(r.updatedAt)],
  },
  {
    name: 'Relationship',
    fetch: (p) => p.relationship.findMany(),
    cols: ['id', 'whiteboardId', 'sourceTableId', 'targetTableId', 'sourceColumnId', 'targetColumnId', 'cardinality', 'label', 'routingPoints', 'createdAt', 'updatedAt'],
    row: (r) => [r.id, r.whiteboardId, r.sourceTableId, r.targetTableId, r.sourceColumnId, r.targetColumnId, r.cardinality, r.label, j(r.routingPoints), d(r.createdAt), d(r.updatedAt)],
  },
  // CollaborationSession intentionally skipped — ephemeral live-presence rows.
]

async function main() {
  if (!process.env.DATABASE_URL) {
    console.error('❌ DATABASE_URL must be set to the direct postgres:// URL')
    process.exit(1)
  }

  if (existsSync(outfile)) {
    console.log(`⚠️  removing existing ${outfile}`)
    rmSync(outfile)
  }

  const sqlite = new Database(outfile, { create: true })
  sqlite.exec('PRAGMA foreign_keys = OFF;') // defer FK checks during bulk insert
  sqlite.exec(SCHEMA_SQL)

  const prisma = new PrismaClient()
  let failures = 0

  try {
    for (const t of TABLES) {
      const rows = await t.fetch(prisma)
      const placeholders = t.cols.map(() => '?').join(', ')
      const quotedCols = t.cols.map((c) => `"${c}"`).join(', ')
      const stmt = sqlite.prepare(`INSERT INTO "${t.name}" (${quotedCols}) VALUES (${placeholders})`)

      const insertAll = sqlite.transaction((items: any[]) => {
        for (const r of items) stmt.run(...(t.row(r) as any[]))
      })
      insertAll(rows)

      const written = (sqlite.query(`SELECT COUNT(*) AS n FROM "${t.name}"`).get() as { n: number }).n
      const ok = written === rows.length
      if (!ok) failures++
      console.log(`${ok ? '✅' : '❌'} ${t.name.padEnd(16)} source=${rows.length}  written=${written}`)
    }
  } finally {
    await prisma.$disconnect()
    sqlite.exec('PRAGMA foreign_keys = ON;')
    sqlite.close()
  }

  if (failures > 0) {
    console.error(`\n❌ ${failures} table(s) had a row-count mismatch — migration NOT trustworthy.`)
    process.exit(1)
  }
  console.log(`\n🎉 Migration complete → ${outfile}`)
}

main().catch((e) => {
  console.error('❌ Migration failed:', e)
  process.exit(1)
})

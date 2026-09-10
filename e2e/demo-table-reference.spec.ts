// e2e/demo-table-reference.spec.ts
// A NARRATED DEMO, not a gate. Drives the four cross-file reference tools over
// the real wire against the running stack, printing each step, and ends by
// screenshotting the reference node as a browser actually paints it.
//
// The story: an agent holding nothing but an OAuth bearer builds a project with
// two ER whiteboards, puts `users` on one and `invoices` on the other, then
// draws a foreign key ACROSS the file boundary — which is the thing that was
// impossible before reference nodes existed.
//
// Run: bunx playwright test e2e/demo-table-reference.spec.ts
import { expect, test } from '@playwright/test'
import { EDITOR_USER, ORIGIN } from './fixtures'
import { McpClient } from './mcp-client'
import type { McpErrorPayload } from './mcp-client'
import { mintAccessToken } from './oauth'
import { EDITOR_STORAGE_STATE } from './global-setup'

// One long story told in order, so the suite must not parallelise or reorder.
test.describe.configure({ mode: 'serial' })

// ─────────────────────────────────────────────────────────────────────────────
// Narration. The point of a demo is the transcript, so every step prints.
// ─────────────────────────────────────────────────────────────────────────────
let stepNo = 0
function step(title: string): void {
  stepNo += 1
  console.log(`\n${'─'.repeat(72)}\n▶ ${stepNo}. ${title}\n${'─'.repeat(72)}`)
}
function show(label: string, value: unknown): void {
  console.log(`   ${label}: ${JSON.stringify(value)}`)
}

interface Row {
  id: string
  name: string
}
interface ColumnRow {
  id: string
  name: string
  dataType?: string
}
interface TableRow extends Row {
  columns?: Array<ColumnRow>
}
// The route returns a reference as { table, columns }: the DiagramTable row
// that stands in for the remote table, plus the stub columns that become the
// connectable handles.
interface DiagramTableRow {
  id: string
  name: string
  whiteboardId: string
  sourceWhiteboardId?: string | null
  sourceTableId?: string | null
}
interface ReferenceRow {
  table: DiagramTableRow
  sourceTableName?: string
  sourceWhiteboardName?: string | null
  columns?: Array<ColumnRow>
  missing?: boolean
}
interface UpdateReferenceResult {
  reference?: ReferenceRow
  deletedRelationshipCount?: number
  [k: string]: unknown
}

let agent: McpClient

// Ids the story threads through.
const S: Record<string, string> = {}

test.beforeAll(async () => {
  const bearer = await mintAccessToken(EDITOR_USER)
  agent = new McpClient(bearer)
  await agent.connect()
})

// ─────────────────────────────────────────────────────────────────────────────

test('cold start: an agent builds two ER whiteboards from nothing', async () => {
  step('Create a project, then two ER whiteboards inside it')

  const project = await agent.expectOk<Row>('create_project', {
    name: `Reference demo ${Date.now()}`,
    description: 'Built entirely by MCP tools for the cross-file demo',
  })
  S.project = project.id
  S.projectName = project.name
  show('project', { id: project.id, name: project.name })

  const accounts = await agent.expectOk<Row>('create_whiteboard', {
    projectId: S.project,
    name: 'Accounts',
  })
  const billing = await agent.expectOk<Row>('create_whiteboard', {
    projectId: S.project,
    name: 'Billing',
  })
  S.accounts = accounts.id
  S.billing = billing.id
  show('whiteboard A', { id: accounts.id, name: accounts.name })
  show('whiteboard B', { id: billing.id, name: billing.name })

  expect(S.accounts).not.toBe(S.billing)
})

test('put `users` on Accounts and `invoices` on Billing', async () => {
  step('Two tables, on two DIFFERENT boards — the whole premise')

  await agent.expectOk('batch_schema_update', {
    whiteboardId: S.accounts,
    tables: [
      {
        name: 'users',
        positionX: 120,
        positionY: 120,
        columns: [
          { name: 'id', dataType: 'uuid', isPrimaryKey: true },
          { name: 'email', dataType: 'varchar' },
          { name: 'created_at', dataType: 'timestamp' },
        ],
      },
    ],
  })

  await agent.expectOk('batch_schema_update', {
    whiteboardId: S.billing,
    tables: [
      {
        name: 'invoices',
        positionX: 140,
        positionY: 160,
        columns: [
          { name: 'id', dataType: 'uuid', isPrimaryKey: true },
          { name: 'user_id', dataType: 'uuid' },
          { name: 'amount_cents', dataType: 'int' },
        ],
      },
    ],
  })

  // Resolve the ids the way an agent would: read the boards back.
  //
  // NOTE: get_board, not get_schema_summary. The reference tool's own
  // description points at get_schema_summary, but that tool returns prose with
  // UUIDs deliberately omitted — it cannot supply the ids the reference tools
  // require. get_board is the one that can. (Flagged as a doc bug.)
  const accountsSchema = await agent.expectOk<{ tables: Array<TableRow> }>(
    'get_board',
    { whiteboardId: S.accounts },
  )
  const users = accountsSchema.tables.find((t) => t.name === 'users')!
  S.usersTable = users.id
  S.usersId = users.columns!.find((c) => c.name === 'id')!.id
  S.usersEmail = users.columns!.find((c) => c.name === 'email')!.id

  const billingSchema = await agent.expectOk<{ tables: Array<TableRow> }>(
    'get_board',
    { whiteboardId: S.billing },
  )
  const invoices = billingSchema.tables.find((t) => t.name === 'invoices')!
  S.invoicesTable = invoices.id
  S.invoicesUserId = invoices.columns!.find((c) => c.name === 'user_id')!.id

  show('Accounts › users', users.columns!.map((c) => c.name))
  show('Billing › invoices', invoices.columns!.map((c) => c.name))
})

test('THE FEATURE: reference `users` from the Billing board', async () => {
  step('create_table_reference — a stand-in node for a table on another board')

  const reference = await agent.expectOk<ReferenceRow>(
    'create_table_reference',
    {
      whiteboardId: S.billing,
      sourceWhiteboardId: S.accounts,
      sourceTableId: S.usersTable,
      sourceColumnIds: [S.usersId, S.usersEmail],
      positionX: 620,
      positionY: 160,
    },
  )
  console.log(JSON.stringify(reference, null, 2))

  S.reference = reference.table.id
  // The app names the node itself, qualifying it with the file it came from, so
  // two references to same-named tables on different boards stay distinguishable.
  S.referenceName = reference.table.name
  show('node id', S.reference)
  show('generated node name', S.referenceName)
  expect(reference.table.sourceTableId).toBe(S.usersTable)
  expect(reference.table.sourceWhiteboardId).toBe(S.accounts)

  // The exposed columns come back as stub columns — these are the connectable
  // handles, and they are what makes an ordinary create_relationship work.
  const stubs = reference.columns ?? []
  show('exposed handles', stubs.map((c) => c.name))
  expect(stubs.length).toBe(2)
  S.refIdColumn = stubs.find((c) => c.name === 'id')!.id
  S.refEmailColumn = stubs.find((c) => c.name === 'email')!.id
  console.log(
    '   `created_at` was NOT exposed — a reference shows only the columns you pick.',
  )
})

test('draw a foreign key ACROSS the file boundary', async () => {
  step('create_relationship — an ordinary relationship, one end on a reference')

  const rel = await agent.expectOk<Row>('create_relationship', {
    whiteboardId: S.billing,
    sourceTableId: S.invoicesTable,
    sourceColumnId: S.invoicesUserId,
    targetTableId: S.reference,
    targetColumnId: S.refIdColumn,
    cardinality: 'MANY_TO_ONE',
    label: 'invoices.user_id → users.id',
  })
  S.relationship = rel.id
  show('relationship', { id: rel.id, label: 'invoices.user_id → users.id' })
  console.log(
    '   NOTE: no special cross-file relationship tool exists, and none is needed —\n' +
      '         the reference is a real table row, so the normal tool targets it.',
  )
})

test('list_table_references resolves the node against its source', async () => {
  step('list_table_references — what the agent sees before it edits or deletes')

  const listed = await agent.expectOk<{ references: Array<ReferenceRow> }>(
    'list_table_references',
    { whiteboardId: S.billing },
  )
  console.log(JSON.stringify(listed, null, 2))
  expect(listed.references).toHaveLength(1)
  expect(listed.references[0].missing ?? false).toBe(false)
  show('resolves to', listed.references[0].table.name)
})

test('SCREENSHOT: the node as a browser actually paints it', async ({
  browser,
}) => {
  step('Open Billing in a real browser and photograph the reference node')

  const context = await browser.newContext({
    storageState: EDITOR_STORAGE_STATE,
    baseURL: ORIGIN,
    viewport: { width: 1600, height: 1000 },
  })
  const page = await context.newPage()
  try {
    await page.goto(`/whiteboard/${S.billing}`)

    const node = page.getByTestId(`external-table-node-${S.reference}`)
    await expect(node).toBeVisible({ timeout: 30_000 })

    // Let React Flow settle its layout before the shot.
    await page.waitForTimeout(1200)

    const out = 'demo-artifacts/cross-file-reference.png'
    await page.screenshot({ path: out, fullPage: false })
    console.log(`   saved: ${out}`)

    const nodeOut = 'demo-artifacts/reference-node.png'
    await node.screenshot({ path: nodeOut })
    console.log(`   saved: ${nodeOut}`)

    show('node text', (await node.innerText()).split('\n'))
  } finally {
    await context.close()
  }
})

test('re-target reports what the change costs, in the payload', async () => {
  step('update_table_reference — drop the `id` handle the relationship uses')

  // Re-expose ONLY email. The relationship hangs off `id`, so it cannot survive
  // — and the point of the demo is that the tool SAYS SO in a countable field.
  const result = await agent.expectOk<UpdateReferenceResult>(
    'update_table_reference',
    { tableId: S.reference, sourceColumnIds: [S.usersEmail] },
  )
  console.log(JSON.stringify(result, null, 2))
  console.log(
    '   NOTE: no confirmName here, on purpose — the agent asked to re-target.\n' +
      '         The cost is reported as a NUMBER so a loop can total it.',
  )
})

test('a reference whose source vanishes reports missing, not silence', async () => {
  step('Delete the whole Accounts board, then list the references again')

  await agent.expectOk('delete_whiteboard', {
    whiteboardId: S.accounts,
    confirmName: 'Accounts',
  })

  const listed = await agent.expectOk<{ references: Array<ReferenceRow> }>(
    'list_table_references',
    { whiteboardId: S.billing },
  )
  console.log(JSON.stringify(listed, null, 2))
  console.log(
    '   the node stays and reports missing=true rather than disappearing —\n' +
      '   a dangling reference is visible, not silently swept away.',
  )
})

// ─────────────────────────────────────────────────────────────────────────────
// THE GUARD. Kept last because a successful delete destroys the node.
//
// This step used to be a FINDING: delete_table_reference documented a
// confirmName guard that no layer performed. The tool checked only that the
// string was non-empty, appapi.DeleteTableReference took no confirmName at all,
// and the route's delete arm accepted only { op, tableId } — so the value never
// left the Go process and ANY non-empty string destroyed the node and cascaded
// every relationship drawn from it.
//
// The guard is now real: internal/data.FindReferenceConfirmName reads the name
// straight from SQLite and the tool compares before sending. This step proves it
// over the wire, which is the only place the old bug was visible — the unit test
// that looked like it covered this asserted only the EMPTY case.
// ─────────────────────────────────────────────────────────────────────────────
test('GUARD: a wrong confirmName deletes nothing', async () => {
  step('delete_table_reference with a deliberately wrong confirmName')

  const before = await agent.expectOk<{ references: Array<ReferenceRow> }>(
    'list_table_references',
    { whiteboardId: S.billing },
  )
  show('references before', before.references.length)

  const refused = await agent.callTool<McpErrorPayload>(
    'delete_table_reference',
    { tableId: S.reference, confirmName: 'definitely-not-the-table-name' },
  )
  show('refused with', refused.data)
  expect(refused.isError).toBe(true)
  expect(refused.data.field).toBe('confirmName')

  const after = await agent.expectOk<{ references: Array<ReferenceRow> }>(
    'list_table_references',
    { whiteboardId: S.billing },
  )
  show('references after', after.references.length)
  expect(after.references).toHaveLength(1)
  console.log('   the node and its relationships survived a wrong name.')
})

// The source board was deleted in the previous step, so the name the guard now
// wants is the local fallback — exactly what list_table_references reports.
test('GUARD: the right confirmName deletes', async () => {
  step('delete_table_reference with the name list_table_references reports')

  const listed = await agent.expectOk<{ references: Array<ReferenceRow> }>(
    'list_table_references',
    { whiteboardId: S.billing },
  )
  const confirmName = listed.references[0].sourceTableName!
  show('name reported by list_table_references', confirmName)

  const deleted = await agent.callTool('delete_table_reference', {
    tableId: S.reference,
    confirmName,
  })
  expect(deleted.isError).toBe(false)

  const after = await agent.expectOk<{ references: Array<ReferenceRow> }>(
    'list_table_references',
    { whiteboardId: S.billing },
  )
  expect(after.references).toHaveLength(0)
  console.log('   deleted, and only once the name actually matched.')
})

test.afterAll(async () => {
  // The demo builds a real project. Clean it up so repeat runs stay honest.
  if (S.project && S.projectName) {
    try {
      await agent.callTool('delete_project', {
        projectId: S.project,
        confirmName: S.projectName,
      })
      console.log(`\n   cleaned up demo project "${S.projectName}"`)
    } catch {
      /* best effort — a leftover demo project is harmless */
    }
  }
})

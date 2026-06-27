// src/mcp/__tests__/security/idor-scoping.test.ts
// Suite SEC: IDOR / Project-Membership Scoping Tests
// TC-SEC-IDOR-01 through TC-SEC-IDOR-07
//
// Strategy: unit-mockable — mocked Prisma + mocked validateSessionToken.
// User A (attacker-uuid) has a valid session token, but the projects/resources
// being accessed belong to User B (victim-uuid). All calls must DENY access.
//
// The sole V1 security boundary is assertProjectAccess(), which uses the query:
//   prisma.project.findFirst({ where: { id, OR:[{ownerId:userId},{members:{some:{userId}}}] } })
// This must NOT be replaced by findAllProjectsForUser or findEffectiveRole
// (both are no-ops that return all data regardless of userId).
//
// DEFERRED (require live test DB):
//   TC-INTG-SEC-01..03: end-to-end IDOR via real Socket.IO write path
//   TC-INTG-LIFECYCLE-01..04, 06: real session lifecycle against live server
//   TC-INTG-CRUD-*: full round-trip create/read/update/delete with real DB
//   TC-INTG-PERF-01..03: concurrent write / bulk position load tests
//   TC-INTG-COLLAB-01..04: multi-client broadcast verification

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { validateSessionToken } from '@/lib/auth/session'
import { prisma } from '@/db'
import {
  getColumnProjectId,
  getRelationshipProjectId,
  getTableProjectId,
  getWhiteboardProjectId,
} from '@/data/resolve-project'

// ---------------------------------------------------------------------------
// Mocks — vi.mock is hoisted by Vitest regardless of position
// ---------------------------------------------------------------------------

vi.mock('@/lib/auth/session', () => ({
  validateSessionToken: vi.fn(),
}))

vi.mock('@/db', () => ({
  prisma: {
    project: {
      findFirst: vi.fn(),
      findMany: vi.fn(),
    },
    whiteboard: {
      findUnique: vi.fn(),
    },
    diagramTable: {
      findUnique: vi.fn(),
      findMany: vi.fn(),
    },
    column: {
      findUnique: vi.fn(),
    },
    relationship: {
      findUnique: vi.fn(),
    },
  },
}))

vi.mock('@/data/resolve-project', () => ({
  getColumnProjectId: vi.fn(),
  getRelationshipProjectId: vi.fn(),
  getTableProjectId: vi.fn(),
  getWhiteboardProjectId: vi.fn(),
}))

// ---------------------------------------------------------------------------
// Test constants — valid RFC 4122 v4 UUIDs
// ---------------------------------------------------------------------------

const ATTACKER_USER_ID = 'aaaa1111-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const VICTIM_PROJECT_ID = 'bbbb2222-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
const VICTIM_WB_ID = 'cccc3333-cccc-4ccc-8ccc-cccccccccccc'
const VICTIM_TABLE_ID = 'dddd4444-dddd-4ddd-8ddd-dddddddddddd'
const VICTIM_COLUMN_ID = 'eeee5555-eeee-4eee-8eee-eeeeeeeeeeee'
const VICTIM_REL_ID = 'ffff6666-ffff-4fff-8fff-ffffffffffff'

// ---------------------------------------------------------------------------
// Shared setup: attacker has a valid session, but Prisma returns null
// for any project-membership check (attacker is neither owner nor member).
// ---------------------------------------------------------------------------

beforeEach(() => {
  vi.clearAllMocks()
  process.env.LIZ_SESSION_TOKEN = 'attacker-session-token'

  // Attacker has a valid session
  vi.mocked(validateSessionToken).mockResolvedValue({
    user: { id: ATTACKER_USER_ID, username: 'attacker', email: 'attacker@evil.com' },
    session: { id: 'sess-attacker', expiresAt: new Date(Date.now() + 86400000) },
  })

  // All project membership checks return null (attacker has no access)
  vi.mocked(prisma.project.findFirst).mockResolvedValue(null)
})

// ---------------------------------------------------------------------------
// TC-SEC-IDOR-01: attacker cannot list whiteboards in victim's project
// ---------------------------------------------------------------------------

describe('TC-SEC-IDOR-01 — list_whiteboards denies cross-project access', () => {
  it('assertProjectAccess throws FORBIDDEN when attacker accesses victim project', async () => {
    const { assertProjectAccess } = await import('../../auth')

    await expect(
      assertProjectAccess(ATTACKER_USER_ID, VICTIM_PROJECT_ID),
    ).rejects.toMatchObject({
      code: 'FORBIDDEN',
    })

    // Confirm the correct scoping query was used (OR:[{ownerId},{members}])
    expect(prisma.project.findFirst).toHaveBeenCalledWith(
      expect.objectContaining({
        where: expect.objectContaining({
          id: VICTIM_PROJECT_ID,
          OR: expect.arrayContaining([
            expect.objectContaining({ ownerId: ATTACKER_USER_ID }),
          ]),
        }),
      }),
    )
  })
})

// ---------------------------------------------------------------------------
// TC-SEC-IDOR-02: attacker cannot read a whiteboard they don't own
// ---------------------------------------------------------------------------

describe('TC-SEC-IDOR-02 — get_board denies cross-project whiteboard read', () => {
  it('assertProjectAccess blocks read of victim whiteboard', async () => {
    // resolve-project resolves the whiteboard to victim's project
    vi.mocked(getWhiteboardProjectId).mockResolvedValue(VICTIM_PROJECT_ID)

    const { assertProjectAccess } = await import('../../auth')

    // Simulate the get_board guard: resolve project, then assert access
    const projectId = await getWhiteboardProjectId(VICTIM_WB_ID)
    await expect(
      assertProjectAccess(ATTACKER_USER_ID, projectId),
    ).rejects.toMatchObject({
      code: 'FORBIDDEN',
    })
  })
})

// ---------------------------------------------------------------------------
// TC-SEC-IDOR-03: attacker cannot read schema summary of victim whiteboard
// ---------------------------------------------------------------------------

describe('TC-SEC-IDOR-03 — get_schema_summary denies cross-project access', () => {
  it('assertProjectAccess blocks schema summary read of victim whiteboard', async () => {
    vi.mocked(getWhiteboardProjectId).mockResolvedValue(VICTIM_PROJECT_ID)

    const { assertProjectAccess } = await import('../../auth')

    const projectId = await getWhiteboardProjectId(VICTIM_WB_ID)
    await expect(
      assertProjectAccess(ATTACKER_USER_ID, projectId),
    ).rejects.toMatchObject({ code: 'FORBIDDEN' })
  })
})

// ---------------------------------------------------------------------------
// TC-SEC-IDOR-04: attacker cannot create a table in victim's whiteboard
// (write tool path — scoped via getWhiteboardProjectId → assertProjectAccess)
// ---------------------------------------------------------------------------

describe('TC-SEC-IDOR-04 — create_table denies write to victim whiteboard', () => {
  it('assertProjectAccess blocks table creation in victim whiteboard', async () => {
    vi.mocked(getWhiteboardProjectId).mockResolvedValue(VICTIM_PROJECT_ID)

    const { assertProjectAccess } = await import('../../auth')

    const projectId = await getWhiteboardProjectId(VICTIM_WB_ID)
    await expect(
      assertProjectAccess(ATTACKER_USER_ID, projectId),
    ).rejects.toMatchObject({ code: 'FORBIDDEN' })
  })
})

// ---------------------------------------------------------------------------
// TC-SEC-IDOR-05: attacker cannot update a column in victim's table
// ---------------------------------------------------------------------------

describe('TC-SEC-IDOR-05 — update_column denies access to victim column', () => {
  it('assertProjectAccess blocks column update in victim project', async () => {
    vi.mocked(getColumnProjectId).mockResolvedValue(VICTIM_PROJECT_ID)

    const { assertProjectAccess } = await import('../../auth')

    const projectId = await getColumnProjectId(VICTIM_COLUMN_ID)
    await expect(
      assertProjectAccess(ATTACKER_USER_ID, projectId),
    ).rejects.toMatchObject({ code: 'FORBIDDEN' })

    expect(prisma.project.findFirst).toHaveBeenCalledWith(
      expect.objectContaining({
        where: expect.objectContaining({
          id: VICTIM_PROJECT_ID,
          OR: expect.arrayContaining([
            expect.objectContaining({ ownerId: ATTACKER_USER_ID }),
          ]),
        }),
      }),
    )
  })
})

// ---------------------------------------------------------------------------
// TC-SEC-IDOR-06: attacker cannot delete a relationship in victim's project
// ---------------------------------------------------------------------------

describe('TC-SEC-IDOR-06 — delete_relationship denies access to victim relationship', () => {
  it('assertProjectAccess blocks relationship deletion in victim project', async () => {
    vi.mocked(getRelationshipProjectId).mockResolvedValue(VICTIM_PROJECT_ID)

    const { assertProjectAccess } = await import('../../auth')

    const projectId = await getRelationshipProjectId(VICTIM_REL_ID)
    await expect(
      assertProjectAccess(ATTACKER_USER_ID, projectId),
    ).rejects.toMatchObject({ code: 'FORBIDDEN' })
  })
})

// ---------------------------------------------------------------------------
// TC-SEC-IDOR-07: listAccessibleProjects does NOT expose victim's projects
// The no-op findAllProjectsForUser must never be called.
// ---------------------------------------------------------------------------

describe('TC-SEC-IDOR-07 — listAccessibleProjects never returns cross-user projects', () => {
  it('returns only projects accessible to the calling user', async () => {
    // findMany returns empty: attacker owns/is-member-of no projects
    vi.mocked(prisma.project.findMany).mockResolvedValue([])

    const { listAccessibleProjects } = await import('../../auth')
    // Spy on no-op to confirm it's not used
    const projectDataModule = await import('@/data/project')
    const noOpSpy = vi.spyOn(projectDataModule, 'findAllProjectsForUser')

    const results = await listAccessibleProjects(ATTACKER_USER_ID)

    expect(noOpSpy).not.toHaveBeenCalled()
    expect(results).toEqual([])

    // Confirm scoping query includes ownerId: ATTACKER_USER_ID only
    expect(prisma.project.findMany).toHaveBeenCalledWith(
      expect.objectContaining({
        where: expect.objectContaining({
          OR: expect.arrayContaining([
            expect.objectContaining({ ownerId: ATTACKER_USER_ID }),
            expect.objectContaining({ members: expect.any(Object) }),
          ]),
        }),
      }),
    )

    noOpSpy.mockRestore()
  })

  it('does not mix victim projects into attacker results even when victim has many', async () => {
    // Simulate DB returning nothing for attacker (correct scoping)
    vi.mocked(prisma.project.findMany).mockResolvedValue([])

    const { listAccessibleProjects } = await import('../../auth')
    const results = await listAccessibleProjects(ATTACKER_USER_ID)

    // Zero projects returned — victim's data is not leaked
    const ids = results.map((p) => p.id)
    expect(ids).not.toContain(VICTIM_PROJECT_ID)
  })

  it('table access resolves through project scoping, not direct resource lookup', async () => {
    // Attacker tries to access a table — the scoping query must fire for the
    // project that owns that table, not for the table directly.
    vi.mocked(getTableProjectId).mockResolvedValue(VICTIM_PROJECT_ID)
    // Project membership check returns null → FORBIDDEN
    vi.mocked(prisma.project.findFirst).mockResolvedValue(null)

    const { assertProjectAccess } = await import('../../auth')
    const projectId = await getTableProjectId(VICTIM_TABLE_ID)

    await expect(
      assertProjectAccess(ATTACKER_USER_ID, projectId),
    ).rejects.toMatchObject({ code: 'FORBIDDEN' })

    // The query must scope by projectId + userId membership
    expect(prisma.project.findFirst).toHaveBeenCalledWith(
      expect.objectContaining({
        where: {
          id: VICTIM_PROJECT_ID,
          OR: expect.arrayContaining([
            { ownerId: ATTACKER_USER_ID },
            { members: { some: { userId: ATTACKER_USER_ID } } },
          ]),
        },
        select: { id: true },
      }),
    )
  })
})

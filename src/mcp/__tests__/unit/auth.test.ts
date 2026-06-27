// src/mcp/__tests__/unit/auth.test.ts
// Suite A: Unit Tests — Auth & Startup
// TC-UNIT-AUTH-01 through TC-UNIT-AUTH-08

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { validateSessionToken } from '@/lib/auth/session'
import { prisma } from '@/db'

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock validateSessionToken from @/lib/auth/session
vi.mock('@/lib/auth/session', () => ({
  validateSessionToken: vi.fn(),
}))

// Mock Prisma for assertProjectAccess / listAccessibleProjects
vi.mock('@/db', () => ({
  prisma: {
    project: {
      findFirst: vi.fn(),
      findMany: vi.fn(),
    },
  },
}))

// Import auth module AFTER mocks are set up
// We need to reset the internal _authedUserId between tests via re-import trick.

describe('auth.ts — startup token validation', () => {
  let originalToken: string | undefined
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let exitSpy: any

  beforeEach(() => {
    originalToken = process.env.LIZ_SESSION_TOKEN
    exitSpy = vi
      .spyOn(process, 'exit')
      .mockImplementation((_code?: number | string | null | undefined) => {
        throw new Error(`process.exit(${_code})`)
      })
    vi.clearAllMocks()
  })

  afterEach(() => {
    if (originalToken !== undefined) {
      process.env.LIZ_SESSION_TOKEN = originalToken
    } else {
      delete process.env.LIZ_SESSION_TOKEN
    }
    exitSpy.mockRestore()
  })

  // TC-UNIT-AUTH-01
  it('valid token resolves user ID and does not exit', async () => {
    process.env.LIZ_SESSION_TOKEN = 'valid-token'
    vi.mocked(validateSessionToken).mockResolvedValue({
      user: { id: 'user-uuid-1', username: 'alice', email: 'alice@test.com' },
      session: { id: 'sess-1', expiresAt: new Date(Date.now() + 86400000) },
    })

    // Re-import to get a fresh module state for _authedUserId
    const { validateStartupToken, getAuthedUserId } = await import('../../auth')

    await validateStartupToken()
    const userId = getAuthedUserId()

    expect(userId).toBe('user-uuid-1')
    expect(exitSpy).not.toHaveBeenCalled()
  })

  // TC-UNIT-AUTH-02
  it('invalid/expired token calls process.exit(1)', async () => {
    process.env.LIZ_SESSION_TOKEN = 'expired-token'
    vi.mocked(validateSessionToken).mockResolvedValue(null)

    const stderrSpy = vi
      .spyOn(process.stderr, 'write')
      .mockImplementation(() => true)

    const { validateStartupToken } = await import('../../auth')

    await expect(validateStartupToken()).rejects.toThrow('process.exit(1)')
    expect(exitSpy).toHaveBeenCalledWith(1)

    // Token value must NOT appear in any stderr output
    const allOutput = stderrSpy.mock.calls
      .map((args) => String(args[0]))
      .join('')
    expect(allOutput).not.toContain('expired-token')

    stderrSpy.mockRestore()
  })

  // TC-UNIT-AUTH-03
  it('missing LIZ_SESSION_TOKEN calls process.exit(1) without DB call', async () => {
    delete process.env.LIZ_SESSION_TOKEN

    const { validateStartupToken } = await import('../../auth')

    await expect(validateStartupToken()).rejects.toThrow('process.exit(1)')
    expect(exitSpy).toHaveBeenCalledWith(1)
    expect(validateSessionToken).not.toHaveBeenCalled()
  })
})

describe('auth.ts — assertProjectAccess', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    process.env.LIZ_SESSION_TOKEN = 'test-token'
    vi.mocked(validateSessionToken).mockResolvedValue({
      user: { id: 'user-uuid', username: 'alice', email: 'alice@test.com' },
      session: { id: 'sess', expiresAt: new Date(Date.now() + 86400000) },
    })
  })

  // TC-UNIT-AUTH-04
  it('owner passes assertProjectAccess', async () => {
    vi.mocked(prisma.project.findFirst).mockResolvedValue({
      id: 'project-uuid',
      name: 'test',
      ownerId: 'user-uuid',
    } as any)

    const { assertProjectAccess } = await import('../../auth')
    await expect(
      assertProjectAccess('user-uuid', 'project-uuid'),
    ).resolves.toBeUndefined()
  })

  // TC-UNIT-AUTH-05
  it('ProjectMember row passes assertProjectAccess', async () => {
    // findFirst returns the project (user is a member)
    vi.mocked(prisma.project.findFirst).mockResolvedValue({
      id: 'project-uuid',
      name: 'test',
      ownerId: 'other-uuid',
    } as any)

    const { assertProjectAccess } = await import('../../auth')
    await expect(
      assertProjectAccess('member-uuid', 'project-uuid'),
    ).resolves.toBeUndefined()
  })

  // TC-UNIT-AUTH-06
  it('no ownership/membership throws FORBIDDEN', async () => {
    vi.mocked(prisma.project.findFirst).mockResolvedValue(null)

    const { assertProjectAccess } = await import('../../auth')
    await expect(
      assertProjectAccess('attacker-uuid', 'project-uuid'),
    ).rejects.toMatchObject({
      code: 'FORBIDDEN',
      message: expect.stringContaining('attacker-uuid'),
    })
  })

  // TC-UNIT-AUTH-07
  it('null ownerId project with no members throws FORBIDDEN', async () => {
    vi.mocked(prisma.project.findFirst).mockResolvedValue(null)

    const { assertProjectAccess } = await import('../../auth')
    await expect(
      assertProjectAccess('any-user-uuid', 'orphan-project-uuid'),
    ).rejects.toMatchObject({
      code: 'FORBIDDEN',
    })
  })

  // TC-UNIT-AUTH-08
  it('listAccessibleProjects uses own Prisma query, NOT findAllProjectsForUser', async () => {
    const mockProjects = [
      { id: 'proj-1', name: 'My Project', ownerId: 'user-uuid' },
    ]
    vi.mocked(prisma.project.findMany).mockResolvedValue(mockProjects as any)

    // Spy on the no-op from @/data/project — it must NOT be called
    const projectDataModule = await import('@/data/project')
    const findAllSpy = vi.spyOn(projectDataModule, 'findAllProjectsForUser')

    const { listAccessibleProjects } = await import('../../auth')
    const results = await listAccessibleProjects('user-uuid')

    expect(findAllSpy).not.toHaveBeenCalled()
    expect(prisma.project.findMany).toHaveBeenCalledWith(
      expect.objectContaining({
        where: expect.objectContaining({
          OR: expect.arrayContaining([
            expect.objectContaining({ ownerId: 'user-uuid' }),
            expect.objectContaining({ members: expect.any(Object) }),
          ]),
        }),
      }),
    )
    expect(results).toEqual(mockProjects)
  })
})

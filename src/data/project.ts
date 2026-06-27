// src/data/project.ts
// Data access layer for Project entity

import { createProjectSchema, updateProjectSchema } from './schema'
import type { CreateProject, UpdateProject } from './schema'
import type { Project } from '@prisma/client'
import { prisma } from '@/db'

/**
 * Create a new project
 * @param data - Project creation data (validated with Zod) + optional ownerId
 * @returns Created project
 * @throws Error if validation fails or database operation fails
 */
export async function createProject(
  data: CreateProject & { ownerId?: string },
): Promise<Project> {
  // Validate the base input fields with Zod schema
  const validated = createProjectSchema.parse(data)

  try {
    const project = await prisma.project.create({
      data: {
        ...validated,
        ownerId: data.ownerId,
      },
    })
    return project
  } catch (error) {
    throw new Error(
      `Failed to create project: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

// findAllProjects (unfiltered) removed — replaced by findAllProjectsForUser

/**
 * Find all projects accessible to a user.
 * NOTE: Membership/invitation checks are intentionally bypassed here — any
 * authenticated user can read all projects. The invitation system is preserved
 * elsewhere (write/delete paths remain gated) and will be re-applied to reads
 * once the invitation flow is complete.
 * @param userId - User UUID (kept for API compatibility; not used for filtering)
 * @returns Array of all projects
 */
export async function findAllProjectsForUser(
  userId: string,
): Promise<Array<Project>> {
  // userId param retained for signature compatibility with callers.
  void userId
  try {
    const projects = await prisma.project.findMany({
      orderBy: { createdAt: 'desc' },
    })
    return projects
  } catch (error) {
    throw new Error(
      `Failed to fetch projects: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Find all projects with their folder and whiteboard structure.
 * NOTE: Membership/invitation checks are intentionally bypassed here — any
 * authenticated user can read all projects. The invitation system is preserved
 * elsewhere (write/delete paths remain gated) and will be re-applied to reads
 * once the invitation flow is complete.
 * @param userId - User UUID (kept for API compatibility; not used for filtering)
 * @returns Array of all projects with nested folders and whiteboards
 */
export async function findAllProjectsWithTreeForUser(userId: string): Promise<
  Array<
    Project & {
      folders: Array<{
        id: string
        name: string
        parentFolderId: string | null
        childFolders: Array<{ id: string; name: string }>
        whiteboards: Array<{ id: string; name: string }>
      }>
      whiteboards: Array<{ id: string; name: string }>
    }
  >
> {
  // userId param retained for signature compatibility with callers.
  void userId
  try {
    const projects = await prisma.project.findMany({
      include: {
        folders: {
          include: {
            childFolders: { select: { id: true, name: true } },
            whiteboards: { select: { id: true, name: true } },
          },
        },
        whiteboards: { select: { id: true, name: true } },
      },
      orderBy: { createdAt: 'desc' },
    })
    return projects
  } catch (error) {
    throw new Error(
      `Failed to fetch project tree for user: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

// findAllProjectsWithTree (unfiltered) removed — replaced by findAllProjectsWithTreeForUser

/**
 * Find a project by ID
 * @param id - Project UUID
 * @returns Project or null if not found
 */
export async function findProjectById(id: string): Promise<Project | null> {
  try {
    const project = await prisma.project.findUnique({
      where: { id },
    })
    return project
  } catch (error) {
    throw new Error(
      `Failed to fetch project: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Update a project
 * @param id - Project UUID
 * @param data - Partial project data to update (validated with Zod)
 * @returns Updated project
 * @throws Error if project not found or validation fails
 */
export async function updateProject(
  id: string,
  data: UpdateProject,
): Promise<Project> {
  // Validate input with Zod schema
  const validated = updateProjectSchema.parse(data)

  try {
    const project = await prisma.project.update({
      where: { id },
      data: validated,
    })
    return project
  } catch (error) {
    throw new Error(
      `Failed to update project: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * ProjectPageContent return type for findProjectPageContent
 */
export interface ProjectPageContent {
  project: { id: string; name: string }
  folders: Array<{
    id: string
    name: string
    createdAt: Date
  }>
  whiteboards: Array<{
    id: string
    name: string
    updatedAt: Date
    _count: { tables: number }
  }>
  breadcrumb: Array<{
    id: string
    name: string
    type: 'project' | 'folder'
  }>
  currentFolder?: { id: string; name: string }
}

/**
 * Find project page content (folders + whiteboards at a given level)
 * @param projectId - Project UUID
 * @param folderId - Optional folder UUID for folder view
 * @returns ProjectPageContent or null if project not found
 */
export async function findProjectPageContent(
  projectId: string,
  folderId?: string,
): Promise<ProjectPageContent | null> {
  try {
    const project = await prisma.project.findUnique({
      where: { id: projectId },
      select: { id: true, name: true },
    })
    if (!project) return null

    if (!folderId) {
      // Root view: folders and whiteboards directly under the project
      const [folders, whiteboards] = await Promise.all([
        prisma.folder.findMany({
          where: { projectId, parentFolderId: null },
          select: { id: true, name: true, createdAt: true },
          orderBy: { name: 'asc' },
        }),
        prisma.whiteboard.findMany({
          where: { projectId, folderId: null },
          select: {
            id: true,
            name: true,
            updatedAt: true,
            _count: { select: { tables: true } },
          },
          orderBy: { updatedAt: 'desc' },
        }),
      ])
      return {
        project,
        folders,
        whiteboards,
        breadcrumb: [],
      }
    }

    // Folder view: validate folder belongs to project
    const targetFolder = await prisma.folder.findUnique({
      where: { id: folderId },
      select: { id: true, name: true, projectId: true, parentFolderId: true },
    })
    if (!targetFolder || targetFolder.projectId !== projectId) {
      throw new Error('Folder not found')
    }

    // Fetch folders and whiteboards under this folder (projectId added for defense-in-depth)
    const [folders, whiteboards] = await Promise.all([
      prisma.folder.findMany({
        where: { projectId, parentFolderId: folderId },
        select: { id: true, name: true, createdAt: true },
        orderBy: { name: 'asc' },
      }),
      prisma.whiteboard.findMany({
        where: { projectId, folderId },
        select: {
          id: true,
          name: true,
          updatedAt: true,
          _count: { select: { tables: true } },
        },
        orderBy: { updatedAt: 'desc' },
      }),
    ])

    // Build breadcrumb via single recursive CTE (one round-trip, no N+1)
    // Starts at the target folder's parent and walks up to the root.
    const breadcrumb: ProjectPageContent['breadcrumb'] = []
    if (targetFolder.parentFolderId) {
      type AncestorRow = {
        id: string
        name: string
        parentFolderId: string | null
      }
      const ancestors = await prisma.$queryRaw<Array<AncestorRow>>`
        WITH RECURSIVE ancestors AS (
          SELECT id, name, "parentFolderId", "projectId"
          FROM "Folder"
          WHERE id = ${targetFolder.parentFolderId}
          UNION ALL
          SELECT f.id, f.name, f."parentFolderId", f."projectId"
          FROM "Folder" f
          INNER JOIN ancestors a ON f.id = a."parentFolderId"
        )
        SELECT id, name, "parentFolderId" FROM ancestors
      `
      // CTE returns leaf→root order; reverse to get root→leaf for the breadcrumb trail
      for (const ancestor of ancestors.reverse()) {
        breadcrumb.push({
          id: ancestor.id,
          name: ancestor.name,
          type: 'folder',
        })
      }
    }
    // Prepend project root
    breadcrumb.unshift({ id: project.id, name: project.name, type: 'project' })

    return {
      project,
      folders,
      whiteboards,
      breadcrumb,
      currentFolder: { id: targetFolder.id, name: targetFolder.name },
    }
  } catch (error) {
    if (error instanceof Error && error.message === 'Folder not found') {
      throw error
    }
    throw new Error(
      `Failed to fetch project page content: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Delete a project (cascade deletes all folders and whiteboards)
 * @param id - Project UUID
 * @returns Deleted project
 * @throws Error if project not found
 */
export async function deleteProject(id: string): Promise<Project> {
  try {
    const project = await prisma.project.delete({
      where: { id },
    })
    return project
  } catch (error) {
    throw new Error(
      `Failed to delete project: ${error instanceof Error ? error.message : 'Unknown error'}`,
    )
  }
}

/**
 * Architectural guardrail: $orgName / $projectName URL trees must sync to store.
 *
 * This is a static file-contents assertion — no runtime routing, no React rendering.
 * It reads layout files as source strings and asserts the required imports and
 * setSelectedOrg / setSelectedProject calls are present.
 *
 * Why it exists (HOL-932):
 *   Adding a new /organizations/$orgName/... or /projects/$projectName/... URL tree
 *   without a layout that syncs the URL param to the canonical useOrg / useProject
 *   store silently breaks the selected-entity state contract.  This test fails fast
 *   when the required layout file is missing or incomplete.
 *
 * See docs/ui/selected-entity-state.md for the full contract.
 */

import { describe, it, expect } from 'vitest'
import { readFile } from 'fs/promises'
import { fileURLToPath } from 'url'
import path from 'path'

// Resolve the routes/_authenticated directory relative to this test file.
const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const authenticatedDir = path.resolve(__dirname, '_authenticated')

/**
 * Each entry in ALLOWLISTED_ORG_TREES describes an active URL tree that owns a
 * $orgName path parameter.  The layout file at `layoutFile` must import
 * `useOrg` from `@/lib/org-context` and call `setSelectedOrg(`.
 *
 * Allowlist is explicit — auto-discovery would pick up unrelated params such as
 * $templateName or $deploymentName that intentionally do NOT sync to org/project state.
 */
const ALLOWLISTED_ORG_TREES: Array<{ tree: string; layoutFile: string }> = [
  {
    tree: 'organizations/$orgName (active)',
    layoutFile: path.join(authenticatedDir, 'organizations', '$orgName.tsx'),
  },
]

/**
 * Each entry in ALLOWLISTED_PROJECT_TREES describes an active URL tree that owns a
 * $projectName path parameter.  The layout file must import `useProject` from
 * `@/lib/project-context` and call `setSelectedProject(`.
 */
const ALLOWLISTED_PROJECT_TREES: Array<{ tree: string; layoutFile: string }> = [
  {
    tree: 'projects/$projectName (active)',
    layoutFile: path.join(authenticatedDir, 'projects', '$projectName.tsx'),
  },
]

// ---------------------------------------------------------------------------
// Org-tree guardrails
// ---------------------------------------------------------------------------

describe('selected-entity state contract — org trees', () => {
  for (const { tree, layoutFile } of ALLOWLISTED_ORG_TREES) {
    describe(`${tree}`, () => {
      it(`layout file exists: ${layoutFile}`, async () => {
        let contents: string
        try {
          contents = await readFile(layoutFile, 'utf8')
        } catch {
          throw new Error(
            `Missing layout file: ${layoutFile}\n` +
              `Every $orgName URL tree requires a sibling layout file that imports ` +
              `useOrg from '@/lib/org-context' and calls setSelectedOrg(orgName).  ` +
              `See docs/ui/selected-entity-state.md for the required pattern.`,
          )
        }
        expect(contents.length).toBeGreaterThan(0)
      })

      it(`layout imports useOrg from '@/lib/org-context': ${layoutFile}`, async () => {
        const contents = await readFile(layoutFile, 'utf8')
        expect(
          contents,
          `${layoutFile} must import useOrg from '@/lib/org-context'.\n` +
            `This import is required so the layout can call setSelectedOrg to sync the ` +
            `URL param to the canonical store.  See docs/ui/selected-entity-state.md.`,
        ).toMatch(/from\s+['"]@\/lib\/org-context['"]/)
      })

      it(`layout calls setSelectedOrg(: ${layoutFile}`, async () => {
        const contents = await readFile(layoutFile, 'utf8')
        expect(
          contents,
          `${layoutFile} must call setSelectedOrg( to sync the $orgName URL param ` +
            `to the useOrg store.\n` +
            `Without this call, the WorkspaceMenu org display and downstream components ` +
            `that read useOrg().selectedOrg will be stale after navigation.\n` +
            `See docs/ui/selected-entity-state.md for the required useEffect pattern.`,
        ).toMatch(/setSelectedOrg\s*\(/)
      })
    })
  }
})

// ---------------------------------------------------------------------------
// Project-tree guardrails
// ---------------------------------------------------------------------------

describe('selected-entity state contract — project trees', () => {
  for (const { tree, layoutFile } of ALLOWLISTED_PROJECT_TREES) {
    describe(`${tree}`, () => {
      it(`layout file exists: ${layoutFile}`, async () => {
        let contents: string
        try {
          contents = await readFile(layoutFile, 'utf8')
        } catch {
          throw new Error(
            `Missing layout file: ${layoutFile}\n` +
              `Every $projectName URL tree requires a sibling layout file that imports ` +
              `useProject from '@/lib/project-context' and calls setSelectedProject(projectName).  ` +
              `See docs/ui/selected-entity-state.md for the required pattern.`,
          )
        }
        expect(contents.length).toBeGreaterThan(0)
      })

      it(`layout imports useProject from '@/lib/project-context': ${layoutFile}`, async () => {
        const contents = await readFile(layoutFile, 'utf8')
        expect(
          contents,
          `${layoutFile} must import useProject from '@/lib/project-context'.\n` +
            `This import is required so the layout can call setSelectedProject to sync the ` +
            `URL param to the canonical store.  See docs/ui/selected-entity-state.md.`,
        ).toMatch(/from\s+['"]@\/lib\/project-context['"]/)
      })

      it(`layout calls setSelectedProject(: ${layoutFile}`, async () => {
        const contents = await readFile(layoutFile, 'utf8')
        expect(
          contents,
          `${layoutFile} must call setSelectedProject( to sync the $projectName URL param ` +
            `to the useProject store.\n` +
            `Without this call, the WorkspaceMenu project display and downstream components ` +
            `that read useProject().selectedProject will be stale after navigation.\n` +
            `See docs/ui/selected-entity-state.md for the required useEffect pattern.`,
        ).toMatch(/setSelectedProject\s*\(/)
      })
    })
  }
})

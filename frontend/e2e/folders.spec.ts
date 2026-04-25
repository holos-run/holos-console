import { test, expect } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiCreateFolder,
  apiDeleteFolder,
  apiCreateProject,
  apiDeleteProject,
} from './helpers'

/**
 * E2E tests for the folder workflow (issue #635).
 *
 * These tests exercise the full folder CRUD flow via the UI and API helpers,
 * verifying that the folder hierarchy is rendered correctly in the frontend.
 *
 * Requires a real Kubernetes cluster (k3d or equivalent).
 * Run with: make test-e2e
 */

test.describe('Folder list page', () => {
  test('shows folders under an org and navigates to folder detail', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-folders-${Date.now()}`
    const folderName = `e2e-folder-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)

    // Navigate to the folder detail page directly.
    await page.goto(`/folders/${folderName}`)
    await page.waitForLoadState('networkidle')

    await expect(
      page.locator('[data-slot="card-title"]', { hasText: folderName }),
    ).toBeVisible({ timeout: 10000 })

    // Cleanup
    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })
})

test.describe('Folder detail page', () => {
  test('shows folder name and organization', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-fld-detail-org-${Date.now()}`
    const folderName = `e2e-fld-detail-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)

    await page.goto(`/folders/${folderName}/settings`)
    await page.waitForLoadState('networkidle')

    // Folder name should appear in the page heading (use role to avoid strict mode violations
    // since the name also appears in the breadcrumb and detail fields)
    await expect(page.getByRole('heading', { name: folderName })).toBeVisible({ timeout: 10000 })
    // Organization should appear in the Organization field row (font-mono span to avoid matching
    // the breadcrumb link which also renders orgName as an anchor element)
    await expect(page.locator('span.font-mono', { hasText: orgName }).first()).toBeVisible({ timeout: 5000 })

    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })
})

test.describe('Nested folder workflow', () => {
  test('creates org → parent folder → child folder, both visible in list', async ({ page }) => {
    await loginViaProfilePage(page)

    const ts = Date.now()
    const orgName = `e2e-nested-org-${ts}`
    const parentFolder = `e2e-parent-${ts}`
    const childFolder = `e2e-child-${ts}`

    await apiCreateOrg(page, orgName)
    // Create parent folder under org
    await apiCreateFolder(page, parentFolder, orgName, 1, orgName)
    // Create child folder under parent folder
    await apiCreateFolder(page, childFolder, orgName, 2, parentFolder)

    // Navigate to parent folder index page — card title should show the folder name
    await page.goto(`/folders/${parentFolder}`)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('[data-slot="card-title"]', { hasText: parentFolder })).toBeVisible({ timeout: 10000 })

    // Navigate to child folder index page
    await page.goto(`/folders/${childFolder}`)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('[data-slot="card-title"]', { hasText: childFolder })).toBeVisible({ timeout: 10000 })

    // Cleanup (child first, then parent, then org)
    await apiDeleteFolder(page, childFolder, orgName)
    await apiDeleteFolder(page, parentFolder, orgName)
    await apiDeleteOrg(page, orgName)
  })

  test('project under folder shows in folder breadcrumb context', async ({ page }) => {
    await loginViaProfilePage(page)

    const ts = Date.now()
    const orgName = `e2e-prj-folder-org-${ts}`
    const folderName = `e2e-prj-folder-${ts}`
    const projectName = `e2e-prj-${ts}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)
    await apiCreateProject(page, projectName, orgName)

    // Navigate to the folder index page
    await page.goto(`/folders/${folderName}`)
    await page.waitForLoadState('networkidle')

    // Folder name should be visible in the card title
    await expect(page.locator('[data-slot="card-title"]', { hasText: folderName })).toBeVisible({ timeout: 10000 })

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })
})

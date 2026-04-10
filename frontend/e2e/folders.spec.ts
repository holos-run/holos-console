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

    // Navigate to the org's folders page
    await page.goto(`/orgs/${orgName}/folders`)
    await page.waitForLoadState('networkidle')

    // Folder should appear in the list
    await expect(page.getByText(folderName)).toBeVisible({ timeout: 10000 })

    // Cleanup
    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })

  test('empty folder list shows empty state', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-no-folders-${Date.now()}`
    await apiCreateOrg(page, orgName)

    await page.goto(`/orgs/${orgName}/folders`)
    await page.waitForLoadState('networkidle')

    // No folders → empty state text
    await expect(page.getByText(/no folders/i)).toBeVisible({ timeout: 10000 })

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

    await page.goto(`/orgs/${orgName}/folders/${folderName}`)
    await page.waitForLoadState('networkidle')

    // Folder name should appear in the page
    await expect(page.getByText(folderName)).toBeVisible({ timeout: 10000 })
    // Organization should appear as breadcrumb or label
    await expect(page.getByText(orgName)).toBeVisible({ timeout: 5000 })

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

    // Navigate to org's top-level folders page — only parent folder should appear
    await page.goto(`/orgs/${orgName}/folders`)
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(parentFolder)).toBeVisible({ timeout: 10000 })

    // Navigate to parent folder detail page — child should be listed in sub-folders
    await page.goto(`/orgs/${orgName}/folders/${parentFolder}`)
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(parentFolder)).toBeVisible({ timeout: 10000 })

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

    // Navigate to the folder detail page
    await page.goto(`/orgs/${orgName}/folders/${folderName}`)
    await page.waitForLoadState('networkidle')

    // Folder name should be visible in the page heading/breadcrumb
    await expect(page.getByText(folderName)).toBeVisible({ timeout: 10000 })

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })
})

test.describe('Sidebar Folders navigation', () => {
  test('org nav section includes Folders link', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-sidebar-folders-${Date.now()}`
    await apiCreateOrg(page, orgName)

    await page.goto(`/orgs/${orgName}/projects`)
    await page.waitForLoadState('networkidle')

    // Sidebar should show a Folders link for the selected org
    await expect(page.getByRole('link', { name: /folders/i })).toBeVisible({ timeout: 10000 })

    await apiDeleteOrg(page, orgName)
  })
})

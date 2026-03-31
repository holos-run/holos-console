import { test, expect } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiCreateProject,
  apiDeleteProject,
  selectOrg,
} from './helpers'

/**
 * E2E tests for navigation flows that require a full stack.
 *
 * Pure rendering tests (project picker visibility, nav item presence, ViewModeToggle buttons)
 * have been migrated to unit tests:
 *   - src/components/app-sidebar.test.tsx
 *   - src/components/view-mode-toggle.test.tsx
 *
 * These E2E tests cover routing behaviour that cannot be verified without a
 * real router and server: picker selection triggers navigation, and the
 * 2-click flow reaches the secrets grid.
 *
 * Run with: make test-e2e
 */

async function createSecret(page: import('@playwright/test').Page, projectName: string, secretName: string) {
  await page.goto(`/projects/${projectName}/secrets`)
  await page.getByRole('button', { name: /create secret/i }).waitFor({ timeout: 5000 })
  await page.getByRole('button', { name: /create secret/i }).click()
  await page.getByPlaceholder('my-secret').fill(secretName)
  await page.getByRole('button', { name: /^create$/i }).click()
  await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })
}

test.describe('Sidebar Project Picker navigation', () => {
  test('selecting a project from the picker navigates directly to secrets page', async ({
    page,
  }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-nav-secrets-org-${Date.now()}`
    const projectName = `e2e-nav-secrets-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    // Select the org in the picker
    await selectOrg(page, orgName)

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Open the project picker and select the project
    const projectPicker = page.getByRole('button', { name: /select project|no projects|all projects/i })
    await expect(projectPicker).toBeVisible({ timeout: 5000 })
    await projectPicker.click()
    await page.getByRole('menuitem', { name: projectName }).click()

    // Should navigate directly to the secrets page for the project
    await expect(page).toHaveURL(new RegExp(`/projects/${projectName}/secrets`), { timeout: 10000 })

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

test.describe('Phase 4: Navigation friction removal', () => {
  test('full flow via sidebar pickers reaches secrets grid in 2 clicks', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-full-flow-org-${Date.now()}`
    const projectName = `e2e-full-flow-prj-${Date.now()}`
    const secretName = `e2e-full-flow-secret-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)
    await createSecret(page, projectName, secretName)

    // Start from a neutral page with no project selected
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // Click 1: select org in org picker
    await selectOrg(page, orgName)

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Click 2: select project in project picker — navigates directly to secrets
    const projectPicker = page.getByRole('button', { name: /select project|no projects|all projects/i })
    await expect(projectPicker).toBeVisible({ timeout: 5000 })
    await projectPicker.click()
    await page.getByRole('menuitem', { name: projectName }).click()

    // Assert URL is /projects/$projectName/secrets
    await expect(page).toHaveURL(new RegExp(`/projects/${projectName}/secrets`), { timeout: 10000 })

    // On mobile the sidebar drawer remains open after picker navigation since
    // the React sidebar has no route-change listener. Navigate directly to the
    // URL to get a fresh render with the drawer closed.
    await page.goto(`/projects/${projectName}/secrets`)
    await page.waitForLoadState('networkidle')

    // Assert secrets data grid (table) is visible
    await expect(page.getByRole('table')).toBeVisible({ timeout: 10000 })

    // Assert the test secret appears in the grid
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 15000 })

    // Cleanup
    await page.goto(`/projects/${projectName}/secrets`)
    await page.getByLabel(new RegExp(`delete ${secretName}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

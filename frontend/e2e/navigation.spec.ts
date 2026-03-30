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
 * E2E tests for Phase 1–4: Project picker, context-aware sidebar nav, and nav friction removal.
 *
 * Part of #205 — Flatten UI navigation.
 * Implements RED tests for #206 — Add project context and project picker to sidebar.
 * Implements RED tests for #207 — Context-aware sidebar navigation.
 * Implements RED tests for #209 — Remove navigation friction and cleanup.
 * Updated for #222 — Remove Organizations and Projects sidebar nav entries and their pages.
 */

async function createSecret(page: import('@playwright/test').Page, projectName: string, secretName: string) {
  await page.goto(`/projects/${projectName}/secrets`)
  await page.getByRole('button', { name: /create secret/i }).waitFor({ timeout: 5000 })
  await page.getByRole('button', { name: /create secret/i }).click()
  await page.getByPlaceholder('my-secret').fill(secretName)
  await page.getByRole('button', { name: /^create$/i }).click()
  await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })
}

test.describe('Sidebar Project Picker', () => {
  test('project picker appears in sidebar after selecting an org', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-nav-picker-org-${Date.now()}`
    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Project picker should now be visible below the org picker
    await expect(
      page.getByRole('button', { name: /select project|no projects|all projects/i }),
    ).toBeVisible({ timeout: 5000 })

    // Cleanup
    await apiDeleteOrg(page, orgName)
  })

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

test.describe('Context-aware sidebar navigation', () => {
  test('sidebar shows project-scoped nav items when a project is selected', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-ctx-nav-org-${Date.now()}`
    const projectName = `e2e-ctx-nav-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    // Select the org
    await selectOrg(page, orgName)

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Select the project from the picker
    const projectPicker = page.getByRole('button', { name: /all projects/i })
    await expect(projectPicker).toBeVisible({ timeout: 5000 })
    await projectPicker.click()
    await page.getByRole('menuitem', { name: projectName }).click()

    // On mobile, reopen sidebar after navigation
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Assert project-scoped nav links are visible
    await expect(page.getByRole('link', { name: /^secrets$/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('link', { name: /^settings$/i })).toBeVisible({ timeout: 5000 })

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('sidebar shows empty nav when no project is selected', async ({ page }) => {
    await loginViaProfilePage(page)

    // Navigate to profile with no project selected
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Assert project-scoped nav links are NOT visible (no project selected)
    await expect(page.getByRole('link', { name: /^secrets$/i })).not.toBeVisible()
    await expect(page.getByRole('link', { name: /^settings$/i })).not.toBeVisible()
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

test.describe('ViewModeToggle shared component', () => {
  /**
   * Assert the shared ViewModeToggle pill appears on the secret detail page and profile page.
   */

  test('secret detail page shows Data/Resource toggle', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-vmt-sec-org-${Date.now()}`
    const projectName = `e2e-vmt-sec-prj-${Date.now()}`
    const secretName = `e2e-vmt-sec-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)
    await createSecret(page, projectName, secretName)

    await page.goto(`/projects/${projectName}/secrets/${secretName}`)
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('button', { name: /^data$/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /^resource$/i })).toBeVisible({ timeout: 5000 })

    await page.getByRole('button', { name: /^resource$/i }).click()
    await expect(page.getByRole('code')).toBeVisible({ timeout: 5000 })

    // Cleanup
    await page.goto(`/projects/${projectName}/secrets`)
    await page.getByLabel(new RegExp(`delete ${secretName}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('profile page shows Claims/Raw toggle', async ({ page }) => {
    await loginViaProfilePage(page)

    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('button', { name: /^claims$/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /^raw$/i })).toBeVisible({ timeout: 5000 })

    await page.getByRole('button', { name: /^raw$/i }).click()
    await expect(page.getByRole('code')).toBeVisible({ timeout: 5000 })
  })
})

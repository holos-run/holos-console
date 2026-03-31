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
 * E2E tests for issue #224 — Persist last-selected org and project across browser sessions.
 *
 * Verifies that localStorage (not sessionStorage) is used for org/project selection,
 * and that the project layout route syncs the sidebar picker from the URL.
 */

test.describe('localStorage persistence across browser sessions', () => {
  test('sidebar shows last-used org after new page load', async ({ page, context }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-persist-org-${Date.now()}`
    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    // Verify localStorage has the org key set
    const storedOrg = await page.evaluate(() => localStorage.getItem('holos-selected-org'))
    expect(storedOrg).toBe(orgName)

    // Open a new page in the same context (same localStorage origin, fresh tab)
    const newPage = await context.newPage()
    await newPage.goto('/profile')
    await newPage.waitForLoadState('networkidle')

    // On mobile, open the sidebar drawer
    const sidebarTrigger = newPage.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Org picker should show the previously selected org without re-selection
    const orgPicker = newPage.getByTestId('org-picker')
    await expect(orgPicker).toBeVisible({ timeout: 5000 })
    await expect(orgPicker).toContainText(orgName, { timeout: 5000 })

    // Cleanup
    await apiDeleteOrg(page, orgName)
  })

  test('sidebar shows last-used project after new page load', async ({ page, context }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-persist-prj-org-${Date.now()}`
    const projectName = `e2e-persist-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Select the project
    const projectPicker = page.getByRole('button', { name: /select project|no projects|all projects/i })
    await expect(projectPicker).toBeVisible({ timeout: 5000 })
    await projectPicker.click()
    await page.getByRole('menuitem', { name: projectName }).click()

    // Wait for navigation to secrets page
    await expect(page).toHaveURL(new RegExp(`/projects/${projectName}/secrets`), { timeout: 10000 })

    // Verify localStorage has the project key set
    const storedProject = await page.evaluate(() => localStorage.getItem('holos-selected-project'))
    expect(storedProject).toBe(projectName)

    // Open a new page in the same context (fresh tab, same localStorage)
    const newPage = await context.newPage()
    await newPage.goto('/profile')
    await newPage.waitForLoadState('networkidle')

    // On mobile, open the sidebar drawer
    const newSidebarTrigger = newPage.getByRole('button', { name: /toggle sidebar/i })
    if (await newSidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await newSidebarTrigger.click()
    }

    // Project picker should show the previously selected project without re-selection
    const newProjectPicker = newPage.getByRole('button', { name: new RegExp(projectName) })
    await expect(newProjectPicker).toBeVisible({ timeout: 5000 })

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('navigating to a project URL syncs sidebar picker to that project', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-url-sync-org-${Date.now()}`
    const projectName = `e2e-url-sync-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    // Navigate directly to the project secrets page via URL (bookmark/deep link)
    await page.goto(`/projects/${projectName}/secrets`)
    await page.waitForLoadState('networkidle')

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Sidebar project picker must reflect the project from the URL
    const projectPicker = page.getByRole('button', { name: new RegExp(projectName) })
    await expect(projectPicker).toBeVisible({ timeout: 5000 })

    // localStorage must also be updated to the URL-derived project
    const storedProject = await page.evaluate(() => localStorage.getItem('holos-selected-project'))
    expect(storedProject).toBe(projectName)

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

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

    // Open a new page in the same context (same localStorage origin, but fresh
    // sessionStorage means the OIDC token is absent — the user must sign in again).
    // Dex's server-side session is shared via cookies so sign-in auto-completes.
    const newPage = await context.newPage()
    await loginViaProfilePage(newPage)
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

    // Navigate back to /profile (selectOrg) to refresh the project list in React Query's
    // cache — without this, the stale empty list prevents the picker from finding the project.
    await selectOrg(page, orgName)

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Select the project from the picker
    const projectPicker = page.getByRole('button', { name: /select project|no projects|all projects/i })
    await expect(projectPicker).toBeVisible({ timeout: 5000 })
    await projectPicker.click()
    await page.getByRole('menuitem', { name: projectName }).click()

    // Wait for navigation to secrets page
    await expect(page).toHaveURL(new RegExp(`/projects/${projectName}/secrets`), { timeout: 10000 })

    // Verify localStorage has the project key set
    const storedProject = await page.evaluate(() => localStorage.getItem('holos-selected-project'))
    expect(storedProject).toBe(projectName)

    // Open a new page in the same context (same localStorage, fresh sessionStorage)
    const newPage = await context.newPage()
    await loginViaProfilePage(newPage)
    await newPage.waitForLoadState('networkidle')

    // On mobile, open the sidebar drawer
    const newSidebarTrigger = newPage.getByRole('button', { name: /toggle sidebar/i })
    if (await newSidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await newSidebarTrigger.click()
    }

    // Project picker should show the previously selected project without re-selection.
    // Use data-testid (not accessible name) and a generous timeout to account for
    // API latency on CI: auth → OrgProvider mounts → ListProjects fetch completes.
    const newProjectPicker = newPage.getByTestId('project-picker')
    await expect(newProjectPicker).toBeVisible({ timeout: 15000 })
    await expect(newProjectPicker).toContainText(projectName, { timeout: 15000 })

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

    // Refresh the project list in React Query's cache so it is available when we
    // navigate directly to the project URL below.
    await selectOrg(page, orgName)

    // Navigate directly to the project secrets page via URL (bookmark/deep link)
    await page.goto(`/projects/${projectName}/secrets`)
    await page.waitForLoadState('networkidle')

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Sidebar project picker must reflect the project from the URL (synced by useEffect
    // in the $projectName route layout).  Use data-testid with a generous timeout to
    // account for the useEffect firing after paint + project list fetch on CI.
    const projectPicker = page.getByTestId('project-picker')
    await expect(projectPicker).toBeVisible({ timeout: 10000 })
    await expect(projectPicker).toContainText(projectName, { timeout: 10000 })

    // localStorage must also be updated to the URL-derived project
    const storedProject = await page.evaluate(() => localStorage.getItem('holos-selected-project'))
    expect(storedProject).toBe(projectName)

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

import { test, expect } from '@playwright/test'
import { loginViaProfilePage } from './helpers'

/**
 * E2E tests for Phase 1–4: Project picker, context-aware sidebar nav, and nav friction removal.
 *
 * Part of #205 — Flatten UI navigation.
 * Implements RED tests for #206 — Add project context and project picker to sidebar.
 * Implements RED tests for #207 — Context-aware sidebar navigation.
 * Implements RED tests for #209 — Remove navigation friction and cleanup.
 */

const TEST_ORG = `e2e-nav-org-${process.pid}`
const TEST_PROJECT = `e2e-nav-prj-${process.pid}`

async function createOrg(page: import('@playwright/test').Page, orgName: string) {
  await page.goto('/organizations')
  await page.getByRole('button', { name: /create organization/i }).click()
  await page.getByPlaceholder('My Organization').fill(orgName)
  await page.getByRole('button', { name: /^create$/i }).click()
  await page.waitForURL(/\/organizations\//, { timeout: 10000 })
}

async function selectOrg(page: import('@playwright/test').Page, orgName: string) {
  await page.goto('/organizations')
  await page.waitForLoadState('networkidle')

  // On mobile, open the sidebar drawer
  const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
  if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
    await sidebarTrigger.click()
  }

  await page.getByTestId('org-picker').waitFor({ timeout: 5000 })
  await page.getByTestId('org-picker').click()
  await page.getByRole('menuitem', { name: orgName }).click()
}

async function createProject(page: import('@playwright/test').Page, projectName: string) {
  await page.goto('/projects')
  await page.getByRole('button', { name: /create project/i }).click()
  await page.getByPlaceholder('My Project').fill(projectName)
  await page.getByRole('button', { name: /^create$/i }).click()
  await page.waitForURL(new RegExp(`/projects/${projectName}`), { timeout: 10000 })
}

async function createSecret(page: import('@playwright/test').Page, projectName: string, secretName: string) {
  await page.goto(`/projects/${projectName}/secrets`)
  await page.getByRole('button', { name: /create secret/i }).waitFor({ timeout: 5000 })
  await page.getByRole('button', { name: /create secret/i }).click()
  await page.getByPlaceholder('my-secret').fill(secretName)
  await page.getByRole('button', { name: /^create$/i }).click()
  await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })
}

async function deleteProject(page: import('@playwright/test').Page, projectName: string) {
  await page.goto('/projects')
  await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
  const btn = page.getByRole('dialog').getByRole('button', { name: /delete/i })
  await btn.click()
}

async function deleteOrg(page: import('@playwright/test').Page, orgName: string) {
  await page.goto('/organizations')
  await page.getByLabel(new RegExp(`delete ${orgName}`, 'i')).click()
  const btn = page.getByRole('dialog').getByRole('button', { name: /delete/i })
  await btn.click()
}

test.describe('Sidebar Project Picker', () => {
  test('project picker appears in sidebar after selecting an org', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-nav-picker-org-${Date.now()}`
    await createOrg(page, orgName)
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
    await deleteOrg(page, orgName)
  })

  test('selecting a project from the picker navigates directly to secrets page', async ({
    page,
  }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-nav-secrets-org-${Date.now()}`
    const projectName = `e2e-nav-secrets-prj-${Date.now()}`

    await createOrg(page, orgName)
    await selectOrg(page, orgName)
    await createProject(page, projectName)

    // Navigate back to a neutral page first
    await page.goto('/organizations')
    await page.waitForLoadState('networkidle')

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
    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })
})

test.describe('Context-aware sidebar navigation', () => {
  test('sidebar shows project-scoped nav items when a project is selected', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-ctx-nav-org-${Date.now()}`
    const projectName = `e2e-ctx-nav-prj-${Date.now()}`

    await createOrg(page, orgName)
    await selectOrg(page, orgName)
    await createProject(page, projectName)

    // Navigate back and re-select the org
    await page.goto('/organizations')
    await page.waitForLoadState('networkidle')
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

    // Assert global nav links are NOT visible
    await expect(page.getByRole('link', { name: /^organizations$/i })).not.toBeVisible()
    await expect(page.getByRole('link', { name: /^projects$/i })).not.toBeVisible()

    // Cleanup
    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })

  test('sidebar reverts to global nav when no project is selected', async ({ page }) => {
    await loginViaProfilePage(page)

    // Clear any session state by navigating fresh (no project selected)
    await page.goto('/organizations')
    await page.waitForLoadState('networkidle')

    // On mobile, open the sidebar drawer
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Assert global nav links are visible
    await expect(page.getByRole('link', { name: /^organizations$/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('link', { name: /^projects$/i })).toBeVisible({ timeout: 5000 })

    // Assert project-scoped nav links are NOT visible
    await expect(page.getByRole('link', { name: /^secrets$/i })).not.toBeVisible()
    await expect(page.getByRole('link', { name: /^settings$/i })).not.toBeVisible()
  })
})

test.describe('Phase 4: Navigation friction removal', () => {
  test('org detail page does not show a "Projects" navigation button', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-no-proj-btn-org-${Date.now()}`
    await createOrg(page, orgName)

    // Navigate to the org detail page
    await page.goto(`/organizations/${orgName}`)
    await page.waitForLoadState('networkidle')

    // Assert there is NO button with text "Projects" on the page
    await expect(page.getByRole('button', { name: /^projects$/i })).not.toBeVisible()

    // Cleanup
    await deleteOrg(page, orgName)
  })

  test('project detail page does not show a "Secrets" navigation button', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-no-sec-btn-org-${Date.now()}`
    const projectName = `e2e-no-sec-btn-prj-${Date.now()}`

    await createOrg(page, orgName)
    await selectOrg(page, orgName)
    await createProject(page, projectName)

    // Navigate directly to the project detail page
    await page.goto(`/projects/${projectName}`)
    await page.waitForLoadState('networkidle')

    // Assert there is NO button (or link button) with text "Secrets" on the page
    await expect(page.getByRole('link', { name: /^secrets$/i })).not.toBeVisible()

    // Cleanup
    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })

  test('full flow via sidebar pickers reaches secrets grid in 2 clicks', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-full-flow-org-${Date.now()}`
    const projectName = `e2e-full-flow-prj-${Date.now()}`
    const secretName = `e2e-full-flow-secret-${Date.now()}`

    await createOrg(page, orgName)
    await selectOrg(page, orgName)
    await createProject(page, projectName)
    await createSecret(page, projectName, secretName)

    // Start from a neutral page with no project selected
    await page.goto('/organizations')
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

    // On mobile, close the sidebar drawer if it is currently open so the
    // main content is visible. Check the drawer's open state via the
    // [data-mobile="true"] panel — present and visible only when open.
    const mobileDrawer = page.locator('[data-mobile="true"]')
    if (await mobileDrawer.isVisible({ timeout: 1000 }).catch(() => false)) {
      await sidebarTrigger.click()
      await mobileDrawer.waitFor({ state: 'hidden', timeout: 5000 })
    }

    // Assert secrets data grid (table) is visible
    await expect(page.getByRole('table')).toBeVisible({ timeout: 10000 })

    // Assert the test secret appears in the grid
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 15000 })

    // Cleanup
    await page.goto(`/projects/${projectName}/secrets`)
    await page.getByLabel(new RegExp(`delete ${secretName}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })
    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })
})

test.describe('ViewModeToggle shared component', () => {
  /**
   * Assert the shared ViewModeToggle pill appears on all four pages that use it,
   * each with context-appropriate labels injected as options.
   */

  test('org detail page shows Editor/Raw toggle', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-vmt-org-${Date.now()}`
    await createOrg(page, orgName)

    await page.goto(`/organizations/${orgName}`)
    await page.waitForLoadState('networkidle')

    // Both toggle options must be visible
    await expect(page.getByRole('button', { name: /^editor$/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /^raw$/i })).toBeVisible({ timeout: 5000 })

    // Clicking Raw switches to raw view (raw JSON content appears)
    await page.getByRole('button', { name: /^raw$/i }).click()
    await expect(page.getByRole('code')).toBeVisible({ timeout: 5000 })

    await deleteOrg(page, orgName)
  })

  test('project detail page shows Editor/Raw toggle', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-vmt-prj-org-${Date.now()}`
    const projectName = `e2e-vmt-prj-${Date.now()}`

    await createOrg(page, orgName)
    await selectOrg(page, orgName)
    await createProject(page, projectName)

    await page.goto(`/projects/${projectName}`)
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('button', { name: /^editor$/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /^raw$/i })).toBeVisible({ timeout: 5000 })

    await page.getByRole('button', { name: /^raw$/i }).click()
    await expect(page.getByRole('code')).toBeVisible({ timeout: 5000 })

    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })

  test('secret detail page shows Data/Resource toggle', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-vmt-sec-org-${Date.now()}`
    const projectName = `e2e-vmt-sec-prj-${Date.now()}`
    const secretName = `e2e-vmt-sec-${Date.now()}`

    await createOrg(page, orgName)
    await selectOrg(page, orgName)
    await createProject(page, projectName)
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
    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
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

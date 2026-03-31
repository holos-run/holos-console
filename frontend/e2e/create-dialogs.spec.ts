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
 * E2E tests for create organization and create project dialogs (issue #234).
 *
 * These tests cover full-stack creation flows via the sidebar pickers.
 *
 * Note: Tests that assume the first-time user empty state (no orgs exist) are
 * omitted here because all browser contexts share the same K8s cluster in CI —
 * chromium desktop tests run before mobile-chrome and may have already created
 * orgs, making empty-state assertions unreliable. That behaviour is covered by
 * unit tests in app-sidebar.test.tsx.
 *
 * Run with: make test-e2e
 */

test.describe('Create Organization dialog', () => {
  test('existing user sees New Organization item at bottom of org picker dropdown', async ({
    page,
  }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-existing-org-${Date.now()}`
    await apiCreateOrg(page, orgName)

    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Open the org picker dropdown
    await page.getByTestId('org-picker').waitFor({ timeout: 5000 })
    await page.getByTestId('org-picker').click()

    // New Organization item should be at the bottom of the menu
    await expect(page.getByRole('menuitem', { name: /new organization/i })).toBeVisible({
      timeout: 5000,
    })

    // Cleanup
    await apiDeleteOrg(page, orgName)
  })
})

test.describe('Create Project dialog', () => {
  test('org with no projects shows New Project CTA', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-no-projects-org-${Date.now()}`
    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // The project picker dropdown should NOT be visible — empty-state CTA instead
    await expect(page.getByTestId('project-picker')).not.toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /new project/i })).toBeVisible({
      timeout: 5000,
    })

    // Cleanup
    await apiDeleteOrg(page, orgName)
  })

  test('create project dialog opens, submits, and navigates to secrets page', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-create-prj-org-${Date.now()}`
    const projectName = `e2e-create-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Click New Project to open the dialog
    await page.getByRole('button', { name: /new project/i }).click()

    // Fill in the form (org should be pre-selected)
    await page.getByPlaceholder('my-project').fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()

    // After creation should navigate to the new project's secrets page
    await expect(page).toHaveURL(new RegExp(`/projects/${projectName}/secrets`), {
      timeout: 15000,
    })

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('existing user sees New Project item at bottom of project picker dropdown', async ({
    page,
  }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-existing-prj-org-${Date.now()}`
    const projectName = `e2e-existing-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)
    await selectOrg(page, orgName)

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Open the project picker dropdown
    await page.getByTestId('project-picker').waitFor({ timeout: 10000 })
    await page.getByTestId('project-picker').click()

    // New Project item should appear at the bottom of the menu
    await expect(page.getByRole('menuitem', { name: /new project/i })).toBeVisible({
      timeout: 5000,
    })

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

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

  test('create project dialog opens, submits via display name auto-slug, and navigates to secrets page', async ({
    page,
  }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-create-prj-org-${Date.now()}`
    const displayName = `E2E Create Project ${Date.now()}`
    // toSlug equivalent: lower, replace non-alnum runs with hyphen, strip leading/trailing hyphens
    const expectedSlug = displayName.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Click New Project to open the dialog
    await page.getByRole('button', { name: /new project/i }).click()

    // Type a Display Name and verify the Name field auto-derives the slug
    await page.getByPlaceholder('My Project').fill(displayName)
    await expect(page.getByPlaceholder('my-project')).toHaveValue(expectedSlug)

    await page.getByRole('button', { name: /^create$/i }).click()

    // After creation should navigate to the new project's secrets page
    await expect(page).toHaveURL(new RegExp(`/projects/${expectedSlug}/secrets`), {
      timeout: 15000,
    })

    // Cleanup
    await apiDeleteProject(page, expectedSlug)
    await apiDeleteOrg(page, orgName)
  })

  test('create project dialog: manually overriding name stops auto-derivation and shows reset affordance', async ({
    page,
  }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-slug-override-org-${Date.now()}`
    const projectName = `e2e-slug-override-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    await page.getByRole('button', { name: /new project/i }).click()

    // Type a Display Name to start auto-derivation
    await page.getByPlaceholder('My Project').fill('Test Project')
    await expect(page.getByPlaceholder('my-project')).toHaveValue('test-project')

    // Override the Name field directly — auto-derivation should stop
    await page.getByPlaceholder('my-project').fill(projectName)

    // Reset affordance should appear
    await expect(page.getByText(/auto-derive from display name/i)).toBeVisible()

    // Further display name changes should NOT update the name field
    await page.getByPlaceholder('My Project').fill('Different Display Name')
    await expect(page.getByPlaceholder('my-project')).toHaveValue(projectName)

    // Click reset — name should re-derive from current display name
    await page.getByText(/auto-derive from display name/i).click()
    await expect(page.getByPlaceholder('my-project')).toHaveValue('different-display-name')
    await expect(page.getByText(/auto-derive from display name/i)).not.toBeVisible()

    // Cleanup (close dialog without submitting)
    await page.getByRole('button', { name: /cancel/i }).click()
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

import { test, expect } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiDeleteProject,
  selectOrg,
} from './helpers'

/**
 * E2E tests for create organization and create project dialogs (issue #234).
 *
 * These tests cover full-stack creation flows via the sidebar pickers:
 *   - First-time user sees "New Organization" button (no blank sidebar)
 *   - Creating an org via the dialog auto-selects it
 *   - Org with no projects shows "New Project" CTA
 *   - Creating a project navigates to the secrets page
 *   - Existing users see "New Organization" and "New Project" at the bottom of dropdowns
 *
 * Run with: make test-e2e
 */

test.describe('Create Organization dialog', () => {
  test('first-time user sees New Organization button when no orgs exist', async ({ page }) => {
    await loginViaProfilePage(page)

    // Navigate to profile to ensure sidebar is loaded
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // The org picker dropdown should NOT be visible — instead the empty-state button
    await expect(page.getByTestId('org-picker')).not.toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /new organization/i })).toBeVisible({
      timeout: 5000,
    })
  })

  test('create org dialog opens, submits, and auto-selects the new org', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-create-org-${Date.now()}`

    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Click the New Organization button to open the dialog
    await page.getByRole('button', { name: /new organization/i }).click()

    // Fill in the form
    await page.getByPlaceholder('my-org').fill(orgName)
    await page.getByRole('button', { name: /^create$/i }).click()

    // After creation, the org picker should now show the new org as selected
    await expect(page.getByTestId('org-picker')).toBeVisible({ timeout: 10000 })
    await expect(page.getByTestId('org-picker')).toContainText(orgName, { timeout: 10000 })

    // Cleanup
    await apiDeleteOrg(page, orgName)
  })

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

    // Use API to create a project so the picker dropdown appears (not empty state)
    await loginViaProfilePage(page)
    const { token, email } = await page.evaluate(() => {
      const key = Object.keys(sessionStorage).find((k) => k.startsWith('oidc.user:'))
      if (!key) throw new Error('No OIDC session')
      const data = JSON.parse(sessionStorage.getItem(key)!) as {
        access_token?: string
        profile?: { email?: string }
      }
      return { token: data.access_token ?? '', email: data.profile?.email ?? '' }
    })

    await page.evaluate(
      async ({ name, organization, email, token }) => {
        const resp = await fetch('/holos.console.v1.ProjectService/CreateProject', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Connect-Protocol-Version': '1',
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({
            name,
            displayName: name,
            organization,
            userGrants: [{ principal: email, role: 3 }],
            roleGrants: [],
          }),
        })
        if (!resp.ok) throw new Error(`CreateProject failed: ${await resp.text()}`)
      },
      { name: projectName, organization: orgName, email, token },
    )

    await selectOrg(page, orgName)

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
    }

    // Open the project picker dropdown
    await page.getByTestId('project-picker').waitFor({ timeout: 5000 })
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

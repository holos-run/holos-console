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
 * E2E tests for Secrets page.
 *
 * These tests verify the secrets CRUD flow through the UI.
 * Secrets are now under projects: /projects/$projectName/secrets/$name
 *
 * Run with: make test-e2e (automatically starts servers)
 *
 * The embedded Dex auto-login connector authenticates as the admin user
 * (configurable via HOLOS_DEX_INITIAL_ADMIN_USERNAME env var, default: admin).
 * No password is required — the auto-connector bypasses the login form.
 */

// Helper function to log in via Dex.
// After PR #230 the auth layout auto-redirects unauthenticated users to Dex
// OIDC — there is no Sign In button. Wait for the automatic redirect instead.
async function loginAndNavigate(page: import('@playwright/test').Page, path: string) {
  await loginViaProfilePage(page)
  if (path !== '/profile') {
    await page.goto(path)
  }
}

test.describe('Secrets Page', () => {
  test('should create secret with sharing and show sharing panel', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-sharing-org-${Date.now()}`
    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    // Create a test project via API
    const projectName = `e2e-sharing-prj-${Date.now()}`
    await apiCreateProject(page, projectName, orgName)

    // Navigate to secrets list for this project
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /new secret/i })).toBeVisible({ timeout: 5000 })

    // Create a new secret via the dedicated create page
    const secretName = `e2e-sharing-${Date.now()}`
    await page.getByRole('button', { name: /new secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/new`), { timeout: 5000 })
    await page.getByPlaceholder('my-secret').fill(secretName)
    await page.getByPlaceholder('key').fill('.env')
    await page.getByPlaceholder('value').fill('TEST_KEY=test_value')
    await page.getByRole('button', { name: /create secret/i }).click()

    // Wait for redirect back to list and secret to appear
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })

    // Navigate to the created secret
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })

    // Verify sharing panel is present
    await expect(page.getByText('Sharing', { exact: true })).toBeVisible({ timeout: 5000 })

    // Verify the creator is shown as owner (admin user email)
    await expect(page.getByText(/admin@example.com|admin/)).toBeVisible()

    // Clean up: delete the secret
    await page.getByRole('button', { name: /^delete$/i }).click()
    await expect(page.getByText(/are you sure/i)).toBeVisible()
    const dialogDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await dialogDeleteButton.click()

    // Should redirect to secrets list
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })

    // Clean up: delete the project and org
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('should update sharing grants on secret page', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-share-upd-org-${Date.now()}`
    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    // Create a test project via API
    const projectName = `e2e-share-upd-${Date.now()}`
    await apiCreateProject(page, projectName, orgName)

    // Navigate to secrets list and create a test secret via the dedicated create page
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /new secret/i })).toBeVisible({ timeout: 5000 })

    const secretName = `e2e-share-update-${Date.now()}`
    await page.getByRole('button', { name: /new secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/new`), { timeout: 5000 })
    await page.getByPlaceholder('my-secret').fill(secretName)
    // The create page shows one empty key-value row
    await page.getByPlaceholder('key').fill('.env')
    await page.getByPlaceholder('value').fill('KEY=value')
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })

    // Navigate to the secret
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })

    // Verify sharing panel and edit button
    await expect(page.getByText('Sharing', { exact: true })).toBeVisible({ timeout: 5000 })

    // Click the sharing Edit button (last Edit button on the page)
    const sharingEditBtn = page.getByRole('button', { name: /^edit$/i }).last()
    await expect(sharingEditBtn).toBeVisible()
    await sharingEditBtn.click()

    // Add a role grant
    await page.getByRole('button', { name: /add role/i }).click()
    const roleInput = page.getByPlaceholder(/role name/i)
    await roleInput.fill('test-team')

    // Save
    // Find the sharing Save button (smaller, not the data save)
    const saveBtns = page.getByRole('button', { name: /^save$/i })
    await saveBtns.last().click()

    // Verify role appears in read mode
    await expect(page.getByText('test-team')).toBeVisible({ timeout: 5000 })

    // Clean up: delete the secret
    await page.getByRole('button', { name: /^delete$/i }).click()
    const dialogDelete = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await dialogDelete.click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })

    // Clean up: delete the project and org
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('should show sharing summary in secrets list', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-list-sum-org-${Date.now()}`
    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    // Create a test project via API
    const projectName = `e2e-list-sum-${Date.now()}`
    await apiCreateProject(page, projectName, orgName)

    // Navigate to secrets list
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /new secret/i })).toBeVisible({ timeout: 5000 })

    // Create a test secret via the dedicated create page
    const secretName = `e2e-list-summary-${Date.now()}`
    await page.getByRole('button', { name: /new secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/new`), { timeout: 5000 })
    await page.getByPlaceholder('my-secret').fill(secretName)
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })

    // Verify the secret shows in the list as a link
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })

    // Clean up: delete via the list
    await page.getByLabel(new RegExp(`delete ${secretName}`, 'i')).click()
    const dialogDelete = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await dialogDelete.click()
    // Wait for delete dialog to close
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })

    // Clean up: delete the project and org
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('should allow adding a key to an empty secret on the detail page', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-empty-org-${Date.now()}`
    await apiCreateOrg(page, orgName)
    await selectOrg(page, orgName)

    // Create a test project via API
    const projectName = `e2e-empty-secret-${Date.now()}`
    await apiCreateProject(page, projectName, orgName)

    // Create a secret with no data (skip key/value, just name and submit)
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /new secret/i })).toBeVisible({ timeout: 5000 })
    const secretName = `e2e-empty-${Date.now()}`
    await page.getByRole('button', { name: /new secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/new`), { timeout: 5000 })
    await page.getByPlaceholder('my-secret').fill(secretName)
    // Do NOT fill key/value — create an empty secret
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })

    // Navigate to the detail page
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })

    // Click Edit to enter edit mode — grid should show one empty row
    // Use the data Edit button (near the Delete button), not the sharing Edit
    const dataEditBtn = page.getByRole('button', { name: /^edit$/i }).first()
    await expect(dataEditBtn).toBeVisible({ timeout: 5000 })
    await dataEditBtn.click()

    // Fill the empty row with key and value
    await page.getByPlaceholder('key').fill('token')
    await page.getByPlaceholder('value').fill('abc123')

    // Save the secret
    await page.getByRole('button', { name: /^save$/i }).click()
    await expect(page.getByRole('button', { name: /^save$/i })).toBeDisabled({ timeout: 5000 })

    // Reload the page and confirm the key persists
    await page.reload()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })
    await expect(page.getByText('token')).toBeVisible({ timeout: 5000 })

    // Clean up: delete the secret
    await page.getByRole('button', { name: /^delete$/i }).click()
    await expect(page.getByText(/are you sure/i)).toBeVisible()
    const dialogDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await dialogDeleteButton.click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })

    // Clean up: delete the project and org
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

test.describe('Mobile Responsive Layout', () => {
  // These tests run with the mobile-chrome project (iPhone 13 viewport)
  // and verify responsive behavior. On desktop viewport they are skipped.
  //
  // The mobile-chrome project runs every remaining spec at a phone viewport
  // and catches responsive regressions for free; the hamburger-visible smoke
  // that used to live here duplicated that coverage and was removed in HOL-658.

  test('should open drawer and show sidebar navigation on mobile', async ({ page }, testInfo) => {
    test.skip(testInfo.project?.name !== 'mobile-chrome', 'mobile-only test')
    await loginAndNavigate(page, '/profile')

    // Tap hamburger to open drawer
    await page.getByRole('button', { name: /toggle sidebar/i }).click()

    // Workspace menu trigger (HOL-603) is rendered at the top of the sidebar
    // and is always present, so it acts as the drawer-visible sentinel.
    await expect(page.getByTestId('workspace-menu')).toBeVisible({ timeout: 5000 })
  })
})

import { test, expect } from '@playwright/test'

/**
 * E2E tests for Secrets page.
 *
 * These tests verify the secrets CRUD flow through the UI.
 * Secrets are now under projects: /projects/$projectName/secrets/$name
 *
 * Run with: make test-e2e (automatically starts servers)
 *
 * Default credentials (configurable via env vars on the Go backend):
 *   Username: admin (HOLOS_DEX_INITIAL_ADMIN_USERNAME)
 *   Password: verysecret (HOLOS_DEX_INITIAL_ADMIN_PASSWORD)
 */

// Default credentials for embedded Dex OIDC provider
const DEFAULT_USERNAME = 'admin'
const DEFAULT_PASSWORD = 'verysecret'

// Test org/project names - created and cleaned up by these tests
const TEST_ORG = `e2e-org-${process.pid}`
const TEST_PROJECT = `e2e-secrets-${process.pid}`

// Helper function to log in via Dex.
// Handles both cases: Dex showing login form or auto-completing with existing session.
async function loginAndNavigate(page: import('@playwright/test').Page, path: string) {
  await page.goto('/profile')

  const signInButton = page.getByRole('button', { name: 'Sign In' })
  await signInButton.click()

  // Wait for either the Dex login page or a redirect back
  await page.waitForURL(/\/dex\/|\/profile|\/pkce\/verify/, { timeout: 10000 })

  // If we landed on Dex, complete the login form
  if (page.url().includes('/dex/')) {
    // Navigate past connector selection if present
    const connectorLink = page.locator('a[href*="connector"]').first()
    if ((await connectorLink.count()) > 0) {
      await connectorLink.click()
      await page.waitForLoadState('networkidle')
    }

    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    await expect(usernameInput).toBeVisible({ timeout: 5000 })
    await usernameInput.fill(DEFAULT_USERNAME)
    await passwordInput.fill(DEFAULT_PASSWORD)

    await page.locator('button[type="submit"]').click()
  }

  await page.waitForURL(/\/profile/, { timeout: 15000 })
  if (path !== '/profile') {
    await page.goto(path)
  }
}

// Helper to create an organization and select it in the org picker
async function createAndSelectOrg(page: import('@playwright/test').Page, orgName: string) {
  await page.goto('/organizations')

  // Click Create Organization and fill display name (slug auto-populates)
  await page.getByRole('button', { name: /create organization/i }).click()
  await page.getByPlaceholder('My Organization').fill(orgName)
  await page.getByRole('button', { name: /^create$/i }).click()

  // Wait for redirect to org detail page (confirms creation succeeded)
  await page.waitForURL(/\/organizations\//, { timeout: 10000 })

  // Select the org in the sidebar dropdown picker
  await page.getByRole('button', { name: /all organizations/i }).click()
  await page.getByRole('menuitem', { name: orgName }).click()
}

// Helper to delete an organization
async function deleteOrg(page: import('@playwright/test').Page, orgName: string) {
  await page.goto('/organizations')
  await page.getByLabel(new RegExp(`delete ${orgName}`, 'i')).click()
  const deleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
  await deleteButton.click()
}

test.describe('Secrets Page', () => {
  test('should show projects link in sidebar', async ({ page }) => {
    await loginAndNavigate(page, '/profile')

    // Verify Projects link is in sidebar
    await expect(page.getByRole('link', { name: 'Projects' })).toBeVisible()
  })

  test('should navigate to projects page from sidebar', async ({ page }) => {
    await loginAndNavigate(page, '/profile')

    // Click Projects link
    await page.getByRole('link', { name: 'Projects' }).click()

    // Verify URL changed to projects page
    await expect(page).toHaveURL(/\/projects/)
  })

  test('should create secret with sharing and show sharing panel', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-sharing-org-${Date.now()}`
    await createAndSelectOrg(page, orgName)

    // Create a test project
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    const projectName = `e2e-sharing-prj-${Date.now()}`
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByPlaceholder('My Project').fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()
    // Wait for redirect to project detail page (confirms creation succeeded)
    await page.waitForURL(new RegExp(`/projects/${projectName}`), { timeout: 10000 })

    // Navigate to secrets list for this project
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    // Create a new secret
    const secretName = `e2e-sharing-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByPlaceholder('my-secret').fill(secretName)
    await page.getByPlaceholder('key').fill('.env')
    await page.getByPlaceholder('value').fill('TEST_KEY=test_value')
    await page.getByRole('button', { name: /^create$/i }).click()

    // Wait for the secret to appear in the list
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })

    // Navigate to the created secret
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })

    // Verify sharing panel is present
    await expect(page.getByText('Sharing')).toBeVisible({ timeout: 5000 })

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
    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
    const projectDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await projectDeleteButton.click()
    await deleteOrg(page, orgName)
  })

  test('should update sharing grants on secret page', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-share-upd-org-${Date.now()}`
    await createAndSelectOrg(page, orgName)

    // Create a test project
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    const projectName = `e2e-share-upd-${Date.now()}`
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByPlaceholder('My Project').fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()
    // Wait for redirect to project detail page (confirms creation succeeded)
    await page.waitForURL(new RegExp(`/projects/${projectName}`), { timeout: 10000 })

    // Navigate to secrets list and create a test secret
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    const secretName = `e2e-share-update-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByPlaceholder('my-secret').fill(secretName)
    await page.getByRole('button', { name: /add key/i }).click()
    await page.getByPlaceholder('key').fill('.env')
    await page.getByPlaceholder('value').fill('KEY=value')
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })

    // Navigate to the secret
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })

    // Verify sharing panel and edit button
    await expect(page.getByText('Sharing')).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /edit/i })).toBeVisible()

    // Enter edit mode
    await page.getByRole('button', { name: /edit/i }).click()

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
    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
    const projectDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await projectDeleteButton.click()
    await deleteOrg(page, orgName)
  })

  test('should show sharing summary in secrets list', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-list-sum-org-${Date.now()}`
    await createAndSelectOrg(page, orgName)

    // Create a test project
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    const projectName = `e2e-list-sum-${Date.now()}`
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByPlaceholder('My Project').fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()
    // Wait for redirect to project detail page (confirms creation succeeded)
    await page.waitForURL(new RegExp(`/projects/${projectName}`), { timeout: 10000 })

    // Navigate to secrets list
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    // Create a test secret
    const secretName = `e2e-list-summary-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByPlaceholder('my-secret').fill(secretName)
    await page.getByRole('button', { name: /^create$/i }).click()

    // Verify the secret shows in the list with sharing summary (at least "1 user" for the creator)
    await expect(page.getByText(secretName)).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/1 user/i)).toBeVisible({ timeout: 5000 })

    // Clean up: delete via the list
    await page.getByLabel(new RegExp(`delete ${secretName}`, 'i')).click()
    const dialogDelete = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await dialogDelete.click()
    // Wait for secret to disappear from the list
    await expect(page.getByText(secretName)).not.toBeVisible({ timeout: 5000 })

    // Clean up: delete the project and org
    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
    const projectDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await projectDeleteButton.click()
    await deleteOrg(page, orgName)
  })

  test('should allow adding a key to an empty secret on the detail page', async ({ page }) => {
    // Login and create an org first
    await loginAndNavigate(page, '/profile')
    const orgName = `e2e-empty-org-${Date.now()}`
    await createAndSelectOrg(page, orgName)

    // Create a test project
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    const projectName = `e2e-empty-secret-${Date.now()}`
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByPlaceholder('My Project').fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()
    // Wait for redirect to project detail page (confirms creation succeeded)
    await page.waitForURL(new RegExp(`/projects/${projectName}`), { timeout: 10000 })

    // Create a secret with no data (skip Add Key, just name and submit)
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })
    const secretName = `e2e-empty-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByPlaceholder('my-secret').fill(secretName)
    // Do NOT click Add Key — create an empty secret
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })

    // Navigate to the detail page
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })

    // Click Edit to enter edit mode — grid should show one empty row
    await expect(page.getByRole('button', { name: /^edit$/i })).toBeVisible({ timeout: 5000 })
    await page.getByRole('button', { name: /^edit$/i }).click()

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
    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
    const projectDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await projectDeleteButton.click()
    await deleteOrg(page, orgName)
  })
})

test.describe('Mobile Responsive Layout', () => {
  // These tests run with the mobile-chrome project (iPhone 13 viewport)
  // and verify responsive behavior. On desktop viewport they are skipped.

  test('should show hamburger menu and hide sidebar on mobile', async ({ page }, testInfo) => {
    test.skip(testInfo.project?.name !== 'mobile-chrome', 'mobile-only test')
    await loginAndNavigate(page, '/profile')

    // Hamburger button should be visible (SidebarTrigger)
    await expect(page.getByRole('button', { name: /toggle sidebar/i })).toBeVisible({ timeout: 5000 })
  })

  test('should open drawer and navigate via hamburger on mobile', async ({ page }, testInfo) => {
    test.skip(testInfo.project?.name !== 'mobile-chrome', 'mobile-only test')
    await loginAndNavigate(page, '/profile')

    // Tap hamburger to open drawer
    await page.getByRole('button', { name: /toggle sidebar/i }).click()

    // Projects link should now be visible in drawer
    await expect(page.getByRole('link', { name: 'Projects' })).toBeVisible({ timeout: 5000 })

    // Navigate to Projects
    await page.getByRole('link', { name: 'Projects' }).click()

    // Should navigate to projects page
    await expect(page).toHaveURL(/\/projects/)
  })
})

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

// Test project name - created and cleaned up by these tests
const TEST_PROJECT = `e2e-secrets-${process.pid}`

// Helper function to log in via Dex
async function login(page: import('@playwright/test').Page) {
  // Click Sign In if present, or navigate to login if needed
  const signInButton = page.getByRole('button', { name: 'Sign In' })
  if (await signInButton.isVisible({ timeout: 2000 }).catch(() => false)) {
    await signInButton.click()
  }

  // Wait for redirect to Dex login page
  await page.waitForURL(/\/dex\//, { timeout: 5000 })

  // Navigate to login form if on connector selection
  const connectorLink = page.locator('a[href*="connector"]').first()
  if ((await connectorLink.count()) > 0) {
    await connectorLink.click()
    await page.waitForLoadState('networkidle')
  }

  // Fill in credentials
  const usernameInput = page.locator('input[name="login"]')
  const passwordInput = page.locator('input[name="password"]')

  await expect(usernameInput).toBeVisible({ timeout: 5000 })
  await usernameInput.fill(DEFAULT_USERNAME)
  await passwordInput.fill(DEFAULT_PASSWORD)

  // Submit login form
  await page.locator('button[type="submit"]').click()
}

// Helper to log in and return to the specified path
async function loginAndNavigate(page: import('@playwright/test').Page, path: string) {
  await page.goto('/profile')
  await login(page)
  await page.waitForURL(/\/profile/, { timeout: 15000 })
  if (path !== '/profile') {
    await page.goto(path)
  }
}

test.describe('Secrets Page', () => {
  test('should show projects link in sidebar', async ({ page }) => {
    await page.goto('/')

    // Verify Projects link is in sidebar
    await expect(page.getByRole('link', { name: 'Projects' })).toBeVisible()
  })

  test('should navigate to projects page from sidebar', async ({ page }) => {
    await page.goto('/')

    // Click Projects link
    await page.getByRole('link', { name: 'Projects' }).click()

    // Verify URL changed to projects page
    await expect(page).toHaveURL(/\/projects/)
  })

  test('should create secret with sharing and show sharing panel', async ({ page }) => {
    // Login first via profile page
    await loginAndNavigate(page, '/profile')

    // Create a test project first
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    const projectName = `e2e-sharing-prj-${Date.now()}`
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByLabel(/name/i).first().fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByText(/created/i)).toBeVisible({ timeout: 5000 })

    // Navigate to secrets list for this project
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    // Create a new secret
    const secretName = `e2e-sharing-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByLabel(/name/i).fill(secretName)
    await page.getByRole('button', { name: /add key/i }).click()
    await page.getByPlaceholder('key').fill('.env')
    await page.getByPlaceholder('value').fill('TEST_KEY=test_value')
    await page.getByRole('button', { name: /^create$/i }).click()

    // Wait for success
    await expect(page.getByText(/created successfully/i)).toBeVisible({ timeout: 5000 })

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

    // Clean up: delete the project
    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
    const projectDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await projectDeleteButton.click()
  })

  test('should update sharing grants on secret page', async ({ page }) => {
    // Login first via profile page
    await loginAndNavigate(page, '/profile')

    // Create a test project
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    const projectName = `e2e-share-upd-${Date.now()}`
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByLabel(/name/i).first().fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByText(/created/i)).toBeVisible({ timeout: 5000 })

    // Navigate to secrets list and create a test secret
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    const secretName = `e2e-share-update-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByLabel(/name/i).fill(secretName)
    await page.getByRole('button', { name: /add key/i }).click()
    await page.getByPlaceholder('key').fill('.env')
    await page.getByPlaceholder('value').fill('KEY=value')
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByText(/created successfully/i)).toBeVisible({ timeout: 5000 })

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

    // Clean up: delete the project
    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
    const projectDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await projectDeleteButton.click()
  })

  test('should show sharing summary in secrets list', async ({ page }) => {
    // Login first via profile page
    await loginAndNavigate(page, '/profile')

    // Create a test project
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    const projectName = `e2e-list-sum-${Date.now()}`
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByLabel(/name/i).first().fill(projectName)
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByText(/created/i)).toBeVisible({ timeout: 5000 })

    // Navigate to secrets list
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    // Create a test secret
    const secretName = `e2e-list-summary-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByLabel(/name/i).fill(secretName)
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByText(/created successfully/i)).toBeVisible({ timeout: 5000 })

    // Verify the secret shows in the list with sharing summary (at least "1 user" for the creator)
    await expect(page.getByText(/1 user/i)).toBeVisible({ timeout: 5000 })

    // Clean up: delete via the list
    await page.getByLabel(new RegExp(`delete ${secretName}`, 'i')).click()
    const dialogDelete = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await dialogDelete.click()
    await expect(page.getByText(/deleted successfully/i)).toBeVisible({ timeout: 5000 })

    // Clean up: delete the project
    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
    const projectDeleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await projectDeleteButton.click()
  })
})

test.describe('Mobile Responsive Layout', () => {
  // These tests run with the mobile-chrome project (iPhone 13 viewport)
  // and verify responsive behavior. On desktop viewport they are skipped.
  test.skip(({ browserName }, testInfo) => {
    return testInfo.project.name !== 'mobile-chrome'
  }, 'mobile-only test')

  test('should show hamburger menu and hide sidebar on mobile', async ({ page }) => {
    await page.goto('/')

    // Hamburger button should be visible (SidebarTrigger)
    await expect(page.getByLabel(/toggle sidebar/i)).toBeVisible({ timeout: 5000 })
  })

  test('should open drawer and navigate via hamburger on mobile', async ({ page }) => {
    await page.goto('/')

    // Tap hamburger to open drawer
    await page.getByLabel(/toggle sidebar/i).click()

    // Projects link should now be visible in drawer
    await expect(page.getByRole('link', { name: 'Projects' })).toBeVisible({ timeout: 5000 })

    // Navigate to Projects
    await page.getByRole('link', { name: 'Projects' }).click()

    // Should navigate to projects page
    await expect(page).toHaveURL(/\/projects/)
  })
})

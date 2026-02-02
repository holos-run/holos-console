import { test, expect } from '@playwright/test'

/**
 * E2E tests for Secrets page.
 *
 * These tests verify the GetSecret RPC flow through the UI.
 * Run with: make test-e2e (automatically starts servers)
 *
 * Default credentials (configurable via env vars on the Go backend):
 *   Username: admin (HOLOS_DEX_INITIAL_ADMIN_USERNAME)
 *   Password: verysecret (HOLOS_DEX_INITIAL_ADMIN_PASSWORD)
 */

// Default credentials for embedded Dex OIDC provider
const DEFAULT_USERNAME = 'admin'
const DEFAULT_PASSWORD = 'verysecret'

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

test.describe('Secrets Page', () => {
  test('should show secrets link in sidebar', async ({ page }) => {
    await page.goto('/ui')

    // Verify Secrets link is in sidebar
    await expect(page.getByRole('link', { name: 'Secrets' })).toBeVisible()
  })

  test('should navigate to secrets page from sidebar', async ({ page }) => {
    await page.goto('/ui')

    // Click Secrets link
    await page.getByRole('link', { name: 'Secrets' }).click()

    // Verify URL changed to secrets page
    await expect(page).toHaveURL(/\/ui\/secrets\//)
  })

  test('should redirect to login when accessing secrets unauthenticated', async ({
    page,
  }) => {
    // Navigate directly to secrets page without auth
    await page.goto('/ui/secrets/dummy-secret')

    // Should redirect to Dex login
    await page.waitForURL(/\/dex\//, { timeout: 10000 })
  })

  test('should display dummy-secret data as key entries after login', async ({
    page,
  }) => {
    // Navigate to secrets page
    await page.goto('/ui/secrets/dummy-secret')

    // Login when redirected
    await login(page)

    // Wait for redirect back to secrets page
    await page.waitForURL(/\/ui\/secrets\/dummy-secret/, { timeout: 15000 })

    // Verify the page shows the secret name
    await expect(page.getByRole('heading', { name: /dummy-secret/i })).toBeVisible({ timeout: 5000 })

    // Verify key fields are present with expected keys
    const filenames = page.getByPlaceholder('key')
    await expect(filenames.first()).toBeVisible({ timeout: 5000 })
    const count = await filenames.count()
    expect(count).toBe(3)

    // Collect all key and value entries
    const filenameValues: string[] = []
    const contentValues: string[] = []
    const contents = page.getByPlaceholder('value')
    for (let i = 0; i < count; i++) {
      filenameValues.push(await filenames.nth(i).inputValue())
      contentValues.push(await contents.nth(i).inputValue())
    }

    expect(filenameValues).toContain('username')
    expect(filenameValues).toContain('password')
    expect(filenameValues).toContain('api-key')

    const idx = (name: string) => filenameValues.indexOf(name)
    expect(contentValues[idx('username')]).toBe('dummy-user')
    expect(contentValues[idx('password')]).toBe('dummy-password')
    expect(contentValues[idx('api-key')]).toBe('dummy-api-key-12345')
  })

  test('should display NotFound error for non-existent secret', async ({
    page,
  }) => {
    // First login via profile page
    await page.goto('/ui/profile')
    await login(page)
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

    // Now navigate to a non-existent secret
    await page.goto('/ui/secrets/non-existent-secret')

    // Wait for error to appear
    await expect(page.getByText(/not found/i)).toBeVisible({ timeout: 10000 })
  })

  test('should create secret with sharing and show sharing panel', async ({ page }) => {
    // Login first via profile page
    await page.goto('/ui/profile')
    await login(page)
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

    // Navigate to secrets list
    await page.goto('/ui/secrets')
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    // Create a new secret
    const secretName = `e2e-sharing-${Date.now()}`
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByLabel(/name/i).fill(secretName)
    await page.getByRole('button', { name: /add key/i }).click()
    await page.getByPlaceholder('key').fill('.env')
    await page.getByPlaceholder('value').fill('TEST_KEY=test_value')
    await page.getByRole('button', { name: /^create$/i }).click()

    // Wait for success snackbar
    await expect(page.getByText(/created successfully/i)).toBeVisible({ timeout: 5000 })

    // Navigate to the created secret
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/ui/secrets/${secretName}`), { timeout: 5000 })

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
    await page.waitForURL(/\/ui\/secrets\/?$/, { timeout: 5000 })
  })

  test('should update sharing grants on secret page', async ({ page }) => {
    // Login first via profile page
    await page.goto('/ui/profile')
    await login(page)
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

    // Navigate to secrets list and create a test secret
    await page.goto('/ui/secrets')
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
    await page.waitForURL(new RegExp(`/ui/secrets/${secretName}`), { timeout: 5000 })

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
    await page.waitForURL(/\/ui\/secrets\/?$/, { timeout: 5000 })
  })

  test('should show sharing summary in secrets list', async ({ page }) => {
    // Login first via profile page
    await page.goto('/ui/profile')
    await login(page)
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

    // Navigate to secrets list
    await page.goto('/ui/secrets')
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
  })

  test('should take screenshot of secrets page', async ({ page }) => {
    // Navigate and login
    await page.goto('/ui/secrets/dummy-secret')
    await login(page)
    await page.waitForURL(/\/ui\/secrets\/dummy-secret/, { timeout: 15000 })

    // Wait for content to load
    await expect(page.getByPlaceholder('key').first()).toBeVisible({ timeout: 5000 })

    // Take screenshot
    await page.screenshot({
      path: 'e2e/screenshots/secrets-page.png',
      fullPage: true,
    })
  })
})

test.describe('Mobile Responsive Layout', () => {
  // These tests run with the mobile-chrome project (iPhone 13 viewport)
  // and verify responsive behavior. On desktop viewport they are skipped.
  test.skip(({ browserName }, testInfo) => {
    return testInfo.project.name !== 'mobile-chrome'
  }, 'mobile-only test')

  test('should show hamburger menu and hide sidebar on mobile', async ({ page }) => {
    await page.goto('/ui')

    // Hamburger button should be visible
    await expect(page.getByLabel(/open menu/i)).toBeVisible({ timeout: 5000 })

    // Sidebar nav links should not be visible initially
    await expect(page.getByRole('link', { name: 'Secrets' })).not.toBeVisible()
  })

  test('should open drawer and navigate via hamburger on mobile', async ({ page }) => {
    await page.goto('/ui')

    // Tap hamburger to open drawer
    await page.getByLabel(/open menu/i).click()

    // Secrets link should now be visible in drawer
    await expect(page.getByRole('link', { name: 'Secrets' })).toBeVisible({ timeout: 5000 })

    // Navigate to Secrets
    await page.getByRole('link', { name: 'Secrets' }).click()

    // Should redirect to Dex login (unauthenticated)
    await page.waitForURL(/\/dex\//, { timeout: 10000 })
  })

  test('should create secret via full-screen dialog on mobile', async ({ page }) => {
    // Login first
    await page.goto('/ui/profile')
    await login(page)
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

    // Navigate to secrets via hamburger
    await page.getByLabel(/open menu/i).click()
    await page.getByRole('link', { name: 'Secrets' }).click()
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })

    // Open create dialog (should be full-screen on mobile)
    await page.getByRole('button', { name: /create secret/i }).click()

    // The dialog should be visible and full-screen
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()

    // Create a secret
    const secretName = `e2e-mobile-${Date.now()}`
    await page.getByLabel(/name/i).fill(secretName)
    await page.getByRole('button', { name: /^create$/i }).click()

    // Wait for success
    await expect(page.getByText(/created successfully/i)).toBeVisible({ timeout: 5000 })

    // Clean up: delete the secret
    await page.getByRole('link', { name: secretName }).click()
    await page.waitForURL(new RegExp(`/ui/secrets/${secretName}`), { timeout: 5000 })
    await page.getByRole('button', { name: /^delete$/i }).click()
    const dialogDelete = page.getByRole('dialog').getByRole('button', { name: /delete/i })
    await dialogDelete.click()
    await page.waitForURL(/\/ui\/secrets\/?$/, { timeout: 5000 })
  })
})

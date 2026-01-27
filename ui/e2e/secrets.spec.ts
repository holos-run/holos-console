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

  test('should display dummy-secret data in env format after login', async ({
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

    // Verify the textbox contains env format data
    const textbox = page.getByRole('textbox')
    await expect(textbox).toBeVisible()

    // Check that the textbox contains expected env format values
    await expect(textbox).toHaveValue(/username=dummy-user/)
    await expect(textbox).toHaveValue(/password=dummy-password/)
    await expect(textbox).toHaveValue(/api-key=dummy-api-key-12345/)
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

  test('should take screenshot of secrets page', async ({ page }) => {
    // Navigate and login
    await page.goto('/ui/secrets/dummy-secret')
    await login(page)
    await page.waitForURL(/\/ui\/secrets\/dummy-secret/, { timeout: 15000 })

    // Wait for content to load
    await expect(page.getByRole('textbox')).toBeVisible({ timeout: 5000 })

    // Take screenshot
    await page.screenshot({
      path: 'e2e/screenshots/secrets-page.png',
      fullPage: true,
    })
  })
})

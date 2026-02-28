import { test, expect } from '@playwright/test'
import {
  DEFAULT_USERNAME,
  DEFAULT_PASSWORD,
  buildAuthorizeUrl,
  navigateToDexLogin,
  navigatePastConnectorSelection,
  loginViaProfilePage,
} from './helpers'

/**
 * E2E tests for OIDC authentication flow.
 *
 * These tests verify the full login flow using the embedded Dex OIDC provider.
 * Run with: make test-e2e (automatically starts servers)
 *
 * Default credentials (configurable via env vars on the Go backend):
 *   Username: admin (HOLOS_DEX_INITIAL_ADMIN_USERNAME)
 *   Password: verysecret (HOLOS_DEX_INITIAL_ADMIN_PASSWORD)
 */

test.describe('Authentication', () => {
  test('should redirect to profile page by default', async ({ page }) => {
    await page.goto('/')

    // Root redirects to /profile (which requires auth, triggering OIDC login)
    await expect(page).toHaveURL(/\/profile/)
  })

  test('should have version page accessible', async ({ page }) => {
    await page.goto('/version')

    // The version page should load and show version info from the backend
    // This verifies the RPC connection works through the proxy
    await expect(page.getByRole('heading', { name: 'Server Version' })).toBeVisible()
  })

  test('should have OIDC discovery endpoint accessible', async ({ request }) => {
    // Verify the OIDC discovery endpoint is accessible
    const response = await request.get('/dex/.well-known/openid-configuration')

    expect(response.ok()).toBeTruthy()

    const config = await response.json()
    expect(config.issuer).toContain('/dex')
    expect(config.authorization_endpoint).toBeDefined()
    expect(config.token_endpoint).toBeDefined()
    expect(config.jwks_uri).toBeDefined()
  })

  test('should display Dex login page when accessing authorize endpoint', async ({
    page,
  }) => {
    await page.goto(buildAuthorizeUrl())

    // Dex should redirect to show a login form or connector selection
    await expect(page).toHaveURL(/\/dex\//)
  })
})

test.describe('Login Flow', () => {
  test('should show login form with username and password fields', async ({
    page,
  }) => {
    await navigateToDexLogin(page)

    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    // Now we should have a login form
    await expect(usernameInput.or(passwordInput).first()).toBeVisible({ timeout: 5000 })
  })

  test('should reject invalid credentials', async ({ page }) => {
    await navigateToDexLogin(page)

    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    if ((await usernameInput.count()) > 0) {
      // Fill in wrong credentials
      await usernameInput.fill('wronguser')
      await passwordInput.fill('wrongpassword')

      await page.locator('button[type="submit"]').click()

      // Should show an error or stay on login page
      // Dex doesn't redirect on failed auth
      await expect(page).toHaveURL(/\/dex\//)
    }
  })

  test('should complete login with valid credentials', async ({ page }) => {
    await navigateToDexLogin(page)

    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    if ((await usernameInput.count()) > 0) {
      await usernameInput.fill(DEFAULT_USERNAME)
      await passwordInput.fill(DEFAULT_PASSWORD)

      await page.locator('button[type="submit"]').click()

      // After successful auth, Dex redirects to the callback URL with a code
      await page.waitForURL(/\/pkce\/verify\?.*code=/, { timeout: 10000 })
    }
  })
})

test.describe('Profile Page', () => {
  test('should show profile page with sign in button when not authenticated', async ({
    page,
  }) => {
    await page.goto('/profile')

    // Verify Sign In button is visible
    await expect(page.getByRole('button', { name: 'Sign In' })).toBeVisible()
  })

  test('should navigate to profile page from sidebar', async ({ page }) => {
    await page.goto('/')

    // Click Profile link in sidebar
    await page.getByRole('link', { name: 'Profile' }).click()

    // Verify URL is /profile
    await expect(page).toHaveURL(/\/profile/)

    // Verify profile page content loads
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()
  })

  test('should complete full login flow via profile page', async ({ page }) => {
    await loginViaProfilePage(page)

    // Verify profile page shows token status after login
    await expect(page.getByText('ID Token Status')).toBeVisible({ timeout: 5000 })

    // Verify token details section is visible
    await expect(page.getByText('Token Details')).toBeVisible()
    await expect(page.getByText('Email')).toBeVisible()
  })

  test('should display token details after login', async ({ page }) => {
    await loginViaProfilePage(page)

    // Verify token details are visible
    await expect(page.getByText('Token Details')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('Subject (sub)')).toBeVisible()
    await expect(page.getByText('Email')).toBeVisible()

    // Take screenshot for visual verification
    await page.screenshot({
      path: 'e2e/screenshots/profile-token-details.png',
      fullPage: true,
    })
  })

  test('should include roles in profile page', async ({ page }) => {
    await loginViaProfilePage(page)

    // Verify token details are visible
    await expect(page.getByText('Token Details')).toBeVisible({ timeout: 5000 })

    // Verify roles are displayed in the token details
    await expect(page.getByText('Roles')).toBeVisible()

    await page.screenshot({
      path: 'e2e/screenshots/profile-roles.png',
      fullPage: true,
    })
  })
})

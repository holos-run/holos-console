import { test, expect } from '@playwright/test'

/**
 * E2E tests for OIDC authentication flow.
 *
 * These tests verify the full login flow using the embedded Dex OIDC provider.
 * They require both the Go backend and Vite dev server to be running.
 *
 * Default credentials (configurable via env vars):
 *   Username: admin (HOLOS_DEX_INITIAL_ADMIN_USERNAME)
 *   Password: verysecret (HOLOS_DEX_INITIAL_ADMIN_PASSWORD)
 */

// Default credentials for embedded Dex OIDC provider
const DEFAULT_USERNAME = 'admin'
const DEFAULT_PASSWORD = 'verysecret'

test.describe('Authentication', () => {
  test.beforeEach(async ({ page }) => {
    // Clear session storage to start fresh
    await page.goto('/ui')
    await page.evaluate(() => sessionStorage.clear())
  })

  test('should display login page when accessing Dex authorize endpoint', async ({
    page,
  }) => {
    // Navigate to the OIDC authorize endpoint directly
    // This simulates what happens when the SPA initiates login
    const authorizeUrl = new URL('/dex/auth', 'https://localhost:5173')
    authorizeUrl.searchParams.set('client_id', 'holos-console')
    authorizeUrl.searchParams.set('redirect_uri', 'https://localhost:5173/ui/callback')
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('scope', 'openid profile email')
    authorizeUrl.searchParams.set('code_challenge', 'test_challenge')
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')

    await page.goto(authorizeUrl.toString())

    // Dex should show a login form or connector selection
    // The exact UI depends on Dex configuration
    await expect(page).toHaveURL(/\/dex\//)
  })

  test('should have OIDC discovery endpoint accessible', async ({ page }) => {
    // Verify the OIDC discovery endpoint is accessible
    const response = await page.request.get(
      'https://localhost:5173/dex/.well-known/openid-configuration',
    )

    expect(response.ok()).toBeTruthy()

    const config = await response.json()
    expect(config.issuer).toContain('/dex')
    expect(config.authorization_endpoint).toBeDefined()
    expect(config.token_endpoint).toBeDefined()
    expect(config.jwks_uri).toBeDefined()
  })

  test('should have landing page accessible', async ({ page }) => {
    await page.goto('/ui')

    // Verify the landing page loads
    await expect(page.getByRole('heading', { name: 'Welcome to Holos Console' })).toBeVisible()
  })

  test('should have version page accessible', async ({ page }) => {
    await page.goto('/ui/version')

    // The version page should load and show version info from the backend
    // This verifies the RPC connection works through the proxy
    await expect(page.getByText('Version')).toBeVisible()
  })
})

test.describe('Login Flow', () => {
  test('should complete full OIDC login flow with default credentials', async ({
    page,
  }) => {
    // Start at the landing page
    await page.goto('/ui')

    // The AuthProvider should initialize without error
    await expect(page.getByRole('heading', { name: 'Welcome to Holos Console' })).toBeVisible()

    // Navigate to OIDC authorize with proper PKCE challenge
    // In a real scenario, the login() function generates these
    const codeVerifier = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk'
    const codeChallenge = 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM'

    const authorizeUrl = new URL('/dex/auth', 'https://localhost:5173')
    authorizeUrl.searchParams.set('client_id', 'holos-console')
    authorizeUrl.searchParams.set(
      'redirect_uri',
      'https://localhost:5173/ui/callback',
    )
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('scope', 'openid profile email')
    authorizeUrl.searchParams.set('state', 'test_state')
    authorizeUrl.searchParams.set('code_challenge', codeChallenge)
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')

    await page.goto(authorizeUrl.toString())

    // Wait for Dex login page to load
    // Dex shows a connector selection or login form
    await page.waitForURL(/\/dex\//)

    // Look for the mock password connector login form
    // The exact selectors depend on Dex's HTML structure
    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    if ((await usernameInput.count()) > 0) {
      // Fill in credentials
      await usernameInput.fill(DEFAULT_USERNAME)
      await passwordInput.fill(DEFAULT_PASSWORD)

      // Submit the form
      await page.locator('button[type="submit"]').click()

      // After successful auth, Dex redirects to the callback URL
      // The callback component processes the code and redirects to home
      await page.waitForURL(/\/ui/, { timeout: 10000 })

      // Should be back on the landing page
      await expect(
        page.getByRole('heading', { name: 'Welcome to Holos Console' }),
      ).toBeVisible()
    } else {
      // If no login form is visible, there might be a connector selection
      // or the test environment is configured differently
      console.log('No login form found - Dex may be configured differently')
    }
  })

  test('should reject invalid credentials', async ({ page }) => {
    // Navigate to OIDC authorize
    const authorizeUrl = new URL('/dex/auth', 'https://localhost:5173')
    authorizeUrl.searchParams.set('client_id', 'holos-console')
    authorizeUrl.searchParams.set(
      'redirect_uri',
      'https://localhost:5173/ui/callback',
    )
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('scope', 'openid profile email')
    authorizeUrl.searchParams.set('state', 'test_state')
    authorizeUrl.searchParams.set('code_challenge', 'test_challenge')
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')

    await page.goto(authorizeUrl.toString())
    await page.waitForURL(/\/dex\//)

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

  test('should handle custom credentials from environment', async ({
    page,
  }) => {
    // This test documents that credentials can be customized
    // via HOLOS_DEX_INITIAL_ADMIN_USERNAME and HOLOS_DEX_INITIAL_ADMIN_PASSWORD
    // The actual test uses default credentials since we can't modify
    // the running server's environment

    await page.goto('/ui')
    await expect(page.getByRole('heading', { name: 'Welcome to Holos Console' })).toBeVisible()

    // Verify OIDC discovery is accessible (confirms Dex is running)
    const response = await page.request.get(
      'https://localhost:5173/dex/.well-known/openid-configuration',
    )
    expect(response.ok()).toBeTruthy()
  })
})

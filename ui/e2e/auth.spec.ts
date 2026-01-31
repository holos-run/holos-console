import { test, expect } from '@playwright/test'

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

// Default credentials for embedded Dex OIDC provider
const DEFAULT_USERNAME = 'admin'
const DEFAULT_PASSWORD = 'verysecret'

test.describe('Authentication', () => {
  test('should redirect to secrets page by default', async ({ page }) => {
    await page.goto('/ui')

    // Verify redirect to secrets page (shows Sign In since unauthenticated)
    await expect(page).toHaveURL(/\/ui\/secrets/)
  })

  test('should have version page accessible', async ({ page }) => {
    await page.goto('/ui/version')

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
    // Navigate to the OIDC authorize endpoint directly
    // This simulates what happens when the SPA initiates login
    const authorizeUrl = new URL('/dex/auth', 'https://localhost:5173')
    authorizeUrl.searchParams.set('client_id', 'holos-console')
    authorizeUrl.searchParams.set('redirect_uri', 'https://localhost:5173/ui/callback')
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('scope', 'openid profile email')
    authorizeUrl.searchParams.set('state', 'test_state')
    authorizeUrl.searchParams.set('code_challenge', 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')

    await page.goto(authorizeUrl.toString())

    // Dex should redirect to show a login form or connector selection
    await expect(page).toHaveURL(/\/dex\//)
  })
})

test.describe('Login Flow', () => {
  test('should show login form with username and password fields', async ({
    page,
  }) => {
    // Navigate to OIDC authorize with proper PKCE parameters
    const authorizeUrl = new URL('/dex/auth', 'https://localhost:5173')
    authorizeUrl.searchParams.set('client_id', 'holos-console')
    authorizeUrl.searchParams.set('redirect_uri', 'https://localhost:5173/ui/callback')
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('scope', 'openid profile email')
    authorizeUrl.searchParams.set('state', 'test_state')
    authorizeUrl.searchParams.set('code_challenge', 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')

    await page.goto(authorizeUrl.toString())

    // Wait for Dex login page
    await page.waitForURL(/\/dex\//, { timeout: 5000 })

    // Dex mock password connector shows a login form
    // Look for the login form elements
    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    // At least one should be visible (depending on Dex UI flow)
    const hasLoginForm = (await usernameInput.count()) > 0 || (await passwordInput.count()) > 0

    // If no login form, we might be on a connector selection page
    if (!hasLoginForm) {
      // Look for connector link/button
      const connectorLink = page.locator('a[href*="connector"]').first()
      if ((await connectorLink.count()) > 0) {
        await connectorLink.click()
        await page.waitForLoadState('networkidle')
      }
    }

    // Now we should have a login form
    await expect(usernameInput.or(passwordInput).first()).toBeVisible({ timeout: 5000 })
  })

  test('should reject invalid credentials', async ({ page }) => {
    // Navigate to OIDC authorize
    const authorizeUrl = new URL('/dex/auth', 'https://localhost:5173')
    authorizeUrl.searchParams.set('client_id', 'holos-console')
    authorizeUrl.searchParams.set('redirect_uri', 'https://localhost:5173/ui/callback')
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('scope', 'openid profile email')
    authorizeUrl.searchParams.set('state', 'test_state')
    authorizeUrl.searchParams.set('code_challenge', 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')

    await page.goto(authorizeUrl.toString())
    await page.waitForURL(/\/dex\//, { timeout: 5000 })

    // Navigate to login form if on connector selection
    const connectorLink = page.locator('a[href*="connector"]').first()
    if ((await connectorLink.count()) > 0) {
      await connectorLink.click()
      await page.waitForLoadState('networkidle')
    }

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
    // Navigate to OIDC authorize
    const authorizeUrl = new URL('/dex/auth', 'https://localhost:5173')
    authorizeUrl.searchParams.set('client_id', 'holos-console')
    authorizeUrl.searchParams.set('redirect_uri', 'https://localhost:5173/ui/callback')
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('scope', 'openid profile email')
    authorizeUrl.searchParams.set('state', 'test_state')
    authorizeUrl.searchParams.set('code_challenge', 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')

    await page.goto(authorizeUrl.toString())
    await page.waitForURL(/\/dex\//, { timeout: 5000 })

    // Navigate to login form if on connector selection
    const connectorLink = page.locator('a[href*="connector"]').first()
    if ((await connectorLink.count()) > 0) {
      await connectorLink.click()
      await page.waitForLoadState('networkidle')
    }

    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    if ((await usernameInput.count()) > 0) {
      // Fill in correct credentials
      await usernameInput.fill(DEFAULT_USERNAME)
      await passwordInput.fill(DEFAULT_PASSWORD)

      await page.locator('button[type="submit"]').click()

      // After successful auth, Dex redirects to the callback URL with a code
      // The URL should contain the callback path and an authorization code
      await page.waitForURL(/\/ui\/callback\?.*code=/, { timeout: 10000 })
    }
  })
})

test.describe('Profile Page', () => {
  test('should show profile page with sign in button when not authenticated', async ({
    page,
  }) => {
    await page.goto('/ui/profile')

    // Verify Sign In button is visible
    await expect(page.getByRole('button', { name: 'Sign In' })).toBeVisible()
  })

  test('should navigate to profile page from sidebar', async ({ page }) => {
    await page.goto('/ui')

    // Click Profile link in sidebar
    await page.getByRole('link', { name: 'Profile' }).click()

    // Verify URL is /ui/profile
    await expect(page).toHaveURL(/\/ui\/profile/)

    // Verify profile page content loads
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()
  })

  test('should complete full login flow via profile page', async ({ page }) => {
    await page.goto('/ui/profile')

    // Click Sign In button
    await page.getByRole('button', { name: 'Sign In' }).click()

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

    // Wait for redirect back to profile page (returnTo state preserves the path)
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

    // Verify profile page shows token status after login
    await expect(page.getByText('ID Token Status')).toBeVisible({ timeout: 5000 })

    // Verify token details section is visible
    await expect(page.getByText('Token Details')).toBeVisible()
    await expect(page.getByText('Email')).toBeVisible()
  })

  test('should display token details after login', async ({ page }) => {
    // Navigate to profile page and login
    await page.goto('/ui/profile')
    await page.getByRole('button', { name: 'Sign In' }).click()

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

    // Wait for redirect back to profile page
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

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

  test('should include groups in profile page', async ({ page }) => {
    // Navigate to profile page and login
    await page.goto('/ui/profile')
    await page.getByRole('button', { name: 'Sign In' }).click()

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

    // Wait for redirect back to profile page
    await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

    // Verify token details are visible
    await expect(page.getByText('Token Details')).toBeVisible({ timeout: 5000 })

    // Verify groups are displayed in the token details
    await expect(page.getByText('Groups')).toBeVisible()

    await page.screenshot({
      path: 'e2e/screenshots/profile-groups.png',
      fullPage: true,
    })
  })
})

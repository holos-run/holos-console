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
  test('should auto-login unauthenticated users via OIDC', async ({ page }) => {
    await page.goto('/')

    // Root → /profile → OIDC auto-redirect (PR #230 removed the Sign In button).
    // With --enable-insecure-dex the Dex auto-connector completes auth without a
    // login form, so the full redirect chain (/ → /dex/auth → /pkce/verify → /profile)
    // resolves faster than Playwright can poll for /dex/. Verify the end state instead:
    // the user is authenticated and the profile page content is visible.
    await expect(page).toHaveURL(/\/profile/, { timeout: 15000 })
    await expect(page.getByText('ID Token Status')).toBeVisible({ timeout: 10000 })
  })

  test('should have about page accessible after login', async ({ page }) => {
    await loginViaProfilePage(page)
    await page.goto('/about')

    // The about page should load and show version info from the backend
    // This verifies the RPC connection works through the proxy
    await expect(page.getByText('Server Version')).toBeVisible({ timeout: 10000 })
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

    // Dex should redirect to show a login form or auto-complete with a code
    // (auto-complete happens when Dex has an existing server-side session)
    await expect(page).toHaveURL(/\/dex\/|\/pkce\/verify/)
  })
})

test.describe('Login Flow', () => {
  test('should show login form with username and password fields', async ({
    page,
  }) => {
    const showedForm = await navigateToDexLogin(page)
    if (!showedForm) {
      // Dex auto-completed — skip this test gracefully
      test.skip()
      return
    }

    const usernameInput = page.locator('input[name="login"]')
    const passwordInput = page.locator('input[name="password"]')

    // Now we should have a login form
    await expect(usernameInput.or(passwordInput).first()).toBeVisible({ timeout: 5000 })
  })

  test('should reject invalid credentials', async ({ page }) => {
    const showedForm = await navigateToDexLogin(page)
    if (!showedForm) {
      test.skip()
      return
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
    const showedForm = await navigateToDexLogin(page)
    if (!showedForm) {
      // Dex auto-completed the auth — verify we got redirected with a code
      await expect(page).toHaveURL(/\/pkce\/verify\?.*code=/)
      return
    }

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
  test('should auto-login unauthenticated users navigating to profile', async ({
    page,
  }) => {
    // After PR #230, unauthenticated users navigating to /profile are
    // automatically redirected through OIDC — no Sign In button is shown.
    // With --enable-insecure-dex the Dex auto-connector completes auth without
    // a form, so verify the end state: user lands back at /profile authenticated.
    await page.goto('/profile')
    await expect(page).toHaveURL(/\/profile/, { timeout: 15000 })
    await expect(page.getByText('ID Token Status')).toBeVisible({ timeout: 10000 })
  })

  test('should navigate to profile page from sidebar', async ({ page }) => {
    // Login + cross-page navigation takes extra time on mobile CI.
    test.setTimeout(60_000)

    await loginViaProfilePage(page)

    // Navigate away from profile to test sidebar navigation. Use a project
    // route rather than /about (sidebar footer, adjacent to Profile) to avoid
    // the About link intercepting clicks on Profile on mobile.
    await page.goto('/projects/e2e-auth-nav-test/secrets')
    await page.waitForLoadState('networkidle')

    // On mobile viewports, open the sidebar drawer first
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible().catch(() => false)) {
      await sidebarTrigger.click()
      // Wait for drawer animation to complete before clicking the link
      await page.waitForTimeout(500)
    }

    // Click Profile link in sidebar
    await page.getByRole('link', { name: 'Profile' }).click()

    // Verify URL is /profile
    await expect(page).toHaveURL(/\/profile/)

    // Verify profile page content loads
    await expect(page.getByText('ID Token Status')).toBeVisible()
  })

  test('should complete full login flow via profile page', async ({ page }) => {
    await loginViaProfilePage(page)

    // Verify profile page shows token status after login
    await expect(page.getByText('ID Token Status')).toBeVisible({ timeout: 5000 })

    // Verify token claims section is visible
    await expect(page.getByText('Token Claims')).toBeVisible()
    await expect(page.getByText('Email', { exact: true })).toBeVisible()
  })

  test('should display token claims after login', async ({ page }) => {
    await loginViaProfilePage(page)

    // Verify claims view is visible by default
    await expect(page.getByText('Token Claims')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('Subject (sub)')).toBeVisible()
    await expect(page.getByText('Email', { exact: true })).toBeVisible()
    await expect(page.getByText('Issuer (iss)')).toBeVisible()
    await expect(page.getByText('Audience (aud)')).toBeVisible()
    await expect(page.getByText('Issued At (iat)')).toBeVisible()
    await expect(page.getByText('Expires (exp)')).toBeVisible()

    await page.screenshot({
      path: 'e2e/screenshots/profile-token-claims.png',
      fullPage: true,
    })
  })

  test('should include roles / groups in claims view', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('Token Claims')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('Roles / Groups')).toBeVisible()

    await page.screenshot({
      path: 'e2e/screenshots/profile-roles.png',
      fullPage: true,
    })
  })

  test('should display iss claim from embedded Dex', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('Token Claims')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('Issuer (iss)')).toBeVisible()
    await expect(page.getByText('Audience (aud)')).toBeVisible()

    // Verify the issuer value from embedded Dex contains /dex
    const issuerValue = page.locator('p.font-mono.break-all')
    await expect(issuerValue).toContainText('/dex')
  })

  test('should switch to raw JSON view and show complete claims', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('Token Claims')).toBeVisible({ timeout: 5000 })

    // Click the Raw button in the segmented control
    await page.getByRole('button', { name: /raw/i }).last().click()

    // Verify JSON is displayed. Use the explicit role="code" attribute on the
    // raw-JSON pre element to avoid matching inline <code> elements that the
    // API Access card adds to the same page.
    const pre = page.locator('[role="code"]')
    await expect(pre).toBeVisible()
    await expect(pre).toContainText('"iss"')
    await expect(pre).toContainText('"aud"')
    await expect(pre).toContainText('"sub"')

    // Verify copy button
    await expect(page.getByRole('button', { name: /copy to clipboard/i })).toBeVisible()

    await page.screenshot({
      path: 'e2e/screenshots/profile-raw-claims.png',
      fullPage: true,
    })
  })
})

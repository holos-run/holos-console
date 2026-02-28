import type { Page } from '@playwright/test'

// Default credentials for embedded Dex OIDC provider
export const DEFAULT_USERNAME = 'admin'
export const DEFAULT_PASSWORD = 'verysecret'

/**
 * Build a Dex OIDC authorize URL with PKCE parameters.
 */
export function buildAuthorizeUrl(): string {
  const url = new URL('/dex/auth', 'https://localhost:5173')
  url.searchParams.set('client_id', 'holos-console')
  url.searchParams.set('redirect_uri', 'https://localhost:5173/pkce/verify')
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', 'openid profile email')
  url.searchParams.set('state', 'test_state')
  url.searchParams.set('code_challenge', 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
  url.searchParams.set('code_challenge_method', 'S256')
  return url.toString()
}

/**
 * Navigate past the Dex connector selection page if present.
 * Call this after landing on /dex/.
 */
export async function navigatePastConnectorSelection(page: Page): Promise<void> {
  const connectorLink = page.locator('a[href*="connector"]').first()
  if ((await connectorLink.count()) > 0) {
    await connectorLink.click()
    await page.waitForLoadState('networkidle')
  }
}

/**
 * Navigate to the Dex authorize endpoint and wait for the login page.
 */
export async function navigateToDexLogin(page: Page): Promise<void> {
  await page.goto(buildAuthorizeUrl())
  await page.waitForURL(/\/dex\//, { timeout: 5000 })
  await navigatePastConnectorSelection(page)
}

/**
 * Complete the full login flow via the profile page: navigate to /profile,
 * click Sign In, fill credentials, submit, and wait for redirect back.
 */
export async function loginViaProfilePage(page: Page): Promise<void> {
  await page.goto('/profile')
  await page.getByRole('button', { name: 'Sign In' }).click()
  await page.waitForURL(/\/dex\//, { timeout: 5000 })
  await navigatePastConnectorSelection(page)

  const usernameInput = page.locator('input[name="login"]')
  const passwordInput = page.locator('input[name="password"]')

  await usernameInput.fill(DEFAULT_USERNAME)
  await passwordInput.fill(DEFAULT_PASSWORD)
  await page.locator('button[type="submit"]').click()

  await page.waitForURL(/\/profile/, { timeout: 15000 })
}

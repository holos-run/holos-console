import { test, expect } from '@playwright/test'
import {
  loginAsPersona,
  switchPersona,
  getPersonaToken,
  apiCreateOrg,
  apiDeleteOrg,
  apiGrantOrgAccess,
  PLATFORM_ENGINEER_EMAIL,
  PRODUCT_ENGINEER_EMAIL,
  SRE_EMAIL,
  ADMIN_EMAIL,
} from './helpers'

/**
 * E2E tests for multi-persona token acquisition and RBAC verification.
 *
 * These tests verify that:
 * 1. The dev token endpoint returns valid tokens for each persona
 * 2. Persona switching works correctly in the browser session
 * 3. RBAC grants are respected across different personas
 *
 * The dev token endpoint (/api/dev/token) is available whenever
 * --enable-insecure-dex is enabled, which is always the case in E2E tests.
 *
 * Tests that create K8s resources (orgs) require a running cluster.
 * Run with: make test-e2e
 */

test.describe('Dev Token Endpoint', () => {
  test('should return a valid token for the platform engineer persona', async ({
    page,
  }) => {
    // Navigate to /profile to establish a page context with the app loaded
    await page.goto('/profile')
    await page.waitForURL(/\/dex\/|\/pkce\/verify|\/profile/, { timeout: 15000 })
    // Wait for the page to settle (auto-login may redirect)
    await page.waitForLoadState('networkidle')

    const tokenData = await getPersonaToken(page, PLATFORM_ENGINEER_EMAIL)

    expect(tokenData.id_token).toBeTruthy()
    expect(tokenData.email).toBe(PLATFORM_ENGINEER_EMAIL)
    expect(tokenData.groups).toContain('owner')
    expect(tokenData.expires_in).toBeGreaterThan(0)
  })

  test('should return a valid token for the product engineer persona', async ({
    page,
  }) => {
    await page.goto('/profile')
    await page.waitForURL(/\/dex\/|\/pkce\/verify|\/profile/, { timeout: 15000 })
    await page.waitForLoadState('networkidle')

    const tokenData = await getPersonaToken(page, PRODUCT_ENGINEER_EMAIL)

    expect(tokenData.id_token).toBeTruthy()
    expect(tokenData.email).toBe(PRODUCT_ENGINEER_EMAIL)
    expect(tokenData.groups).toContain('editor')
    expect(tokenData.expires_in).toBeGreaterThan(0)
  })

  test('should return a valid token for the SRE persona', async ({ page }) => {
    await page.goto('/profile')
    await page.waitForURL(/\/dex\/|\/pkce\/verify|\/profile/, { timeout: 15000 })
    await page.waitForLoadState('networkidle')

    const tokenData = await getPersonaToken(page, SRE_EMAIL)

    expect(tokenData.id_token).toBeTruthy()
    expect(tokenData.email).toBe(SRE_EMAIL)
    expect(tokenData.groups).toContain('viewer')
    expect(tokenData.expires_in).toBeGreaterThan(0)
  })

  test('should reject unknown email addresses', async ({ page }) => {
    await page.goto('/profile')
    await page.waitForURL(/\/dex\/|\/pkce\/verify|\/profile/, { timeout: 15000 })
    await page.waitForLoadState('networkidle')

    const error = await page
      .evaluate(async () => {
        const resp = await fetch('/api/dev/token', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: 'unknown@example.com' }),
        })
        return { status: resp.status, body: await resp.text() }
      })
      .catch((err) => ({ status: 0, body: String(err) }))

    expect(error.status).toBe(400)
    expect(error.body).toContain('unknown test user email')
  })
})

test.describe('Persona Switching', () => {
  test('should login as platform engineer and show correct email', async ({
    page,
  }) => {
    await loginAsPersona(page, PLATFORM_ENGINEER_EMAIL)

    // Verify the profile page shows the platform engineer's email
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(PLATFORM_ENGINEER_EMAIL)).toBeVisible({
      timeout: 10000,
    })
  })

  test('should switch from admin to SRE persona', async ({ page }) => {
    // Start as admin
    await loginAsPersona(page, ADMIN_EMAIL)
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(ADMIN_EMAIL)).toBeVisible({ timeout: 10000 })

    // Switch to SRE
    await switchPersona(page, SRE_EMAIL)
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(SRE_EMAIL)).toBeVisible({ timeout: 10000 })
  })

  test('should switch between all three non-admin personas', async ({
    page,
  }) => {
    // Login as platform engineer
    await loginAsPersona(page, PLATFORM_ENGINEER_EMAIL)
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(PLATFORM_ENGINEER_EMAIL)).toBeVisible({
      timeout: 10000,
    })

    // Switch to product engineer
    await switchPersona(page, PRODUCT_ENGINEER_EMAIL)
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(PRODUCT_ENGINEER_EMAIL)).toBeVisible({
      timeout: 10000,
    })

    // Switch to SRE
    await switchPersona(page, SRE_EMAIL)
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(SRE_EMAIL)).toBeVisible({ timeout: 10000 })
  })
})

test.describe('Multi-Persona RBAC', () => {
  // These tests require a K8s cluster for org creation.
  // They are skipped gracefully if the cluster is unavailable.

  const orgName = `e2e-persona-${Date.now()}`

  test.afterAll(async ({ browser }) => {
    // Clean up: delete the test org as admin (who always has access)
    const context = await browser.newContext({ ignoreHTTPSErrors: true })
    const page = await context.newPage()
    try {
      await loginAsPersona(page, ADMIN_EMAIL)
      await apiDeleteOrg(page, orgName)
    } catch {
      // Best-effort cleanup; org may not exist if test was skipped
    } finally {
      await context.close()
    }
  })

  test('platform engineer can create an org and grant SRE viewer access', async ({
    page,
  }) => {
    // Login as platform engineer (owner role)
    await loginAsPersona(page, PLATFORM_ENGINEER_EMAIL)

    // Create an org — the creator is automatically OWNER
    try {
      await apiCreateOrg(page, orgName)
    } catch (err) {
      // If org creation fails (e.g. no K8s cluster), skip this test gracefully
      test.skip(true, `Org creation failed (K8s cluster may be unavailable): ${err}`)
      return
    }

    // Grant SRE viewer access
    await apiGrantOrgAccess(page, orgName, SRE_EMAIL, 1) // 1 = VIEWER
    // Grant product engineer editor access
    await apiGrantOrgAccess(page, orgName, PRODUCT_ENGINEER_EMAIL, 2) // 2 = EDITOR
  })

  test('SRE can list the org after being granted viewer access', async ({
    page,
  }) => {
    // Login as SRE (who was granted VIEWER on the org in the previous test)
    await loginAsPersona(page, SRE_EMAIL)

    // Verify SRE can see the org in the orgs list
    await page.goto('/')
    await page.waitForLoadState('networkidle')

    // The org should appear in the sidebar org picker or in the org list.
    // Check the sidebar org picker for the org name.
    const orgPickerVisible = await page
      .getByTestId('org-picker')
      .isVisible({ timeout: 5000 })
      .catch(() => false)

    if (orgPickerVisible) {
      await page.getByTestId('org-picker').click()
      await expect(
        page.getByRole('menuitem', { name: orgName }),
      ).toBeVisible({ timeout: 5000 })
    } else {
      // If no org picker, the org should appear somewhere on the page
      await expect(page.getByText(orgName)).toBeVisible({ timeout: 5000 })
    }
  })

  test('product engineer can access the org with editor privileges', async ({
    page,
  }) => {
    // Login as product engineer (who was granted EDITOR on the org)
    await loginAsPersona(page, PRODUCT_ENGINEER_EMAIL)

    // Verify product engineer can see the org
    await page.goto('/')
    await page.waitForLoadState('networkidle')

    const orgPickerVisible = await page
      .getByTestId('org-picker')
      .isVisible({ timeout: 5000 })
      .catch(() => false)

    if (orgPickerVisible) {
      await page.getByTestId('org-picker').click()
      await expect(
        page.getByRole('menuitem', { name: orgName }),
      ).toBeVisible({ timeout: 5000 })
    } else {
      await expect(page.getByText(orgName)).toBeVisible({ timeout: 5000 })
    }
  })
})

import { test, expect } from '@playwright/test'
import {
  loginAsPersona,
  apiCreateOrg,
  apiDeleteOrg,
  apiGrantOrgAccess,
  PLATFORM_ENGINEER_EMAIL,
  PRODUCT_ENGINEER_EMAIL,
  SRE_EMAIL,
  ADMIN_EMAIL,
} from './helpers'

/**
 * E2E tests for multi-persona RBAC verification across a real K8s cluster.
 *
 * After HOL-656 only the RBAC-grant cases remain here. The dev-token-endpoint
 * contract tests and the persona-switching UI rendering tests live at:
 *   - console/oidc/token_exchange_test.go (token claim contents, signature
 *     verification, rejection of unknown emails — the four cases that used
 *     to make up the "Dev Token Endpoint" describe block)
 *   - frontend/src/routes/_authenticated/-profile.test.tsx (per-persona email
 *     rendering — the cases that used to make up the "Persona Switching"
 *     describe block)
 *
 * The tests below verify that:
 * 1. A non-admin owner (platform engineer) can create an org and grant
 *    viewer/editor access to other personas.
 * 2. A viewer (SRE) can list the org they were granted access to.
 * 3. An editor (product engineer) can list the org they were granted access
 *    to.
 *
 * These require a real K8s cluster because org creation writes a namespace
 * and the RBAC grants are persisted as namespace annotations; they are the
 * legitimate multi-persona use case for E2E (the audit doc lists them as
 * Keep). Run with: make test-e2e
 */

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

    // Navigate to profile and verify the org is accessible via the sidebar.
    // Use /profile so the sidebar is loaded with org data.
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // On mobile viewports, open the sidebar drawer first.
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
      await page.waitForTimeout(500) // Wait for drawer animation
    }

    // The org should appear in the sidebar org picker.
    await page.getByTestId('org-picker').waitFor({ timeout: 5000 })
    await page.getByTestId('org-picker').click()
    await expect(
      page.getByRole('menuitem', { name: orgName }),
    ).toBeVisible({ timeout: 5000 })
  })

  test('product engineer can access the org with editor privileges', async ({
    page,
  }) => {
    // Login as product engineer (who was granted EDITOR on the org)
    await loginAsPersona(page, PRODUCT_ENGINEER_EMAIL)

    // Navigate to profile and verify the org is accessible via the sidebar.
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // On mobile viewports, open the sidebar drawer first.
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
      await page.waitForTimeout(500) // Wait for drawer animation
    }

    // The org should appear in the sidebar org picker.
    await page.getByTestId('org-picker').waitFor({ timeout: 5000 })
    await page.getByTestId('org-picker').click()
    await expect(
      page.getByRole('menuitem', { name: orgName }),
    ).toBeVisible({ timeout: 5000 })
  })
})

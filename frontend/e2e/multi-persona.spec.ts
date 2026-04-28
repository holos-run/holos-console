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
 * After HOL-1066, every assertion in this spec exercises real Kubernetes
 * RoleBindings provisioned by the per-resource RBAC reconcilers from
 * HOL-1062 / HOL-1063. UpdateOrganizationSharing now writes the sharing
 * annotation and synchronously reconciles the matching ClusterRoleBindings
 * before returning, so a granted user's impersonated client can list/get the
 * resource on the very next call. The legacy "claims.role decides" gating
 * is gone — these tests assert behavior the K8s API server (acting as the
 * sole arbiter per ADR 036) attributes to the persona's OIDC subject via
 * its bindings.
 *
 * The tests below verify that:
 * 1. A non-admin owner (platform engineer) can create an org and grant
 *    viewer/editor access to other personas.
 * 2. A viewer (SRE) can list the org they were granted access to via their
 *    real RoleBinding.
 * 3. An editor (product engineer) can list the org they were granted access
 *    to via their real RoleBinding.
 *
 * These require a real K8s cluster because the impersonated apiserver call
 * is what gates visibility; nothing in the in-process Go code answers
 * "can this user see this org?" anymore. Run with: make test-e2e
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

    // HOL-603 moved org switching from a sidebar picker to the /organizations
    // page reached via the workspace menu. The org should be listed there for
    // a user granted VIEWER access.
    await page.goto('/organizations')
    await page.waitForLoadState('networkidle')

    await expect(
      page.getByRole('row').filter({ hasText: orgName }).first(),
    ).toBeVisible({ timeout: 5000 })
  })

  // Re-enabled in HOL-1066. apiGrantOrgAccess now drives
  // UpdateOrganizationSharing, which (after HOL-1064 and the synchronous
  // EnsureResourceRBAC follow-up in commit a162225) reconciles a real
  // ClusterRoleBinding for the editor persona before returning. The
  // impersonated apiserver call from the editor's session can therefore
  // see the org on the next request without polling.
  test('product engineer can access the org with editor privileges', async ({
    page,
  }) => {
    // Login as product engineer (who was granted EDITOR on the org)
    await loginAsPersona(page, PRODUCT_ENGINEER_EMAIL)

    // HOL-603 moved org switching to the /organizations page (reached via
    // the workspace menu). Verify the org is listed for the granted user.
    await page.goto('/organizations')
    await page.waitForLoadState('networkidle')

    await expect(
      page.getByRole('row').filter({ hasText: orgName }).first(),
    ).toBeVisible({ timeout: 5000 })
  })
})

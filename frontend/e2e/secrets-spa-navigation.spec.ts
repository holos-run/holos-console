/**
 * E2E regression spec for HOL-921 / HOL-923.
 *
 * Asserts that clicking a secret row link from the secrets list page performs
 * a client-side SPA navigation to the secret detail page, without
 * re-entering the OIDC login flow (i.e. no round-trip through /dex/ or
 * /pkce/verify).
 *
 * Root cause of the regression (HOL-920): the secret row was rendered as a
 * raw <a href="..."> tag, which triggers a full-document navigation. The
 * browser discards the in-memory OIDC session, the auth layout detects the
 * missing token, and calls login() — re-entering the Dex redirect chain.
 *
 * HOL-922 fixed this by replacing the raw <a> with a TanStack Router <Link>
 * inside ResourceGrid. This spec locks in that fix and will catch any future
 * regression that reintroduces a full-document navigation from a
 * ResourceGrid row.
 *
 * Needs Kubernetes: YES (creates a real org, project, and secret via RPC API).
 */

import { test, expect } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiCreateProject,
  apiDeleteProject,
} from './helpers'

test.describe('Secrets SPA Navigation (HOL-923 regression)', () => {
  test('clicking a secret row navigates to detail page without OIDC redirect', async ({ page }) => {
    // 1. Authenticate via the full OIDC flow (real Dex session).
    await loginViaProfilePage(page)

    // 2. Seed a project with a secret so the list page has at least one row.
    const orgName = `e2e-spa-nav-org-${Date.now()}`
    const projectName = `e2e-spa-nav-${Date.now()}`
    const secretName = `e2e-spa-secret-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    // Create the secret via the UI so it exists in Kubernetes.
    await page.goto(`/projects/${projectName}/secrets`)
    await expect(page.getByRole('button', { name: /new secret/i })).toBeVisible({ timeout: 5000 })
    await page.getByRole('button', { name: /new secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/new`), { timeout: 5000 })
    await page.getByPlaceholder('my-secret').fill(secretName)
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })
    await expect(page.getByRole('link', { name: secretName, exact: true })).toBeVisible({ timeout: 10000 })

    // 3. Collect top-level main-frame navigation requests during the click so
    //    we can assert that none of them hit the Dex or PKCE endpoints.
    //
    //    We scope to isNavigationRequest() on the main frame deliberately:
    //    - SPA navigation via TanStack Router <Link> does NOT trigger a
    //      top-level document navigation — the URL changes via pushState and
    //      no new document request is issued.
    //    - A raw <a href="..."> tag WOULD trigger a top-level document
    //      navigation, which the browser then follows to the new URL; because
    //      the OIDC session lives only in sessionStorage, the new document
    //      loses the token and the auth layout re-triggers the /dex/ redirect.
    //    - We exclude hidden-iframe requests (automaticSilentRenew from
    //      oidc-client-ts) to avoid flaky false-positives from background
    //      token-refresh traffic, which hits /dex/auth even on a correct SPA
    //      navigation.
    const mainFrameNavUrls: string[] = []
    const onRequest = (req: import('@playwright/test').Request) => {
      if (req.isNavigationRequest() && req.frame() === page.mainFrame()) {
        mainFrameNavUrls.push(req.url())
      }
    }
    page.on('request', onRequest)

    // 4. Click the display-name link for the secret row.
    await page.getByRole('link', { name: secretName, exact: true }).click()

    // 5. Assert the URL became the detail page (SPA navigation succeeded).
    await page.waitForURL(
      new RegExp(`/projects/${projectName}/secrets/${secretName}`),
      { timeout: 10000 },
    )
    await expect(page).toHaveURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`))

    // 6. Assert the page did NOT round-trip through the OIDC login flow.
    //    A full-document navigation would trigger /dex/ or /pkce/verify as
    //    the main-frame navigation destination.
    page.off('request', onRequest)
    const oidcNavUrls = mainFrameNavUrls.filter(
      (url) => url.includes('/dex/') || url.includes('/pkce/verify'),
    )
    expect(
      oidcNavUrls,
      `Expected no OIDC redirect in main-frame navigations but got: ${oidcNavUrls.join(', ')}`,
    ).toHaveLength(0)

    // 7. Assert the back button returns to the list page (no extra history
    //    entries from a redirect loop).
    await page.goBack()
    await expect(page).toHaveURL(
      new RegExp(`/projects/${projectName}/secrets/?$`),
      { timeout: 5000 },
    )

    // 8. Cleanup.
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

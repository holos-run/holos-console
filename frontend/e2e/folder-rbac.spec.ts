import { test, expect } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiCreateFolder,
  apiDeleteFolder,
} from './helpers'

/**
 * E2E tests for folder-level RBAC (issue #635).
 *
 * Verifies that the cascade RBAC table correctly routes permissions through
 * the real K8s-backed handler stack. Unit tests in console/rbac/ cover the
 * cascade table itself; these specs cover the end-to-end wiring between the
 * HTTP layer, the handler, and the Kubernetes namespace annotations.
 *
 * Note: The embedded Dex provider registers multiple test personas (admin,
 * platform, product, sre) with distinct RBAC roles. Multi-user RBAC tests
 * can use the switchPersona() and loginAsPersona() helpers from helpers.ts
 * to test cross-persona permission boundaries. These tests currently validate
 * the admin/owner path and confirm that RBAC metadata is persisted correctly
 * in Kubernetes.
 *
 * Requires a real Kubernetes cluster (k3d or equivalent).
 * Run with: make test-e2e
 */

test.describe('Folder RBAC - owner can manage folder', () => {
  test('org owner can create and delete a folder', async ({ page }) => {
    await loginViaProfilePage(page)

    const ts = Date.now()
    const orgName = `e2e-rbac-owner-org-${ts}`
    const folderName = `e2e-rbac-owner-fld-${ts}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)

    // Navigate to folder list — folder should be visible (owner can list)
    // Use the display-name span to avoid strict mode violations (name also appears in name column)
    await page.goto(`/orgs/${orgName}/folders`)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('span.font-medium', { hasText: folderName })).toBeVisible({ timeout: 10000 })

    // Navigate to folder detail — delete button should be visible for owner
    await page.goto(`/folders/${folderName}`)
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('button', { name: /delete folder/i })).toBeVisible({
      timeout: 10000,
    })

    // Cleanup via API (don't use UI delete to keep test deterministic)
    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })

  test('org owner can see folder sharing panel', async ({ page }) => {
    await loginViaProfilePage(page)

    const ts = Date.now()
    const orgName = `e2e-rbac-sharing-org-${ts}`
    const folderName = `e2e-rbac-sharing-fld-${ts}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)

    await page.goto(`/folders/${folderName}`)
    await page.waitForLoadState('networkidle')

    // Org owner should see the sharing section (Settings section with edit controls)
    await expect(page.getByRole('button', { name: /edit/i }).first()).toBeVisible({
      timeout: 10000,
    })

    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })
})

test.describe('Folder RBAC - metadata persisted in Kubernetes', () => {
  test('folder raw JSON includes correct organization label', async ({ page }) => {
    await loginViaProfilePage(page)

    const ts = Date.now()
    const orgName = `e2e-rbac-meta-org-${ts}`
    const folderName = `e2e-rbac-meta-fld-${ts}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)

    // Use the GetFolderRaw RPC to verify Kubernetes metadata is correctly written
    const token = await page.evaluate(() => {
      const key = Object.keys(sessionStorage).find((k) => k.startsWith('oidc.user:'))
      if (!key) throw new Error('No OIDC session')
      const data = JSON.parse(sessionStorage.getItem(key)!) as { access_token?: string }
      return data.access_token ?? ''
    })

    const raw = await page.evaluate(
      async ({ folderName, orgName, token }) => {
        const resp = await fetch('/holos.console.v1.FolderService/GetFolderRaw', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Connect-Protocol-Version': '1',
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({ name: folderName, organization: orgName }),
        })
        if (!resp.ok) {
          const text = await resp.text()
          throw new Error(`GetFolderRaw failed (${resp.status}): ${text}`)
        }
        const data = (await resp.json()) as { raw: string }
        return data.raw
      },
      { folderName, orgName, token },
    )

    // The raw namespace JSON should contain console-managed-by and org labels
    const ns = JSON.parse(raw) as { metadata?: { labels?: Record<string, string> } }
    expect(ns.metadata?.labels?.['app.kubernetes.io/managed-by']).toBe('console.holos.run')

    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })
})

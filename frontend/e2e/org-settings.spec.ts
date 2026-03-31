import { test, expect } from '@playwright/test'
import { loginViaProfilePage, apiCreateOrg, apiDeleteOrg, selectOrg } from './helpers'

/**
 * E2E tests for the Org Settings page at /orgs/$orgName/settings.
 *
 * These tests require a full stack (Go backend + Vite dev server) but do NOT
 * require a Kubernetes cluster — they only test page rendering and navigation.
 *
 * Run with: make test-e2e
 */

test.describe('Org Settings page', () => {
  test('settings link appears in sidebar when org is selected', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-org-settings-${Date.now()}`
    await apiCreateOrg(page, orgName)

    try {
      await selectOrg(page, orgName)

      // On mobile, open the sidebar drawer
      const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
      if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
        await sidebarTrigger.click()
      }

      await expect(page.getByRole('link', { name: /^settings$/i })).toBeVisible({ timeout: 5000 })
    } finally {
      await apiDeleteOrg(page, orgName)
    }
  })

  test('clicking Settings in sidebar navigates to settings page', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-org-settings-nav-${Date.now()}`
    await apiCreateOrg(page, orgName)

    try {
      await selectOrg(page, orgName)

      // On mobile, open the sidebar drawer
      const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
      if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
        await sidebarTrigger.click()
      }

      // Click the Settings link in the sidebar
      await page.getByRole('link', { name: /^settings$/i }).click()

      // Settings page renders once authenticated data loads
      await expect(page.getByText(`${orgName} / Settings`)).toBeVisible({ timeout: 10000 })
      await expect(page.getByText('Display Name')).toBeVisible()
      await expect(page.getByText('Name (slug)')).toBeVisible()
    } finally {
      await apiDeleteOrg(page, orgName)
    }
  })
})

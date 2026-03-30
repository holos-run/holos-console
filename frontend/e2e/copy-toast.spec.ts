import { test, expect } from '@playwright/test'
import { loginViaProfilePage } from './helpers'

/**
 * E2E tests for copy-to-clipboard toast feedback.
 *
 * Verifies that clicking the copy button on a secret's data grid shows a
 * "Copied to clipboard" toast notification. Requires a running backend with
 * an active Kubernetes cluster.
 *
 * Run with: make test-e2e
 */

const TEST_ORG = `e2e-copy-toast-org-${process.pid}`
const TEST_PROJECT = `e2e-copy-toast-prj-${process.pid}`
const TEST_SECRET = `e2e-copy-toast-secret-${process.pid}`

async function createAndSelectOrg(page: import('@playwright/test').Page, orgName: string) {
  await page.goto('/organizations')
  await page.getByRole('button', { name: /create organization/i }).click()
  await page.getByPlaceholder('My Organization').fill(orgName)
  await page.getByRole('button', { name: /^create$/i }).click()
  await page.waitForURL(/\/organizations\//, { timeout: 10000 })

  await page.goto('/organizations')
  await page.waitForLoadState('networkidle')

  const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
  if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
    await sidebarTrigger.click()
  }

  await page.getByRole('button', { name: /all organizations/i }).waitFor({ timeout: 5000 })
  await page.getByRole('button', { name: /all organizations/i }).click()
  await page.getByRole('menuitem', { name: orgName }).click()
}

async function deleteOrg(page: import('@playwright/test').Page, orgName: string) {
  await page.goto('/organizations')
  await page.getByLabel(new RegExp(`delete ${orgName}`, 'i')).click()
  await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
}

test.describe('Copy button toast feedback', () => {
  test('copy button on secret data grid shows Copied to clipboard toast', async ({ page, context }) => {
    // Grant clipboard permissions so navigator.clipboard.writeText works in Playwright
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])

    await loginViaProfilePage(page)
    await createAndSelectOrg(page, TEST_ORG)

    // Create project
    await page.goto('/projects')
    await expect(page.getByRole('button', { name: /create project/i })).toBeVisible({ timeout: 5000 })
    await page.getByRole('button', { name: /create project/i }).click()
    await page.getByPlaceholder('My Project').fill(TEST_PROJECT)
    await page.getByRole('button', { name: /^create$/i }).click()
    await page.waitForURL(new RegExp(`/projects/${TEST_PROJECT}`), { timeout: 10000 })

    // Create secret with a known key-value
    await page.goto(`/projects/${TEST_PROJECT}/secrets`)
    await expect(page.getByRole('button', { name: /create secret/i })).toBeVisible({ timeout: 5000 })
    await page.getByRole('button', { name: /create secret/i }).click()
    await page.getByPlaceholder('my-secret').fill(TEST_SECRET)
    await page.getByPlaceholder('key').fill('api-key')
    await page.getByPlaceholder('value').fill('supersecret')
    await page.getByRole('button', { name: /^create$/i }).click()
    await expect(page.getByRole('link', { name: TEST_SECRET })).toBeVisible({ timeout: 10000 })

    // Navigate to secret detail page
    await page.getByRole('link', { name: TEST_SECRET }).click()
    await page.waitForURL(new RegExp(`/projects/${TEST_PROJECT}/secrets/${TEST_SECRET}`), { timeout: 5000 })

    // Click the copy button for the key
    await page.getByRole('button', { name: /^copy$/i }).first().click()

    // Verify the toast appears
    await expect(page.getByText('Copied to clipboard')).toBeVisible({ timeout: 5000 })

    // Clean up
    await page.getByRole('button', { name: /^delete$/i }).click()
    await expect(page.getByText(/are you sure/i)).toBeVisible()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await page.waitForURL(new RegExp(`/projects/${TEST_PROJECT}/secrets/?$`), { timeout: 5000 })

    await page.goto('/projects')
    await page.getByLabel(new RegExp(`delete ${TEST_PROJECT}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await deleteOrg(page, TEST_ORG)
  })
})

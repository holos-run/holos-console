import { test, expect } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiCreateProject,
  apiDeleteProject,
  selectOrg,
} from './helpers'

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

test.describe('Copy button toast feedback', () => {
  test('copy button on secret data grid shows Copied to clipboard toast', async ({ page, context }) => {
    // Grant clipboard permissions so navigator.clipboard.writeText works in Playwright
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])

    await loginViaProfilePage(page)
    await apiCreateOrg(page, TEST_ORG)
    await selectOrg(page, TEST_ORG)
    await apiCreateProject(page, TEST_PROJECT, TEST_ORG)

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

    await apiDeleteProject(page, TEST_PROJECT)
    await apiDeleteOrg(page, TEST_ORG)
  })
})

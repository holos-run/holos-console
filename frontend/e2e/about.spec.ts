import { test, expect } from '@playwright/test'
import { loginViaProfilePage } from './helpers'

/**
 * E2E tests for the About page and sidebar navigation order.
 *
 * Verifies that:
 * - The sidebar contains an "About" link (not "Version")
 * - "Profile" appears after "About" in the sidebar footer
 * - The About page renders the Server Version card
 * - The About page renders a copyright/license card
 */

test.describe('About page', () => {
  test('sidebar shows About link, not Version', async ({ page }) => {
    await loginViaProfilePage(page)

    // On mobile viewports, open the sidebar drawer first
    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
      await page.waitForTimeout(500)
    }

    await expect(page.getByRole('link', { name: 'About' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Version' })).not.toBeVisible()
  })

  test('About appears before Profile in sidebar footer DOM order', async ({ page }) => {
    await loginViaProfilePage(page)

    const sidebarTrigger = page.getByRole('button', { name: /toggle sidebar/i })
    if (await sidebarTrigger.isVisible({ timeout: 2000 }).catch(() => false)) {
      await sidebarTrigger.click()
      await page.waitForTimeout(500)
    }

    const aboutLink = page.getByRole('link', { name: 'About' })
    const profileLink = page.getByRole('link', { name: 'Profile' })

    await expect(aboutLink).toBeVisible()
    await expect(profileLink).toBeVisible()

    // About must appear before Profile in the DOM
    const aboutBox = await aboutLink.boundingBox()
    const profileBox = await profileLink.boundingBox()
    expect(aboutBox).not.toBeNull()
    expect(profileBox).not.toBeNull()
    expect(aboutBox!.y).toBeLessThan(profileBox!.y)
  })

  test('About page renders Server Version card', async ({ page }) => {
    await loginViaProfilePage(page)
    await page.goto('/about')
    await expect(page.getByText('Server Version')).toBeVisible({ timeout: 10000 })
  })

  test('About page renders copyright and Apache 2.0 license card', async ({ page }) => {
    await loginViaProfilePage(page)
    await page.goto('/about')
    await expect(page.getByText(/Apache/i)).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/copyright/i)).toBeVisible({ timeout: 10000 })
  })
})

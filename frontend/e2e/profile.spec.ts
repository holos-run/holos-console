import { test, expect } from '@playwright/test'
import { loginViaProfilePage } from './helpers'

/**
 * E2E tests for the profile page API Access section — copy snippet and shell history tabs.
 *
 * These tests verify the simplified copy snippet (no bundled history wrapper) and the
 * tabbed shell history guidance added in issue #611.
 *
 * Run with: make test-e2e (automatically starts servers)
 */

test.describe('Profile page — API Access copy snippet', () => {
  test('pre block shows single-line export without history wrapper', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('API Access')).toBeVisible({ timeout: 10000 })

    // The pre block should show a single-line export (masked by default)
    const preBlock = page.locator('pre').filter({ hasText: 'export HOLOS_ID_TOKEN=' }).first()
    await expect(preBlock).toBeVisible()

    // Must not contain set +o history / set -o history
    await expect(preBlock).not.toContainText('set +o history')
    await expect(preBlock).not.toContainText('set -o history')
  })

  test('shell history tabs are visible with zsh and bash triggers', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('API Access')).toBeVisible({ timeout: 10000 })

    // Both zsh and bash tab triggers should be present
    await expect(page.getByRole('tab', { name: /zsh/i })).toBeVisible()
    await expect(page.getByRole('tab', { name: /bash/i })).toBeVisible()
  })

  test('zsh tab is selected by default and shows setopt instructions', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('API Access')).toBeVisible({ timeout: 10000 })

    // zsh tab should be active by default — its content should be visible
    const zshTab = page.getByRole('tab', { name: /zsh/i })
    await expect(zshTab).toHaveAttribute('data-state', 'active')

    // zsh content includes setopt
    await expect(page.getByRole('tabpanel')).toContainText('setopt')
  })

  test('clicking bash tab reveals bash-specific instructions', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('API Access')).toBeVisible({ timeout: 10000 })

    // Click bash tab
    await page.getByRole('tab', { name: /bash/i }).click()

    // The bash tab panel should now be active and show bash instructions
    const panel = page.getByRole('tabpanel')
    await expect(panel).toContainText('set +o history')

    await page.screenshot({
      path: 'e2e/screenshots/profile-token-bash-tab.png',
      fullPage: false,
    })
  })

  test('clicking zsh tab after bash returns to zsh content', async ({ page }) => {
    await loginViaProfilePage(page)

    await expect(page.getByText('API Access')).toBeVisible({ timeout: 10000 })

    // Switch to bash, then back to zsh
    await page.getByRole('tab', { name: /bash/i }).click()
    await page.getByRole('tab', { name: /zsh/i }).click()

    // zsh tab should be active again
    const zshTab = page.getByRole('tab', { name: /zsh/i })
    await expect(zshTab).toHaveAttribute('data-state', 'active')

    await page.screenshot({
      path: 'e2e/screenshots/profile-token-zsh-tab.png',
      fullPage: false,
    })
  })
})

import { test, expect } from '@playwright/test'
import { loginViaProfilePage } from './helpers'

/**
 * E2E tests for the secrets data grid (Phase 3: #208).
 *
 * These tests verify that the secrets list renders as a sortable data grid
 * with column headers, clickable name links, and proper sorting behavior.
 *
 * Run with: make test-e2e
 */

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
  const deleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
  await deleteButton.click()
}

async function createProject(page: import('@playwright/test').Page, projectName: string) {
  await page.goto('/projects')
  await page.getByRole('button', { name: /create project/i }).waitFor({ timeout: 5000 })
  await page.getByRole('button', { name: /create project/i }).click()
  await page.getByPlaceholder('My Project').fill(projectName)
  await page.getByRole('button', { name: /^create$/i }).click()
  await page.waitForURL(new RegExp(`/projects/${projectName}`), { timeout: 10000 })
}

async function deleteProject(page: import('@playwright/test').Page, projectName: string) {
  await page.goto('/projects')
  await page.getByLabel(new RegExp(`delete ${projectName}`, 'i')).click()
  const deleteButton = page.getByRole('dialog').getByRole('button', { name: /delete/i })
  await deleteButton.click()
}

async function createSecret(page: import('@playwright/test').Page, projectName: string, secretName: string, description?: string) {
  await page.goto(`/projects/${projectName}/secrets`)
  await page.getByRole('button', { name: /create secret/i }).waitFor({ timeout: 5000 })
  await page.getByRole('button', { name: /create secret/i }).click()
  await page.getByPlaceholder('my-secret').fill(secretName)
  if (description) {
    await page.getByPlaceholder('What is this secret used for?').fill(description)
  }
  await page.getByRole('button', { name: /^create$/i }).click()
}

test.describe('Secrets Data Grid', () => {
  test('secrets list renders as a data grid with column headers', async ({ page }) => {
    await loginViaProfilePage(page)
    const orgName = `e2e-grid-org-${Date.now()}`
    await createAndSelectOrg(page, orgName)

    const projectName = `e2e-grid-proj-${Date.now()}`
    await createProject(page, projectName)

    const secret1 = `e2e-grid-s1-${Date.now()}`
    const secret2 = `e2e-grid-s2-${Date.now()}`
    await createSecret(page, projectName, secret1, 'First secret description')
    await createSecret(page, projectName, secret2, 'Second secret description')

    await page.goto(`/projects/${projectName}/secrets`)

    // Assert a table element is visible
    await expect(page.getByRole('table')).toBeVisible({ timeout: 10000 })

    // Assert column headers
    await expect(page.getByRole('columnheader', { name: /^name$/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('columnheader', { name: /^description$/i })).toBeVisible({ timeout: 5000 })

    // Both secrets should be visible in the grid
    await expect(page.getByRole('link', { name: secret1 })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('link', { name: secret2 })).toBeVisible({ timeout: 5000 })

    // Cleanup
    await page.goto(`/projects/${projectName}/secrets`)
    await page.getByLabel(new RegExp(`delete ${secret1}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })

    await page.getByLabel(new RegExp(`delete ${secret2}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })

    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })

  test('clicking a secret row navigates to the secret detail page', async ({ page }) => {
    await loginViaProfilePage(page)
    const orgName = `e2e-nav-org-${Date.now()}`
    await createAndSelectOrg(page, orgName)

    const projectName = `e2e-nav-proj-${Date.now()}`
    await createProject(page, projectName)

    const secretName = `e2e-nav-secret-${Date.now()}`
    await createSecret(page, projectName, secretName)

    await page.goto(`/projects/${projectName}/secrets`)

    // Click the secret name link in the grid row
    await expect(page.getByRole('link', { name: secretName })).toBeVisible({ timeout: 10000 })
    await page.getByRole('link', { name: secretName }).click()

    // Assert URL is /projects/$projectName/secrets/$secretName
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/${secretName}`), { timeout: 5000 })

    // Cleanup
    await page.getByRole('button', { name: /^delete$/i }).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await page.waitForURL(new RegExp(`/projects/${projectName}/secrets/?$`), { timeout: 5000 })

    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })

  test('secrets grid is sortable by name', async ({ page }) => {
    await loginViaProfilePage(page)
    const orgName = `e2e-sort-org-${Date.now()}`
    await createAndSelectOrg(page, orgName)

    const projectName = `e2e-sort-proj-${Date.now()}`
    await createProject(page, projectName)

    const secretAaa = `aaa-secret-${Date.now()}`
    const secretZzz = `zzz-secret-${Date.now()}`
    await createSecret(page, projectName, secretZzz)
    await createSecret(page, projectName, secretAaa)

    await page.goto(`/projects/${projectName}/secrets`)

    // Wait for both secrets to appear
    await expect(page.getByRole('link', { name: secretAaa })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('link', { name: secretZzz })).toBeVisible({ timeout: 5000 })

    // The Name column header renders as a <th> containing a sort <button>
    const nameHeaderBtn = page.locator('thead').getByRole('button', { name: /name/i })
    await expect(nameHeaderBtn).toBeVisible()

    // Default sort is ascending — aaa should appear before zzz
    const firstRow = page.locator('tbody tr').first()
    await expect(firstRow).toContainText(secretAaa, { timeout: 5000 })

    // Click Name header button once → toggles ascending→descending (zzz first)
    await nameHeaderBtn.click()
    await expect(firstRow).toContainText(secretZzz, { timeout: 5000 })

    // Click Name header button again → back to ascending (aaa first)
    await nameHeaderBtn.click()
    await expect(firstRow).toContainText(secretAaa, { timeout: 5000 })

    // Cleanup
    await page.goto(`/projects/${projectName}/secrets`)
    await page.getByLabel(new RegExp(`delete ${secretAaa}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })

    await page.getByLabel(new RegExp(`delete ${secretZzz}`, 'i')).click()
    await page.getByRole('dialog').getByRole('button', { name: /delete/i }).click()
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 10000 })

    await deleteProject(page, projectName)
    await deleteOrg(page, orgName)
  })
})

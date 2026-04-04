import { test, expect } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiCreateProject,
  apiDeleteProject,
} from './helpers'
import type { Page } from '@playwright/test'

/**
 * E2E tests for the Create Deployment modal's no-templates affordance (issue #317).
 *
 * Asserts that when no deployment templates exist in a project, the Create Deployment
 * modal shows "No templates yet. Create one now." and a button to open the
 * CreateTemplateModal sub-modal.
 *
 * Run with: make test-e2e
 */

async function apiCreateDeploymentTemplate(
  page: Page,
  project: string,
  name: string,
): Promise<void> {
  await page.evaluate(
    async ({ project, name }) => {
      const key = Object.keys(sessionStorage).find((k) => k.startsWith('oidc.user:'))
      if (!key) throw new Error('No OIDC session found')
      const data = JSON.parse(sessionStorage.getItem(key)!) as { access_token?: string }
      const token = data.access_token!
      const resp = await fetch('/holos.console.v1.DeploymentTemplateService/CreateDeploymentTemplate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ project, name, displayName: name, description: '', cueTemplate: '' }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`CreateDeploymentTemplate failed (${resp.status}): ${text}`)
      }
    },
    { project, name },
  )
}

test.describe('Create Deployment modal — no-templates affordance', () => {
  test('shows "No templates yet. Create one now." when no templates exist', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-deploy-tmpl-org-${Date.now()}`
    const projectName = `e2e-deploy-tmpl-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    // Navigate to the deployments page for the new project (no templates)
    await page.goto(`/projects/${projectName}/deployments`)
    await page.waitForLoadState('networkidle')

    // Open the Create Deployment modal
    await page.getByRole('button', { name: /create deployment/i }).first().click()
    await expect(page.getByRole('dialog')).toBeVisible({ timeout: 5000 })

    // Assert the no-templates empty-state affordance is visible
    await expect(page.getByText(/no templates yet/i)).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /create one now/i })).toBeVisible()

    // Close the modal
    await page.getByRole('button', { name: /cancel/i }).click()

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('does not show no-templates affordance when templates exist', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-deploy-has-tmpl-org-${Date.now()}`
    const projectName = `e2e-deploy-has-tmpl-prj-${Date.now()}`
    const templateName = `e2e-tmpl-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)
    await apiCreateDeploymentTemplate(page, projectName, templateName)

    await page.goto(`/projects/${projectName}/deployments`)
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: /create deployment/i }).first().click()
    await expect(page.getByRole('dialog')).toBeVisible({ timeout: 5000 })

    // Affordance should NOT be present when templates exist
    await expect(page.getByText(/no templates yet/i)).not.toBeVisible()
    await expect(page.getByRole('button', { name: /create one now/i })).not.toBeVisible()

    await page.getByRole('button', { name: /cancel/i }).click()

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('clicking "Create one now" opens template creation sub-modal', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-deploy-submodal-org-${Date.now()}`
    const projectName = `e2e-deploy-submodal-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    await page.goto(`/projects/${projectName}/deployments`)
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: /create deployment/i }).first().click()
    await expect(page.getByRole('dialog')).toBeVisible({ timeout: 5000 })

    // Click "Create one now" to open the sub-modal
    await page.getByRole('button', { name: /create one now/i }).click()

    // The CreateTemplateModal should open (has its own dialog title)
    await expect(page.getByText(/create deployment template/i)).toBeVisible({ timeout: 5000 })

    // Cancel the sub-modal
    await page.getByRole('button', { name: /cancel/i }).click()

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

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
 * E2E tests for the Create Deployment page (issue #396).
 *
 * Asserts that clicking "Create Deployment" navigates to the dedicated
 * /projects/$projectName/deployments/new page, and that the page shows
 * the appropriate state when no templates exist.
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
      // HOL-619: Use the unified TemplateService with a namespace-keyed request.
      const resp = await fetch('/holos.console.v1.TemplateService/CreateTemplate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          namespace: `holos-prj-${project}`,
          template: { name, displayName: name, description: '', cueTemplate: '' },
        }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`CreateTemplate failed (${resp.status}): ${text}`)
      }
    },
    { project, name },
  )
}

test.describe('Create Deployment page — no-templates affordance', () => {
  test('shows "No templates available. Create a template first." when no templates exist', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-deploy-tmpl-org-${Date.now()}`
    const projectName = `e2e-deploy-tmpl-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    // Navigate directly to the Create Deployment page (no templates exist)
    await page.goto(`/projects/${projectName}/deployments/new`)
    await page.waitForLoadState('networkidle')

    // Assert the no-templates affordance is visible
    await expect(page.getByText(/no templates available/i)).toBeVisible({ timeout: 5000 })
    // The link to create a template should be present
    await expect(page.getByRole('link', { name: /create a template/i })).toBeVisible()

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

    // Navigate to the Create Deployment page
    await page.goto(`/projects/${projectName}/deployments/new`)
    await page.waitForLoadState('networkidle')

    // Affordance should NOT be present when templates exist
    await expect(page.getByText(/no templates available/i)).not.toBeVisible()
    // The Create Deployment submit button should be present
    await expect(page.getByRole('button', { name: /create deployment/i })).toBeVisible()

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })

  test('clicking "Create Deployment" link on list page navigates to new page', async ({ page }) => {
    await loginViaProfilePage(page)

    const orgName = `e2e-deploy-nav-org-${Date.now()}`
    const projectName = `e2e-deploy-nav-prj-${Date.now()}`

    await apiCreateOrg(page, orgName)
    await apiCreateProject(page, projectName, orgName)

    await page.goto(`/projects/${projectName}/deployments`)
    await page.waitForLoadState('networkidle')

    // Click Create Deployment link — should navigate to new page, not open a modal
    await page.getByRole('link', { name: /create deployment/i }).first().click()
    await expect(page).toHaveURL(new RegExp(`/projects/${projectName}/deployments/new`))

    // The Create Deployment form should be visible (submit button)
    await expect(page.getByRole('button', { name: /create deployment/i })).toBeVisible({ timeout: 5000 })
    // No dialog element (old modal behavior)
    await expect(page.getByRole('dialog')).not.toBeVisible()

    // Cleanup
    await apiDeleteProject(page, projectName)
    await apiDeleteOrg(page, orgName)
  })
})

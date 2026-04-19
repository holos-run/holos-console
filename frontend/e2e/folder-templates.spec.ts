import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import {
  loginViaProfilePage,
  apiCreateOrg,
  apiDeleteOrg,
  apiCreateFolder,
  apiDeleteFolder,
} from './helpers'

/**
 * E2E tests for folder-scoped templates (issue #635).
 *
 * Verifies that templates created at the folder scope are visible in the
 * folder's template list page. Template rendering unification is covered
 * by unit tests in render_test.go; this spec covers the full-stack wiring
 * that unit tests cannot exercise (real K8s API, real ConnectRPC handler).
 *
 * Requires a real Kubernetes cluster (k3d or equivalent).
 * Run with: make test-e2e
 */

/**
 * Create a folder-scoped template via the RPC API.
 *
 * HOL-619: TemplateScope / TemplateScopeRef were removed from proto. Requests
 * now carry a Kubernetes namespace (e.g., holos-fld-<folder>) in place of the
 * legacy (scope, scopeName) pair.
 */
async function apiCreateFolderTemplate(
  page: Page,
  folderName: string,
  _organization: string,
  templateName: string,
  cueSource: string,
): Promise<void> {
  const token = await page.evaluate(() => {
    const key = Object.keys(sessionStorage).find((k) => k.startsWith('oidc.user:'))
    if (!key) throw new Error('No OIDC session')
    const data = JSON.parse(sessionStorage.getItem(key)!) as { access_token?: string }
    return data.access_token ?? ''
  })

  await page.evaluate(
    async ({ folderName, templateName, cueSource, token }) => {
      // CreateTemplateRequest shape (post HOL-619): { namespace, template }.
      // Template fields use camelCase JSON names matching the proto snake_case fields.
      const resp = await fetch('/holos.console.v1.TemplateService/CreateTemplate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          namespace: `holos-fld-${folderName}`,
          template: {
            name: templateName,
            displayName: templateName,
            description: 'E2E test folder template',
            cueTemplate: cueSource,
            enabled: true,
            mandatory: false,
          },
        }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`CreateTemplate failed (${resp.status}): ${text}`)
      }
    },
    { folderName, templateName, cueSource, token },
  )
}

/**
 * Delete a folder-scoped template via the RPC API.
 */
async function apiDeleteFolderTemplate(
  page: Page,
  folderName: string,
  organization: string,
  templateName: string,
): Promise<void> {
  const token = await page.evaluate(() => {
    const key = Object.keys(sessionStorage).find((k) => k.startsWith('oidc.user:'))
    if (!key) throw new Error('No OIDC session')
    const data = JSON.parse(sessionStorage.getItem(key)!) as { access_token?: string }
    return data.access_token ?? ''
  })

  await page.evaluate(
    async ({ folderName, organization, templateName, token }) => {
      const resp = await fetch('/holos.console.v1.TemplateService/DeleteTemplate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          namespace: `holos-fld-${folderName}`,
          name: templateName,
          organization,
        }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`DeleteTemplate failed (${resp.status}): ${text}`)
      }
    },
    { folderName, organization, templateName, token },
  )
}

// Minimal CUE template that produces a ServiceAccount — just enough to be
// syntactically valid and produce a renderable resource.
const minimalFolderTemplate = `
input: #ProjectInput
platform: #PlatformInput

projectResources: {
  namespacedResources: (platform.namespace): {
    ServiceAccount: "folder-e2e-sa": {
      apiVersion: "v1"
      kind:       "ServiceAccount"
      metadata: {
        name:      "folder-e2e-sa"
        namespace: platform.namespace
        labels: {
          "app.kubernetes.io/managed-by": "console.holos.run"
        }
      }
    }
  }
  clusterResources: {}
}
`

test.describe('Folder-scoped templates', () => {
  test('folder template appears in folder templates list page', async ({ page }) => {
    await loginViaProfilePage(page)

    const ts = Date.now()
    const orgName = `e2e-tmpl-org-${ts}`
    const folderName = `e2e-tmpl-folder-${ts}`
    const templateName = `e2e-folder-tmpl-${ts}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)
    await apiCreateFolderTemplate(page, folderName, orgName, templateName, minimalFolderTemplate)

    // Navigate to the folder's templates page
    await page.goto(`/folders/${folderName}/templates`)
    await page.waitForLoadState('networkidle')

    // The template should appear in the list
    await expect(page.getByText(templateName)).toBeVisible({ timeout: 10000 })

    // Cleanup (template first, then folder, then org)
    await apiDeleteFolderTemplate(page, folderName, orgName, templateName)
    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })

  test('folder without templates shows empty state', async ({ page }) => {
    await loginViaProfilePage(page)

    const ts = Date.now()
    const orgName = `e2e-notmpl-org-${ts}`
    const folderName = `e2e-notmpl-folder-${ts}`

    await apiCreateOrg(page, orgName)
    await apiCreateFolder(page, folderName, orgName, 1, orgName)

    await page.goto(`/folders/${folderName}/templates`)
    await page.waitForLoadState('networkidle')

    // Empty state message (actual text: "No platform templates found for this folder.")
    await expect(page.getByText(/no platform templates/i)).toBeVisible({ timeout: 10000 })

    await apiDeleteFolder(page, folderName, orgName)
    await apiDeleteOrg(page, orgName)
  })
})

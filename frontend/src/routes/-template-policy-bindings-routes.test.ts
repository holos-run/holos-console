// Route tree guard for HOL-597 / HOL-918: TemplatePolicyBindings, like
// TemplatePolicies, must NEVER be mounted under a project-scoped path.
// Bindings live only at folder or organization scope — the same
// storage-isolation guarantee as the policies they reference.
//
// HOL-918: the org-scoped binding route was renamed from
// /orgs/$orgName/template-policy-bindings to /orgs/$orgName/template-bindings.
// The folder-scoped route is unchanged.
//
// This test reads the generated route tree and asserts:
//   1. No path matching `/projects/.+/template-policy-bindings` exists.
//   2. The folder-scoped template-policy-bindings tree is present.
//   3. The org-scoped template-bindings tree (renamed) is present.
//   4. No org-scoped template-policy-bindings route exists (old path removed).
import fs from 'node:fs'
import path from 'node:path'

const routeTreePath = path.resolve(__dirname, '../routeTree.gen.ts')

describe('TemplatePolicyBindings route tree', () => {
  const source = fs.readFileSync(routeTreePath, 'utf-8')

  it('does not include any project-scoped template-policy-bindings route', () => {
    const forbidden = /\/projects\/[^'"\s]+\/template-policy-bindings/
    expect(source).not.toMatch(forbidden)
  })

  it('includes folder-scoped template-policy-bindings route', () => {
    expect(source).toMatch(/\/folders\/\$folderName\/template-policy-bindings/)
  })

  it('includes org-scoped template-bindings route (HOL-918 rename)', () => {
    expect(source).toMatch(/\/orgs\/\$orgName\/template-bindings/)
  })

  it('does not include org-scoped template-policy-bindings route (removed by HOL-918)', () => {
    const removed = /\/orgs\/\$orgName\/template-policy-bindings/
    expect(source).not.toMatch(removed)
  })
})

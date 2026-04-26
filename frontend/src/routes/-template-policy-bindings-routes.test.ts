// Route tree guard for HOL-597 / HOL-918 / HOL-978: TemplatePolicyBindings,
// like TemplatePolicies, must NEVER be mounted under a project-scoped path.
// Per HOL-978 the folder-scoped routes have been removed (folder hierarchy cut
// for MVP); bindings now live only at organization scope.
//
// HOL-918: the org-scoped binding route was renamed from
// /organizations/$orgName/template-policy-bindings to /organizations/$orgName/template-bindings.
// HOL-978: the folder-scoped route is removed entirely (cut for MVP).
//
// This test reads the generated route tree and asserts:
//   1. No path matching `/projects/.+/template-policy-bindings` exists.
//   2. No folder-scoped template-policy-bindings route exists (cut for MVP by HOL-978).
//   3. The org-scoped template-bindings tree (renamed by HOL-918) is present.
//   4. No org-scoped template-policy-bindings route exists (old path removed by HOL-918).
import fs from 'node:fs'
import path from 'node:path'

const routeTreePath = path.resolve(__dirname, '../routeTree.gen.ts')

describe('TemplatePolicyBindings route tree', () => {
  const source = fs.readFileSync(routeTreePath, 'utf-8')

  it('does not include any project-scoped template-policy-bindings route', () => {
    const forbidden = /\/projects\/[^'"\s]+\/template-policy-bindings/
    expect(source).not.toMatch(forbidden)
  })

  it('does not include folder-scoped template-policy-bindings route (cut for MVP by HOL-978)', () => {
    expect(source).not.toMatch(/\/folders\/\$folderName\/template-policy-bindings/)
  })

  it('includes org-scoped template-bindings route (HOL-918 rename)', () => {
    expect(source).toMatch(/\/organizations\/\$orgName\/template-bindings/)
  })

  it('does not include org-scoped template-policy-bindings route (removed by HOL-918)', () => {
    const removed = /\/organizations\/\$orgName\/template-policy-bindings/
    expect(source).not.toMatch(removed)
  })
})

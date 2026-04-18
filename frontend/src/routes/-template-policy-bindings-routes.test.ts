// Route tree guard for HOL-597: TemplatePolicyBindings, like TemplatePolicies,
// must NEVER be mounted under a project-scoped path. Bindings live only at
// folder or organization scope — the same storage-isolation guarantee as the
// policies they reference.
//
// This test reads the generated route tree and asserts:
//   1. No path matching `/projects/.+/template-policy-bindings` exists.
//   2. The folder and org template-policy-bindings trees are present (sanity
//      check so a stray rename doesn't let the guard pass vacuously).
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

  it('includes org-scoped template-policy-bindings route', () => {
    expect(source).toMatch(/\/orgs\/\$orgName\/template-policy-bindings/)
  })
})

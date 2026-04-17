// Route tree guard for HOL-558: TemplatePolicies must NEVER be mounted under
// a project-scoped path. Per the ticket's "Storage isolation: folder-only
// routes" note, policies live only at folder or organization scope.
//
// This test reads the generated route tree and asserts:
//   1. No path matching `/projects/.+/template-policies` exists.
//   2. The folder and org template-policies trees are present (sanity check
//      so a stray rename doesn't let the guard pass vacuously).
import fs from 'node:fs'
import path from 'node:path'

const routeTreePath = path.resolve(__dirname, '../routeTree.gen.ts')

describe('TemplatePolicies route tree', () => {
  const source = fs.readFileSync(routeTreePath, 'utf-8')

  it('does not include any project-scoped template-policies route', () => {
    // The literal path appears inside `fullPath:` and `id:` strings in the
    // generated file. Any match indicates a regression of the storage
    // isolation guarantee.
    const forbidden = /\/projects\/[^'"\s]+\/template-polic/
    expect(source).not.toMatch(forbidden)
  })

  it('includes folder-scoped template-policies route', () => {
    expect(source).toMatch(/\/folders\/\$folderName\/template-policies/)
  })

  it('includes org-scoped template-policies route', () => {
    expect(source).toMatch(/\/orgs\/\$orgName\/template-policies/)
  })
})

// Route tree guard for HOL-558 / HOL-978: TemplatePolicies must NEVER be
// mounted under a project-scoped path. Per HOL-978 the folder-scoped routes
// have been removed (folder hierarchy cut for MVP); policies now live only at
// organization scope.
//
// This test reads the generated route tree and asserts:
//   1. No path matching `/projects/.+/template-policies` exists.
//   2. No folder-scoped template-policies route exists (cut for MVP by HOL-978).
//   3. The org-scoped template-policies tree is present (canonical replacement).
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

  it('does not include folder-scoped template-policies route (cut for MVP by HOL-978)', () => {
    expect(source).not.toMatch(/\/folders\/\$folderName\/template-policies/)
  })

  it('includes org-scoped template-policies route', () => {
    expect(source).toMatch(/\/organizations\/\$orgName\/template-policies/)
  })
})

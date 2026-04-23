import { render, screen, cleanup } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'

// Parameterized test for HOL-607 AC: the consolidated editor must render the
// same shape regardless of whether the template's namespace originates at the
// org, folder, or project scope. Scope discrimination lives on the backend —
// the frontend references only {namespace, name, display_name} + the CUE body.

// The params vary per case, so the mock defers to a mutable holder.
// NOTE: This relies on Vitest's default sequential execution within a single
// file — each it.each case runs serially so mutations to currentParams are
// isolated between cases. Do not add `concurrent` to this suite.
let currentParams: { orgName: string; namespace: string; name: string } = {
  orgName: 'test-org',
  namespace: '',
  name: '',
}

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => currentParams,
    }),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/templates', () => ({
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useDeleteTemplate: vi.fn(),
  useListTemplateExamples: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
  useGetTemplateDefaults: vi.fn(),
  useRenderTemplate: vi.fn().mockReturnValue({
    data: undefined,
    error: null,
    isFetching: false,
  }),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

import {
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useGetTemplateDefaults,
} from '@/queries/templates'
import { ConsolidatedTemplateEditorPage } from './$namespace.$name'

const cases = [
  { scope: 'org', namespace: 'holos-org-test-org', name: 'reference-grant' },
  { scope: 'folder', namespace: 'holos-fld-team-alpha', name: 'reference-grant' },
  { scope: 'project', namespace: 'holos-prj-billing', name: 'reference-grant' },
]

describe('ConsolidatedTemplateEditorPage cross-scope equivalence (HOL-607)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(useGetTemplateDefaults as Mock).mockReturnValue({ data: undefined })
  })

  it.each(cases)(
    'renders identical structure for the $scope scope',
    ({ namespace, name }) => {
      currentParams = { orgName: 'test-org', namespace, name }
      ;(useGetTemplate as Mock).mockReturnValue({
        data: {
          name,
          namespace,
          displayName: 'ReferenceGrant',
          description: 'cross-scope fixture',
          cueTemplate: '// body',
          enabled: true,
        },
        isPending: false,
        error: null,
      })
      ;(useUpdateTemplate as Mock).mockReturnValue({
        mutateAsync: vi.fn(),
        isPending: false,
      })
      ;(useDeleteTemplate as Mock).mockReturnValue({
        mutateAsync: vi.fn(),
        isPending: false,
        error: null,
      })

      render(<ConsolidatedTemplateEditorPage />)

      // Identity — same primitives visible regardless of scope.
      expect(screen.getByRole('heading', { name: 'ReferenceGrant' })).toBeInTheDocument()
      expect(screen.getByText('Namespace')).toBeInTheDocument()
      expect(screen.getByText('Name')).toBeInTheDocument()
      expect(screen.getAllByText(namespace).length).toBeGreaterThan(0)
      expect(screen.getAllByText(name).length).toBeGreaterThan(0)

      // Every scope loads its template via the same RPC-level primitives.
      expect(useGetTemplate).toHaveBeenCalledWith(namespace, name)
      expect(useUpdateTemplate).toHaveBeenCalledWith(namespace, name)

      cleanup()
    },
  )
})

import { render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'

const navigateSpy = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'billing', templateName: 'reference-grant' }),
    }),
    useNavigate: () => navigateSpy,
  }
})

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

import { ProjectTemplateRedirect } from './$templateName'
import { useGetProject } from '@/queries/projects'
import { namespaceForProject } from '@/lib/scope-labels'

describe('project-templates redirect shim', () => {
  beforeEach(() => {
    navigateSpy.mockReset()
    ;(useGetProject as Mock).mockReset()
  })

  it('renders an sr-only status announcement', () => {
    ;(useGetProject as Mock).mockReturnValue({
      data: { name: 'billing', organization: 'test-org' },
      isPending: false,
      error: null,
    })
    render(<ProjectTemplateRedirect />)
    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/redirecting/i)
  })

  it('renders an error alert when the project lookup fails', () => {
    ;(useGetProject as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('project not found'),
    })
    render(<ProjectTemplateRedirect />)
    expect(screen.getByText('project not found')).toBeInTheDocument()
  })

  it('waits until the project record resolves before navigating', () => {
    ;(useGetProject as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    render(<ProjectTemplateRedirect />)
    expect(navigateSpy).not.toHaveBeenCalled()
  })

  it('navigates to the consolidated editor under the project namespace', async () => {
    ;(useGetProject as Mock).mockReturnValue({
      data: { name: 'billing', organization: 'test-org' },
      isPending: false,
      error: null,
    })
    render(<ProjectTemplateRedirect />)
    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/orgs/$orgName/templates/$namespace/$name',
          params: {
            orgName: 'test-org',
            namespace: namespaceForProject('billing'),
            name: 'reference-grant',
          },
          replace: true,
        }),
      )
    })
  })
})

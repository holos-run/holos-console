import { render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'

const navigateSpy = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'team-alpha', templateName: 'reference-grant' }),
    }),
    useNavigate: () => navigateSpy,
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { FolderTemplateRedirect } from './$templateName'
import { useGetFolder } from '@/queries/folders'
import { namespaceForFolder } from '@/lib/scope-labels'

describe('folder-templates redirect shim', () => {
  beforeEach(() => {
    navigateSpy.mockReset()
    ;(useGetFolder as Mock).mockReset()
  })

  it('renders an sr-only status announcement', () => {
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'team-alpha', organization: 'test-org' },
      isPending: false,
      error: null,
    })
    render(<FolderTemplateRedirect />)
    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/redirecting/i)
  })

  it('renders an error alert when the folder lookup fails', () => {
    ;(useGetFolder as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('folder not found'),
    })
    render(<FolderTemplateRedirect />)
    expect(screen.getByText('folder not found')).toBeInTheDocument()
  })

  it('waits until the folder record resolves before navigating', () => {
    ;(useGetFolder as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    render(<FolderTemplateRedirect />)
    expect(navigateSpy).not.toHaveBeenCalled()
  })

  it('navigates to the consolidated editor under the folder namespace', async () => {
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'team-alpha', organization: 'test-org' },
      isPending: false,
      error: null,
    })
    render(<FolderTemplateRedirect />)
    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/organizations/$orgName/templates/$namespace/$name',
          params: {
            orgName: 'test-org',
            namespace: namespaceForFolder('team-alpha'),
            name: 'reference-grant',
          },
          replace: true,
        }),
      )
    })
  })
})

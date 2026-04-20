import { render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'

const navigateSpy = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', templateName: 'reference-grant' }),
    }),
    useNavigate: () => navigateSpy,
  }
})

import { OrgTemplateRedirect } from './$templateName'
import { namespaceForOrg } from '@/lib/scope-labels'

describe('org-templates redirect shim', () => {
  beforeEach(() => {
    navigateSpy.mockReset()
  })

  it('renders an sr-only status announcement', () => {
    render(<OrgTemplateRedirect />)
    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/redirecting/i)
  })

  it('navigates to the consolidated editor under the org namespace', async () => {
    render(<OrgTemplateRedirect />)
    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/orgs/$orgName/templates/$namespace/$name',
          params: {
            orgName: 'test-org',
            namespace: namespaceForOrg('test-org'),
            name: 'reference-grant',
          },
          replace: true,
        }),
      )
    })
  })
})

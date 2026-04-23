import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({ useParams: () => ({ projectName: 'my-proj' }) }),
    Link: ({ children, className }: { children: React.ReactNode; className?: string }) => (
      <a href="#" className={className}>{children}</a>
    ),
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/queries/templates', () => ({
  useCreateTemplate: vi.fn(),
  useRenderTemplate: vi.fn().mockReturnValue({ data: null, isPending: false, error: null }),
  useListTemplateExamples: vi.fn().mockReturnValue({ data: [], isPending: false }),
}))

vi.mock('@/queries/projects', () => ({ useGetProject: vi.fn() }))
vi.mock('@/queries/organizations', () => ({ useGetOrganization: vi.fn() }))
vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

import { useCreateTemplate } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateTemplatePage } from './new'

const mutateAsync = vi.fn()

beforeEach(() => {
  mockNavigate.mockReset()
  mutateAsync.mockReset().mockResolvedValue({})
  ;(useCreateTemplate as unknown as Mock).mockReturnValue({ mutateAsync, isPending: false })
  ;(useGetProject as unknown as Mock).mockReturnValue({
    data: { name: 'my-proj', organization: 'my-org', userRole: Role.OWNER },
  })
  ;(useGetOrganization as unknown as Mock).mockReturnValue({ data: null })
})

test('project wrapper navigates to template detail after create', async () => {
  render(<CreateTemplatePage projectName="my-proj" />)
  const displayName = screen.getByLabelText('Display Name')
  fireEvent.change(displayName, { target: { value: 'My Template' } })
  fireEvent.click(screen.getByRole('button', { name: /^create template$/i }))

  await waitFor(() => {
    expect(mutateAsync).toHaveBeenCalledTimes(1)
  })
  expect(mutateAsync.mock.calls[0][0]).toMatchObject({ name: 'my-template' })
  await waitFor(() => {
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/projects/$projectName/templates/$templateName',
      params: { projectName: 'my-proj', templateName: 'my-template' },
    })
  })
})

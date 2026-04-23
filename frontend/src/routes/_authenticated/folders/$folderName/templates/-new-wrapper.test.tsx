import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({ useParams: () => ({ folderName: 'my-folder' }) }),
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

vi.mock('@/queries/folders', () => ({ useGetFolder: vi.fn() }))
vi.mock('@/queries/organizations', () => ({ useGetOrganization: vi.fn() }))
vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

import { useCreateTemplate } from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateFolderTemplatePage } from './new'

const mutateAsync = vi.fn()

beforeEach(() => {
  mockNavigate.mockReset()
  mutateAsync.mockReset().mockResolvedValue({})
  ;(useCreateTemplate as unknown as Mock).mockReturnValue({ mutateAsync, isPending: false })
  ;(useGetFolder as unknown as Mock).mockReturnValue({
    data: { name: 'my-folder', organization: 'my-org', userRole: Role.OWNER },
  })
  ;(useGetOrganization as unknown as Mock).mockReturnValue({ data: null })
})

test('folder wrapper navigates to template detail after create', async () => {
  render(<CreateFolderTemplatePage folderName="my-folder" />)
  fireEvent.change(screen.getByLabelText('Display Name'), {
    target: { value: 'My Template' },
  })
  fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

  await waitFor(() => {
    expect(mutateAsync).toHaveBeenCalledTimes(1)
  })
  expect(mutateAsync.mock.calls[0][0]).toMatchObject({ name: 'my-template' })
  await waitFor(() => {
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/folders/$folderName/templates/$templateName',
      params: { folderName: 'my-folder', templateName: 'my-template' },
    })
  })
})

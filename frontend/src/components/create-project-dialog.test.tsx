import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/organizations', () => ({
  useListOrganizations: vi.fn(),
  useGetOrganization: vi.fn(),
  useCreateOrganization: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useListProjects: vi.fn(),
  useCreateProject: vi.fn(),
}))

vi.mock('@/queries/folders', () => ({
  useListFolders: vi.fn(),
}))

vi.mock('@/gen/holos/console/v1/folders_pb', () => ({
  ParentType: { UNSPECIFIED: 0, ORGANIZATION: 1, FOLDER: 2 },
}))

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/components/ui/dialog', () => ({
  Dialog: ({ children, open }: { children: React.ReactNode; open?: boolean }) =>
    open ? <div data-testid="dialog">{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

vi.mock('@/components/ui/input', () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}))

vi.mock('@/components/ui/label', () => ({
  Label: ({ children, ...props }: React.LabelHTMLAttributes<HTMLLabelElement> & { children?: React.ReactNode }) => (
    <label {...props}>{children}</label>
  ),
}))

vi.mock('@/components/ui/textarea', () => ({
  Textarea: (props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
}))

vi.mock('@/components/ui/button', () => ({
  Button: ({ children, onClick, type, disabled }: {
    children: React.ReactNode
    onClick?: () => void
    type?: string
    disabled?: boolean
  }) => (
    <button onClick={onClick} type={type as 'button' | 'submit' | 'reset'} disabled={disabled}>
      {children}
    </button>
  ),
}))

vi.mock('@/components/ui/alert', () => ({
  Alert: ({ children }: { children: React.ReactNode }) => <div role="alert">{children}</div>,
  AlertDescription: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}))

vi.mock('@/components/ui/combobox', () => ({
  Combobox: ({ items, value, onValueChange, 'aria-label': ariaLabel }: {
    items: { value: string; label: string }[]
    value: string
    onValueChange: (v: string) => void
    'aria-label'?: string
  }) => (
    <select
      data-testid="select"
      aria-label={ariaLabel ?? 'Organization'}
      value={value}
      onChange={(e) => onValueChange(e.target.value)}
    >
      {items.map((item) => (
        <option key={item.value} value={item.value}>{item.label}</option>
      ))}
    </select>
  ),
}))

import { useListOrganizations, useGetOrganization } from '@/queries/organizations'
import { useCreateProject } from '@/queries/projects'
import { useListFolders } from '@/queries/folders'
import { CreateProjectDialog } from './create-project-dialog'

describe('CreateProjectDialog', () => {
  const mockMutateAsync = vi.fn()
  const onOpenChange = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    ;(useListOrganizations as Mock).mockReturnValue({
      data: { organizations: [{ name: 'my-org', displayName: 'My Org' }] },
      isLoading: false,
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { defaultFolder: '' },
      isPending: false,
      error: null,
    })
    ;(useListFolders as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })
    ;(useCreateProject as Mock).mockReturnValue({
      mutateAsync: mockMutateAsync,
      isPending: false,
    })
  })

  it('renders org select, folder select, displayName, name, and description when open', () => {
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByPlaceholderText(/my-project/i)).toBeDefined()
    expect(screen.getByPlaceholderText(/my project/i)).toBeDefined()
    expect(screen.getByPlaceholderText(/optional description/i)).toBeDefined()
    expect(screen.getByLabelText('Organization')).toBeDefined()
    expect(screen.getByLabelText('Folder')).toBeDefined()
  })

  it('does not render when closed', () => {
    render(<CreateProjectDialog open={false} onOpenChange={onOpenChange} />)
    expect(screen.queryByTestId('dialog')).toBeNull()
  })

  it('pre-selects defaultOrganization in the org select', () => {
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="my-org" />)
    const select = screen.getByLabelText('Organization') as HTMLSelectElement
    expect(select.value).toBe('my-org')
  })

  it('auto-derives name from display name as user types', () => {
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Test Project' } })
    expect((screen.getByPlaceholderText(/my-project/i) as HTMLInputElement).value).toBe('test-project')
  })

  it('stops auto-deriving name once user manually edits name field', () => {
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Test Project' } })
    fireEvent.change(screen.getByPlaceholderText(/my-project/i), { target: { value: 'custom-slug' } })
    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Test Project Updated' } })
    expect((screen.getByPlaceholderText(/my-project/i) as HTMLInputElement).value).toBe('custom-slug')
  })

  it('shows reset link when name has been manually edited', () => {
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Test Project' } })
    fireEvent.change(screen.getByPlaceholderText(/my-project/i), { target: { value: 'custom-slug' } })
    expect(screen.getByText(/auto-derive from display name/i)).toBeDefined()
  })

  it('re-enables auto-derivation when reset link is clicked', () => {
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Test Project' } })
    fireEvent.change(screen.getByPlaceholderText(/my-project/i), { target: { value: 'custom-slug' } })
    fireEvent.click(screen.getByText(/auto-derive from display name/i))
    expect((screen.getByPlaceholderText(/my-project/i) as HTMLInputElement).value).toBe('test-project')
    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'New Project' } })
    expect((screen.getByPlaceholderText(/my-project/i) as HTMLInputElement).value).toBe('new-project')
  })

  it('calls mutateAsync with correct organization field on submit', async () => {
    mockMutateAsync.mockResolvedValue({ name: 'new-project' })
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="my-org" />)

    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'New Project' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'new-project', organization: 'my-org' })
      )
    })
  })

  it('closes dialog on successful create', async () => {
    mockMutateAsync.mockResolvedValue({ name: 'new-project' })
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="my-org" />)

    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'New Project' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  it('renders error alert on server error', async () => {
    mockMutateAsync.mockRejectedValue(new Error('project already exists'))
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="my-org" />)

    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Taken Project' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeDefined()
      expect(screen.getByText(/project already exists/i)).toBeDefined()
    })
  })

  it('does not close dialog on error', async () => {
    mockMutateAsync.mockRejectedValue(new Error('server error'))
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="my-org" />)

    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Bad Project' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(onOpenChange).not.toHaveBeenCalledWith(false)
    })
  })
})

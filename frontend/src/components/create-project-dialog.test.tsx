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

// Hoisted navigate spy so tests can assert the post-create navigation target.
// The E2E test this replaces
// (`create-dialogs.spec.ts > create project dialog opens, submits via display
// name auto-slug, and navigates to secrets page`) asserted the URL via
// `toHaveURL(/projects/<slug>/secrets)` after the backend round-trip. With the
// mutation mocked, the only invariant worth preserving is that the dialog's
// post-create navigate() call matches the slug the server returned.
const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
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
    mockNavigate.mockReset()
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

  it('resets folder state when organization changes so submit uses org as parent', async () => {
    // Both orgs have no default folder. The user manually selects folder-a
    // from org-a, then switches to org-b and submits. Without an explicit
    // reset the folder state retains 'folder-a', causing handleSubmit to send
    // parentType=FOLDER + parentName='folder-a' — an invalid cross-org link.
    mockMutateAsync.mockResolvedValue({ name: 'new-project' })
    ;(useListOrganizations as Mock).mockReturnValue({
      data: {
        organizations: [
          { name: 'org-a', displayName: 'Org A' },
          { name: 'org-b', displayName: 'Org B' },
        ],
      },
      isLoading: false,
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { defaultFolder: '' },
      isPending: false,
      error: null,
    })
    ;(useListFolders as Mock).mockImplementation((org: string) => {
      if (org === 'org-a') return { data: [{ name: 'folder-a', displayName: 'Folder A' }], isPending: false, error: null }
      return { data: [], isPending: false, error: null }
    })

    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="org-a" />)

    // Manually select folder-a from org-a's folder list.
    const folderSelect = screen.getByLabelText('Folder') as HTMLSelectElement
    fireEvent.change(folderSelect, { target: { value: 'folder-a' } })
    expect(folderSelect.value).toBe('folder-a')

    // Switch to org-b.
    const orgSelect = screen.getByLabelText('Organization') as HTMLSelectElement
    fireEvent.change(orgSelect, { target: { value: 'org-b' } })

    // Fill in required fields and submit.
    fireEvent.change(screen.getByPlaceholderText(/my project/i), { target: { value: 'Test' } })
    fireEvent.submit(screen.getByRole('form'))

    // The submit must use organization as the parent (folder was cleared).
    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          organization: 'org-b',
          parentType: 1, // ParentType.ORGANIZATION
          parentName: 'org-b',
        })
      )
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

  // The tests below migrate coverage from the now-deleted
  // `frontend/e2e/create-dialogs.spec.ts` (HOL-654). They assert on the mocked
  // mutation call shape and the mocked router navigate() call rather than on a
  // real network round-trip, per the E2E refactor audit
  // (docs/agents/e2e-refactor-audit.md) which classified all 5 tests in that
  // spec as Refactor-to-unit.

  it('submits with auto-derived slug and navigates to the new project secrets page using the server-returned name', async () => {
    const displayName = 'E2E Create Project'
    const localSlug = 'e2e-create-project'
    // The backend canonicalises project names (e.g. normalising length or
    // resolving collisions), so the name we navigate to MUST come from the
    // mutation response, not from local state. Return a slug that differs from
    // `localSlug` — if a regression makes the component navigate using the
    // locally-derived name, the navigate assertion will fail.
    const serverSlug = 'e2e-create-project-canonical'
    mockMutateAsync.mockResolvedValue({ name: serverSlug })

    render(
      <CreateProjectDialog
        open={true}
        onOpenChange={onOpenChange}
        defaultOrganization="my-org"
      />,
    )

    // Fill in the Display Name — the Name field should auto-derive the slug.
    fireEvent.change(screen.getByPlaceholderText(/my project/i), {
      target: { value: displayName },
    })
    expect(
      (screen.getByPlaceholderText(/my-project/i) as HTMLInputElement).value,
    ).toBe(localSlug)

    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: localSlug,
          displayName,
          organization: 'my-org',
        }),
      )
    })

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({
        to: '/projects/$projectName/secrets',
        params: { projectName: serverSlug },
      })
    })
  })

  it('manually overriding name stops auto-derivation and the reset link restores it', () => {
    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="my-org" />)

    const displayInput = screen.getByPlaceholderText(/my project/i) as HTMLInputElement
    const nameInput = screen.getByPlaceholderText(/my-project/i) as HTMLInputElement

    // Typing in the display name auto-derives the slug.
    fireEvent.change(displayInput, { target: { value: 'Test Project' } })
    expect(nameInput.value).toBe('test-project')

    // Overriding the name disables auto-derivation and shows the reset link.
    fireEvent.change(nameInput, { target: { value: 'e2e-slug-override' } })
    expect(screen.getByText(/auto-derive from display name/i)).toBeDefined()

    // Further changes to display name do NOT update the name field.
    fireEvent.change(displayInput, { target: { value: 'Different Display Name' } })
    expect(nameInput.value).toBe('e2e-slug-override')

    // Clicking the reset link re-derives the slug from the current display name
    // and hides the reset affordance.
    fireEvent.click(screen.getByText(/auto-derive from display name/i))
    expect(nameInput.value).toBe('different-display-name')
    expect(screen.queryByText(/auto-derive from display name/i)).toBeNull()
  })

  it('disables submit and shows Creating… label while the mutation is pending', () => {
    ;(useCreateProject as Mock).mockReturnValue({
      mutateAsync: mockMutateAsync,
      isPending: true,
    })

    render(<CreateProjectDialog open={true} onOpenChange={onOpenChange} defaultOrganization="my-org" />)

    fireEvent.change(screen.getByPlaceholderText(/my project/i), {
      target: { value: 'Pending Project' },
    })

    const submit = screen.getByRole('button', { name: /creating…/i }) as HTMLButtonElement
    expect(submit).toBeDefined()
    expect(submit.disabled).toBe(true)
  })
})

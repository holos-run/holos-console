import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/organizations', () => ({
  useListOrganizations: vi.fn(),
  useCreateOrganization: vi.fn(),
}))

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

vi.mock('@/components/ui/checkbox', () => ({
  Checkbox: ({ id, checked, onCheckedChange, ...props }: {
    id?: string
    checked?: boolean
    onCheckedChange?: (checked: boolean) => void
    [key: string]: unknown
  }) => (
    <input
      type="checkbox"
      id={id}
      checked={checked ?? false}
      onChange={(e) => onCheckedChange?.(e.target.checked)}
      role="checkbox"
      {...props}
    />
  ),
}))

vi.mock('@/components/ui/tooltip', () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <div data-testid="tooltip-content">{children}</div>,
}))

import { useCreateOrganization } from '@/queries/organizations'
import { CreateOrgDialog } from './create-org-dialog'

describe('CreateOrgDialog', () => {
  const mockMutateAsync = vi.fn()
  const onOpenChange = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    ;(useCreateOrganization as Mock).mockReturnValue({
      mutateAsync: mockMutateAsync,
      isPending: false,
    })
  })

  it('renders displayName, name, and description fields when open', () => {
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByPlaceholderText(/my-org/i)).toBeDefined()
    expect(screen.getByPlaceholderText(/my organization/i)).toBeDefined()
    expect(screen.getByPlaceholderText(/optional description/i)).toBeDefined()
  })

  it('does not render when closed', () => {
    render(<CreateOrgDialog open={false} onOpenChange={onOpenChange} />)
    expect(screen.queryByTestId('dialog')).toBeNull()
  })

  it('auto-derives name from display name as user types', () => {
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'Test Org' } })
    expect((screen.getByPlaceholderText(/my-org/i) as HTMLInputElement).value).toBe('test-org')
  })

  it('stops auto-deriving name once user manually edits name field', () => {
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'Test Org' } })
    fireEvent.change(screen.getByPlaceholderText(/my-org/i), { target: { value: 'custom-slug' } })
    // Further display name changes should not override the custom name
    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'Test Org Updated' } })
    expect((screen.getByPlaceholderText(/my-org/i) as HTMLInputElement).value).toBe('custom-slug')
  })

  it('shows reset link when name has been manually edited', () => {
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'Test Org' } })
    fireEvent.change(screen.getByPlaceholderText(/my-org/i), { target: { value: 'custom-slug' } })
    expect(screen.getByText(/auto-derive from display name/i)).toBeDefined()
  })

  it('re-enables auto-derivation when reset link is clicked', () => {
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'Test Org' } })
    fireEvent.change(screen.getByPlaceholderText(/my-org/i), { target: { value: 'custom-slug' } })
    fireEvent.click(screen.getByText(/auto-derive from display name/i))
    // After reset, name should be re-derived from current display name
    expect((screen.getByPlaceholderText(/my-org/i) as HTMLInputElement).value).toBe('test-org')
    // And further display name changes should update name again
    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'New Org' } })
    expect((screen.getByPlaceholderText(/my-org/i) as HTMLInputElement).value).toBe('new-org')
  })

  it('calls mutateAsync with form values on submit', async () => {
    mockMutateAsync.mockResolvedValue({ name: 'new-org' })
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'New Org' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'new-org', displayName: 'New Org' })
      )
    })
  })

  it('closes dialog on successful create', async () => {
    mockMutateAsync.mockResolvedValue({ name: 'new-org' })
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'New Org' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  it('renders error alert on server error', async () => {
    mockMutateAsync.mockRejectedValue(new Error('name already taken'))
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'Taken Org' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeDefined()
      expect(screen.getByText(/name already taken/i)).toBeDefined()
    })
  })

  it('does not close dialog on error', async () => {
    mockMutateAsync.mockRejectedValue(new Error('server error'))
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

    fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'Bad Org' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(onOpenChange).not.toHaveBeenCalledWith(false)
    })
  })

  describe('populate defaults checkbox', () => {
    it('renders checkbox unchecked by default', () => {
      render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
      const checkbox = screen.getByRole('checkbox')
      expect(checkbox).toBeDefined()
      expect((checkbox as HTMLInputElement).checked).toBe(false)
    })

    it('renders label text for the checkbox', () => {
      render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
      expect(screen.getByText('Populate with example resources')).toBeDefined()
    })

    it('renders tooltip with expected content', () => {
      render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
      expect(
        screen.getByText(/Creates a default folder and project structure with example templates/)
      ).toBeDefined()
    })

    it('passes populateDefaults: true when checkbox is checked', async () => {
      mockMutateAsync.mockResolvedValue({ name: 'new-org' })
      render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

      fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'New Org' } })
      fireEvent.click(screen.getByRole('checkbox'))
      fireEvent.submit(screen.getByRole('form'))

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ populateDefaults: true })
        )
      })
    })

    it('does not send populateDefaults when checkbox is unchecked', async () => {
      mockMutateAsync.mockResolvedValue({ name: 'new-org' })
      render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

      fireEvent.change(screen.getByPlaceholderText(/my organization/i), { target: { value: 'New Org' } })
      fireEvent.submit(screen.getByRole('form'))

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalled()
        const args = mockMutateAsync.mock.calls[0][0]
        expect(args).not.toHaveProperty('populateDefaults')
      })
    })
  })
})

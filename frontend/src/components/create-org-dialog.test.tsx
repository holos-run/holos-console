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

  it('renders name, displayName, and description fields when open', () => {
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByPlaceholderText(/my-org/i)).toBeDefined()
    expect(screen.getByPlaceholderText(/my organization/i)).toBeDefined()
    expect(screen.getByPlaceholderText(/optional description/i)).toBeDefined()
  })

  it('does not render when closed', () => {
    render(<CreateOrgDialog open={false} onOpenChange={onOpenChange} />)
    expect(screen.queryByTestId('dialog')).toBeNull()
  })

  it('calls mutateAsync with form values on submit', async () => {
    mockMutateAsync.mockResolvedValue({ name: 'new-org' })
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

    fireEvent.change(screen.getByPlaceholderText(/my-org/i), { target: { value: 'new-org' } })
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

    fireEvent.change(screen.getByPlaceholderText(/my-org/i), { target: { value: 'new-org' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  it('renders error alert on server error', async () => {
    mockMutateAsync.mockRejectedValue(new Error('name already taken'))
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

    fireEvent.change(screen.getByPlaceholderText(/my-org/i), { target: { value: 'taken-org' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeDefined()
      expect(screen.getByText(/name already taken/i)).toBeDefined()
    })
  })

  it('does not close dialog on error', async () => {
    mockMutateAsync.mockRejectedValue(new Error('server error'))
    render(<CreateOrgDialog open={true} onOpenChange={onOpenChange} />)

    fireEvent.change(screen.getByPlaceholderText(/my-org/i), { target: { value: 'bad-org' } })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => {
      expect(onOpenChange).not.toHaveBeenCalledWith(false)
    })
  })
})

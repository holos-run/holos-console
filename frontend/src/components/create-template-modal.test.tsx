import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/deployment-templates', () => ({
  useCreateDeploymentTemplate: vi.fn(),
}))

vi.mock('@/components/ui/dialog', () => ({
  Dialog: ({ children, open }: { children: React.ReactNode; open?: boolean }) =>
    open ? <div data-testid="dialog">{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <p>{children}</p>,
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
  Button: ({ children, onClick, variant, type, disabled }: {
    children: React.ReactNode
    onClick?: () => void
    variant?: string
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

import { useCreateDeploymentTemplate } from '@/queries/deployment-templates'
import { CreateTemplateModal } from './create-template-modal'

function setupMocks() {
  ;(useCreateDeploymentTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
    reset: vi.fn(),
  })
}

describe('CreateTemplateModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders dialog when open', () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    expect(screen.getByTestId('dialog')).toBeInTheDocument()
  })

  it('does not render dialog when closed', () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={false}
        onOpenChange={vi.fn()}
      />,
    )
    expect(screen.queryByTestId('dialog')).not.toBeInTheDocument()
  })

  it('CUE template textarea has fixed height and overflow classes', () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    const textarea = screen.getByRole('textbox', { name: /cue template/i })
    expect(textarea.className).toContain('field-sizing-normal')
    expect(textarea.className).toContain('max-h-96')
    expect(textarea.className).toContain('overflow-y-auto')
  })

  it('calls mutateAsync with form values on create', async () => {
    setupMocks()
    const mutateAsync = (useCreateDeploymentTemplate as Mock).mock.results[0]?.value?.mutateAsync
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText('Display Name'), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    const mutate = (useCreateDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutate).toHaveBeenCalledWith(
        expect.objectContaining({ displayName: 'My Template', name: 'my-template' }),
      )
    })
  })

  it('shows error when name is empty on create', async () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    // Clear the display name to ensure name is empty
    fireEvent.change(screen.getByLabelText('Display Name'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Name slug'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
  })
})

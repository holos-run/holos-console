import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/deployment-templates', () => ({
  useCreateDeploymentTemplate: vi.fn(),
  useRenderDeploymentTemplate: vi.fn(),
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

import { useCreateDeploymentTemplate, useRenderDeploymentTemplate } from '@/queries/deployment-templates'
import { CreateTemplateModal } from './create-template-modal'

function setupMocks({
  renderResult = { data: undefined, isLoading: false, isError: false, error: null },
}: {
  renderResult?: { data?: { renderedYaml: string; renderedJson: string }; isLoading?: boolean; isError?: boolean; error?: Error | null }
} = {}) {
  ;(useCreateDeploymentTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
    reset: vi.fn(),
  })
  ;(useRenderDeploymentTemplate as Mock).mockReturnValue(renderResult)
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

  it('shows Preview button in the modal', () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: /preview/i })).toBeInTheDocument()
  })

  it('shows rendered JSON when preview data is available', async () => {
    const jsonOutput = '[\n  { "kind": "Deployment" }\n]'
    setupMocks({ renderResult: { data: { renderedYaml: '', renderedJson: jsonOutput }, isLoading: false, isError: false, error: null } })
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /^preview$/i }))
    await waitFor(() => {
      // The rendered JSON is displayed in a <pre> element
      const pre = document.querySelector('pre')
      expect(pre).toBeInTheDocument()
      expect(pre?.textContent).toContain('"kind": "Deployment"')
    })
  })

  it('shows loading state while rendering', async () => {
    setupMocks({ renderResult: { data: undefined, isLoading: true, isError: false, error: null } })
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /preview/i }))
    await waitFor(() => {
      expect(screen.getByText(/rendering/i)).toBeInTheDocument()
    })
  })

  it('shows error alert when render fails', async () => {
    setupMocks({
      renderResult: { data: undefined, isLoading: false, isError: true, error: new Error('CUE render error') },
    })
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /preview/i }))
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
      expect(screen.getByText(/CUE render error/i)).toBeInTheDocument()
    })
  })

  it('does not call useRenderDeploymentTemplate with enabled=true when modal is closed', () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={false}
        onOpenChange={vi.fn()}
      />,
    )
    // The hook is always called (rules of hooks), but enabled should be false
    const calls = (useRenderDeploymentTemplate as Mock).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    // enabled argument should be false (open=false)
    const lastCall = calls[calls.length - 1]
    expect(lastCall[5]).toBe(false)
  })

  it('does not pass enabled=true when modal is open but preview is closed', () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    // Preview is not open by default, so enabled should be false
    const calls = (useRenderDeploymentTemplate as Mock).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    const lastCall = calls[calls.length - 1]
    expect(lastCall[5]).toBe(false)
  })

  it('passes enabled=true when modal is open and preview is open', async () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /preview/i }))
    await waitFor(() => {
      const calls = (useRenderDeploymentTemplate as Mock).mock.calls
      // Find the last call where enabled is true
      const enabledCall = calls.find((c) => c[5] === true)
      expect(enabledCall).toBeDefined()
    })
  })

  it('default CUE template uses structured namespaced/cluster output format', () => {
    setupMocks()
    render(
      <CreateTemplateModal
        projectName="test-project"
        open={true}
        onOpenChange={vi.fn()}
      />,
    )
    const textarea = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
    // Verify the default template uses the structured output format required by the backend
    expect(textarea.value).toContain('package deployment')
    expect(textarea.value).toContain('namespaced:')
    expect(textarea.value).toContain('cluster:')
    expect(textarea.value).toContain('#Input')
    expect(textarea.value).toContain('input: #Input')
    expect(textarea.value).toContain('"app.kubernetes.io/managed-by": "console.holos.run"')
  })
})

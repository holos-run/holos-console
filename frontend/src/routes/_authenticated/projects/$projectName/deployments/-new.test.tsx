import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
    }),
    useNavigate: () => mockNavigate,
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

vi.mock('@/queries/deployments', () => ({
  useCreateDeployment: vi.fn(),
  useListNamespaceSecrets: vi.fn().mockReturnValue({ data: [], isLoading: false }),
  useListNamespaceConfigMaps: vi.fn().mockReturnValue({ data: [], isLoading: false }),
}))

vi.mock('@/queries/deployment-templates', () => ({
  useListDeploymentTemplates: vi.fn(),
}))

vi.mock('@/components/ui/select', () => ({
  Select: ({ value, onValueChange, children }: { value?: string; onValueChange?: (v: string) => void; children: React.ReactNode }) => (
    <select data-testid="template-select" data-value={value} value={value} onChange={(e) => onValueChange?.(e.target.value)}>
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: React.ReactNode }) => (
    <option value={value}>{children}</option>
  ),
  SelectValue: () => null,
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useCreateDeployment } from '@/queries/deployments'
import { useListDeploymentTemplates } from '@/queries/deployment-templates'
import { CreateDeploymentPage } from './new'

function makeTemplate(name: string, defaults?: { image?: string; tag?: string; command?: string[]; args?: string[]; env?: unknown[] }) {
  return { name, project: 'test-project', displayName: '', description: '', cueTemplate: '', defaults }
}

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({ name: 'my-api' }),
  templates = [makeTemplate('web-app'), makeTemplate('worker-tmpl')],
) {
  ;(useCreateDeployment as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useListDeploymentTemplates as Mock).mockReturnValue({
    data: templates,
    isLoading: false,
  })
}

describe('CreateDeploymentPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateDeploymentPage />)
    // CardTitle renders as a div (not a heading); button also says "Create Deployment"
    const elements = screen.getAllByText('Create Deployment')
    expect(elements.length).toBeGreaterThan(0)
  })

  it('renders Display Name field', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name (slug) field', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders Template select', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByTestId('template-select')).toBeInTheDocument()
  })

  it('renders Image field', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/^image$/i)).toBeInTheDocument()
  })

  it('renders Tag field', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/^tag$/i)).toBeInTheDocument()
  })

  it('renders Command section', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByText(/^command$/i)).toBeInTheDocument()
  })

  it('renders Args section', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByText(/^args$/i)).toBeInTheDocument()
  })

  it('renders Environment Variables section', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getAllByText(/environment variables/i).length).toBeGreaterThan(0)
  })

  it('renders Create Deployment submit button', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByRole('button', { name: /create deployment/i })).toBeInTheDocument()
  })

  it('renders a Cancel link back to the deployments list', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('auto-derives slug from display name', () => {
    render(<CreateDeploymentPage />)
    const displayNameInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayNameInput, { target: { value: 'My Web App' } })
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('my-web-app')
  })

  it('shows template options', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByText('web-app')).toBeInTheDocument()
    expect(screen.getByText('worker-tmpl')).toBeInTheDocument()
  })

  it('shows validation error when name is empty', async () => {
    render(<CreateDeploymentPage />)
    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))
    await waitFor(() => {
      expect(screen.getByText(/name is required/i)).toBeInTheDocument()
    })
  })

  it('shows validation error when template is not selected', async () => {
    render(<CreateDeploymentPage />)
    const displayInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayInput, { target: { value: 'My API' } })
    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))
    await waitFor(() => {
      expect(screen.getByText(/template is required/i)).toBeInTheDocument()
    })
  })

  it('shows validation error when image is empty', async () => {
    render(<CreateDeploymentPage />)
    const displayInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayInput, { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))
    await waitFor(() => {
      expect(screen.getByText(/image is required/i)).toBeInTheDocument()
    })
  })

  it('shows validation error when tag is empty', async () => {
    render(<CreateDeploymentPage />)
    const displayInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayInput, { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))
    await waitFor(() => {
      expect(screen.getByText(/tag is required/i)).toBeInTheDocument()
    })
  })

  it('calls createMutation with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-api' })
    setupMocks(mutateAsync)
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.change(screen.getByLabelText(/^tag$/i), { target: { value: 'v1.0.0' } })

    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-api',
          displayName: 'My API',
          template: 'web-app',
          image: 'ghcr.io/org/api',
          tag: 'v1.0.0',
        }),
      )
    })
  })

  it('navigates to deployment detail page after successful creation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-api' })
    setupMocks(mutateAsync)
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.change(screen.getByLabelText(/^tag$/i), { target: { value: 'v1.0.0' } })

    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/projects/$projectName/deployments/$deploymentName',
          params: expect.objectContaining({ deploymentName: 'my-api' }),
        }),
      )
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.change(screen.getByLabelText(/^tag$/i), { target: { value: 'v1.0.0' } })

    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('shows "no templates" link to create templates page when no templates exist', () => {
    setupMocks(vi.fn(), [])
    render(<CreateDeploymentPage />)
    expect(screen.getByText(/no templates available/i)).toBeInTheDocument()
    const link = screen.getByRole('link', { name: /create a template/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toContain('templates/new')
  })

  it('does not show "no templates" message when templates exist', () => {
    render(<CreateDeploymentPage />)
    expect(screen.queryByText(/no templates available/i)).not.toBeInTheDocument()
  })

  it('selecting a template with defaults pre-fills image and tag', async () => {
    const templates = [makeTemplate('web-app', { image: 'ghcr.io/org/web', tag: 'v2.0.0' })]
    setupMocks(vi.fn(), templates)
    render(<CreateDeploymentPage />)

    expect(screen.getByLabelText(/^image$/i)).toHaveValue('')
    expect(screen.getByLabelText(/^tag$/i)).toHaveValue('')

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })

    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/web')
      expect(screen.getByLabelText(/^tag$/i)).toHaveValue('v2.0.0')
    })
  })

  it('passes command and args to mutateAsync', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-api' })
    setupMocks(mutateAsync)
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.change(screen.getByLabelText(/^tag$/i), { target: { value: 'v1.0.0' } })

    fireEvent.change(screen.getByLabelText(/command entry/i), { target: { value: 'myapp' } })
    fireEvent.click(screen.getByRole('button', { name: /add command/i }))

    fireEvent.change(screen.getByLabelText(/args entry/i), { target: { value: '--port' } })
    fireEvent.click(screen.getByRole('button', { name: /add args/i }))

    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ command: ['myapp'], args: ['--port'] }),
      )
    })
  })
})

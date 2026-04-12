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

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  makeProjectScope: vi.fn().mockReturnValue({ scope: 1, scopeName: 'test-project' }),
}))

vi.mock('@/components/ui/combobox', () => ({
  Combobox: ({ items, value, onValueChange, 'aria-label': ariaLabel }: {
    items: { value: string; label: string }[]
    value: string
    onValueChange: (v: string) => void
    'aria-label'?: string
  }) => (
    <select
      data-testid="template-select"
      aria-label={ariaLabel ?? 'Template'}
      value={value}
      onChange={(e) => onValueChange(e.target.value)}
    >
      {items.map((item) => (
        <option key={item.value} value={item.value}>{item.label}</option>
      ))}
    </select>
  ),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useCreateDeployment } from '@/queries/deployments'
import { useListTemplates } from '@/queries/templates'
import { CreateDeploymentPage } from './new'

function makeTemplate(name: string, defaults?: { name?: string; image?: string; tag?: string; command?: string[]; args?: string[]; env?: unknown[]; port?: number; description?: string }) {
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
  ;(useListTemplates as Mock).mockReturnValue({
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

  it('selecting a template with defaults pre-fills name and slug', async () => {
    const templates = [makeTemplate('web-app', { name: 'httpbin', image: 'ghcr.io/org/web', tag: 'v2.0.0' })]
    setupMocks(vi.fn(), templates)
    render(<CreateDeploymentPage />)

    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('')

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })

    await waitFor(() => {
      const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
      expect(slugInput.value).toBe('httpbin')
    })
  })

  it('selecting a template with description default pre-fills description field', async () => {
    const templates = [makeTemplate('web-app', { image: 'ghcr.io/org/web', tag: 'v2.0.0', description: 'A simple HTTP service' })]
    setupMocks(vi.fn(), templates)
    render(<CreateDeploymentPage />)

    const descInput = screen.getByLabelText(/^description$/i) as HTMLInputElement
    expect(descInput.value).toBe('')

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })

    await waitFor(() => {
      const descInput = screen.getByLabelText(/^description$/i) as HTMLInputElement
      expect(descInput.value).toBe('A simple HTTP service')
    })
  })

  it('Combobox renders with correct aria-label for template selection', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/^template$/i)).toBeInTheDocument()
  })

  it('renders Port field', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/^port$/i)).toBeInTheDocument()
  })

  it('Port field defaults to 8080', () => {
    render(<CreateDeploymentPage />)
    const portInput = screen.getByLabelText(/^port$/i) as HTMLInputElement
    expect(portInput.value).toBe('8080')
  })

  it('selecting a template with port default pre-fills port field', async () => {
    const templates = [makeTemplate('web-app', { image: 'ghcr.io/org/web', tag: 'v2.0.0', port: 3000 })]
    setupMocks(vi.fn(), templates)
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })

    await waitFor(() => {
      const portInput = screen.getByLabelText(/^port$/i) as HTMLInputElement
      expect(portInput.value).toBe('3000')
    })
  })

  it('selecting a template without port default uses 8080', async () => {
    const templates = [makeTemplate('web-app', { image: 'ghcr.io/org/web', tag: 'v2.0.0' })]
    setupMocks(vi.fn(), templates)
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })

    await waitFor(() => {
      const portInput = screen.getByLabelText(/^port$/i) as HTMLInputElement
      expect(portInput.value).toBe('8080')
    })
  })

  it('shows validation error when port is out of range', async () => {
    render(<CreateDeploymentPage />)
    const displayInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayInput, { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.change(screen.getByLabelText(/^tag$/i), { target: { value: 'v1.0.0' } })
    fireEvent.change(screen.getByLabelText(/^port$/i), { target: { value: '99999' } })
    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))
    await waitFor(() => {
      expect(screen.getByText(/port must be between 1 and 65535/i)).toBeInTheDocument()
    })
  })

  it('port value is sent in mutation payload', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-api' })
    setupMocks(mutateAsync)
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My API' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.change(screen.getByLabelText(/^tag$/i), { target: { value: 'v1.0.0' } })
    fireEvent.change(screen.getByLabelText(/^port$/i), { target: { value: '3000' } })

    fireEvent.click(screen.getByRole('button', { name: /create deployment/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ port: 3000 }),
      )
    })
  })

  // --- Field-ordering regression tests (issue #796) ---

  it('renders Template as the first form field, before Display Name', () => {
    render(<CreateDeploymentPage />)
    const labels = screen.getAllByText(
      /^(template|display name|name \(slug\)|description|image|tag|port)$/i,
    )
    const templateIndex = labels.findIndex(
      (el) => el.textContent === 'Template',
    )
    const displayNameIndex = labels.findIndex((el) =>
      /display name/i.test(el.textContent ?? ''),
    )
    expect(templateIndex).toBeGreaterThanOrEqual(0)
    expect(displayNameIndex).toBeGreaterThanOrEqual(0)
    expect(templateIndex).toBeLessThan(displayNameIndex)
    expect(templateIndex).toBe(0) // Template is the very first field
  })

  it('renders Combobox (not Select) for template selection when templates exist', () => {
    render(<CreateDeploymentPage />)
    // The Combobox mock renders a <select> with data-testid="template-select".
    // A native Select component would not carry this test id.
    const combobox = screen.getByTestId('template-select')
    expect(combobox).toBeInTheDocument()
    // Verify it contains the template options from setupMocks
    expect(screen.getByText('web-app')).toBeInTheDocument()
    expect(screen.getByText('worker-tmpl')).toBeInTheDocument()
  })

  it('renders "No templates available" fallback as the first field when templates list is empty', () => {
    setupMocks(vi.fn(), [])
    render(<CreateDeploymentPage />)
    // The fallback text and link should render
    expect(screen.getByText(/no templates available/i)).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /create a template/i }),
    ).toBeInTheDocument()
    // The fallback should still appear before Display Name in DOM order
    const labels = screen.getAllByText(
      /^(template|display name|name \(slug\)|description|image|tag|port)$/i,
    )
    const templateIndex = labels.findIndex(
      (el) => el.textContent === 'Template',
    )
    const displayNameIndex = labels.findIndex((el) =>
      /display name/i.test(el.textContent ?? ''),
    )
    expect(templateIndex).toBeGreaterThanOrEqual(0)
    expect(displayNameIndex).toBeGreaterThanOrEqual(0)
    expect(templateIndex).toBeLessThan(displayNameIndex)
    expect(templateIndex).toBe(0)
  })

  // --- End field-ordering regression tests ---

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

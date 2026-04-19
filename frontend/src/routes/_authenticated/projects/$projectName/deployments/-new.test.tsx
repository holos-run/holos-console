import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
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
  useGetTemplateDefaults: vi.fn(),
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
      <option value="" />
      {items.map((item) => (
        <option key={item.value} value={item.value}>{item.label}</option>
      ))}
    </select>
  ),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useCreateDeployment } from '@/queries/deployments'
import { useListTemplates, useGetTemplateDefaults } from '@/queries/templates'
import { CreateDeploymentPage } from './new'

type Defaults = {
  name?: string
  displayName?: string
  description?: string
  image?: string
  tag?: string
  port?: number
  command?: string[]
  args?: string[]
  env?: unknown[]
}

function makeTemplate(name: string) {
  return { name, project: 'test-project', displayName: '', description: '', cueTemplate: '' }
}

/**
 * Install a programmable useGetTemplateDefaults mock. It returns a fresh
 * object each call whose data is derived from the `defaultsByName` table
 * keyed on the template name passed to the hook. It also records every call
 * via a spy stored on the mock so tests can assert call counts and the
 * refetch count from Load defaults.
 */
function installDefaultsMock(defaultsByName: Record<string, Defaults | undefined>) {
  const rpcSpy = vi.fn()
  const refetchSpy = vi.fn()
  ;(useGetTemplateDefaults as Mock).mockImplementation((params: { scope: unknown; name: string }) => {
    // Record every render call with the current template name to model the
    // "RPC fires on every template change" contract. The hook only actually
    // refetches when name changes, but counting renders with distinct names
    // is a close proxy that the component consumers can assert on.
    rpcSpy(params.name)
    const d = params.name ? defaultsByName[params.name] : undefined
    const hasResolved = !!params.name
    return {
      data: d,
      isFetching: false,
      isSuccess: hasResolved,
      isError: false,
      error: null,
      refetch: async () => {
        refetchSpy(params.name)
        return { status: 'success', data: defaultsByName[params.name] }
      },
    }
  })
  return { rpcSpy, refetchSpy }
}

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({ name: 'my-api' }),
  templates: ReturnType<typeof makeTemplate>[] = [makeTemplate('web-app'), makeTemplate('worker-tmpl')],
  defaultsByName: Record<string, Defaults | undefined> = {},
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
  return installDefaultsMock(defaultsByName)
}

describe('CreateDeploymentPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateDeploymentPage />)
    const elements = screen.getAllByText('Create Deployment')
    expect(elements.length).toBeGreaterThan(0)
  })

  it('renders as a standalone page (not inside a dialog)', () => {
    // Regression: the Create Deployment affordance used to open a modal
    // dialog; issue #396 moved it to a dedicated /deployments/new route.
    // The E2E test asserted page.getByRole(dialog) was not visible;
    // replicate that anti-regression at the component level so the Vitest
    // migration (HOL-655) preserves the invariant.
    render(<CreateDeploymentPage />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
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

  it('renders Port field', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/^port$/i)).toBeInTheDocument()
  })

  it('Port field defaults to 8080', () => {
    render(<CreateDeploymentPage />)
    const portInput = screen.getByLabelText(/^port$/i) as HTMLInputElement
    expect(portInput.value).toBe('8080')
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
    expect(templateIndex).toBe(0)
  })

  it('renders Combobox (not Select) for template selection when templates exist', () => {
    render(<CreateDeploymentPage />)
    const combobox = screen.getByTestId('template-select')
    expect(combobox).toBeInTheDocument()
    expect(screen.getByText('web-app')).toBeInTheDocument()
    expect(screen.getByText('worker-tmpl')).toBeInTheDocument()
  })

  it('renders "No templates available" fallback as the first field when templates list is empty', () => {
    setupMocks(vi.fn(), [])
    render(<CreateDeploymentPage />)
    expect(screen.getByText(/no templates available/i)).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /create a template/i }),
    ).toBeInTheDocument()
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

  it('Combobox renders with correct aria-label for template selection', () => {
    render(<CreateDeploymentPage />)
    expect(screen.getByLabelText(/^template$/i)).toBeInTheDocument()
  })

  // --- ADR 027 pre-fill behavior: pristine selection path -------------------

  it('pristine form: selecting httpbin-v1 fills every defaultable field from the RPC response', async () => {
    setupMocks(vi.fn(), [makeTemplate('httpbin-v1')], {
      'httpbin-v1': {
        name: 'httpbin',
        description: 'A simple HTTP service',
        image: 'ghcr.io/mccutchen/go-httpbin',
        tag: '2.21.0',
        port: 9090,
        command: ['/bin/httpbin'],
        args: ['--port', '9090'],
      },
    })
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'httpbin-v1' } })

    await waitFor(() => {
      expect(screen.getByLabelText(/display name/i)).toHaveValue('httpbin')
      expect(screen.getByLabelText(/name slug/i)).toHaveValue('httpbin')
      expect(screen.getByLabelText(/^description$/i)).toHaveValue('A simple HTTP service')
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/mccutchen/go-httpbin')
      expect(screen.getByLabelText(/^tag$/i)).toHaveValue('2.21.0')
      expect(screen.getByLabelText(/^port$/i)).toHaveValue(9090)
    })
    expect(screen.getByText('/bin/httpbin')).toBeInTheDocument()
    expect(screen.getByText('--port')).toBeInTheDocument()
    expect(screen.getByText('9090')).toBeInTheDocument()
  })

  it('pristine form: switching from templateA to templateB replaces fields with templateB defaults', async () => {
    setupMocks(vi.fn(), [makeTemplate('template-a'), makeTemplate('template-b')], {
      'template-a': {
        name: 'alpha-svc', description: 'Alpha service',
        image: 'ghcr.io/org/alpha', tag: '1.0.0', port: 3000,
        command: ['/alpha'], args: ['--verbose'],
      },
      'template-b': {
        name: 'beta-svc', description: 'Beta service',
        image: 'ghcr.io/org/beta', tag: '2.0.0', port: 4000,
        command: ['/beta'], args: ['--quiet'],
      },
    })
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-a' } })
    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/alpha')
    })
    expect(screen.getByText('/alpha')).toBeInTheDocument()

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-b' } })

    await waitFor(() => {
      expect(screen.getByLabelText(/display name/i)).toHaveValue('beta-svc')
      expect(screen.getByLabelText(/name slug/i)).toHaveValue('beta-svc')
      expect(screen.getByLabelText(/^description$/i)).toHaveValue('Beta service')
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/beta')
      expect(screen.getByLabelText(/^tag$/i)).toHaveValue('2.0.0')
      expect(screen.getByLabelText(/^port$/i)).toHaveValue(4000)
    })
    expect(screen.queryByText('/alpha')).not.toBeInTheDocument()
    expect(screen.queryByText('--verbose')).not.toBeInTheDocument()
    expect(screen.getByText('/beta')).toBeInTheDocument()
    expect(screen.getByText('--quiet')).toBeInTheDocument()
  })

  it('selecting a template with partial defaults leaves port at 8080 and empty fields blank', async () => {
    setupMocks(vi.fn(), [makeTemplate('partial-tmpl')], {
      'partial-tmpl': { image: 'ghcr.io/org/partial', tag: 'latest' },
    })
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'partial-tmpl' } })

    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/partial')
      expect(screen.getByLabelText(/^tag$/i)).toHaveValue('latest')
    })
    expect(screen.getByLabelText(/display name/i)).toHaveValue('')
    expect(screen.getByLabelText(/name slug/i)).toHaveValue('')
    expect(screen.getByLabelText(/^description$/i)).toHaveValue('')
    // Port preserved at user-friendly default since response carried no port.
    expect(screen.getByLabelText(/^port$/i)).toHaveValue(8080)
    expect(screen.queryAllByRole('button', { name: /remove item/i })).toHaveLength(0)
    expect(screen.getByLabelText(/command entry/i)).toHaveValue('')
    expect(screen.getByLabelText(/args entry/i)).toHaveValue('')
  })

  // --- ADR 027 pre-fill behavior: dirty form path ---------------------------

  it('dirty form: user edits image, then switching to templateB does NOT overwrite fields', async () => {
    setupMocks(vi.fn(), [makeTemplate('template-a'), makeTemplate('template-b')], {
      'template-a': {
        name: 'alpha-svc', image: 'ghcr.io/org/alpha', tag: '1.0.0', port: 3000,
      },
      'template-b': {
        name: 'beta-svc', image: 'ghcr.io/org/beta', tag: '2.0.0', port: 4000,
      },
    })
    render(<CreateDeploymentPage />)

    // Pristine select fills from template-a.
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-a' } })
    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/alpha')
    })

    // User edits image — form is now dirty.
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/custom/image' } })
    expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/custom/image')

    // Switch template. RPC fires (via hook re-render under new key) but no
    // overwrite happens because the form is dirty.
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-b' } })

    // Give effects a tick to run.
    await act(async () => { await Promise.resolve() })

    expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/custom/image')
    expect(screen.getByLabelText(/display name/i)).toHaveValue('alpha-svc')
    expect(screen.getByLabelText(/^port$/i)).toHaveValue(3000)
  })

  it('dirty form: clicking Load defaults overwrites every field and resets pristine', async () => {
    const { refetchSpy } = setupMocks(vi.fn(), [makeTemplate('template-a'), makeTemplate('template-b')], {
      'template-a': {
        name: 'alpha-svc', description: 'Alpha service',
        image: 'ghcr.io/org/alpha', tag: '1.0.0', port: 3000,
        command: ['/alpha'], args: ['--a'],
      },
      'template-b': {
        name: 'beta-svc', description: 'Beta service',
        image: 'ghcr.io/org/beta', tag: '2.0.0', port: 4000,
        command: ['/beta'], args: ['--b'],
      },
    })
    render(<CreateDeploymentPage />)

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-a' } })
    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/alpha')
    })

    // Dirty the form.
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/custom/image' } })

    // Switch to template-b on a dirty form — no overwrite expected.
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-b' } })
    await act(async () => { await Promise.resolve() })
    expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/custom/image')

    // Click Load defaults — overwrite unconditionally from template-b data.
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /load defaults/i }))
    })
    expect(refetchSpy).toHaveBeenCalledWith('template-b')
    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/beta')
      expect(screen.getByLabelText(/^tag$/i)).toHaveValue('2.0.0')
      expect(screen.getByLabelText(/^port$/i)).toHaveValue(4000)
      expect(screen.getByLabelText(/display name/i)).toHaveValue('beta-svc')
      expect(screen.getByLabelText(/name slug/i)).toHaveValue('beta-svc')
      expect(screen.getByLabelText(/^description$/i)).toHaveValue('Beta service')
    })
    expect(screen.getByText('/beta')).toBeInTheDocument()
    expect(screen.getByText('--b')).toBeInTheDocument()

    // Pristine was reset — but now edit a field and switch again: no overwrite.
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/custom/again' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-a' } })
    await act(async () => { await Promise.resolve() })
    expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/custom/again')
  })

  it('Load defaults: stale response for previous template does not overwrite new selection', async () => {
    // Build a deferred refetch so we can resolve it *after* the user has
    // already switched templates. This simulates the race where the user
    // clicks Load defaults for template-a, then switches to template-b
    // before the RPC resolves.
    let resolveRefetch: ((value: { status: 'success'; data: Defaults | undefined }) => void) | null = null
    const deferred = new Promise<{ status: 'success'; data: Defaults | undefined }>((resolve) => {
      resolveRefetch = resolve
    })

    const defaultsByName: Record<string, Defaults | undefined> = {
      'template-a': {
        name: 'alpha-svc', description: 'Alpha service',
        image: 'ghcr.io/org/alpha', tag: '1.0.0', port: 3000,
        command: ['/alpha'], args: ['--a'],
      },
      'template-b': {
        name: 'beta-svc', description: 'Beta service',
        image: 'ghcr.io/org/beta', tag: '2.0.0', port: 4000,
        command: ['/beta'], args: ['--b'],
      },
    }

    ;(useCreateDeployment as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
      reset: vi.fn(),
    })
    ;(useListTemplates as Mock).mockReturnValue({
      data: [makeTemplate('template-a'), makeTemplate('template-b')],
      isLoading: false,
    })
    ;(useGetTemplateDefaults as Mock).mockImplementation((params: { scope: unknown; name: string }) => {
      const d = params.name ? defaultsByName[params.name] : undefined
      return {
        data: d,
        isFetching: false,
        isSuccess: !!params.name,
        isError: false,
        error: null,
        // For template-a, return the deferred promise so the test can resolve
        // it at will. For any other template, resolve synchronously.
        refetch: async () => {
          if (params.name === 'template-a') {
            return deferred
          }
          return { status: 'success' as const, data: defaultsByName[params.name] }
        },
      }
    })

    render(<CreateDeploymentPage />)

    // Select template-a on a pristine form. The hook's `data` path will apply
    // template-a's defaults synchronously (mock returns data immediately).
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-a' } })
    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/alpha')
    })

    // User dirties the form so the pristine-selection path will not fire on
    // subsequent template change — we want to isolate the Load-defaults race.
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/custom/image' } })

    // Click Load defaults while template-a is selected. refetch is deferred.
    fireEvent.click(screen.getByRole('button', { name: /load defaults/i }))

    // Before the refetch resolves, switch to template-b.
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-b' } })
    // Let any effects from the template change settle.
    await act(async () => { await Promise.resolve() })

    // Form is dirty, so switching does not auto-apply template-b's defaults;
    // user's custom image should still be present.
    expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/custom/image')

    // Now resolve the stale Load-defaults promise for template-a.
    await act(async () => {
      resolveRefetch!({ status: 'success', data: defaultsByName['template-a'] })
      await Promise.resolve()
      await Promise.resolve()
    })

    // The stale result MUST be dropped: the user's dirtied image must be
    // preserved (Load defaults would have overwritten it to template-a's
    // value if the stale response were applied).
    expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/custom/image')
    expect(screen.getByLabelText(/^image$/i)).not.toHaveValue('ghcr.io/org/alpha')
    // No error surfaced for the silently-dropped stale response.
    expect(screen.queryByText(/failed to load defaults/i)).not.toBeInTheDocument()
  })

  it('Load defaults: switching template mid-flight still auto-prefills new template on pristine form', async () => {
    // Regression test for the secondary race: when Load defaults is pending
    // for template-a and the user switches to template-b on a pristine form,
    // the stale response for template-a must be dropped AND the new template's
    // defaults must auto-prefill once the pending flag clears.
    let resolveRefetch: ((value: { status: 'success'; data: Defaults | undefined }) => void) | null = null
    const deferred = new Promise<{ status: 'success'; data: Defaults | undefined }>((resolve) => {
      resolveRefetch = resolve
    })

    const defaultsByName: Record<string, Defaults | undefined> = {
      'template-a': {
        name: 'alpha-svc', description: 'Alpha service',
        image: 'ghcr.io/org/alpha', tag: '1.0.0', port: 3000,
        command: ['/alpha'], args: ['--a'],
      },
      'template-b': {
        name: 'beta-svc', description: 'Beta service',
        image: 'ghcr.io/org/beta', tag: '2.0.0', port: 4000,
        command: ['/beta'], args: ['--b'],
      },
    }

    ;(useCreateDeployment as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
      reset: vi.fn(),
    })
    ;(useListTemplates as Mock).mockReturnValue({
      data: [makeTemplate('template-a'), makeTemplate('template-b')],
      isLoading: false,
    })
    ;(useGetTemplateDefaults as Mock).mockImplementation((params: { scope: unknown; name: string }) => {
      const d = params.name ? defaultsByName[params.name] : undefined
      return {
        data: d,
        isFetching: false,
        isSuccess: !!params.name,
        isError: false,
        error: null,
        refetch: async () => {
          if (params.name === 'template-a') {
            return deferred
          }
          return { status: 'success' as const, data: defaultsByName[params.name] }
        },
      }
    })

    render(<CreateDeploymentPage />)

    // Pristine form: select template-a. The synchronous `data` path applies
    // alpha's defaults on render.
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-a' } })
    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/alpha')
    })

    // Click Load defaults (refetch is deferred). Form stays pristine since
    // pre-fill writes do not dirty the form.
    fireEvent.click(screen.getByRole('button', { name: /load defaults/i }))

    // Switch to template-b BEFORE the in-flight refetch resolves. The pristine
    // pre-fill effect sees loadDefaultsPending=true and must bail for now...
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'template-b' } })
    await act(async () => { await Promise.resolve() })

    // ...but once the stale refetch resolves and pending flips false, the
    // pristine effect must re-run and apply template-b's defaults.
    await act(async () => {
      resolveRefetch!({ status: 'success', data: defaultsByName['template-a'] })
      await Promise.resolve()
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/beta')
      expect(screen.getByLabelText(/^tag$/i)).toHaveValue('2.0.0')
      expect(screen.getByLabelText(/^port$/i)).toHaveValue(4000)
      expect(screen.getByLabelText(/display name/i)).toHaveValue('beta-svc')
    })
  })

  it('Load defaults button is disabled when no template is selected', () => {
    setupMocks()
    render(<CreateDeploymentPage />)
    const btn = screen.getByRole('button', { name: /load defaults/i })
    expect(btn).toBeDisabled()
  })

  it('Load defaults button is enabled once a template is selected', async () => {
    setupMocks(vi.fn(), [makeTemplate('web-app')], { 'web-app': { image: 'x', tag: 'y' } })
    render(<CreateDeploymentPage />)
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /load defaults/i })).not.toBeDisabled()
    })
  })

  it('response with port=0 keeps the form\'s prior port (avoids clobbering 8080)', async () => {
    setupMocks(vi.fn(), [makeTemplate('web-app')], {
      // Intentionally omit port (equivalent to proto default 0).
      'web-app': { name: 'web', image: 'ghcr.io/org/web', tag: 'v1' },
    })
    render(<CreateDeploymentPage />)

    expect(screen.getByLabelText(/^port$/i)).toHaveValue(8080)

    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })

    await waitFor(() => {
      expect(screen.getByLabelText(/^image$/i)).toHaveValue('ghcr.io/org/web')
    })
    // Port preserved.
    expect(screen.getByLabelText(/^port$/i)).toHaveValue(8080)
  })

  // --- End ADR 027 pre-fill tests ------------------------------------------

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

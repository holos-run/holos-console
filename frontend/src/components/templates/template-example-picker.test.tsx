import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'

vi.mock('@/queries/templates', () => ({
  useListTemplateExamples: vi.fn(),
}))

import { useListTemplateExamples } from '@/queries/templates'
import { TemplateExamplePicker } from './template-example-picker'

const EXAMPLE_HTTPROUTE = {
  name: 'httproute-v1',
  displayName: 'HTTPRoute Ingress',
  description: 'Provides an HTTPRoute for the org-configured ingress gateway.',
  cueTemplate: '// example CUE\nplatformResources: {}\n',
}

const EXAMPLE_CONFIGMAP = {
  name: 'configmap-v1',
  displayName: 'ConfigMap Starter',
  description: 'A minimal ConfigMap scaffold for project-scope templates.',
  cueTemplate: '// another example\nprojectResources: {}\n',
}

function setHook({
  data,
  isPending = false,
  error = null,
}: {
  data?: typeof EXAMPLE_HTTPROUTE[]
  isPending?: boolean
  error?: Error | null
}) {
  ;(useListTemplateExamples as Mock).mockReturnValue({
    data,
    isPending,
    error,
  })
}

describe('TemplateExamplePicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the trigger button with the default label', () => {
    setHook({ data: [EXAMPLE_HTTPROUTE] })
    render(<TemplateExamplePicker onSelect={vi.fn()} />)
    expect(screen.getByRole('combobox', { name: /load example/i })).toBeInTheDocument()
  })

  it('renders a custom label when provided', () => {
    setHook({ data: [EXAMPLE_HTTPROUTE] })
    render(<TemplateExamplePicker onSelect={vi.fn()} label="Pick Starter" />)
    expect(screen.getByRole('combobox', { name: /pick starter/i })).toBeInTheDocument()
  })

  it('shows a loading indicator while examples are being fetched', () => {
    setHook({ data: undefined, isPending: true })
    render(<TemplateExamplePicker onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))
    expect(screen.getByRole('status')).toHaveTextContent(/loading examples/i)
  })

  it('shows the empty-state message when the server returns no examples', () => {
    setHook({ data: [] })
    render(<TemplateExamplePicker onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))
    expect(screen.getByText(/no examples found/i)).toBeInTheDocument()
  })

  it('shows an error message when the hook reports an error', () => {
    setHook({ data: undefined, error: new Error('boom') })
    render(<TemplateExamplePicker onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))
    expect(screen.getByRole('alert')).toHaveTextContent(/failed to load examples/i)
  })

  it('renders each example with its display name and description', () => {
    setHook({ data: [EXAMPLE_HTTPROUTE, EXAMPLE_CONFIGMAP] })
    render(<TemplateExamplePicker onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))

    expect(screen.getByText(EXAMPLE_HTTPROUTE.displayName)).toBeInTheDocument()
    expect(screen.getByText(EXAMPLE_HTTPROUTE.description)).toBeInTheDocument()
    expect(screen.getByText(EXAMPLE_CONFIGMAP.displayName)).toBeInTheDocument()
    expect(screen.getByText(EXAMPLE_CONFIGMAP.description)).toBeInTheDocument()
  })

  it('invokes onSelect with the full example payload when an item is clicked', async () => {
    const onSelect = vi.fn()
    setHook({ data: [EXAMPLE_HTTPROUTE, EXAMPLE_CONFIGMAP] })
    render(<TemplateExamplePicker onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))

    const item = await screen.findByText(EXAMPLE_HTTPROUTE.displayName)
    fireEvent.click(item)

    await waitFor(() => {
      expect(onSelect).toHaveBeenCalledTimes(1)
    })
    expect(onSelect).toHaveBeenCalledWith(EXAMPLE_HTTPROUTE)
  })

  it('filters the list when the user types in the search input', async () => {
    setHook({ data: [EXAMPLE_HTTPROUTE, EXAMPLE_CONFIGMAP] })
    render(<TemplateExamplePicker onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))

    // Both items are visible initially.
    expect(screen.getByText(EXAMPLE_HTTPROUTE.displayName)).toBeInTheDocument()
    expect(screen.getByText(EXAMPLE_CONFIGMAP.displayName)).toBeInTheDocument()

    const searchInput = screen.getByPlaceholderText(/search examples/i)
    fireEvent.change(searchInput, { target: { value: 'HTTPRoute' } })

    await waitFor(() => {
      expect(screen.getByText(EXAMPLE_HTTPROUTE.displayName)).toBeInTheDocument()
      expect(screen.queryByText(EXAMPLE_CONFIGMAP.displayName)).not.toBeInTheDocument()
    })
  })

  it('matches the search input against descriptions as well as display names', async () => {
    setHook({ data: [EXAMPLE_HTTPROUTE, EXAMPLE_CONFIGMAP] })
    render(<TemplateExamplePicker onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))

    const searchInput = screen.getByPlaceholderText(/search examples/i)
    // "ingress" appears only in the HTTPRoute description.
    fireEvent.change(searchInput, { target: { value: 'ingress' } })

    await waitFor(() => {
      expect(screen.getByText(EXAMPLE_HTTPROUTE.displayName)).toBeInTheDocument()
      expect(screen.queryByText(EXAMPLE_CONFIGMAP.displayName)).not.toBeInTheDocument()
    })
  })

  it('disables the trigger when disabled is true', () => {
    setHook({ data: [EXAMPLE_HTTPROUTE] })
    render(<TemplateExamplePicker onSelect={vi.fn()} disabled />)
    expect(screen.getByRole('combobox', { name: /load example/i })).toBeDisabled()
  })
})

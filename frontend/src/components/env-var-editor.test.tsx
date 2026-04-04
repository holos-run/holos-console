import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/deployments', () => ({
  useListNamespaceSecrets: vi.fn(),
  useListNamespaceConfigMaps: vi.fn(),
}))

vi.mock('@/components/ui/select', () => ({
  Select: ({ value, onValueChange, children }: { value?: string; onValueChange?: (v: string) => void; children: React.ReactNode }) => (
    <select data-testid="mock-select" data-value={value} value={value ?? ''} onChange={(e) => onValueChange?.(e.target.value)}>
      {children}
    </select>
  ),
  SelectTrigger: ({ children, 'aria-label': ariaLabel }: { children: React.ReactNode; 'aria-label'?: string }) => (
    <span aria-label={ariaLabel}>{children}</span>
  ),
  SelectContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: React.ReactNode }) => (
    <option value={value}>{children}</option>
  ),
  SelectValue: ({ placeholder }: { placeholder?: string }) => <span>{placeholder}</span>,
}))

import { useListNamespaceSecrets, useListNamespaceConfigMaps } from '@/queries/deployments'
import { EnvVarEditor } from './env-var-editor'
import type { EnvVar } from '@/gen/holos/console/v1/deployments_pb'

const mockSecrets = [
  { name: 'db-secret', keys: ['password', 'username'] },
  { name: 'api-keys', keys: ['token'] },
]

const mockConfigMaps = [
  { name: 'app-config', keys: ['config.yaml', 'env'] },
]

function setupMocks() {
  ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: mockSecrets, isLoading: false })
  ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: mockConfigMaps, isLoading: false })
}

describe('EnvVarEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders an Add environment variable button', () => {
    render(<EnvVarEditor project="test-project" value={[]} onChange={vi.fn()} />)
    expect(screen.getByRole('button', { name: /add environment variable/i })).toBeInTheDocument()
  })

  it('renders no rows when value is empty', () => {
    render(<EnvVarEditor project="test-project" value={[]} onChange={vi.fn()} />)
    expect(screen.queryByLabelText(/env var name/i)).not.toBeInTheDocument()
  })

  it('adds a new row when Add environment variable is clicked', () => {
    const onChange = vi.fn()
    render(<EnvVarEditor project="test-project" value={[]} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: /add environment variable/i }))
    expect(onChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ name: '', source: { case: 'value', value: '' } }),
      ]),
    )
  })

  it('renders a row for each env var in value', () => {
    const envVars: EnvVar[] = [
      { name: 'DATABASE_URL', source: { case: 'value', value: 'postgres://localhost' } } as unknown as EnvVar,
      { name: 'API_TOKEN', source: { case: 'value', value: 'secret123' } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={vi.fn()} />)
    expect(screen.getAllByLabelText(/env var name/i)).toHaveLength(2)
  })

  it('calls onChange with updated name when name input changes', () => {
    const onChange = vi.fn()
    const envVars: EnvVar[] = [
      { name: 'OLD_NAME', source: { case: 'value', value: '' } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText(/env var name/i), { target: { value: 'NEW_NAME' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ name: 'NEW_NAME' }),
      ]),
    )
  })

  it('calls onChange with updated literal value when value input changes', () => {
    const onChange = vi.fn()
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'value', value: '' } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText(/literal value/i), { target: { value: 'hello' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ source: { case: 'value', value: 'hello' } }),
      ]),
    )
  })

  it('removes a row when the remove button is clicked', () => {
    const onChange = vi.fn()
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'value', value: 'x' } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: /remove env var/i }))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('switches to Secret source when source selector changes to secret', () => {
    const onChange = vi.fn()
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'value', value: '' } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={onChange} />)
    const selects = screen.getAllByTestId('mock-select')
    const sourceSelect = selects.find((s) => s.getAttribute('data-value') === 'value')!
    fireEvent.change(sourceSelect, { target: { value: 'secret' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ source: { case: 'secretKeyRef', value: { name: '', key: '' } } }),
      ]),
    )
  })

  it('switches to ConfigMap source when source selector changes to configmap', () => {
    const onChange = vi.fn()
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'value', value: '' } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={onChange} />)
    const selects = screen.getAllByTestId('mock-select')
    const sourceSelect = selects.find((s) => s.getAttribute('data-value') === 'value')!
    fireEvent.change(sourceSelect, { target: { value: 'configmap' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ source: { case: 'configMapKeyRef', value: { name: '', key: '' } } }),
      ]),
    )
  })

  it('renders secret name and key selects when source is secretKeyRef', () => {
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'secretKeyRef', value: { name: 'db-secret', key: 'password' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={vi.fn()} />)
    // The source type select shows 'secret', the secret name shows 'db-secret', the key shows 'password'
    const selects = screen.getAllByTestId('mock-select')
    const values = selects.map((s) => s.getAttribute('data-value'))
    expect(values).toContain('secret')
    expect(values).toContain('db-secret')
    expect(values).toContain('password')
  })

  it('renders configmap name and key selects when source is configMapKeyRef', () => {
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'configMapKeyRef', value: { name: 'app-config', key: 'config.yaml' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={vi.fn()} />)
    // The source type select shows 'configmap', the configmap name shows 'app-config', the key shows 'config.yaml'
    const selects = screen.getAllByTestId('mock-select')
    const values = selects.map((s) => s.getAttribute('data-value'))
    expect(values).toContain('configmap')
    expect(values).toContain('app-config')
    expect(values).toContain('config.yaml')
  })

  it('updates secret name selection and resets key', () => {
    const onChange = vi.fn()
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'secretKeyRef', value: { name: 'db-secret', key: 'password' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={onChange} />)
    const selects = screen.getAllByTestId('mock-select')
    const secretNameSelect = selects.find((s) => s.getAttribute('data-value') === 'db-secret')!
    fireEvent.change(secretNameSelect, { target: { value: 'api-keys' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ source: { case: 'secretKeyRef', value: { name: 'api-keys', key: '' } } }),
      ]),
    )
  })

  it('updates secret key selection', () => {
    const onChange = vi.fn()
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'secretKeyRef', value: { name: 'db-secret', key: '' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={onChange} />)
    const selects = screen.getAllByTestId('mock-select')
    const keySelect = selects.find((s) => s.getAttribute('data-value') === '')!
    // There may be multiple empty-value selects; find the key select (second empty one after source and secret name)
    const emptySelects = selects.filter((s) => s.getAttribute('data-value') === '' || s.getAttribute('data-value') === 'db-secret')
    // The key select is among the remaining selects after the source select
    // Use the last empty one
    const lastEmpty = [...selects].reverse().find((s) => s.getAttribute('data-value') === '')!
    fireEvent.change(lastEmpty, { target: { value: 'username' } })
    expect(onChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ source: { case: 'secretKeyRef', value: { name: 'db-secret', key: 'username' } } }),
      ]),
    )
  })

  it('populates secret key options from the selected secret', () => {
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'secretKeyRef', value: { name: 'db-secret', key: '' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={vi.fn()} />)
    expect(screen.getByRole('option', { name: 'password' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'username' })).toBeInTheDocument()
  })

  it('populates configmap key options from the selected configmap', () => {
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'configMapKeyRef', value: { name: 'app-config', key: '' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={vi.fn()} />)
    expect(screen.getByRole('option', { name: 'config.yaml' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'env' })).toBeInTheDocument()
  })

  it('renders secret names as options in the secret name select', () => {
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'secretKeyRef', value: { name: '', key: '' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={vi.fn()} />)
    expect(screen.getByRole('option', { name: 'db-secret' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'api-keys' })).toBeInTheDocument()
  })

  it('renders configmap names as options in the configmap name select', () => {
    const envVars: EnvVar[] = [
      { name: 'MY_VAR', source: { case: 'configMapKeyRef', value: { name: '', key: '' } } } as unknown as EnvVar,
    ]
    render(<EnvVarEditor project="test-project" value={envVars} onChange={vi.fn()} />)
    expect(screen.getByRole('option', { name: 'app-config' })).toBeInTheDocument()
  })
})

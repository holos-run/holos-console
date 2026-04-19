import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/templates', () => ({
  useListReleases: vi.fn(),
  useCreateRelease: vi.fn(),
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
  Button: ({ children, onClick, type, disabled, ...rest }: {
    children: React.ReactNode
    onClick?: () => void
    type?: string
    disabled?: boolean
    variant?: string
    size?: string
    className?: string
  }) => (
    <button onClick={onClick} type={type as 'button' | 'submit' | 'reset'} disabled={disabled}>
      {children}
    </button>
  ),
}))

vi.mock('@/components/ui/badge', () => ({
  Badge: ({ children, ...rest }: { children: React.ReactNode; variant?: string; className?: string }) => (
    <span data-testid="badge">{children}</span>
  ),
}))

vi.mock('@/components/ui/separator', () => ({
  Separator: () => <hr />,
}))

vi.mock('@/components/ui/alert', () => ({
  Alert: ({ children }: { children: React.ReactNode }) => <div role="alert">{children}</div>,
  AlertDescription: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}))

vi.mock('lucide-react', () => ({
  Plus: () => <span data-testid="icon-plus" />,
  Tag: () => <span data-testid="icon-tag" />,
}))

import { useListReleases, useCreateRelease } from '@/queries/templates'
import { TemplateReleases } from './template-releases'
import { TemplateScope, namespaceFor } from '@/lib/scope-shim'

const testScope = { scope: TemplateScope.ORGANIZATION, scopeName: 'test-org' } as any

function makeRelease(version: string, changelog: string, upgradeAdvice = '', createdAt?: Date) {
  return {
    templateName: 'my-template',
    namespace: namespaceFor(TemplateScope.ORGANIZATION, 'test-org'),
    version,
    changelog,
    upgradeAdvice,
    cueTemplate: '// cue',
    defaults: undefined,
    createdAt: createdAt ? { seconds: BigInt(Math.floor(createdAt.getTime() / 1000)), nanos: 0 } : undefined,
  }
}

describe('TemplateReleases', () => {
  const mockMutateAsync = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    ;(useListReleases as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })
    ;(useCreateRelease as Mock).mockReturnValue({
      mutateAsync: mockMutateAsync,
      isPending: false,
    })
  })

  it('renders empty state when no releases exist', () => {
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    expect(screen.getByText(/no releases/i)).toBeInTheDocument()
  })

  it('renders release list in descending version order', () => {
    ;(useListReleases as Mock).mockReturnValue({
      data: [
        makeRelease('2.0.0', 'Breaking changes'),
        makeRelease('1.1.0', 'Minor feature'),
        makeRelease('1.0.0', 'Initial release'),
      ],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    const versions = screen.getAllByText(/^\d+\.\d+\.\d+$/)
    expect(versions[0]).toHaveTextContent('2.0.0')
    expect(versions[1]).toHaveTextContent('1.1.0')
    expect(versions[2]).toHaveTextContent('1.0.0')
  })

  it('highlights the latest release', () => {
    ;(useListReleases as Mock).mockReturnValue({
      data: [
        makeRelease('1.1.0', 'Latest feature'),
        makeRelease('1.0.0', 'Initial release'),
      ],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    expect(screen.getByText('Latest')).toBeInTheDocument()
  })

  it('shows truncated changelog text', () => {
    const longChangelog = 'A'.repeat(200)
    ;(useListReleases as Mock).mockReturnValue({
      data: [makeRelease('1.0.0', longChangelog)],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    // The truncated text should be present (first 120 chars + ellipsis)
    const truncated = screen.getByText(new RegExp('^A{20,}'))
    expect(truncated.textContent!.length).toBeLessThan(200)
  })

  it('shows Create Release button when canWrite is true', () => {
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    expect(screen.getByText(/create release/i)).toBeInTheDocument()
  })

  it('hides Create Release button when canWrite is false', () => {
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={false} />
    )
    expect(screen.queryByText(/create release/i)).toBeNull()
  })

  it('opens create dialog with suggested version when button is clicked', () => {
    ;(useListReleases as Mock).mockReturnValue({
      data: [makeRelease('1.2.3', 'Last release')],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    expect(screen.getByTestId('dialog')).toBeInTheDocument()
    // Version field should be pre-filled with suggested next patch version
    const versionInput = screen.getByLabelText(/version/i) as HTMLInputElement
    expect(versionInput.value).toBe('1.2.4')
  })

  it('validates invalid semver format', () => {
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    const versionInput = screen.getByLabelText(/version/i)
    fireEvent.change(versionInput, { target: { value: 'not-semver' } })
    fireEvent.click(screen.getByText(/^publish$/i))
    expect(screen.getByText(/valid semver/i)).toBeInTheDocument()
  })

  it('validates duplicate version', () => {
    ;(useListReleases as Mock).mockReturnValue({
      data: [makeRelease('1.0.0', 'Initial')],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    const versionInput = screen.getByLabelText(/version/i)
    fireEvent.change(versionInput, { target: { value: '1.0.0' } })
    fireEvent.click(screen.getByText(/^publish$/i))
    expect(screen.getByText(/already exists/i)).toBeInTheDocument()
  })

  it('submits create release form successfully', async () => {
    mockMutateAsync.mockResolvedValue({ release: makeRelease('1.0.0', 'First release') })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    const versionInput = screen.getByLabelText(/version/i)
    fireEvent.change(versionInput, { target: { value: '1.0.0' } })
    const changelogInput = screen.getByLabelText(/changelog/i)
    fireEvent.change(changelogInput, { target: { value: 'First release' } })
    fireEvent.click(screen.getByText(/^publish$/i))

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          version: '1.0.0',
          changelog: 'First release',
        })
      )
    })
  })

  it('shows upgrade advice field for major version bumps', () => {
    ;(useListReleases as Mock).mockReturnValue({
      data: [makeRelease('1.2.3', 'Previous')],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    const versionInput = screen.getByLabelText(/version/i)
    fireEvent.change(versionInput, { target: { value: '2.0.0' } })
    // Upgrade advice textarea should now be visible
    expect(screen.getByLabelText(/upgrade advice/i)).toBeInTheDocument()
  })

  it('hides upgrade advice field for non-major version bumps', () => {
    ;(useListReleases as Mock).mockReturnValue({
      data: [makeRelease('1.2.3', 'Previous')],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    // Default suggested version is 1.2.4 (patch), so no upgrade advice
    expect(screen.queryByLabelText(/upgrade advice/i)).toBeNull()
  })

  it('shows error when create release fails', async () => {
    mockMutateAsync.mockRejectedValue(new Error('release creation failed'))
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    const versionInput = screen.getByLabelText(/version/i)
    fireEvent.change(versionInput, { target: { value: '1.0.0' } })
    fireEvent.click(screen.getByText(/^publish$/i))

    await waitFor(() => {
      expect(screen.getByText(/release creation failed/i)).toBeInTheDocument()
    })
  })

  it('suggests version options as radio buttons', () => {
    ;(useListReleases as Mock).mockReturnValue({
      data: [makeRelease('1.2.3', 'Previous')],
      isPending: false,
      error: null,
    })
    render(
      <TemplateReleases scope={testScope} templateName="my-template" canWrite={true} />
    )
    fireEvent.click(screen.getByText(/create release/i))
    // Should show patch, minor, major radio options
    expect(screen.getByLabelText(/1\.2\.4.*patch/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/1\.3\.0.*minor/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/2\.0\.0.*major/i)).toBeInTheDocument()
  })
})

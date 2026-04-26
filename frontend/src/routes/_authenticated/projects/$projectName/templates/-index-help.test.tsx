/**
 * Tests for the Templates index help pane integration (HOL-860, updated HOL-974).
 *
 * Covers:
 *   - ? icon button is rendered in the page header
 *   - Clicking the ? icon opens the help sheet (navigates with help=1)
 *   - URL ?help=1 opens the sheet on initial render
 *   - Esc key closes the sheet (calls navigate to drop help param)
 *   - Copy blocks are present when open
 */

import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

// ---------------------------------------------------------------------------
// Radix pointer-capture polyfills for jsdom
// ---------------------------------------------------------------------------

if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

// ---------------------------------------------------------------------------
// Router mock — useSearch is configurable per test via searchRef
// ---------------------------------------------------------------------------

const searchRef = { current: {} as Record<string, unknown> }
const navigateMock = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
      useSearch: () => searchRef.current,
      fullPath: '/projects/test-project/templates/',
    }),
    Link: ({
      children,
      to,
      className,
    }: {
      children: React.ReactNode
      to?: string
      className?: string
    }) => (
      <a href={to ?? '#'} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => navigateMock,
  }
})

// ---------------------------------------------------------------------------
// Console-config mock — predictable namespace prefixes
// ---------------------------------------------------------------------------

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn().mockReturnValue({
    namespacePrefix: '',
    organizationPrefix: 'org-',
    folderPrefix: 'folder-',
    projectPrefix: 'project-',
  }),
}))

// ---------------------------------------------------------------------------
// Query mocks — refactored to project-scoped hooks (HOL-974)
// ---------------------------------------------------------------------------

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  useDeleteTemplate: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useListTemplates, useDeleteTemplate } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { ProjectTemplatesIndexPage } from './index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

function makeTemplate(name: string, namespace = 'project-test-project') {
  return { name, namespace, displayName: name, description: '', cueTemplate: '', createdAt: '' }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

function setupMocks({
  templates = [makeTemplate('my-template')],
  projectRole = Role.OWNER,
}: {
  templates?: ReturnType<typeof makeTemplate>[]
  projectRole?: number
} = {}) {
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole: projectRole },
    isPending: false,
  })
  ;(useListTemplates as Mock).mockReturnValue({
    data: templates,
    isPending: false,
    error: null,
  })
  ;(useDeleteTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ProjectTemplatesIndexPage — help pane', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    searchRef.current = {}
    navigateMock.mockClear()
  })

  it('renders the ? help icon button', () => {
    setupMocks()
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(
      screen.getByRole('button', { name: /help.*templates overview/i }),
    ).toBeInTheDocument()
  })

  it('clicking the ? icon triggers navigate with help=1', () => {
    setupMocks()
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    const helpBtn = screen.getByRole('button', { name: /help.*templates overview/i })
    fireEvent.click(helpBtn)

    expect(navigateMock).toHaveBeenCalled()
    // Extract the search updater and call it to verify it sets help=1
    const callArg = navigateMock.mock.calls[0][0] as {
      search: (prev: Record<string, unknown>) => Record<string, unknown>
    }
    const result = callArg.search({})
    expect(result.help).toBe('1')
  })

  it('opens the help sheet when URL search has help=1', () => {
    searchRef.current = { help: '1' }
    setupMocks()
    render(<ProjectTemplatesIndexPage projectName="test-project" />)

    // Sheet content should be visible
    expect(screen.getByTestId('help-section-template')).toBeInTheDocument()
    expect(screen.getByTestId('help-section-template-policy')).toBeInTheDocument()
    expect(screen.getByTestId('help-section-template-policy-binding')).toBeInTheDocument()
    expect(screen.getByTestId('help-section-summary')).toBeInTheDocument()
  })

  it('help sheet is closed (no sections) when URL has no help param', () => {
    searchRef.current = {}
    setupMocks()
    render(<ProjectTemplatesIndexPage projectName="test-project" />)

    expect(screen.queryByTestId('help-section-template')).not.toBeInTheDocument()
    expect(screen.queryByTestId('help-section-template-policy')).not.toBeInTheDocument()
    expect(screen.queryByTestId('help-section-template-policy-binding')).not.toBeInTheDocument()
  })

  it('Esc key closes the sheet by calling navigate to drop help param', () => {
    searchRef.current = { help: '1' }
    setupMocks()
    // navigateMock must not throw during Radix's internal event dispatch
    navigateMock.mockImplementation(() => undefined)

    render(<ProjectTemplatesIndexPage projectName="test-project" />)

    // Sheet should be open
    expect(screen.getByTestId('help-section-template')).toBeInTheDocument()

    // Press Esc — Radix Sheet fires onOpenChange(false) on Esc
    fireEvent.keyDown(document.body, { key: 'Escape' })

    // navigate should have been called to remove the help param
    expect(navigateMock).toHaveBeenCalled()
    const callArg = navigateMock.mock.calls[0][0] as {
      search: (prev: Record<string, unknown>) => Record<string, unknown>
    }
    const result = callArg.search({ help: '1', kind: 'Template' })
    expect(result.help).toBeUndefined()
  })

  it('copy blocks contain expected text when open', () => {
    searchRef.current = { help: '1' }
    setupMocks()
    render(<ProjectTemplatesIndexPage projectName="test-project" />)

    const templateSection = screen.getByTestId('help-section-template')
    expect(templateSection.textContent).toMatch(/reusable CUE configuration/i)

    const policySection = screen.getByTestId('help-section-template-policy')
    expect(policySection.textContent).toMatch(/constraint defined at organization/i)

    const bindingSection = screen.getByTestId('help-section-template-policy-binding')
    expect(bindingSection.textContent).toMatch(/enforcement point/i)

    const summarySection = screen.getByTestId('help-section-summary')
    expect(summarySection.textContent).toMatch(/Authors write templates/i)
  })
})

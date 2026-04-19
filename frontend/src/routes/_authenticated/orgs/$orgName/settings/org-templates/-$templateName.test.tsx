import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', templateName: 'platform-base' }),
    }),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/templates', () => ({
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useCloneTemplate: vi.fn(),
  useRenderTemplate: vi.fn(),
  useListReleases: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
  useCreateRelease: vi.fn().mockReturnValue({ mutateAsync: vi.fn(), isPending: false }),
  makeOrgScope: vi.fn().mockReturnValue({ scope: 1, scopeName: 'test-org' }),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// Identity debounce so tests do not have to manage timers.
vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

import { useGetTemplate, useUpdateTemplate, useCloneTemplate, useRenderTemplate } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplateDetailPage } from './$templateName'

const mockTemplate = {
  name: 'platform-base',
  displayName: 'Platform Base',
  description: 'Base platform template',
  cueTemplate: '// cue template',
  enabled: true,
}

function setupMocks(userRole = Role.OWNER, orgGatewayNamespace = '') {
  ;(useGetTemplate as Mock).mockReturnValue({ data: mockTemplate, isPending: false, error: null })
  ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'clone' }), isPending: false })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole, gatewayNamespace: orgGatewayNamespace },
    isPending: false,
    error: null,
  })
  ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: 'apiVersion: v1\n', renderedJson: '' }, error: null, isFetching: false })
}

describe('OrgTemplateDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // HOL-646: the Platform Input preview default mirrors the authoring org's
  // configured gatewayNamespace so the preview matches what the backend
  // injects at render time. Falls back to "istio-ingress" when unset.
  it('Platform Input uses org gatewayNamespace when configured', async () => {
    setupMocks(Role.OWNER, 'custom-gateway-ns')
    const user = userEvent.setup()
    render(<OrgTemplateDetailPage orgName="test-org" templateName="platform-base" />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const platformInput = screen.getByRole('textbox', { name: /platform input/i }) as HTMLTextAreaElement
    expect(platformInput.value).toContain('gatewayNamespace: "custom-gateway-ns"')
    expect(platformInput.value).not.toContain('gatewayNamespace: "istio-ingress"')
  })

  it('Platform Input falls back to istio-ingress when org gatewayNamespace is empty', async () => {
    setupMocks(Role.OWNER, '')
    const user = userEvent.setup()
    render(<OrgTemplateDetailPage orgName="test-org" templateName="platform-base" />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const platformInput = screen.getByRole('textbox', { name: /platform input/i }) as HTMLTextAreaElement
    expect(platformInput.value).toContain('gatewayNamespace: "istio-ingress"')
  })

  // HOL-646 review round 1: org-query failure (e.g. user can read the
  // template via a folder/project grant but cannot read the parent org)
  // must NOT silently substitute the platform default. Omit the field
  // entirely so the preview does not advertise a value that may differ
  // from what the backend actually injects at render time.
  it('Platform Input omits gatewayNamespace when org query is pending', async () => {
    ;(useGetTemplate as Mock).mockReturnValue({ data: mockTemplate, isPending: false, error: null })
    ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
    ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'clone' }), isPending: false })
    ;(useGetOrganization as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
    const user = userEvent.setup()
    render(<OrgTemplateDetailPage orgName="test-org" templateName="platform-base" />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const platformInput = screen.getByRole('textbox', { name: /platform input/i }) as HTMLTextAreaElement
    expect(platformInput.value).not.toContain('gatewayNamespace')
  })

  it('Platform Input omits gatewayNamespace when org query errors', async () => {
    ;(useGetTemplate as Mock).mockReturnValue({ data: mockTemplate, isPending: false, error: null })
    ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
    ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'clone' }), isPending: false })
    ;(useGetOrganization as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('forbidden') })
    ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
    const user = userEvent.setup()
    render(<OrgTemplateDetailPage orgName="test-org" templateName="platform-base" />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const platformInput = screen.getByRole('textbox', { name: /platform input/i }) as HTMLTextAreaElement
    expect(platformInput.value).not.toContain('gatewayNamespace')
  })
})

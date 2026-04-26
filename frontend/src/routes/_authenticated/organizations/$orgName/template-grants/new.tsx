import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplateGrant } from '@/queries/templateGrants'
import { namespaceForOrg } from '@/lib/scope-labels'
import { useGetOrganization } from '@/queries/organizations'
import { ScopePicker } from '@/components/scope-picker/ScopePicker'
import type { Scope } from '@/components/scope-picker/ScopePicker'
import { GrantForm } from '@/components/template-grants/GrantForm'
import { useState } from 'react'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-grants/new',
)({
  component: CreateOrgTemplateGrantRoute,
})

function CreateOrgTemplateGrantRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplateGrantPage orgName={orgName} />
}

export function CreateOrgTemplateGrantPage({
  orgName: propOrgName,
}: {
  orgName?: string
} = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const navigate = useNavigate()
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // ScopePicker controls org vs folder scope. Defaults to 'organization'.
  const [scope, setScope] = useState<Scope>('organization')

  // TemplateGrants are org/folder-scoped.
  const namespace = scope === 'organization' ? namespaceForOrg(orgName) : ''

  const createMutation = useCreateTemplateGrant(namespace)

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link
              to="/organizations/$orgName/settings"
              params={{ orgName }}
              className="hover:underline"
            >
              {orgName}
            </Link>
            {' / '}
            <Link
              to="/organizations/$orgName/template-grants"
              params={{ orgName }}
              className="hover:underline"
            >
              Template Grants
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">Create Template Grant</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <div className="mb-4 flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Scope:</span>
          <ScopePicker value={scope} onChange={setScope} disabled={!canWrite} />
        </div>
        {scope === 'project' ? (
          <p className="text-sm text-muted-foreground">
            TemplateGrants are organization-scoped. Select Organization scope to create one.
          </p>
        ) : (
          <GrantForm
            mode="create"
            namespace={namespace}
            canWrite={canWrite}
            submitLabel="Create"
            pendingLabel="Creating..."
            isPending={createMutation.isPending}
            onSubmit={async (values) => {
              await createMutation.mutateAsync(values)
              await navigate({
                to: '/organizations/$orgName/template-grants/$grantName',
                params: { orgName, grantName: values.name },
              })
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-grants',
                params: { orgName },
              })
            }}
          />
        )}
      </CardContent>
    </Card>
  )
}

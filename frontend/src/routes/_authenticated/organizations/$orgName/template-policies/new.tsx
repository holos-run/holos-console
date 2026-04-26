import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplatePolicy } from '@/queries/templatePolicies'
import { namespaceForOrg, namespaceForProject } from '@/lib/scope-labels'
import { useGetOrganization } from '@/queries/organizations'
import { useProject } from '@/lib/project-context'
import { ScopePicker } from '@/components/scope-picker/ScopePicker'
import type { Scope } from '@/components/scope-picker/ScopePicker'
import { PolicyForm } from '@/components/template-policies/PolicyForm'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-policies/new',
)({
  component: CreateOrgTemplatePolicyRoute,
})

function CreateOrgTemplatePolicyRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplatePolicyPage orgName={orgName} />
}

export function CreateOrgTemplatePolicyPage({
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
  const { selectedProject } = useProject()

  const userRole = org?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too.
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // ScopePicker controls which namespace the policy is created in.
  // Defaults to 'organization' so the existing behaviour is preserved.
  const [scope, setScope] = useState<Scope>('organization')

  const namespace =
    scope === 'project' && selectedProject
      ? namespaceForProject(selectedProject)
      : namespaceForOrg(orgName)

  const createMutation = useCreateTemplatePolicy(namespace)

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link to="/organizations/$orgName/settings" params={{ orgName }} className="hover:underline">
              {orgName}
            </Link>
            {' / '}
            <Link
              to="/organizations/$orgName/template-policies"
              params={{ orgName }}
              className="hover:underline"
            >
              Template Policies
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">Create Template Policy</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <div className="mb-4 flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Scope:</span>
          <ScopePicker value={scope} onChange={setScope} disabled={!canWrite} />
        </div>
        {scope === 'project' && !selectedProject ? (
          <p className="text-sm text-muted-foreground">
            Select a project from the switcher to create a policy in a project namespace.
          </p>
        ) : (
          <PolicyForm
            mode="create"
            scopeType={scope === 'project' ? 'project' : 'organization'}
            namespace={namespace}
            canWrite={canWrite}
            submitLabel="Create"
            pendingLabel="Creating..."
            isPending={createMutation.isPending}
            onSubmit={async (values) => {
              await createMutation.mutateAsync(values)
              await navigate({
                to: '/organizations/$orgName/template-policies/$policyName',
                params: { orgName, policyName: values.name },
              })
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-policies',
                params: { orgName },
              })
            }}
          />
        )}
      </CardContent>
    </Card>
  )
}

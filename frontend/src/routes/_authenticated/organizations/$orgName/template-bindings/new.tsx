import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { namespaceForOrg, namespaceForProject } from '@/lib/scope-labels'
import { useGetOrganization } from '@/queries/organizations'
import { useProject } from '@/lib/project-context'
import { ScopePicker } from '@/components/scope-picker/ScopePicker'
import type { Scope } from '@/components/scope-picker/ScopePicker'
import { BindingForm } from '@/components/template-policy-bindings/BindingForm'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-bindings/new',
)({
  component: CreateOrgTemplateBindingRoute,
})

function CreateOrgTemplateBindingRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplateBindingPage orgName={orgName} />
}

export function CreateOrgTemplateBindingPage({
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
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // ScopePicker controls which namespace the binding is created in.
  // Defaults to 'organization' so the existing behaviour is preserved.
  const [scope, setScope] = useState<Scope>('organization')

  const namespace =
    scope === 'project' && selectedProject
      ? namespaceForProject(selectedProject)
      : namespaceForOrg(orgName)

  const createMutation = useCreateTemplatePolicyBinding(namespace)

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
              to="/organizations/$orgName/template-bindings"
              params={{ orgName }}
              className="hover:underline"
            >
              Template Bindings
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">
            Create Template Binding
          </CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <div className="mb-4 flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Scope:</span>
          <ScopePicker value={scope} onChange={setScope} disabled={!canWrite} />
        </div>
        {scope === 'project' && !selectedProject ? (
          <p className="text-sm text-muted-foreground">
            Select a project from the switcher to create a binding in a project namespace.
          </p>
        ) : (
          <BindingForm
            mode="create"
            scopeType={scope === 'project' ? 'project' : 'organization'}
            namespace={namespace}
            organization={orgName}
            canWrite={canWrite}
            submitLabel="Create"
            pendingLabel="Creating..."
            isPending={createMutation.isPending}
            onSubmit={async (values) => {
              await createMutation.mutateAsync(values)
              await navigate({
                to: '/organizations/$orgName/template-bindings/$bindingName',
                params: { orgName, bindingName: values.name },
              })
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-bindings',
                params: { orgName },
              })
            }}
          />
        )}
      </CardContent>
    </Card>
  )
}

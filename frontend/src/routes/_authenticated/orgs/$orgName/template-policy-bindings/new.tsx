import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { makeOrgScope } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import {
  BindingForm,
  type BindingScope,
} from '@/components/template-policy-bindings/BindingForm'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/template-policy-bindings/new',
)({
  component: CreateOrgTemplatePolicyBindingRoute,
})

function CreateOrgTemplatePolicyBindingRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplatePolicyBindingPage orgName={orgName} />
}

export function CreateOrgTemplatePolicyBindingPage({
  orgName: propOrgName,
  forcedScopeType,
}: {
  orgName?: string
  forcedScopeType?: BindingScope
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
  const scope = makeOrgScope(orgName)
  const createMutation = useCreateTemplatePolicyBinding(scope)
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  const scopeType: BindingScope = forcedScopeType ?? 'organization'

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link
              to="/orgs/$orgName/settings"
              params={{ orgName }}
              className="hover:underline"
            >
              {orgName}
            </Link>
            {' / '}
            <Link
              to="/orgs/$orgName/template-policy-bindings"
              params={{ orgName }}
              className="hover:underline"
            >
              Template Policy Bindings
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">
            Create Template Policy Binding
          </CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <BindingForm
          mode="create"
          scopeType={scopeType}
          scopeRef={scope}
          organization={orgName}
          canWrite={canWrite}
          submitLabel="Create"
          pendingLabel="Creating..."
          isPending={createMutation.isPending}
          onSubmit={async (values) => {
            await createMutation.mutateAsync(values)
            await navigate({
              to: '/orgs/$orgName/template-policy-bindings/$bindingName',
              params: { orgName, bindingName: values.name },
            })
          }}
          onCancel={() => {
            void navigate({
              to: '/orgs/$orgName/template-policy-bindings',
              params: { orgName },
            })
          }}
        />
      </CardContent>
    </Card>
  )
}

import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplatePolicy } from '@/queries/templatePolicies'
import { namespaceForOrg } from '@/lib/scope-labels'
import { useGetOrganization } from '@/queries/organizations'
import { PolicyForm, type PolicyScope } from '@/components/template-policies/PolicyForm'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/template-policies/new',
)({
  component: CreateOrgTemplatePolicyRoute,
})

function CreateOrgTemplatePolicyRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplatePolicyPage orgName={orgName} />
}

export function CreateOrgTemplatePolicyPage({
  orgName: propOrgName,
  forcedScopeType,
}: {
  orgName?: string
  forcedScopeType?: PolicyScope
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
  const namespace = namespaceForOrg(orgName)
  const createMutation = useCreateTemplatePolicy(namespace)
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too.
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  const scopeType: PolicyScope = forcedScopeType ?? 'organization'

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
              {orgName}
            </Link>
            {' / '}
            <Link
              to="/orgs/$orgName/template-policies"
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
        <PolicyForm
          mode="create"
          scopeType={scopeType}
          namespace={namespace}
          canWrite={canWrite}
          submitLabel="Create"
          pendingLabel="Creating..."
          isPending={createMutation.isPending}
          onSubmit={async (values) => {
            await createMutation.mutateAsync(values)
            await navigate({
              to: '/orgs/$orgName/template-policies/$policyName',
              params: { orgName, policyName: values.name },
            })
          }}
          onCancel={() => {
            void navigate({
              to: '/orgs/$orgName/template-policies',
              params: { orgName },
            })
          }}
        />
      </CardContent>
    </Card>
  )
}

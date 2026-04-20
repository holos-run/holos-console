import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplatePolicy } from '@/queries/templatePolicies'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'
import { PolicyForm, type PolicyScope } from '@/components/template-policies/PolicyForm'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/template-policies/new',
)({
  component: CreateFolderTemplatePolicyRoute,
})

function CreateFolderTemplatePolicyRoute() {
  const { folderName } = Route.useParams()
  return <CreateFolderTemplatePolicyPage folderName={folderName} />
}

export function CreateFolderTemplatePolicyPage({
  folderName: propFolderName,
  // forcedScopeType allows tests to assert the form-level scope guard by
  // simulating a contrived path where scope resolves to 'project'.
  forcedScopeType,
}: {
  folderName?: string
  forcedScopeType?: PolicyScope
} = {}) {
  let routeFolderName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeFolderName = Route.useParams().folderName
  } catch {
    routeFolderName = undefined
  }
  const folderName = propFolderName ?? routeFolderName ?? ''

  const navigate = useNavigate()
  const namespace = namespaceForFolder(folderName)
  const createMutation = useCreateTemplatePolicy(namespace)
  const { data: folder } = useGetFolder(folderName)

  const orgName = folder?.organization ?? ''
  const userRole = folder?.userRole ?? Role.VIEWER
  // Template policies grant PERMISSION_TEMPLATE_POLICIES_WRITE to both OWNER
  // and EDITOR roles (see console/rbac/template_policy_cascade.go). The UI
  // gate must mirror that cascade so editors are not misled into a read-only
  // view for operations the backend will allow (HOL-558 review round 1).
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // Scope resolution: this route is mounted under /folders/$folderName so the
  // scope is always 'folder' unless a test contrives an override.
  const scopeType: PolicyScope = forcedScopeType ?? 'folder'

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
              {orgName}
            </Link>
            {' / '}
            <Link to="/orgs/$orgName/folders" params={{ orgName }} className="hover:underline">
              Folders
            </Link>
            {' / '}
            <Link
              to="/folders/$folderName/settings"
              params={{ folderName }}
              className="hover:underline"
            >
              {folderName}
            </Link>
            {' / '}
            <Link
              to="/folders/$folderName/template-policies"
              params={{ folderName }}
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
              to: '/folders/$folderName/template-policies/$policyName',
              params: { folderName, policyName: values.name },
            })
          }}
          onCancel={() => {
            void navigate({
              to: '/folders/$folderName/template-policies',
              params: { folderName },
            })
          }}
        />
      </CardContent>
    </Card>
  )
}

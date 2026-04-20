import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'
import {
  BindingForm,
  type BindingScope,
} from '@/components/template-policy-bindings/BindingForm'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/template-policy-bindings/new',
)({
  component: CreateFolderTemplatePolicyBindingRoute,
})

function CreateFolderTemplatePolicyBindingRoute() {
  const { folderName } = Route.useParams()
  return <CreateFolderTemplatePolicyBindingPage folderName={folderName} />
}

export function CreateFolderTemplatePolicyBindingPage({
  folderName: propFolderName,
  forcedScopeType,
}: {
  folderName?: string
  forcedScopeType?: BindingScope
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
  const createMutation = useCreateTemplatePolicyBinding(namespace)
  const { data: folder } = useGetFolder(folderName)

  const orgName = folder?.organization ?? ''
  const userRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  const scopeType: BindingScope = forcedScopeType ?? 'folder'

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
              to="/orgs/$orgName/folders"
              params={{ orgName }}
              className="hover:underline"
            >
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
              to="/folders/$folderName/template-policy-bindings"
              params={{ folderName }}
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
          namespace={namespace}
          organization={orgName}
          canWrite={canWrite}
          submitLabel="Create"
          pendingLabel="Creating..."
          isPending={createMutation.isPending}
          onSubmit={async (values) => {
            await createMutation.mutateAsync(values)
            await navigate({
              to: '/folders/$folderName/template-policy-bindings/$bindingName',
              params: { folderName, bindingName: values.name },
            })
          }}
          onCancel={() => {
            void navigate({
              to: '/folders/$folderName/template-policy-bindings',
              params: { folderName },
            })
          }}
        />
      </CardContent>
    </Card>
  )
}

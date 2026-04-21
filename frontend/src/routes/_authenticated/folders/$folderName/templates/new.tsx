import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplate } from '@/queries/templates'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'
import { TemplateCreateForm } from '@/components/templates/TemplateCreateForm'

export const Route = createFileRoute('/_authenticated/folders/$folderName/templates/new')({
  component: CreateFolderTemplateRoute,
})

function CreateFolderTemplateRoute() {
  const { folderName } = Route.useParams()
  return <CreateFolderTemplatePage folderName={folderName} />
}

export function CreateFolderTemplatePage({ folderName: propFolderName }: { folderName?: string } = {}) {
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
  const createMutation = useCreateTemplate(namespace)
  const { data: folder } = useGetFolder(folderName)

  const orgName = folder?.organization ?? ''
  const userRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
              {orgName}
            </Link>
            {' / '}
            <Link to="/orgs/$orgName/resources" params={{ orgName }} className="hover:underline">
              Resources
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
              to="/folders/$folderName/templates"
              params={{ folderName }}
              className="hover:underline"
            >
              Platform Templates
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">Create Platform Template</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <TemplateCreateForm
          scopeType="folder"
          namespace={namespace}
          organization={orgName}
          canWrite={canWrite}
          isPending={createMutation.isPending}
          onSubmit={async (values) => {
            await createMutation.mutateAsync(values)
            await navigate({
              to: '/folders/$folderName/templates/$templateName',
              params: { folderName, templateName: values.name },
            })
          }}
          onCancel={() => {
            void navigate({ to: '/folders/$folderName/templates', params: { folderName } })
          }}
        />
      </CardContent>
    </Card>
  )
}

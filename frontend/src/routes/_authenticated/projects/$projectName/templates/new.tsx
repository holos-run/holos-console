import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplate } from '@/queries/templates'
import { namespaceForProject } from '@/lib/scope-labels'
import { useGetProject } from '@/queries/projects'
import { TemplateCreateForm } from '@/components/templates/TemplateCreateForm'

export const Route = createFileRoute('/_authenticated/projects/$projectName/templates/new')({
  component: CreateTemplateRoute,
})

function CreateTemplateRoute() {
  const { projectName } = Route.useParams()
  return <CreateTemplatePage projectName={projectName} />
}

export function CreateTemplatePage({ projectName: propProjectName }: { projectName?: string } = {}) {
  let routeProjectName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeProjectName = Route.useParams().projectName
  } catch {
    routeProjectName = undefined
  }
  const projectName = propProjectName ?? routeProjectName ?? ''

  const navigate = useNavigate()
  const namespace = namespaceForProject(projectName)
  const createMutation = useCreateTemplate(namespace)
  const { data: project } = useGetProject(projectName)

  const userRole = project?.userRole ?? Role.VIEWER
  const canLink = userRole === Role.OWNER

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create Deployment Template</CardTitle>
      </CardHeader>
      <CardContent>
        <TemplateCreateForm
          scopeType="project"
          namespace={namespace}
          organization={project?.organization ?? ''}
          projectName={projectName}
          // Project-scope create is intentionally available to all project roles;
          // only the linking section is OWNER-gated via canLink. Mirrors the pre-HOL-816
          // behavior. Server-side RBAC remains the source of truth on submit.
          canWrite={true}
          canLink={canLink}
          isPending={createMutation.isPending}
          onSubmit={async (values) => {
            await createMutation.mutateAsync(values)
            await navigate({
              to: '/projects/$projectName/templates/$templateName',
              params: { projectName, templateName: values.name },
            })
          }}
          onCancel={() => {
            void navigate({ to: '/projects/$projectName/templates', params: { projectName } })
          }}
        />
      </CardContent>
    </Card>
  )
}

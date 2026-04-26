import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplateDependency } from '@/queries/templateDependencies'
import { namespaceForProject } from '@/lib/scope-labels'
import { useGetOrganization } from '@/queries/organizations'
import { useProject } from '@/lib/project-context'
import { ScopePicker } from '@/components/scope-picker/ScopePicker'
import type { Scope } from '@/components/scope-picker/ScopePicker'
import { DependencyForm } from '@/components/template-dependencies/DependencyForm'
import { useState } from 'react'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-dependencies/new',
)({
  component: CreateOrgTemplateDependencyRoute,
})

function CreateOrgTemplateDependencyRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplateDependencyPage orgName={orgName} />
}

export function CreateOrgTemplateDependencyPage({
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

  // ScopePicker controls which namespace the dependency is created in.
  // Defaults to 'project' if a project is selected, else 'organization'.
  const [scope, setScope] = useState<Scope>(selectedProject ? 'project' : 'organization')

  const namespace =
    scope === 'project' && selectedProject
      ? namespaceForProject(selectedProject)
      : ''

  const createMutation = useCreateTemplateDependency(namespace)

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
              to="/organizations/$orgName/template-dependencies"
              params={{ orgName }}
              className="hover:underline"
            >
              Template Dependencies
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">Create Template Dependency</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <div className="mb-4 flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Scope:</span>
          <ScopePicker value={scope} onChange={setScope} disabled={!canWrite} />
        </div>
        {scope === 'project' && !selectedProject ? (
          <p className="text-sm text-muted-foreground">
            Select a project from the switcher to create a dependency in a project namespace.
          </p>
        ) : (
          <DependencyForm
            mode="create"
            namespace={namespace}
            canWrite={canWrite}
            submitLabel="Create"
            pendingLabel="Creating..."
            isPending={createMutation.isPending}
            onSubmit={async (values) => {
              await createMutation.mutateAsync(values)
              await navigate({
                to: '/organizations/$orgName/template-dependencies/$dependencyName',
                params: { orgName, dependencyName: values.name },
              })
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-dependencies',
                params: { orgName },
              })
            }}
          />
        )}
      </CardContent>
    </Card>
  )
}

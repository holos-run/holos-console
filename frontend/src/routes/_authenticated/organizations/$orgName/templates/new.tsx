import { useMemo } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useCreateTemplate } from '@/queries/templates'
import { useResourcePermissions } from '@/queries/permissions'
import { namespaceForOrg } from '@/lib/scope-labels'
import { TemplateCreateForm } from '@/components/templates/TemplateCreateForm'
import {
  createTemplateResourcePermission,
  hasPermission,
  templateResources,
} from '@/lib/resource-permissions'

export const Route = createFileRoute('/_authenticated/organizations/$orgName/templates/new')({
  component: CreateOrgTemplateRoute,
})

function CreateOrgTemplateRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplatePage orgName={orgName} />
}

export function CreateOrgTemplatePage({ orgName: propOrgName }: { orgName?: string } = {}) {
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
  const createMutation = useCreateTemplate(namespace)

  const createPermission = useMemo(
    () => createTemplateResourcePermission(templateResources.templates, namespace),
    [namespace],
  )
  const permissionsQuery = useResourcePermissions([createPermission])
  const canWrite = hasPermission(permissionsQuery.data, createPermission)

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link to="/organizations/$orgName/settings" params={{ orgName }} className="hover:underline">
              {orgName}
            </Link>
            {' / '}
            <Link to="/organizations/$orgName/templates" params={{ orgName }} className="hover:underline">
              Templates
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">Create Platform Template</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <TemplateCreateForm
          scopeType="organization"
          namespace={namespace}
          organization={orgName}
          canWrite={canWrite}
          isPending={createMutation.isPending}
          onSubmit={async (values) => {
            await createMutation.mutateAsync(values)
            await navigate({
              to: '/organizations/$orgName/templates/$namespace/$name',
              params: { orgName, namespace, name: values.name },
            })
          }}
          onCancel={() => {
            void navigate({ to: '/organizations/$orgName/templates', params: { orgName } })
          }}
        />
      </CardContent>
    </Card>
  )
}

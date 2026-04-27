import { useMemo, useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useCreateTemplateRequirement } from '@/queries/templateRequirements'
import { useResourcePermissions } from '@/queries/permissions'
import { namespaceForOrg } from '@/lib/scope-labels'
import { ScopePicker } from '@/components/scope-picker/ScopePicker'
import type { Scope } from '@/components/scope-picker/ScopePicker'
import { RequirementForm } from '@/components/template-requirements/RequirementForm'
import {
  createTemplateResourcePermission,
  hasPermission,
  templateResources,
} from '@/lib/resource-permissions'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-requirements/new',
)({
  component: CreateOrgTemplateRequirementRoute,
})

function CreateOrgTemplateRequirementRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplateRequirementPage orgName={orgName} />
}

export function CreateOrgTemplateRequirementPage({
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

  // ScopePicker controls org vs folder scope. Defaults to 'organization'.
  const [scope, setScope] = useState<Scope>('organization')

  // TemplateRequirements are org/folder-scoped.
  const namespace = scope === 'organization' ? namespaceForOrg(orgName) : ''

  const createMutation = useCreateTemplateRequirement(namespace)
  const createPermission = useMemo(
    () => createTemplateResourcePermission(templateResources.templateRequirements, namespace),
    [namespace],
  )
  const permissionsQuery = useResourcePermissions([createPermission])
  const canWrite = hasPermission(permissionsQuery.data, createPermission)

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
              to="/organizations/$orgName/template-requirements"
              params={{ orgName }}
              className="hover:underline"
            >
              Template Requirements
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">Create Template Requirement</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <div className="mb-4 flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Scope:</span>
          <ScopePicker value={scope} onChange={setScope} disabled={!canWrite} />
        </div>
        {scope === 'project' ? (
          <p className="text-sm text-muted-foreground">
            TemplateRequirements are organization-scoped. Select Organization scope to create one.
          </p>
        ) : (
          <RequirementForm
            mode="create"
            namespace={namespace}
            canWrite={canWrite}
            submitLabel="Create"
            pendingLabel="Creating..."
            isPending={createMutation.isPending}
            onSubmit={async (values) => {
              await createMutation.mutateAsync(values)
              await navigate({
                to: '/organizations/$orgName/template-requirements/$requirementName',
                params: { orgName, requirementName: values.name },
              })
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-requirements',
                params: { orgName },
              })
            }}
          />
        )}
      </CardContent>
    </Card>
  )
}

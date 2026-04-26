import { useMemo, useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { ConfirmDeleteDialog } from '@/components/ui/confirm-delete-dialog'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import {
  useGetTemplateRequirement,
  useUpdateTemplateRequirement,
  useDeleteTemplateRequirement,
} from '@/queries/templateRequirements'
import { useGetOrganization } from '@/queries/organizations'
import { namespaceForOrg } from '@/lib/scope-labels'
import { RequirementForm } from '@/components/template-requirements/RequirementForm'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-requirements/$requirementName',
)({
  component: OrgTemplateRequirementDetailRoute,
})

function OrgTemplateRequirementDetailRoute() {
  const { orgName, requirementName } = Route.useParams()
  return (
    <OrgTemplateRequirementDetailPage orgName={orgName} requirementName={requirementName} />
  )
}

export function OrgTemplateRequirementDetailPage({
  orgName: propOrgName,
  requirementName: propRequirementName,
  namespaceOverride,
}: {
  orgName?: string
  requirementName?: string
  namespaceOverride?: string
} = {}) {
  let routeParams: { orgName?: string; requirementName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const requirementName = propRequirementName ?? routeParams.requirementName ?? ''

  const navigate = useNavigate()
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  // Delete is OWNER-only.
  const canDelete = userRole === Role.OWNER

  // Namespace resolution: explicit override (for tests) > org namespace.
  const namespace = namespaceOverride ?? namespaceForOrg(orgName)

  const {
    data: requirement,
    isPending,
    error,
  } = useGetTemplateRequirement(namespace, requirementName)

  const updateMutation = useUpdateTemplateRequirement(namespace, requirementName)
  const deleteMutation = useDeleteTemplateRequirement(namespace)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<Error | null>(null)

  const initialValues = useMemo(() => {
    if (!requirement) return undefined
    return {
      name: requirement.name,
      requiresNamespace: requirement.requires?.namespace ?? '',
      requiresName: requirement.requires?.name ?? '',
      cascadeDelete: requirement.cascadeDelete ?? true,
    }
  }, [requirement])

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
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
              {' / '}
              <span>{requirementName}</span>
            </p>
            <CardTitle className="mt-1">{requirementName}</CardTitle>
          </div>
          {canDelete && (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setDeleteOpen(true)}
              aria-label="Delete requirement"
            >
              Delete Requirement
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          <RequirementForm
            mode="edit"
            namespace={namespace}
            canWrite={canWrite}
            initialValues={initialValues}
            lockName
            submitLabel="Save"
            pendingLabel="Saving..."
            isPending={updateMutation.isPending}
            onSubmit={async (values) => {
              await updateMutation.mutateAsync({
                requires: values.requires,
                cascadeDelete: values.cascadeDelete,
              })
              toast.success('Requirement saved')
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-requirements',
                params: { orgName },
              })
            }}
          />
        </CardContent>
      </Card>

      <ConfirmDeleteDialog
        open={deleteOpen}
        onOpenChange={(open) => {
          setDeleteOpen(open)
          if (!open) setDeleteError(null)
        }}
        name={requirementName}
        namespace={namespace}
        isDeleting={deleteMutation.isPending}
        error={deleteError}
        onConfirm={async () => {
          setDeleteError(null)
          try {
            await deleteMutation.mutateAsync({ name: requirementName })
            setDeleteOpen(false)
            await navigate({
              to: '/organizations/$orgName/template-requirements',
              params: { orgName },
            })
            toast.success('Requirement deleted')
          } catch (err) {
            setDeleteError(err instanceof Error ? err : new Error(String(err)))
          }
        }}
      />
    </>
  )
}

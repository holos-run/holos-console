import { useMemo, useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { ConfirmDeleteDialog } from '@/components/ui/confirm-delete-dialog'
import {
  useGetTemplateGrant,
  useUpdateTemplateGrant,
  useDeleteTemplateGrant,
} from '@/queries/templateGrants'
import { useResourcePermissions } from '@/queries/permissions'
import { namespaceForOrg } from '@/lib/scope-labels'
import { GrantForm } from '@/components/template-grants/GrantForm'
import { connectErrorMessage } from '@/lib/connect-toast'
import {
  deleteTemplateResourcePermission,
  hasPermission,
  templateResources,
  updateTemplateResourcePermission,
} from '@/lib/resource-permissions'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-grants/$grantName',
)({
  component: OrgTemplateGrantDetailRoute,
})

function OrgTemplateGrantDetailRoute() {
  const { orgName, grantName } = Route.useParams()
  return <OrgTemplateGrantDetailPage orgName={orgName} grantName={grantName} />
}

export function OrgTemplateGrantDetailPage({
  orgName: propOrgName,
  grantName: propGrantName,
  namespaceOverride,
}: {
  orgName?: string
  grantName?: string
  namespaceOverride?: string
} = {}) {
  let routeParams: { orgName?: string; grantName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const grantName = propGrantName ?? routeParams.grantName ?? ''

  const navigate = useNavigate()

  // Namespace resolution: explicit override (for tests) > org namespace.
  const namespace = namespaceOverride ?? namespaceForOrg(orgName)
  const updatePermission = useMemo(
    () => updateTemplateResourcePermission(
      templateResources.templateGrants,
      namespace,
      grantName,
    ),
    [namespace, grantName],
  )
  const deletePermission = useMemo(
    () => deleteTemplateResourcePermission(
      templateResources.templateGrants,
      namespace,
      grantName,
    ),
    [namespace, grantName],
  )
  const permissionsQuery = useResourcePermissions([updatePermission, deletePermission])
  const canWrite = hasPermission(permissionsQuery.data, updatePermission)
  const canDelete = hasPermission(permissionsQuery.data, deletePermission)

  const {
    data: grant,
    isPending,
    error,
  } = useGetTemplateGrant(namespace, grantName)

  const updateMutation = useUpdateTemplateGrant(namespace, grantName)
  const deleteMutation = useDeleteTemplateGrant(namespace)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<Error | null>(null)

  const initialValues = useMemo(() => {
    if (!grant) return undefined
    return {
      name: grant.name,
      fromNamespace: grant.from[0]?.namespace ?? '',
      toNamespace: grant.to[0]?.namespace ?? '',
      toName: grant.to[0]?.name ?? '',
    }
  }, [grant])

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
                to="/organizations/$orgName/template-grants"
                params={{ orgName }}
                className="hover:underline"
              >
                Template Grants
              </Link>
              {' / '}
              <span>{grantName}</span>
            </p>
            <CardTitle className="mt-1">{grantName}</CardTitle>
          </div>
          {canDelete && (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setDeleteOpen(true)}
              aria-label="Delete grant"
            >
              Delete Grant
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          <GrantForm
            mode="edit"
            namespace={namespace}
            canWrite={canWrite}
            initialValues={initialValues}
            lockName
            submitLabel="Save"
            pendingLabel="Saving..."
            isPending={updateMutation.isPending}
            onSubmit={async (values) => {
              try {
                await updateMutation.mutateAsync({
                  from: values.from,
                  to: values.to,
                })
                toast.success('Grant saved')
              } catch (err) {
                toast.error(connectErrorMessage(err))
              }
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-grants',
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
        name={grantName}
        namespace={namespace}
        isDeleting={deleteMutation.isPending}
        error={deleteError}
        onConfirm={async () => {
          setDeleteError(null)
          try {
            await deleteMutation.mutateAsync({ name: grantName })
            setDeleteOpen(false)
            await navigate({
              to: '/organizations/$orgName/template-grants',
              params: { orgName },
            })
            toast.success('Grant deleted')
          } catch (err) {
            const message = connectErrorMessage(err)
            setDeleteError(new Error(message))
            toast.error(message)
          }
        }}
      />
    </>
  )
}

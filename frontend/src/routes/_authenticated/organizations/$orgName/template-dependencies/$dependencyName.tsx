import { useMemo, useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { z } from 'zod'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { ConfirmDeleteDialog } from '@/components/ui/confirm-delete-dialog'
import {
  useGetTemplateDependency,
  useUpdateTemplateDependency,
  useDeleteTemplateDependency,
} from '@/queries/templateDependencies'
import { useResourcePermissions } from '@/queries/permissions'
import { useProject } from '@/lib/project-context'
import { namespaceForProject } from '@/lib/scope-labels'
import { DependencyForm } from '@/components/template-dependencies/DependencyForm'
import { connectErrorMessage } from '@/lib/connect-toast'
import {
  deleteTemplateResourcePermission,
  hasPermission,
  templateResources,
  updateTemplateResourcePermission,
} from '@/lib/resource-permissions'

// The detail route accepts an optional `namespace` search param so the
// project-scoped index can link directly to a dependency in a known namespace.
// When absent, the namespace falls back to the currently-selected project.
const searchSchema = z.object({
  namespace: z.string().optional(),
})

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-dependencies/$dependencyName',
)({
  validateSearch: searchSchema,
  component: OrgTemplateDependencyDetailRoute,
})

function OrgTemplateDependencyDetailRoute() {
  const { orgName, dependencyName } = Route.useParams()
  return (
    <OrgTemplateDependencyDetailPage orgName={orgName} dependencyName={dependencyName} />
  )
}

export function OrgTemplateDependencyDetailPage({
  orgName: propOrgName,
  dependencyName: propDependencyName,
  namespaceOverride,
}: {
  orgName?: string
  dependencyName?: string
  namespaceOverride?: string
} = {}) {
  let routeParams: { orgName?: string; dependencyName?: string } = {}
  let searchNamespace: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const search = Route.useSearch()
    searchNamespace = search.namespace
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const dependencyName = propDependencyName ?? routeParams.dependencyName ?? ''

  const navigate = useNavigate()
  const { selectedProject } = useProject()

  // Namespace resolution: explicit override (for tests) > search param > selected project.
  const namespace =
    namespaceOverride ??
    searchNamespace ??
    (selectedProject ? namespaceForProject(selectedProject) : '')
  const updatePermission = useMemo(
    () => updateTemplateResourcePermission(
      templateResources.templateDependencies,
      namespace,
      dependencyName,
    ),
    [namespace, dependencyName],
  )
  const deletePermission = useMemo(
    () => deleteTemplateResourcePermission(
      templateResources.templateDependencies,
      namespace,
      dependencyName,
    ),
    [namespace, dependencyName],
  )
  const permissionsQuery = useResourcePermissions([updatePermission, deletePermission])
  const canWrite = hasPermission(permissionsQuery.data, updatePermission)
  const canDelete = hasPermission(permissionsQuery.data, deletePermission)

  const {
    data: dependency,
    isPending,
    error,
  } = useGetTemplateDependency(namespace, dependencyName)

  const updateMutation = useUpdateTemplateDependency(namespace, dependencyName)
  const deleteMutation = useDeleteTemplateDependency(namespace)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<Error | null>(null)

  const initialValues = useMemo(() => {
    if (!dependency) return undefined
    return {
      name: dependency.name,
      dependentNamespace: dependency.dependent?.namespace ?? namespace,
      dependentName: dependency.dependent?.name ?? '',
      requiresNamespace: dependency.requires?.namespace ?? '',
      requiresName: dependency.requires?.name ?? '',
      cascadeDelete: dependency.cascadeDelete ?? true,
    }
  }, [dependency, namespace])

  if (!namespace) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>
              Unable to determine the dependency namespace. Select a project from the switcher or
              navigate here from the dependencies list.
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

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
                to="/organizations/$orgName/template-dependencies"
                params={{ orgName }}
                className="hover:underline"
              >
                Template Dependencies
              </Link>
              {' / '}
              <span>{dependencyName}</span>
            </p>
            <CardTitle className="mt-1">{dependencyName}</CardTitle>
          </div>
          {canDelete && (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setDeleteOpen(true)}
              aria-label="Delete dependency"
            >
              Delete Dependency
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          <DependencyForm
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
                  dependent: values.dependent,
                  requires: values.requires,
                  cascadeDelete: values.cascadeDelete,
                })
                toast.success('Dependency saved')
              } catch (err) {
                toast.error(connectErrorMessage(err))
              }
            }}
            onCancel={() => {
              void navigate({
                to: '/organizations/$orgName/template-dependencies',
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
        name={dependencyName}
        namespace={namespace}
        isDeleting={deleteMutation.isPending}
        error={deleteError}
        onConfirm={async () => {
          setDeleteError(null)
          try {
            await deleteMutation.mutateAsync({ name: dependencyName })
            setDeleteOpen(false)
            await navigate({
              to: '/organizations/$orgName/template-dependencies',
              params: { orgName },
            })
            toast.success('Dependency deleted')
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

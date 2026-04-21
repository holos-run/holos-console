import { useMemo, useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import {
  useGetTemplatePolicyBinding,
  useUpdateTemplatePolicyBinding,
  useDeleteTemplatePolicyBinding,
} from '@/queries/templatePolicyBindings'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'
import {
  BindingForm,
  type BindingScope,
} from '@/components/template-policy-bindings/BindingForm'
import { bindingProtoToDraft } from '@/components/template-policy-bindings/binding-draft'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/template-policy-bindings/$bindingName',
)({
  component: FolderTemplatePolicyBindingDetailRoute,
})

function FolderTemplatePolicyBindingDetailRoute() {
  const { folderName, bindingName } = Route.useParams()
  return (
    <FolderTemplatePolicyBindingDetailPage
      folderName={folderName}
      bindingName={bindingName}
    />
  )
}

export function FolderTemplatePolicyBindingDetailPage({
  folderName: propFolderName,
  bindingName: propBindingName,
  forcedScopeType,
}: {
  folderName?: string
  bindingName?: string
  forcedScopeType?: BindingScope
} = {}) {
  let routeParams: { folderName?: string; bindingName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const folderName = propFolderName ?? routeParams.folderName ?? ''
  const bindingName = propBindingName ?? routeParams.bindingName ?? ''

  const navigate = useNavigate()
  const namespace = namespaceForFolder(folderName)
  const { data: folder } = useGetFolder(folderName)
  const orgName = folder?.organization ?? ''
  const userRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const scopeType: BindingScope = forcedScopeType ?? 'folder'

  const {
    data: binding,
    isPending,
    error,
  } = useGetTemplatePolicyBinding(namespace, bindingName)
  const updateMutation = useUpdateTemplatePolicyBinding(namespace, bindingName)
  const deleteMutation = useDeleteTemplatePolicyBinding(namespace)

  const [deleteOpen, setDeleteOpen] = useState(false)

  const initialValues = useMemo(() => {
    if (!binding) return undefined
    return bindingProtoToDraft(binding)
  }, [binding])

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
                to="/orgs/$orgName/settings"
                params={{ orgName }}
                className="hover:underline"
              >
                {orgName}
              </Link>
              {' / '}
              <Link
                to="/orgs/$orgName/resources"
                params={{ orgName }}
                className="hover:underline"
              >
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
                to="/folders/$folderName/template-policy-bindings"
                params={{ folderName }}
                className="hover:underline"
              >
                Template Policy Bindings
              </Link>
              {' / '}
              <span>{bindingName}</span>
            </p>
            <CardTitle className="mt-1">
              {binding?.displayName || bindingName}
            </CardTitle>
          </div>
          {canDelete && (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setDeleteOpen(true)}
              aria-label="Delete binding"
            >
              Delete Binding
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          <BindingForm
            mode="edit"
            scopeType={scopeType}
            namespace={namespace}
            organization={orgName}
            canWrite={canWrite}
            initialValues={initialValues}
            lockName
            submitLabel="Save"
            pendingLabel="Saving..."
            isPending={updateMutation.isPending}
            onSubmit={async (values) => {
              await updateMutation.mutateAsync(values)
              toast.success('Binding saved')
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

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Template Policy Binding</DialogTitle>
            <DialogDescription>
              This will permanently delete the binding &quot;{bindingName}
              &quot;. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={deleteMutation.isPending}
              onClick={async () => {
                try {
                  await deleteMutation.mutateAsync({ name: bindingName })
                  setDeleteOpen(false)
                  await navigate({
                    to: '/folders/$folderName/template-policy-bindings',
                    params: { folderName },
                  })
                  toast.success('Binding deleted')
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : String(err))
                }
              }}
            >
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

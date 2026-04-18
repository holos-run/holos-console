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
import { makeOrgScope } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import {
  BindingForm,
  type BindingScope,
} from '@/components/template-policy-bindings/BindingForm'
import { bindingProtoToDraft } from '@/components/template-policy-bindings/binding-draft'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/template-policy-bindings/$bindingName',
)({
  component: OrgTemplatePolicyBindingDetailRoute,
})

function OrgTemplatePolicyBindingDetailRoute() {
  const { orgName, bindingName } = Route.useParams()
  return (
    <OrgTemplatePolicyBindingDetailPage
      orgName={orgName}
      bindingName={bindingName}
    />
  )
}

export function OrgTemplatePolicyBindingDetailPage({
  orgName: propOrgName,
  bindingName: propBindingName,
  forcedScopeType,
}: {
  orgName?: string
  bindingName?: string
  forcedScopeType?: BindingScope
} = {}) {
  let routeParams: { orgName?: string; bindingName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const bindingName = propBindingName ?? routeParams.bindingName ?? ''

  const navigate = useNavigate()
  const scope = makeOrgScope(orgName)
  const { data: org } = useGetOrganization(orgName)
  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  // PERMISSION_TEMPLATE_POLICIES_DELETE is OWNER-only in the RBAC cascade;
  // bindings reuse the policy permission family so the same constraint
  // applies.
  const canDelete = userRole === Role.OWNER

  const scopeType: BindingScope = forcedScopeType ?? 'organization'

  const {
    data: binding,
    isPending,
    error,
  } = useGetTemplatePolicyBinding(scope, bindingName)
  const updateMutation = useUpdateTemplatePolicyBinding(scope, bindingName)
  const deleteMutation = useDeleteTemplatePolicyBinding(scope)

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
                to="/orgs/$orgName/template-policy-bindings"
                params={{ orgName }}
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
            scopeRef={scope}
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
                to: '/orgs/$orgName/template-policy-bindings',
                params: { orgName },
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
                    to: '/orgs/$orgName/template-policy-bindings',
                    params: { orgName },
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

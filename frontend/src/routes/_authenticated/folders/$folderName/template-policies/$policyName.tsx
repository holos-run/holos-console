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
  useGetTemplatePolicy,
  useUpdateTemplatePolicy,
  useDeleteTemplatePolicy,
} from '@/queries/templatePolicies'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'
import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import {
  PolicyForm,
  type PolicyScope,
} from '@/components/template-policies/PolicyForm'
import { ruleProtoToDraft } from '@/components/template-policies/rule-draft'
import { PolicyBindingsSection } from '@/components/template-policies/PolicyBindingsSection'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/template-policies/$policyName',
)({
  component: FolderTemplatePolicyDetailRoute,
})

function FolderTemplatePolicyDetailRoute() {
  const { folderName, policyName } = Route.useParams()
  return (
    <FolderTemplatePolicyDetailPage folderName={folderName} policyName={policyName} />
  )
}

export function FolderTemplatePolicyDetailPage({
  folderName: propFolderName,
  policyName: propPolicyName,
  forcedScopeType,
}: {
  folderName?: string
  policyName?: string
  forcedScopeType?: PolicyScope
} = {}) {
  let routeParams: { folderName?: string; policyName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const folderName = propFolderName ?? routeParams.folderName ?? ''
  const policyName = propPolicyName ?? routeParams.policyName ?? ''

  const navigate = useNavigate()
  const namespace = namespaceForFolder(folderName)
  const { data: folder } = useGetFolder(folderName)
  const orgName = folder?.organization ?? ''
  const userRole = folder?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too.
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  // PERMISSION_TEMPLATE_POLICIES_DELETE is OWNER-only in the RBAC cascade
  // table, so editors must not see the destructive control.
  const canDelete = userRole === Role.OWNER

  const scopeType: PolicyScope = forcedScopeType ?? 'folder'

  const {
    data: policy,
    isPending,
    error,
  } = useGetTemplatePolicy(namespace, policyName)
  const updateMutation = useUpdateTemplatePolicy(namespace, policyName)
  const deleteMutation = useDeleteTemplatePolicy(namespace)
  // HOL-598: list folder-scope bindings and filter to those referencing this
  // policy by name. Folder-scope bindings live in the same namespace as the
  // folder-scope policy itself; binding detail links target the folder route.
  const bindingsQuery = useListTemplatePolicyBindings(namespace)

  const [deleteOpen, setDeleteOpen] = useState(false)

  const initialValues = useMemo(() => {
    if (!policy) return undefined
    return {
      name: policy.name,
      displayName: policy.displayName ?? '',
      description: policy.description ?? '',
      rules: (policy.rules ?? []).map(ruleProtoToDraft),
    }
  }, [policy])

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
              <Link to="/organizations/$orgName/settings" params={{ orgName }} className="hover:underline">
                {orgName}
              </Link>
              {' / '}
              <Link to="/organizations/$orgName/projects" params={{ orgName }} className="hover:underline">
                Projects
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
                to="/folders/$folderName/template-policies"
                params={{ folderName }}
                className="hover:underline"
              >
                Template Policies
              </Link>
              {' / '}
              <span>{policyName}</span>
            </p>
            <CardTitle className="mt-1">{policy?.displayName || policyName}</CardTitle>
          </div>
          {canDelete && (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setDeleteOpen(true)}
              aria-label="Delete policy"
            >
              Delete Policy
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          <PolicyForm
            mode="edit"
            scopeType={scopeType}
            namespace={namespace}
            canWrite={canWrite}
            initialValues={initialValues}
            lockName
            submitLabel="Save"
            pendingLabel="Saving..."
            isPending={updateMutation.isPending}
            onSubmit={async (values) => {
              await updateMutation.mutateAsync(values)
              toast.success('Policy saved')
            }}
            onCancel={() => {
              void navigate({
                to: '/folders/$folderName/template-policies',
                params: { folderName },
              })
            }}
          />
          <Separator className="my-6" />
          <PolicyBindingsSection
            scopeType="folder"
            folderName={folderName}
            policyName={policyName}
            bindings={bindingsQuery.data ?? []}
            isPending={bindingsQuery.isPending}
            error={bindingsQuery.error}
          />
        </CardContent>
      </Card>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Template Policy</DialogTitle>
            <DialogDescription>
              This will permanently delete the policy &quot;{policyName}&quot;. This action cannot
              be undone.
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
                  await deleteMutation.mutateAsync({ name: policyName })
                  setDeleteOpen(false)
                  await navigate({
                    to: '/folders/$folderName/template-policies',
                    params: { folderName },
                  })
                  toast.success('Policy deleted')
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

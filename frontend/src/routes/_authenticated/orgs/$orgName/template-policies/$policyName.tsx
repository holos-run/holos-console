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
import { makeOrgScope } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import {
  PolicyForm,
  type PolicyScope,
} from '@/components/template-policies/PolicyForm'
import { ruleProtoToDraft } from '@/components/template-policies/rule-draft'
import { PolicyBindingsSection } from '@/components/template-policies/PolicyBindingsSection'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/template-policies/$policyName',
)({
  component: OrgTemplatePolicyDetailRoute,
})

function OrgTemplatePolicyDetailRoute() {
  const { orgName, policyName } = Route.useParams()
  return <OrgTemplatePolicyDetailPage orgName={orgName} policyName={policyName} />
}

export function OrgTemplatePolicyDetailPage({
  orgName: propOrgName,
  policyName: propPolicyName,
  forcedScopeType,
}: {
  orgName?: string
  policyName?: string
  forcedScopeType?: PolicyScope
} = {}) {
  let routeParams: { orgName?: string; policyName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const policyName = propPolicyName ?? routeParams.policyName ?? ''

  const navigate = useNavigate()
  const scope = makeOrgScope(orgName)
  const { data: org } = useGetOrganization(orgName)
  const userRole = org?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too.
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  // PERMISSION_TEMPLATE_POLICIES_DELETE is OWNER-only in the RBAC cascade
  // table, so editors must not see the destructive control.
  const canDelete = userRole === Role.OWNER

  const scopeType: PolicyScope = forcedScopeType ?? 'organization'

  const {
    data: policy,
    isPending,
    error,
  } = useGetTemplatePolicy(scope, policyName)
  const updateMutation = useUpdateTemplatePolicy(scope, policyName)
  const deleteMutation = useDeleteTemplatePolicy(scope)
  // HOL-598: surface TemplatePolicyBindings that reference this policy. The
  // list RPC returns every binding at the org scope; the section filters to
  // ones whose `policyRef.name` matches the current policy.
  const bindingsQuery = useListTemplatePolicyBindings(scope)

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
              <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
                {orgName}
              </Link>
              {' / '}
              <Link
                to="/orgs/$orgName/template-policies"
                params={{ orgName }}
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
            scopeRef={scope}
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
                to: '/orgs/$orgName/template-policies',
                params: { orgName },
              })
            }}
          />
          <Separator className="my-6" />
          <PolicyBindingsSection
            scopeType="organization"
            orgName={orgName}
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
                    to: '/orgs/$orgName/template-policies',
                    params: { orgName },
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

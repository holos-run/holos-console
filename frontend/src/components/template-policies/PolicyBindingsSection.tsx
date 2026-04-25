import { Link } from '@tanstack/react-router'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import type { TemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { namespaceForOrg, namespaceForFolder } from '@/lib/scope-labels'

/**
 * PolicyBindingsSection surfaces the TemplatePolicyBindings that attach the
 * current policy to specific render targets.
 *
 * Per HOL-598, attachment is expressed exclusively through TemplatePolicyBinding
 * (the glob Target fields on each rule were removed from the editor). The
 * section calls `useListTemplatePolicyBindings(scope)` at the policy's own
 * scope and filters client-side by the full `(scope, scope_name, name)`
 * triple on `policyRef`. Matching on the name alone would conflate a
 * folder-scope and an organization-scope policy that happen to share a
 * slug; the resolver's own policy key uses the same triple, so the UI
 * mirrors it to avoid pointing an operator at the wrong binding.
 */
export type PolicyBindingsSectionProps =
  | {
      scopeType: 'organization'
      orgName: string
      policyName: string
      bindings: TemplatePolicyBinding[]
      isPending: boolean
      error: Error | null
    }
  | {
      scopeType: 'folder'
      folderName: string
      policyName: string
      bindings: TemplatePolicyBinding[]
      isPending: boolean
      error: Error | null
    }

export function PolicyBindingsSection(props: PolicyBindingsSectionProps) {
  const { policyName, bindings, isPending, error } = props

  const expectedNamespace =
    props.scopeType === 'organization'
      ? namespaceForOrg(props.orgName)
      : namespaceForFolder(props.folderName)

  const matched = bindings.filter((b) => {
    if (b.policyRef?.name !== policyName) return false
    return b.policyRef?.namespace === expectedNamespace
  })

  return (
    <section className="space-y-3" aria-labelledby="policy-bindings-heading">
      <div>
        <h3
          id="policy-bindings-heading"
          className="text-base font-semibold"
        >
          Bindings
        </h3>
        <p className="text-xs text-muted-foreground mt-1">
          TemplatePolicyBindings attach this policy to specific project
          templates and deployments. Create a binding to apply the policy.
        </p>
      </div>

      {isPending ? (
        <div className="space-y-2">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
        </div>
      ) : error ? (
        <Alert variant="destructive" data-testid="policy-bindings-error">
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      ) : matched.length === 0 ? (
        <div
          data-testid="policy-bindings-empty"
          className="rounded-md border border-dashed border-border p-4 text-sm text-muted-foreground"
        >
          No bindings reference this policy yet. Create a binding from the
          Template Policy Bindings page to attach it to specific targets.
        </div>
      ) : (
        <ul className="space-y-2" data-testid="policy-bindings-list">
          {matched.map((binding) => (
            <li key={binding.name}>
              {props.scopeType === 'organization' ? (
                <Link
                  to="/organizations/$orgName/template-bindings/$bindingName"
                  params={{
                    orgName: props.orgName,
                    bindingName: binding.name,
                  }}
                  className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors border border-border"
                >
                  <BindingRow binding={binding} />
                </Link>
              ) : (
                <Link
                  to="/folders/$folderName/template-policy-bindings/$bindingName"
                  params={{
                    folderName: props.folderName,
                    bindingName: binding.name,
                  }}
                  className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors border border-border"
                >
                  <BindingRow binding={binding} />
                </Link>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function BindingRow({ binding }: { binding: TemplatePolicyBinding }) {
  const targetCount = binding.targetRefs?.length ?? 0
  return (
    <div className="flex-1 min-w-0">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-sm font-medium font-mono">{binding.name}</span>
        <Badge variant="outline" className="text-xs">
          {targetCount} target{targetCount === 1 ? '' : 's'}
        </Badge>
      </div>
      {binding.displayName && binding.displayName !== binding.name && (
        <p className="text-xs text-muted-foreground truncate mt-0.5">
          {binding.displayName}
        </p>
      )}
      {binding.description && (
        <p className="text-xs text-muted-foreground truncate mt-0.5">
          {binding.description}
        </p>
      )}
    </div>
  )
}

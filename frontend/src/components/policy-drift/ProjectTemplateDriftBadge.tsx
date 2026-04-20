import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { PolicyDriftBadge } from './PolicySection'
import { useGetProjectTemplatePolicyState } from '@/queries/templates'

// ProjectTemplateDriftBadge renders the PolicyDriftBadge on a project
// template list row when the backend reports drift for the template.
//
// Unlike deployments, project-scope templates do not carry a
// ProjectTemplateStatusSummary surface (see the HOL-567 scope decision
// recorded in templates.proto): the per-row drift signal is fetched via
// GetProjectTemplatePolicyState instead. The response's PolicyState is
// sourced from the folder-namespace render-state store — never read drift
// state from project-namespace resources directly.
//
// Rendering is strictly conditional on `state.drift === true` so the list
// view stays visually clean for templates that are in sync or have never
// been applied through the HOL-567 path. The component renders nothing
// while the RPC is pending or errors.
export function ProjectTemplateDriftBadge({
  namespace,
  templateName,
}: {
  namespace: string
  templateName: string
}) {
  const { data: state } = useGetProjectTemplatePolicyState(namespace, templateName)
  if (!state?.drift) return null
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span>
            <PolicyDriftBadge />
          </span>
        </TooltipTrigger>
        <TooltipContent>
          The template was rendered before a template policy changed; click Reconcile to re-render.
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

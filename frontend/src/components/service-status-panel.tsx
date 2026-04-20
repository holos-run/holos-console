import { Check, HelpCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

type Status = 'ok' | 'unknown'

// Placeholder service status. Only the Deployment Service is a true signal
// (green if the page loaded — we're talking to the backend). Database and
// Identity Provider are dependency placeholders until the observability
// integration lands (HOL-609). Unknown rows render with a muted HelpCircle
// icon so a user scanning the panel at a glance is never misled into
// thinking an unmeasured dependency has been confirmed healthy.
const DEPENDENCIES: { name: string; tooltip: string }[] = [
  {
    name: 'Database',
    tooltip:
      'Planned: health will come from the deployment service readiness probe plus a shallow database ping, reported via a new ObservabilityService RPC.',
  },
  {
    name: 'Identity Provider',
    tooltip:
      'Planned: OIDC discovery + token refresh will be sampled by the deployment service and reported via the same ObservabilityService RPC.',
  },
]

function StatusIcon({ status }: { status: Status }) {
  if (status === 'ok') {
    return (
      <Check
        className="h-4 w-4 text-green-600 dark:text-green-400"
        aria-hidden="true"
      />
    )
  }
  return (
    <HelpCircle
      className="h-4 w-4 text-muted-foreground"
      aria-hidden="true"
    />
  )
}

function StatusRow({
  name,
  status,
  tooltip,
}: {
  name: string
  status: Status
  tooltip?: string
}) {
  const statusWord = status === 'ok' ? 'OK' : 'not yet reported'
  const accessibleLabel = `${name}: ${statusWord}`

  const labelNode = tooltip ? (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          tabIndex={0}
          role="button"
          className="cursor-help border-b border-dotted border-muted-foreground/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded"
        >
          {name}
        </span>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  ) : (
    <span>{name}</span>
  )

  return (
    <li
      className="flex items-center gap-2 py-1"
      aria-label={accessibleLabel}
    >
      <StatusIcon status={status} />
      {labelNode}
    </li>
  )
}

export function ServiceStatusPanel() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Service Status</CardTitle>
      </CardHeader>
      <CardContent>
        <TooltipProvider>
          <ul className="text-sm">
            <StatusRow name="Deployment Service" status="ok" />
            {DEPENDENCIES.map((dep) => (
              <StatusRow
                key={dep.name}
                name={dep.name}
                status="unknown"
                tooltip={dep.tooltip}
              />
            ))}
          </ul>
        </TooltipProvider>
      </CardContent>
    </Card>
  )
}

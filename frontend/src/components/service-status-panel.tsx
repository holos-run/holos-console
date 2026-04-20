import { Check } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

// Placeholder service status. Only the Deployment Service is a true signal
// (green if the page loaded — we're talking to the backend). Database and
// Identity Provider are dependency placeholders until the observability
// integration lands (HOL-609).
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

function StatusRow({ name, tooltip }: { name: string; tooltip?: string }) {
  const label = tooltip ? (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="cursor-help border-b border-dotted border-muted-foreground/50">
            {name}
          </span>
        </TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    <span>{name}</span>
  )

  return (
    <li className="flex items-center gap-2 py-1">
      <Check
        className="h-4 w-4 text-green-600 dark:text-green-400"
        aria-hidden="true"
      />
      <span className="sr-only">ok</span>
      {label}
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
        <ul className="text-sm">
          <StatusRow name="Deployment Service" />
          {DEPENDENCIES.map((dep) => (
            <StatusRow key={dep.name} name={dep.name} tooltip={dep.tooltip} />
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

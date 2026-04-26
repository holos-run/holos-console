// ReverseDependents.tsx — "who depends on me?" component for Template and
// Deployment detail pages (HOL-987 / HOL-986).
//
// Accepts a (kind, namespace, name) triple and dispatches to the correct
// reverse-dependency hook. Renders a table of dependents with scope badges
// derived from the API response.

import React from 'react'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  useListTemplateDependents,
  useListDeploymentDependents,
  DependencyScope,
} from '@/queries/templateDependencies'
import type {
  TemplateDependentRecord,
  DeploymentDependentRecord,
} from '@/queries/templateDependencies'
import { scopeNameFromNamespace } from '@/lib/scope-labels'

// Props accepted by the top-level ReverseDependents component.
export interface ReverseDependentsProps {
  /** 'Template' uses ListTemplateDependents; 'Deployment' uses ListDeploymentDependents. */
  kind: 'Template' | 'Deployment'
  /** Kubernetes namespace of the queried Template or Deployment. */
  namespace: string
  /** Name (DNS label slug) of the queried Template or Deployment. */
  name: string
  /** When false the RPC is skipped and nothing is rendered. Defaults to true. */
  enabled?: boolean
}

// DependencyScopeBadge renders the ADR-032 scope label for a
// TemplateDependentRecord.scope value.
function DependencyScopeBadge({ scope }: { scope: DependencyScope }) {
  switch (scope) {
    case DependencyScope.INSTANCE:
      return (
        <Badge variant="outline" className="text-xs">
          instance
        </Badge>
      )
    case DependencyScope.PROJECT:
      return (
        <Badge variant="outline" className="text-xs">
          project
        </Badge>
      )
    case DependencyScope.REMOTE_PROJECT:
      return (
        <Badge variant="outline" className="text-xs">
          remote-project
        </Badge>
      )
    default:
      return (
        <Badge variant="outline" className="text-xs text-muted-foreground">
          unknown
        </Badge>
      )
  }
}

// TemplateDependentsTable renders a table of TemplateDependentRecord items.
function TemplateDependentsTable({
  records,
}: {
  records: TemplateDependentRecord[]
}) {
  if (records.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">No dependents found.</p>
    )
  }
  return (
    <Table data-testid="reverse-dependents-template">
      <TableHeader>
        <TableRow>
          <TableHead>Dependent</TableHead>
          <TableHead>Scope</TableHead>
          <TableHead>Requiring Template</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {records.map((r) => {
          const depKey = `${r.dependentNamespace}/${r.dependentName}`
          const reqKey = r.requiringTemplateNamespace
            ? `${r.requiringTemplateNamespace}/${r.requiringTemplateName}`
            : '—'
          return (
            <TableRow key={depKey}>
              <TableCell className="font-mono text-sm">{depKey}</TableCell>
              <TableCell>
                <DependencyScopeBadge scope={r.scope} />
              </TableCell>
              <TableCell className="font-mono text-sm text-muted-foreground">
                {reqKey}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

// DeploymentDependentsTable renders a table of DeploymentDependentRecord items.
function DeploymentDependentsTable({
  records,
}: {
  records: DeploymentDependentRecord[]
}) {
  if (records.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">No dependents found.</p>
    )
  }
  return (
    <Table data-testid="reverse-dependents-deployment">
      <TableHeader>
        <TableRow>
          <TableHead>Project</TableHead>
          <TableHead>Deployment</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {records.map((r) => {
          const projectName = scopeNameFromNamespace(r.dependentNamespace)
          return (
            <TableRow key={`${r.dependentNamespace}/${r.dependentName}`}>
              <TableCell className="font-mono text-sm">
                {projectName || r.dependentNamespace}
              </TableCell>
              <TableCell className="font-mono text-sm">
                {r.dependentName}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

// TemplateDependentsView fetches and renders reverse-dependents for a Template.
function TemplateDependentsView({
  namespace,
  name,
  enabled,
}: {
  namespace: string
  name: string
  enabled: boolean
}) {
  const { data, isPending, error } = useListTemplateDependents(
    enabled ? namespace : '',
    enabled ? name : '',
  )

  if (!enabled) return null

  if (isPending) {
    return (
      <div className="space-y-2" data-testid="reverse-dependents-loading">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }

  if (error) {
    return (
      <Alert variant="destructive" data-testid="reverse-dependents-error">
        <AlertDescription>
          {error instanceof Error ? error.message : String(error)}
        </AlertDescription>
      </Alert>
    )
  }

  return <TemplateDependentsTable records={data ?? []} />
}

// DeploymentDependentsView fetches and renders reverse-dependents for a Deployment.
function DeploymentDependentsView({
  namespace,
  name,
  enabled,
}: {
  namespace: string
  name: string
  enabled: boolean
}) {
  const { data, isPending, error } = useListDeploymentDependents(
    enabled ? namespace : '',
    enabled ? name : '',
  )

  if (!enabled) return null

  if (isPending) {
    return (
      <div className="space-y-2" data-testid="reverse-dependents-loading">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    )
  }

  if (error) {
    return (
      <Alert variant="destructive" data-testid="reverse-dependents-error">
        <AlertDescription>
          {error instanceof Error ? error.message : String(error)}
        </AlertDescription>
      </Alert>
    )
  }

  return <DeploymentDependentsTable records={data ?? []} />
}

// ReverseDependents renders the "who depends on me?" view for a Template or
// Deployment detail page. It is the single shared entry point for both hosts.
export function ReverseDependents({
  kind,
  namespace,
  name,
  enabled = true,
}: ReverseDependentsProps) {
  if (kind === 'Deployment') {
    return (
      <DeploymentDependentsView
        namespace={namespace}
        name={name}
        enabled={enabled}
      />
    )
  }
  return (
    <TemplateDependentsView namespace={namespace} name={name} enabled={enabled} />
  )
}

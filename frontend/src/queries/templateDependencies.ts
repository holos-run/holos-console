// templateDependencies.ts — query hooks for the reverse-dependency RPCs
// introduced in HOL-986 (ListTemplateDependents, ListDeploymentDependents).
//
// These hooks back the <ReverseDependents> component and the Dependencies
// facet on the unified Templates surface (HOL-987).

import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery } from '@tanstack/react-query'
import { TemplateService, DependencyScope } from '@/gen/holos/console/v1/templates_pb.js'
import type {
  TemplateDependentRecord,
  DeploymentDependentRecord,
} from '@/gen/holos/console/v1/templates_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

// Re-export proto types and enum so consumers import from one place.
export type { TemplateDependentRecord, DeploymentDependentRecord }
export { DependencyScope }

// useListTemplateDependents fetches all dependents of a given template
// identified by (namespace, name). Returns the array of TemplateDependentRecord
// items sorted by (scope, dependentNamespace, dependentName) as the server
// guarantees.
//
// Backed by TemplateService.ListTemplateDependents introduced in HOL-986.
export function useListTemplateDependents(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])

  return useQuery({
    queryKey: keys.templateDependencies.templateDependents(namespace, name),
    queryFn: async () => {
      const response = await client.listTemplateDependents({ namespace, name })
      return response.dependents
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

// useListDeploymentDependents fetches all dependent Deployment instances that
// require the given singleton deployment (cross-project view per ADR 032
// Decision 3 point 4).
//
// Backed by TemplateService.ListDeploymentDependents introduced in HOL-986.
export function useListDeploymentDependents(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])

  return useQuery({
    queryKey: keys.templateDependencies.deploymentDependents(namespace, name),
    queryFn: async () => {
      const response = await client.listDeploymentDependents({ namespace, name })
      return response.dependents
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

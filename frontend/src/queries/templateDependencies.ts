// templateDependencies.ts — query hooks for TemplateDependency resources.
//
// This file has two sections:
//   1. Reverse-dependency read hooks (HOL-986): useListTemplateDependents,
//      useListDeploymentDependents — back the <ReverseDependents> component
//      and the Dependencies facet on the unified Templates surface (HOL-987).
//   2. CRUD hooks for TemplateDependencyService (HOL-1013):
//      useListTemplateDependencies, useDeleteTemplateDependency — back the
//      project-scoped Dependencies ResourceGrid page.

import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import {
  keepPreviousData,
  useQuery,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { TemplateService, DependencyScope } from '@/gen/holos/console/v1/templates_pb.js'
import type {
  TemplateDependentRecord,
  DeploymentDependentRecord,
} from '@/gen/holos/console/v1/templates_pb.js'
import {
  TemplateDependencyService,
} from '@/gen/holos/console/v1/template_dependencies_pb.js'
import type { TemplateDependency } from '@/gen/holos/console/v1/template_dependencies_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

// Re-export proto types and enum so consumers import from one place.
export type { TemplateDependentRecord, DeploymentDependentRecord }
export { DependencyScope }

// Re-export TemplateDependency so consumers import from one place.
export type { TemplateDependency }

// ---------------------------------------------------------------------------
// Reverse-dependency read hooks (HOL-986)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// CRUD hooks for TemplateDependencyService (HOL-1013)
//
// TemplateDependency lives in a project namespace. The hooks below back the
// project-scoped Dependencies ResourceGrid page.
// ---------------------------------------------------------------------------

// useListTemplateDependencies lists all TemplateDependency resources in a
// project namespace. Backed by TemplateDependencyService.ListTemplateDependencies.
export function useListTemplateDependencies(namespace: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateDependencyService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templateDependencies.list(namespace),
    queryFn: async () => {
      const response = await client.listTemplateDependencies({ namespace })
      return response.dependencies
    },
    enabled: isAuthenticated && !!namespace,
    placeholderData: keepPreviousData,
  })
}

// useDeleteTemplateDependency deletes a TemplateDependency by name.
export function useDeleteTemplateDependency(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateDependencyService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplateDependency({ namespace, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: keys.templateDependencies.list(namespace),
      })
    },
  })
}

import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery } from '@tanstack/react-query'
import { TemplateService } from '@/gen/holos/console/v1/templates_pb.js'
import type { TemplateScopeRef } from '@/gen/holos/console/v1/templates_pb.js'
import { useAuth } from '@/lib/auth'

// Re-export types used by consumers.
export type { TemplateScopeRef }

function templateListKey(scope: TemplateScopeRef) {
  return ['templates', 'list', scope.scope, scope.scopeName] as const
}

export function useListTemplates(scope: TemplateScopeRef) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: templateListKey(scope),
    queryFn: async () => {
      const response = await client.listTemplates({ scope })
      return response.templates
    },
    enabled: isAuthenticated && !!scope.scopeName,
  })
}

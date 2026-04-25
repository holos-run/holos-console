import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ProjectSettingsService } from '@/gen/holos/console/v1/project_settings_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

export function useGetProjectSettings(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectSettingsService, transport), [transport])
  return useQuery({
    queryKey: keys.projectSettings.get(project),
    queryFn: async () => {
      const response = await client.getProjectSettings({ project })
      return response.settings
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useGetProjectSettingsRaw(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectSettingsService, transport), [transport])
  return useQuery({
    queryKey: keys.projectSettings.raw(project),
    queryFn: async () => {
      const response = await client.getProjectSettingsRaw({ project })
      return response.raw
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useUpdateProjectSettings(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectSettingsService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { deploymentsEnabled: boolean }) =>
      client.updateProjectSettings({
        project,
        settings: { project, ...params },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.projectSettings.get(project) })
    },
  })
}

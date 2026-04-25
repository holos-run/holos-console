import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { FolderService } from '@/gen/holos/console/v1/folders_pb.js'
import type { ParentType } from '@/gen/holos/console/v1/folders_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

export function useListFolders(organization: string, parentType?: ParentType, parentName?: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  return useQuery({
    queryKey: keys.folders.list(organization, parentType, parentName),
    queryFn: async () => {
      const response = await client.listFolders({ organization, parentType, parentName })
      return response.folders
    },
    enabled: isAuthenticated && !!organization,
  })
}

export function useGetFolder(name: string, organization?: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  return useQuery({
    queryKey: keys.folders.get(name, organization),
    queryFn: async () => {
      const response = await client.getFolder({ organization: organization ?? '', name })
      return response.folder
    },
    enabled: isAuthenticated && !!name,
  })
}

export function useGetFolderRaw(name: string, organization?: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  return useQuery({
    queryKey: keys.folders.raw(organization, name),
    queryFn: async () => {
      const response = await client.getFolderRaw({ organization: organization ?? '', name })
      return response.raw
    },
    enabled: isAuthenticated && !!name,
  })
}

export function useCreateFolder(organization: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      displayName: string
      description: string
      parentType: ParentType
      parentName: string
      userGrants?: { principal: string; role: number }[]
      roleGrants?: { principal: string; role: number }[]
    }) =>
      client.createFolder({ organization, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.folders.listScope(organization) })
    },
  })
}

export function useUpdateFolder(organization: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { displayName?: string; description?: string; parentType?: ParentType; parentName?: string }) =>
      client.updateFolder({ organization, name, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.folders.list(organization) })
      queryClient.invalidateQueries({ queryKey: keys.folders.getScope() })
    },
  })
}

export function useUpdateFolderSharing(organization: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      userGrants: { principal: string; role: number }[]
      roleGrants: { principal: string; role: number }[]
    }) => client.updateFolderSharing({ name, organization, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.folders.list(organization) })
      queryClient.invalidateQueries({ queryKey: keys.folders.getScope() })
    },
  })
}

export function useUpdateFolderDefaultSharing(organization: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      defaultUserGrants: { principal: string; role: number }[]
      defaultRoleGrants: { principal: string; role: number }[]
    }) => client.updateFolderDefaultSharing({ name, organization, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.folders.list(organization) })
      queryClient.invalidateQueries({ queryKey: keys.folders.getScope() })
    },
  })
}

export function useDeleteFolder(organization: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteFolder({ organization, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.folders.list(organization) })
      queryClient.invalidateQueries({ queryKey: keys.folders.getScope() })
    },
  })
}

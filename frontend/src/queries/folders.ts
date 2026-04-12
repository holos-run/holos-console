import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { FolderService } from '@/gen/holos/console/v1/folders_pb.js'
import type { ShareGrant } from '@/gen/holos/console/v1/secrets_pb.js'
import type { ParentType } from '@/gen/holos/console/v1/folders_pb.js'
import { useAuth } from '@/lib/auth'

export type { ParentType }

function folderListKey(organization: string, parentType?: number, parentName?: string) {
  return ['folders', 'list', organization, parentType, parentName] as const
}

function folderGetKey(name: string, organization?: string) {
  return ['folders', 'get', organization ?? '', name] as const
}

export function useListFolders(organization: string, parentType?: ParentType, parentName?: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  return useQuery({
    queryKey: folderListKey(organization, parentType, parentName),
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
    queryKey: folderGetKey(name, organization),
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
    queryKey: ['folders', 'raw', organization ?? '', name] as const,
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
      userGrants?: ShareGrant[]
      roleGrants?: ShareGrant[]
    }) =>
      client.createFolder({ organization, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['folders', 'list', organization] })
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
      queryClient.invalidateQueries({ queryKey: folderListKey(organization) })
      queryClient.invalidateQueries({ queryKey: ['folders', 'get'] })
    },
  })
}

export function useUpdateFolderSharing(organization: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      userGrants: ShareGrant[]
      roleGrants: ShareGrant[]
    }) => client.updateFolderSharing({ name, organization, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: folderListKey(organization) })
      queryClient.invalidateQueries({ queryKey: ['folders', 'get'] })
    },
  })
}

export function useUpdateFolderDefaultSharing(organization: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(FolderService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      defaultUserGrants: ShareGrant[]
      defaultRoleGrants: ShareGrant[]
    }) => client.updateFolderDefaultSharing({ name, organization, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: folderListKey(organization) })
      queryClient.invalidateQueries({ queryKey: ['folders', 'get'] })
    },
  })
}


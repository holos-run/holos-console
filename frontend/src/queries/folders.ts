import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery } from '@tanstack/react-query'
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

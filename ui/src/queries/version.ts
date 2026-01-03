import { create } from '@bufbuild/protobuf'
import { useQuery } from '@connectrpc/connect-query'
import {
  GetVersionRequestSchema,
  VersionService,
} from '../gen/holos/console/v1/version_pb.js'

export function useVersion() {
  return useQuery(VersionService.method.getVersion, create(GetVersionRequestSchema), {
    staleTime: Infinity,
    gcTime: Infinity,
  })
}

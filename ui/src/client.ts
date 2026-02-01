import { createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { VersionService } from './gen/holos/console/v1/version_pb.js'
import { SecretsService } from './gen/holos/console/v1/secrets_pb.js'
import { OrganizationService } from './gen/holos/console/v1/organizations_pb.js'

// In development with Vite proxy, use relative path (proxied to backend).
// In production, the frontend is served by the Go backend at the same origin.
export const transport = createConnectTransport({
  baseUrl: '/',
})

export const versionClient = createClient(VersionService, transport)
export const secretsClient = createClient(SecretsService, transport)
export const organizationsClient = createClient(OrganizationService, transport)

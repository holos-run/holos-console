import { QueryClient } from '@tanstack/react-query'
import { ConnectError, Code } from '@connectrpc/connect'

// shouldRetry returns false for ConnectRPC Unauthenticated errors so that
// TanStack Query does not amplify the 401 burst with its default 3-retry
// backoff. The transport-level interceptor handles token renewal; once it
// exhausts its single retry the error should propagate immediately.
// All other errors use the default TanStack Query retry limit of 3.
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (error instanceof ConnectError && error.code === Code.Unauthenticated) {
    return false
  }
  return failureCount < 3
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: shouldRetry,
    },
  },
})

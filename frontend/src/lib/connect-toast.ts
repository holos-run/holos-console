// connect-toast.ts — translate ConnectRPC errors into friendly toast text.
//
// ADR 036 splits UI gating into two patterns: optimistic+toast (leave the
// button in place, show a toast on 403) and up-front SSAR (hide the button
// before the user clicks). This helper backs the optimistic+toast side: any
// connect error that escapes a mutation can be passed in and the helper
// returns a string that surfaces the right user-facing message — most
// importantly, mapping CodePermissionDenied to a stable "access denied"
// label so the UI does not leak the API server's internal forbidden-reason
// strings into a snackbar.

import { ConnectError, Code } from '@connectrpc/connect'

// connectErrorMessage returns a string suitable for toast.error / toast
// from any thrown value. Non-Error inputs are coerced via String(). Connect
// errors with code PermissionDenied get the stable "access denied" message;
// other connect errors return their rawMessage so handlers continue to see
// the API server's error text.
export function connectErrorMessage(err: unknown): string {
  if (err instanceof ConnectError) {
    if (err.code === Code.PermissionDenied) {
      return 'Access denied: you do not have permission to perform this action.'
    }
    if (err.code === Code.Unauthenticated) {
      return 'Your session has expired. Please sign in again.'
    }
    return err.rawMessage || err.message
  }
  if (err instanceof Error) return err.message
  return String(err)
}

// isPermissionDenied is a narrow predicate so callers can branch on the
// gating decision without re-implementing the type guard.
export function isPermissionDenied(err: unknown): boolean {
  return err instanceof ConnectError && err.code === Code.PermissionDenied
}

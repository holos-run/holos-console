/**
 * return-to.ts — open-redirect-safe helpers for the `returnTo` search param.
 *
 * Resource-creation routes (`/organization/new`, `/folder/new`,
 * `/project/new`) need to redirect the user back to the page they launched
 * the creation from.  This module provides three pure helper functions that
 * keep the search-param contract consistent across all creation routes.
 *
 * ## Security contract
 *
 * Only same-origin, in-app paths are allowed.  A valid `returnTo` value:
 *   - begins with a single `/` (not `//`, which is protocol-relative)
 *   - contains no `:` in the first path segment (blocks `javascript:`, etc.)
 *   - contains no backslashes (blocks path-traversal tricks on Windows)
 *   - is non-empty
 *   - round-trips through `decodeURIComponent` without throwing (valid UTF-8)
 *
 * Any value that fails validation falls back to the caller-supplied default.
 *
 * ## Usage
 *
 * In a link/button that opens a creation page:
 *
 *   import { buildReturnTo } from '@/lib/return-to'
 *   // router.state.location is a one-shot snapshot captured at click time inside
 *   // an event handler — intentionally not reactive.  Use useLocation() for any
 *   // route reads that need to re-render on navigation.
 *   const search = { returnTo: buildReturnTo(router.state.location) }
 *   <Link to="/organization/new" search={search}>New Org</Link>
 *
 * In the creation route's `onSuccess` handler:
 *
 *   import { resolveReturnTo } from '@/lib/return-to'
 *   const target = resolveReturnTo(search, '/organizations')
 *   navigate({ to: target })
 *
 * @module
 */

/**
 * A validated same-origin path.  The branded type lets TypeScript callers
 * distinguish a trusted, validated path from an arbitrary string.
 */
export type SafeReturnPath = string & { readonly __safeReturnPath: unique symbol }

/**
 * isValidReturnTo returns true when `value` is safe to use as a redirect
 * destination after a resource-creation action.
 *
 * Rules (all must pass):
 *   1. Non-empty string.
 *   2. Starts with `/` but NOT `//`.
 *   3. No colon (`:`) before the first `/` after the leading slash.
 *      (Blocks `javascript:alert()`, `https://evil.com`, etc.)
 *   4. No backslash anywhere.  (Blocks Windows-style path tricks.)
 *   5. `decodeURIComponent` does not throw.  (Ensures valid UTF-8 encoding.)
 */
export function isValidReturnTo(value: unknown): value is SafeReturnPath {
  if (typeof value !== 'string' || value.length === 0) return false

  // Must start with a single '/'.
  if (!value.startsWith('/')) return false

  // Must NOT start with '//' (protocol-relative URL).
  if (value.startsWith('//')) return false

  // No backslashes anywhere.
  if (value.includes('\\')) return false

  // No colon before the first path separator after the leading slash.
  // Extract the first segment (everything between the leading '/' and the next '/').
  const afterLeadingSlash = value.slice(1)
  const firstSegmentEnd = afterLeadingSlash.indexOf('/')
  const firstSegment =
    firstSegmentEnd === -1 ? afterLeadingSlash : afterLeadingSlash.slice(0, firstSegmentEnd)
  if (firstSegment.includes(':')) return false

  // Must round-trip through decodeURIComponent without throwing.
  try {
    decodeURIComponent(value)
  } catch {
    return false
  }

  return true
}

/**
 * resolveReturnTo returns the redirect target from the `returnTo` search
 * param when valid, or `fallback` otherwise.
 *
 * @param returnTo - The raw `returnTo` value from the URL search params.
 *   Accepts `string | undefined | null`.
 * @param fallback - The path to use when `returnTo` is absent or invalid.
 *   Must be a valid in-app path (the caller is responsible for passing a
 *   safe default; it is NOT re-validated here to keep the helper simple).
 *
 * @returns A path string suitable for TanStack Router's `navigate({ to })`.
 */
export function resolveReturnTo(returnTo: string | undefined | null, fallback: string): string {
  if (isValidReturnTo(returnTo)) return returnTo
  return fallback
}

/**
 * Location represents the minimal subset of a TanStack Router (or browser)
 * location object needed to build a `returnTo` value.
 *
 * TanStack Router's `ParsedLocation` satisfies this interface, as does the
 * browser's `window.location`.
 */
export interface Location {
  pathname: string
  search?: string
}

/**
 * buildReturnTo encodes `location.pathname` (plus `location.search` when
 * present) into a single string suitable for use as the `returnTo` search
 * param.
 *
 * The encoded value is the raw path + query string — no additional encoding
 * is applied at this layer.  TanStack Router serialises search params when
 * constructing the URL, so callers should pass the plain string returned here
 * directly into the `search` object:
 *
 *   <Link to="/organization/new" search={{ returnTo: buildReturnTo(location) }}>
 *
 * Returns an empty string when `location.pathname` is empty or falsy.
 */
export function buildReturnTo(location: Location): string {
  if (!location.pathname) return ''
  const search = location.search && location.search !== '?' ? location.search : ''
  return location.pathname + search
}

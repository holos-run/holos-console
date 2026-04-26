/**
 * url-state.ts — helpers for parsing and serialising the ResourceGrid's URL
 * search params.
 *
 * Both the ResourceGrid component itself and each consumer route's
 * `validateSearch` import from this module so the encoding is never
 * duplicated.
 */

import type { ResourceGridSearch } from './types'

/**
 * Parse an untrusted query-string object into a validated `ResourceGridSearch`.
 * All fields are optional; unknown / invalid values fall back to the defaults
 * documented below.
 *
 * Consumers call this from their route's `validateSearch`:
 *
 * ```ts
 * import { parseGridSearch } from '@/components/resource-grid/url-state'
 *
 * export const Route = createFileRoute('...')({
 *   validateSearch: parseGridSearch,
 *   component: MyPage,
 * })
 * ```
 */
export function parseGridSearch(raw: Record<string, unknown>): ResourceGridSearch {
  const result: ResourceGridSearch = {}

  const kind = raw['kind']
  if (typeof kind === 'string' && kind.length > 0) {
    result.kind = kind
  }

  const search = raw['search']
  if (typeof search === 'string' && search.length > 0) {
    result.search = search
  }

  const sort = raw['sort']
  if (typeof sort === 'string' && sort.length > 0) {
    result.sort = sort
  }

  const sortDir = raw['sortDir']
  if (sortDir === 'asc' || sortDir === 'desc') {
    result.sortDir = sortDir
  }

  const fields = raw['fields']
  if (typeof fields === 'string' && fields.length > 0) {
    result.fields = fields
  }

  return result
}

/**
 * Serialise a `ResourceGridSearch` back to a plain object suitable for
 * TanStack Router's `search` param (undefined fields are omitted, which
 * removes them from the URL).
 */
export function serialiseGridSearch(
  params: ResourceGridSearch,
): Record<string, string | undefined> {
  return {
    kind: params.kind || undefined,
    search: params.search || undefined,
    sort: params.sort || undefined,
    sortDir: params.sortDir || undefined,
    fields: params.fields || undefined,
  }
}

/** Parse comma-separated kind IDs from the URL `?kind=` param. */
export function parseKindIds(raw: string | undefined): string[] {
  if (!raw) return []
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

/** Serialise an array of kind IDs to the `?kind=` param value. */
export function serialiseKindIds(ids: string[]): string | undefined {
  if (ids.length === 0) return undefined
  return ids.join(',')
}

/**
 * Parse the comma-separated `?fields=` param. Falls back to `defaults` when
 * the param is missing so callers do not have to inline the default list at
 * each render.
 */
export function parseSearchFieldIds(
  raw: string | undefined,
  defaults: string[],
): string[] {
  if (!raw) return [...defaults]
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

/**
 * Serialise the search-field selection to the `?fields=` param value.
 * Returns undefined when the selection equals `defaults`, so the URL stays
 * clean for users who never opened the filter popover.
 */
export function serialiseSearchFieldIds(
  ids: string[],
  defaults: string[],
): string | undefined {
  if (ids.length === 0) return undefined
  // Match against defaults treated as a set (order-independent).
  const idSet = new Set(ids)
  const defaultSet = new Set(defaults)
  if (idSet.size === defaultSet.size) {
    let same = true
    for (const id of idSet) {
      if (!defaultSet.has(id)) {
        same = false
        break
      }
    }
    if (same) return undefined
  }
  return ids.join(',')
}

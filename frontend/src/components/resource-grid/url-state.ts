/**
 * url-state.ts — helpers for parsing and serialising the ResourceGrid's URL
 * search params.
 *
 * Both the ResourceGrid component itself and each consumer route's
 * `validateSearch` import from this module so the encoding is never
 * duplicated.
 */

import type { LineageDirection, ResourceGridSearch } from './types'

const VALID_LINEAGE_DIRECTIONS = new Set<string>([
  'ancestors',
  'descendants',
  'both',
])

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

  const lineage = raw['lineage']
  if (typeof lineage === 'string' && VALID_LINEAGE_DIRECTIONS.has(lineage)) {
    result.lineage = lineage as LineageDirection
  }

  const recursive = raw['recursive']
  if (recursive === '1') {
    result.recursive = '1'
  } else if (recursive === '0') {
    result.recursive = '0'
  }
  // Default (absent) → treated as '0' (non-recursive) by the grid.

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
    lineage: params.lineage || undefined,
    recursive: params.recursive,
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

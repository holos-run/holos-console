/**
 * ResourceGrid v1 — reusable table component for Secrets, Deployments, and
 * Templates.
 *
 * Features:
 *  - TanStack Table with global search (includesString)
 *  - Multi-kind filter (URL ?kind=a,b)
 *  - New button / dropdown
 *  - Row-level delete via ConfirmDeleteDialog
 *  - URL sync via TanStack Router useSearch / useNavigate
 *
 * See docs/agents/data-grid-architecture.md for the design note and extension
 * guide.
 */

import { useMemo, useCallback, type ReactNode } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  flexRender,
  createColumnHelper,
  type VisibilityState,
  type SortingState,
  type FilterFn,
} from '@tanstack/react-table'
import { ArrowUpDown, ArrowUp, ArrowDown, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ConfirmDeleteDialog } from '@/components/ui/confirm-delete-dialog'

import type { ColumnDef } from '@tanstack/react-table'
import type {
  ExtraSearchField,
  Kind,
  Row,
  ResourceGridSearch,
} from './types'
import { DEFAULT_SEARCH_FIELD_IDS } from './types'
import { NewButton, Toolbar } from './Toolbar'
import { SearchFieldsFilter } from './SearchFieldsFilter'
import { useDeleteConfirm } from './useDeleteConfirm'
import {
  parseKindIds,
  parseSearchFieldIds,
  serialiseKindIds,
  serialiseSearchFieldIds,
} from './url-state'

// ---------------------------------------------------------------------------
// Column helper
// ---------------------------------------------------------------------------

const columnHelper = createColumnHelper<Row>()

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface ResourceGridProps {
  /** Title shown in the Card header. */
  title: string
  /** The resource kinds this grid indexes. */
  kinds: Kind[]
  /** All rows — filtering happens inside the component. */
  rows: Row[]
  /**
   * Async callback invoked after the user confirms a deletion.
   * Throw to surface an error toast.
   */
  onDelete: (row: Row) => Promise<void>
  /** While true, renders a skeleton loader instead of the table. */
  isLoading?: boolean
  /** If set and no rows exist yet, renders an error card. */
  error?: Error | null
  /**
   * The currently active search params from TanStack Router.  Pass the
   * result of `useSearch(...)` from the parent route.
   */
  search?: ResourceGridSearch
  /**
   * Called when the grid needs to update the URL.  The signature matches
   * TanStack Router's `navigate({ search: updater })` pattern.
   */
  onSearchChange?: (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => void
  /**
   * Optional extra columns appended after the Description column and before
   * the Created At column. Callers (e.g. Deployments) use this to add
   * kind-specific columns such as phase badges or drift indicators without
   * altering the default column shape.
   */
  extraColumns?: ColumnDef<Row>[]
  /**
   * Optional node rendered inside the Card header, below the title and
   * above the table. Used for description banners.
   */
  headerContent?: ReactNode
  /**
   * Optional extra actions rendered in the Card header to the left of the
   * New button. Use for icon buttons such as the Templates help pane toggle.
   */
  headerActions?: ReactNode
  /**
   * Optional set of column IDs that should be rendered with a sort toggle
   * button. Defaults to ['displayName', 'createdAt'] when unset so the key
   * fields and Created At are sortable. Pass an empty array to disable all
   * sorting.
   */
  sortableColumns?: string[]
  /**
   * Caller-supplied hidden search fields (e.g. `creator`). Each field appears
   * as a checkbox in the search-fields filter popover. The values are read
   * from `Row.extraSearch[id]`. Hidden fields contribute to the global search
   * only when checked; they are never rendered as table columns.
   */
  extraSearchFields?: ExtraSearchField[]
  /**
   * Default sort applied when the URL omits `?sort=`. Defaults to
   * `{ id: 'createdAt', desc: true }` (newest first) so every grid is
   * always sorted (HOL-990 AC1.3).
   */
  defaultSort?: { id: string; desc: boolean }
  /**
   * Optional override for the empty state body. When provided and there are
   * zero rows, this is rendered in place of the default "No resources found."
   * message. Use it to surface a context-specific call-to-action (e.g. an
   * organization-scoped Create Project link).
   */
  emptyStateContent?: ReactNode
  /**
   * When true (default) each row renders a trash-icon delete action. Set to
   * false on indexes that intentionally do not expose row-level deletion
   * (e.g. the Projects index, where delete lives on the project detail page).
   */
  showDeleteAction?: boolean
}

// ---------------------------------------------------------------------------
// ResourceGrid
// ---------------------------------------------------------------------------

export function ResourceGrid({
  title,
  kinds,
  rows,
  onDelete,
  isLoading = false,
  error,
  search = {},
  onSearchChange,
  extraColumns = [],
  headerContent,
  headerActions,
  sortableColumns = ['parentId', 'resourceId', 'displayName', 'createdAt'],
  extraSearchFields = [],
  defaultSort = { id: 'createdAt', desc: true },
  emptyStateContent,
  showDeleteAction = true,
}: ResourceGridProps) {
  const navigate = useNavigate()

  // --- Derive local state from URL params --------------------------------

  const selectedKindIds = useMemo(
    () => parseKindIds(search.kind),
    [search.kind],
  )

  const globalFilter = search.search ?? ''

  // Derive sorting state from URL params, falling back to defaultSort so the
  // grid is always sorted on first render (HOL-990 AC1.3). The URL still
  // wins when present so back/forward state is stable.
  const sorting: SortingState = useMemo(() => {
    if (search.sort) {
      return [{ id: search.sort, desc: search.sortDir === 'desc' }]
    }
    return [{ id: defaultSort.id, desc: defaultSort.desc }]
  }, [search.sort, search.sortDir, defaultSort.id, defaultSort.desc])

  // --- Search-field selection (HOL-990 AC1.3) ----------------------------
  // The IDs in this list determine which fields the global search input
  // matches against. URL omission means "use the key-field defaults".

  const selectedSearchFieldIds = useMemo(
    () => parseSearchFieldIds(search.fields, DEFAULT_SEARCH_FIELD_IDS),
    [search.fields],
  )

  // --- URL updater helpers -----------------------------------------------

  const updateSearch = useCallback(
    (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => {
      onSearchChange?.(updater)
    },
    [onSearchChange],
  )

  const setGlobalFilter = useCallback(
    (value: string) => {
      updateSearch((prev) => ({ ...prev, search: value || undefined }))
    },
    [updateSearch],
  )

  const setKindIds = useCallback(
    (ids: string[]) => {
      updateSearch((prev) => ({
        ...prev,
        kind: serialiseKindIds(ids) ?? undefined,
      }))
    },
    [updateSearch],
  )

  const setSearchFieldIds = useCallback(
    (ids: string[]) => {
      updateSearch((prev) => ({
        ...prev,
        fields:
          serialiseSearchFieldIds(ids, [...DEFAULT_SEARCH_FIELD_IDS]) ??
          undefined,
      }))
    },
    [updateSearch],
  )

  const handleSortingChange = useCallback(
    (updaterOrValue: SortingState | ((prev: SortingState) => SortingState)) => {
      const next = typeof updaterOrValue === 'function' ? updaterOrValue(sorting) : updaterOrValue
      const first = next[0]
      updateSearch((prev) => ({
        ...prev,
        sort: first?.id ?? undefined,
        sortDir: first ? (first.desc ? 'desc' : 'asc') : undefined,
      }))
    },
    [updateSearch, sorting],
  )

  const {
    deleteTarget,
    isDeleting,
    deleteError,
    handleDeleteClick,
    handleDeleteConfirm,
    handleDeleteOpenChange,
  } = useDeleteConfirm({ onDelete })

  // --- Derive unique parent IDs in the current row set -------------------

  const uniqueParentIds = useMemo(
    () => Array.from(new Set(rows.map((r) => r.parentId).filter(Boolean))),
    [rows],
  )
  const singleParent = uniqueParentIds.length === 1

  // --- Column visibility: hide parentId when exactly one parent ----------

  const columnVisibility: VisibilityState = useMemo(
    () => ({ parentId: !singleParent }),
    [singleParent],
  )

  // --- Filter rows by selected kind IDs ----------------------------------

  const kindFilteredRows = useMemo(() => {
    if (selectedKindIds.length === 0) return rows
    const kindSet = new Set(selectedKindIds)
    return rows.filter((r) => kindSet.has(r.kind))
  }, [rows, selectedKindIds])

  // --- Custom global filter that respects the search-field selection -----
  // Defined here (not at module scope) so it closes over the current
  // `selectedSearchFieldIds`. TanStack Table reruns the filter whenever the
  // function identity changes.

  const globalFilterFn: FilterFn<Row> = useCallback(
    (row, _columnId, filterValue) => {
      if (typeof filterValue !== 'string' || filterValue.length === 0) {
        return true
      }
      const needle = filterValue.toLowerCase()
      const r = row.original
      for (const fieldId of selectedSearchFieldIds) {
        let haystack = ''
        switch (fieldId) {
          case 'parent':
            haystack = r.parentLabel || r.parentId
            break
          case 'name':
            haystack = r.name
            break
          case 'displayName':
            haystack = r.displayName || r.name
            break
          default:
            // Any non-key field id resolves through the row's extraSearch bag.
            haystack = r.extraSearch?.[fieldId] ?? ''
        }
        if (haystack && haystack.toLowerCase().includes(needle)) {
          return true
        }
      }
      return false
    },
    [selectedSearchFieldIds],
  )

  // --- Stable set of sortable column IDs --------------------------------

  const sortableSet = useMemo(() => new Set(sortableColumns), [sortableColumns])

  // --- TanStack Table columns --------------------------------------------

  const columns = useMemo(
    () => [
      columnHelper.accessor((row) => row.parentLabel || row.parentId, {
        id: 'parentId',
        header: ({ column }) => {
          if (!sortableSet.has('parentId')) {
            return <span>Parent</span>
          }
          const isSorted = column.getIsSorted()
          return (
            <Button
              variant="ghost"
              size="sm"
              className="-ml-3 h-8 font-normal"
              onClick={() => column.toggleSorting(isSorted === 'asc')}
              aria-label="Sort by Parent"
            >
              Parent
              {isSorted === 'asc' ? (
                <ArrowUp className="ml-1 h-4 w-4" aria-hidden="true" />
              ) : isSorted === 'desc' ? (
                <ArrowDown className="ml-1 h-4 w-4" aria-hidden="true" />
              ) : (
                <ArrowUpDown
                  className="ml-1 h-4 w-4 opacity-50"
                  aria-hidden="true"
                />
              )}
            </Button>
          )
        },
        enableSorting: sortableSet.has('parentId'),
        sortingFn: 'alphanumeric',
        cell: ({ row }) => (
          <span className="text-muted-foreground text-sm">
            {row.original.parentLabel || row.original.parentId}
          </span>
        ),
      }),
      columnHelper.accessor('id', {
        id: 'resourceId',
        header: ({ column }) => {
          if (!sortableSet.has('resourceId')) {
            return <span>Name</span>
          }
          const isSorted = column.getIsSorted()
          return (
            <Button
              variant="ghost"
              size="sm"
              className="-ml-3 h-8 font-normal"
              onClick={() => column.toggleSorting(isSorted === 'asc')}
              aria-label="Sort by Name"
            >
              Name
              {isSorted === 'asc' ? (
                <ArrowUp className="ml-1 h-4 w-4" aria-hidden="true" />
              ) : isSorted === 'desc' ? (
                <ArrowDown className="ml-1 h-4 w-4" aria-hidden="true" />
              ) : (
                <ArrowUpDown
                  className="ml-1 h-4 w-4 opacity-50"
                  aria-hidden="true"
                />
              )}
            </Button>
          )
        },
        enableSorting: sortableSet.has('resourceId'),
        sortingFn: 'alphanumeric',
        cell: ({ row, getValue }) => {
          const value = getValue()
          if (row.original.detailHref) {
            return (
              <Link
                to={row.original.detailHref}
                className="font-mono text-muted-foreground text-sm hover:underline"
                onClick={(e) => e.stopPropagation()}
              >
                {value}
              </Link>
            )
          }
          return (
            <span className="font-mono text-muted-foreground text-sm">
              {value}
            </span>
          )
        },
      }),
      columnHelper.accessor(
        (row) => row.displayName || row.name,
        {
          id: 'displayName',
          header: ({ column }) => {
            if (!sortableSet.has('displayName')) {
              return <span>Display Name</span>
            }
            const isSorted = column.getIsSorted()
            return (
              <Button
                variant="ghost"
                size="sm"
                className="-ml-3 h-8 font-normal"
                onClick={() => column.toggleSorting(isSorted === 'asc')}
                aria-label="Sort by Display Name"
              >
                Display Name
                {isSorted === 'asc' ? (
                  <ArrowUp className="ml-1 h-4 w-4" aria-hidden="true" />
                ) : isSorted === 'desc' ? (
                  <ArrowDown className="ml-1 h-4 w-4" aria-hidden="true" />
                ) : (
                  <ArrowUpDown
                    className="ml-1 h-4 w-4 opacity-50"
                    aria-hidden="true"
                  />
                )}
              </Button>
            )
          },
          enableSorting: sortableSet.has('displayName'),
          sortingFn: 'alphanumeric',
          cell: ({ row }) => {
            const label = row.original.displayName || row.original.name
            if (row.original.detailHref) {
              return (
                <Link
                  to={row.original.detailHref}
                  className="font-medium hover:underline"
                  title={row.original.name}
                  onClick={(e) => e.stopPropagation()}
                >
                  {label}
                </Link>
              )
            }
            return (
              <span className="font-medium" title={row.original.name}>
                {label}
              </span>
            )
          },
        },
      ),
      columnHelper.accessor('description', {
        id: 'description',
        header: 'Description',
        cell: ({ getValue }) => (
          <span
            className="max-w-md truncate block text-muted-foreground text-sm"
            title={getValue()}
          >
            {getValue()}
          </span>
        ),
      }),
      // Caller-supplied extra columns (e.g. Phase badge, Policy Drift badge)
      // appear after Description and before Created At.
      ...extraColumns,
      columnHelper.accessor('createdAt', {
        id: 'createdAt',
        header: ({ column }) => {
          if (!sortableSet.has('createdAt')) {
            return <span>Created At</span>
          }
          const isSorted = column.getIsSorted()
          return (
            <Button
              variant="ghost"
              size="sm"
              className="-ml-3 h-8 font-normal"
              onClick={() => column.toggleSorting(isSorted === 'asc')}
              aria-label="Sort by Created At"
            >
              Created At
              {isSorted === 'asc' ? (
                <ArrowUp className="ml-1 h-4 w-4" aria-hidden="true" />
              ) : isSorted === 'desc' ? (
                <ArrowDown className="ml-1 h-4 w-4" aria-hidden="true" />
              ) : (
                <ArrowUpDown className="ml-1 h-4 w-4 opacity-50" aria-hidden="true" />
              )}
            </Button>
          )
        },
        enableSorting: sortableSet.has('createdAt'),
        sortingFn: (rowA, rowB) => {
          const a = rowA.original.createdAt
          const b = rowB.original.createdAt
          if (!a && !b) return 0
          if (!a) return 1
          if (!b) return -1
          return a < b ? -1 : a > b ? 1 : 0
        },
        cell: ({ getValue }) => {
          const raw = getValue()
          if (!raw) {
            return (
              <span className="text-muted-foreground text-sm">—</span>
            )
          }
          const date = new Date(raw)
          if (Number.isNaN(date.getTime())) {
            return (
              <span className="text-muted-foreground text-sm">—</span>
            )
          }
          return (
            <span className="text-muted-foreground text-sm whitespace-nowrap">
              {date.toLocaleDateString()}
            </span>
          )
        },
      }),
      // Actions column — only mounted when row-level delete is enabled.
      ...(showDeleteAction
        ? [
            columnHelper.display({
              id: 'actions',
              header: '',
              cell: ({ row }) => (
                <div className="flex justify-end">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`delete ${row.original.displayName || row.original.name}`}
                    onClick={(e) => {
                      e.stopPropagation()
                      handleDeleteClick(row.original)
                    }}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              ),
            }),
          ]
        : []),
    ],
    [handleDeleteClick, extraColumns, sortableSet, showDeleteAction],
  )

  // --- TanStack Table instance -------------------------------------------

  const table = useReactTable({
    data: kindFilteredRows,
    columns,
    state: { globalFilter, columnVisibility, sorting },
    onGlobalFilterChange: setGlobalFilter,
    onSortingChange: handleSortingChange,
    globalFilterFn: globalFilterFn,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  // --- Loading skeleton --------------------------------------------------

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2" data-testid="resource-grid-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        </CardContent>
      </Card>
    )
  }

  // --- Error state (only when no rows available) -------------------------

  if (error && rows.length === 0) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  // --- Determine New button behaviour ------------------------------------

  const creatableKinds = kinds.filter((k) => k.canCreate && k.newHref)

  // --- Render ------------------------------------------------------------

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <div className="flex items-center gap-1">
            <CardTitle>{title}</CardTitle>
            <SearchFieldsFilter
              extraFields={extraSearchFields}
              selectedIds={selectedSearchFieldIds}
              onChange={setSearchFieldIds}
            />
          </div>
          <div className="flex items-center gap-2">
            {headerActions}
            <NewButton kinds={creatableKinds} />
          </div>
        </CardHeader>
        {headerContent && (
          <CardContent className="pt-0 pb-0">
            {headerContent}
          </CardContent>
        )}
        <CardContent>
          {/* Inline partial-error banner */}
          {error && rows.length > 0 && (
            <Alert
              variant="destructive"
              className="mb-4"
              data-testid="resource-grid-partial-error"
            >
              <AlertDescription>{error.message}</AlertDescription>
            </Alert>
          )}

          <Toolbar
            title={title}
            kinds={kinds}
            selectedKindIds={selectedKindIds}
            globalFilter={globalFilter}
            onGlobalFilterChange={setGlobalFilter}
            onKindIdsChange={setKindIds}
          />

          {/* Empty state */}
          {rows.length === 0 ? (
            emptyStateContent ?? (
              <div className="flex flex-col items-center gap-3 py-8 text-center">
                <p className="text-muted-foreground">No resources found.</p>
              </div>
            )
          ) : (
            <>
              {table.getRowModel().rows.length === 0 && (
                <div className="mb-3 rounded-md border border-dashed border-border p-4 text-center">
                  <p className="text-sm text-muted-foreground">
                    No resources match the current filters.
                  </p>
                </div>
              )}
              <Table>
                <TableHeader>
                  {table.getHeaderGroups().map((headerGroup) => (
                    <TableRow key={headerGroup.id}>
                      {headerGroup.headers.map((header) => (
                        <TableHead key={header.id}>
                          {header.isPlaceholder
                            ? null
                            : flexRender(
                                header.column.columnDef.header,
                                header.getContext(),
                              )}
                        </TableHead>
                      ))}
                    </TableRow>
                  ))}
                </TableHeader>
                <TableBody>
                  {table.getRowModel().rows.map((row) => (
                    <TableRow
                      key={row.id}
                      className={row.original.detailHref ? 'cursor-pointer' : undefined}
                      onClick={
                        row.original.detailHref
                          ? () => navigate({ to: row.original.detailHref! })
                          : undefined
                      }
                    >
                      {row.getVisibleCells().map((cell) => (
                        <TableCell key={cell.id}>
                          {flexRender(
                            cell.column.columnDef.cell,
                            cell.getContext(),
                          )}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </>
          )}
        </CardContent>
      </Card>

      {/* Delete confirmation dialog */}
      <ConfirmDeleteDialog
        open={deleteTarget !== null}
        onOpenChange={handleDeleteOpenChange}
        displayName={deleteTarget?.displayName}
        name={deleteTarget?.name ?? ''}
        namespace={deleteTarget?.namespace ?? ''}
        onConfirm={handleDeleteConfirm}
        isDeleting={isDeleting}
        error={deleteError}
      />
    </>
  )
}

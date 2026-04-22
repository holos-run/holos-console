/**
 * ResourceGrid v1 — reusable table component for Secrets, Deployments, and
 * Templates.
 *
 * Features:
 *  - TanStack Table with global search (includesString)
 *  - Multi-kind filter (URL ?kind=a,b)
 *  - Lineage filter (URL ?lineage=ancestors&recursive=0)
 *  - New button / dropdown
 *  - Row-level delete via ConfirmDeleteDialog
 *  - URL sync via TanStack Router useSearch / useNavigate
 *
 * See HOL-855 for the full acceptance criteria.
 */

import { useState, useMemo, useCallback } from 'react'
import { Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  flexRender,
  createColumnHelper,
  type VisibilityState,
} from '@tanstack/react-table'
import { Trash2, ChevronDown } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { ConfirmDeleteDialog } from '@/components/ui/confirm-delete-dialog'

import type { Kind, Row, LineageDirection, ResourceGridSearch } from './types'
import { parseKindIds, serialiseKindIds } from './url-state'

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
}: ResourceGridProps) {
  // --- Derive local state from URL params --------------------------------

  const selectedKindIds = useMemo(
    () => parseKindIds(search.kind),
    [search.kind],
  )

  const globalFilter = search.search ?? ''
  const lineageDirection: LineageDirection = search.lineage ?? 'descendants'
  const recursive = search.recursive === '1'

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

  const setLineageDirection = useCallback(
    (dir: LineageDirection) => {
      updateSearch((prev) => ({ ...prev, lineage: dir }))
    },
    [updateSearch],
  )

  const setRecursive = useCallback(
    (val: boolean) => {
      updateSearch((prev) => ({ ...prev, recursive: val ? '1' : '0' }))
    },
    [updateSearch],
  )

  // --- Delete dialog state -----------------------------------------------

  const [deleteTarget, setDeleteTarget] = useState<Row | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<Error | null>(null)

  const handleDeleteClick = useCallback((row: Row) => {
    setDeleteTarget(row)
    setDeleteError(null)
  }, [])

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return
    setIsDeleting(true)
    setDeleteError(null)
    try {
      await onDelete(deleteTarget)
      setDeleteTarget(null)
      toast.success(`Deleted ${deleteTarget.displayName || deleteTarget.name}`)
    } catch (err) {
      const e = err instanceof Error ? err : new Error(String(err))
      setDeleteError(e)
      toast.error(e.message)
    } finally {
      setIsDeleting(false)
    }
  }, [deleteTarget, onDelete])

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

  // --- Lineage filter ----------------------------------------------------

  const lineageFilteredRows = useMemo(() => {
    // Lineage filtering is applied by the consumer's data-fetching layer
    // in later phases.  The grid itself exposes the URL state; advanced
    // multi-level walk logic belongs in the route.  We pass rows through
    // unchanged here so the component stays data-source-agnostic.
    //
    // The lineage direction and recursive values are exposed via the search
    // props so parent routes can consume them in their query hooks.
    return kindFilteredRows
  }, [kindFilteredRows])

  // --- TanStack Table columns --------------------------------------------

  const columns = useMemo(
    () => [
      columnHelper.accessor('parentId', {
        id: 'parentId',
        header: 'Parent',
        cell: ({ row }) => (
          <span className="text-muted-foreground text-sm">
            {row.original.parentLabel || row.original.parentId}
          </span>
        ),
      }),
      columnHelper.accessor('id', {
        id: 'resourceId',
        header: 'Resource ID',
        cell: ({ getValue }) => (
          <span className="font-mono text-muted-foreground text-sm">
            {getValue()}
          </span>
        ),
      }),
      columnHelper.accessor(
        (row) => row.displayName || row.name,
        {
          id: 'displayName',
          header: 'Display Name',
          cell: ({ row }) => {
            const label = row.original.displayName || row.original.name
            if (row.original.detailHref) {
              return (
                <a
                  href={row.original.detailHref}
                  className="font-medium hover:underline"
                  title={row.original.name}
                >
                  {label}
                </a>
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
      columnHelper.accessor('createdAt', {
        id: 'createdAt',
        header: 'Created At',
        cell: ({ getValue }) => {
          const raw = getValue()
          try {
            return (
              <span className="text-muted-foreground text-sm whitespace-nowrap">
                {new Date(raw).toLocaleDateString()}
              </span>
            )
          } catch {
            return <span className="text-muted-foreground text-sm">{raw}</span>
          }
        },
      }),
      // Actions column — no accessor, uses the full row
      columnHelper.display({
        id: 'actions',
        header: '',
        cell: ({ row }) => (
          <div className="flex justify-end">
            <Button
              variant="ghost"
              size="icon"
              aria-label={`delete ${row.original.displayName || row.original.name}`}
              onClick={() => handleDeleteClick(row.original)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        ),
      }),
    ],
    [handleDeleteClick],
  )

  // --- TanStack Table instance -------------------------------------------

  const table = useReactTable({
    data: lineageFilteredRows,
    columns,
    state: { globalFilter, columnVisibility },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
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
          <CardTitle>{title}</CardTitle>
          <NewButton kinds={creatableKinds} />
        </CardHeader>
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

          {/* Filter toolbar */}
          <div className="mb-3 flex flex-col sm:flex-row gap-2 sm:items-center flex-wrap">
            <Input
              placeholder={`Search ${title.toLowerCase()}…`}
              value={globalFilter}
              onChange={(e) => setGlobalFilter(e.target.value)}
              className="max-w-sm"
              aria-label={`Search ${title}`}
            />

            {/* Multi-kind filter — only when more than one kind */}
            {kinds.length > 1 && (
              <KindFilter
                kinds={kinds}
                selectedKindIds={selectedKindIds}
                onChange={setKindIds}
              />
            )}

            {/* Lineage filter */}
            <LineageFilterControl
              direction={lineageDirection}
              recursive={recursive}
              onDirectionChange={setLineageDirection}
              onRecursiveChange={setRecursive}
            />
          </div>

          {/* Empty state */}
          {rows.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No resources found.</p>
            </div>
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
                    <TableRow key={row.id}>
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
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
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

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

/** "New" button — single link when one kind, dropdown when multiple. */
function NewButton({ kinds }: { kinds: Kind[] }) {
  if (kinds.length === 0) return null

  if (kinds.length === 1) {
    const kind = kinds[0]
    return (
      <Link to={kind.newHref!}>
        <Button size="sm">New {kind.label}</Button>
      </Link>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm">
          New <ChevronDown className="ml-1 h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {kinds.map((kind) => (
          <DropdownMenuItem key={kind.id} asChild>
            <Link to={kind.newHref!}>{kind.label}</Link>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** Kind filter checkboxes — rendered only when kinds.length > 1. */
function KindFilter({
  kinds,
  selectedKindIds,
  onChange,
}: {
  kinds: Kind[]
  selectedKindIds: string[]
  onChange: (ids: string[]) => void
}) {
  const selectedSet = new Set(selectedKindIds)

  const toggle = (id: string) => {
    const next = new Set(selectedSet)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    onChange(Array.from(next))
  }

  return (
    <div
      className="flex flex-wrap gap-2 items-center"
      aria-label="Filter by kind"
      data-testid="kind-filter"
    >
      {kinds.map((kind) => (
        <div key={kind.id} className="flex items-center gap-1">
          <Checkbox
            id={`kind-${kind.id}`}
            checked={selectedSet.size === 0 || selectedSet.has(kind.id)}
            onCheckedChange={() => toggle(kind.id)}
            aria-label={`Filter ${kind.label}`}
          />
          <Label htmlFor={`kind-${kind.id}`} className="text-sm cursor-pointer">
            {kind.label}
          </Label>
        </div>
      ))}
    </div>
  )
}

/** Lineage direction select + recursive checkbox. */
function LineageFilterControl({
  direction,
  recursive,
  onDirectionChange,
  onRecursiveChange,
}: {
  direction: LineageDirection
  recursive: boolean
  onDirectionChange: (d: LineageDirection) => void
  onRecursiveChange: (r: boolean) => void
}) {
  return (
    <div
      className="flex items-center gap-2"
      aria-label="Lineage filter"
      data-testid="lineage-filter"
    >
      <Select
        value={direction}
        onValueChange={(v) => onDirectionChange(v as LineageDirection)}
      >
        <SelectTrigger className="w-[160px]" aria-label="Lineage direction">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="ancestors">Ancestors</SelectItem>
          <SelectItem value="descendants">Descendants</SelectItem>
          <SelectItem value="both">Both</SelectItem>
        </SelectContent>
      </Select>
      <div className="flex items-center gap-1">
        <Checkbox
          id="lineage-recursive"
          checked={recursive}
          onCheckedChange={(checked) => onRecursiveChange(checked === true)}
          aria-label="Recursive lineage"
          data-testid="lineage-recursive-checkbox"
        />
        <Label
          htmlFor="lineage-recursive"
          className="text-sm cursor-pointer"
        >
          Recursive
        </Label>
      </div>
    </div>
  )
}

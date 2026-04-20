import { useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  flexRender,
  createColumnHelper,
} from '@tanstack/react-table'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
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
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useAllTemplatePoliciesForOrg } from '@/queries/templatePolicies'
import type { TemplatePolicy } from '@/queries/templatePolicies'
import { useGetOrganization } from '@/queries/organizations'
import {
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
} from '@/lib/scope-labels'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/template-policies/',
)({
  component: OrgTemplatePoliciesIndexRoute,
})

function OrgTemplatePoliciesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatePoliciesIndexPage orgName={orgName} />
}

const columnHelper = createColumnHelper<TemplatePolicy>()

export function OrgTemplatePoliciesIndexPage({
  orgName: propOrgName,
}: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const { data: policies, isPending, error } = useAllTemplatePoliciesForOrg(orgName)
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too.
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  const [globalFilter, setGlobalFilter] = useState('')

  const rows = useMemo(() => policies ?? [], [policies])

  const columns = useMemo(
    () => [
      columnHelper.accessor((row) => row.displayName || row.name, {
        id: 'displayName',
        header: 'Display Name',
        cell: ({ row }) => {
          const p = row.original
          const label = p.displayName || p.name
          const scope = scopeLabelFromNamespace(p.namespace)
          // HOL-590 guarantees policies live only at org or folder scope.
          // If the server ever surfaces a project-scoped or unprefixed
          // namespace (stale cache, proto drift) we render a plain cell
          // rather than forging a link to a page that will 404.
          if (scope === 'folder') {
            const folderName = scopeNameFromNamespace(p.namespace)
            if (folderName) {
              return (
                <Link
                  to="/folders/$folderName/template-policies/$policyName"
                  params={{ folderName, policyName: p.name }}
                  title={p.name}
                  className="hover:underline font-medium"
                >
                  {label}
                </Link>
              )
            }
          } else if (scope === 'org') {
            return (
              <Link
                to="/orgs/$orgName/template-policies/$policyName"
                params={{ orgName, policyName: p.name }}
                title={p.name}
                className="hover:underline font-medium"
              >
                {label}
              </Link>
            )
          }
          return (
            <span className="font-medium" title={p.name}>
              {label}
            </span>
          )
        },
      }),
      columnHelper.accessor('namespace', {
        header: 'Namespace',
        cell: ({ getValue }) => (
          <span className="text-muted-foreground font-mono text-sm">
            {getValue()}
          </span>
        ),
      }),
      columnHelper.accessor('name', {
        header: 'Name',
        cell: ({ getValue }) => (
          <span className="text-muted-foreground font-mono text-sm">
            {getValue()}
          </span>
        ),
      }),
    ],
    [orgName],
  )

  const table = useReactTable({
    data: rows,
    columns,
    state: { globalFilter },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })

  if (isPending) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Template Policies</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2" data-testid="policies-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        </CardContent>
      </Card>
    )
  }

  if (error) {
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

  return (
    <Card>
      <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
        <div>
          <p className="text-sm text-muted-foreground">
            {orgName} / Template Policies
          </p>
          <CardTitle className="mt-1">Template Policies</CardTitle>
        </div>
        {canWrite && (
          <Link to="/orgs/$orgName/template-policies/new" params={{ orgName }}>
            <Button size="sm">Create Policy</Button>
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-6 text-center">
            <p className="text-sm font-medium">No template policies yet.</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Policies attach templates to projects through REQUIRE or EXCLUDE
              rules. Rules apply to both project templates and deployments.
              Policies live only at folder or organization scope.
            </p>
          </div>
        ) : (
          <>
            <div className="mb-3">
              <Input
                placeholder="Search policies…"
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="max-w-sm"
                aria-label="Search template policies"
              />
            </div>
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
  )
}

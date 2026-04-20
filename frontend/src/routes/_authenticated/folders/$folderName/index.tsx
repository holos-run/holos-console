import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Settings } from 'lucide-react'
import { useGetFolder } from '@/queries/folders'
import { useListTemplates } from '@/queries/templates'
import { useListTemplatePolicies } from '@/queries/templatePolicies'
import { useListProjectsByParent } from '@/queries/projects'
import { namespaceForFolder } from '@/lib/scope-labels'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import type { Template } from '@/gen/holos/console/v1/templates_pb'
import type { TemplatePolicy } from '@/gen/holos/console/v1/template_policies_pb'
import type { Project } from '@/gen/holos/console/v1/projects_pb'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/',
)({
  component: FolderIndexRoute,
})

function FolderIndexRoute() {
  const { folderName } = Route.useParams()
  return <FolderIndexPage folderName={folderName} />
}

// The summary list caps each section at this many items. Deep browsing
// happens via the per-section "View all" link.
const SECTION_PREVIEW_LIMIT = 5

export function FolderIndexPage({
  folderName: propFolderName,
}: { folderName?: string } = {}) {
  let routeFolderName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeFolderName = Route.useParams().folderName
  } catch {
    routeFolderName = undefined
  }
  const folderName = propFolderName ?? routeFolderName ?? ''

  const {
    data: folder,
    isPending: folderPending,
    error: folderError,
  } = useGetFolder(folderName)
  const orgName = folder?.organization ?? ''
  const namespace = namespaceForFolder(folderName)

  const templatesQuery = useListTemplates(namespace)
  const policiesQuery = useListTemplatePolicies(namespace)
  // Projects fan out by parent reference; the RPC filter is non-recursive
  // by construction — it only returns projects whose immediate parent is
  // this folder, never grandchildren.
  const projectsQuery = useListProjectsByParent(
    orgName,
    ParentType.FOLDER,
    folderName,
  )

  if (folderError) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{folderError.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  if (folderPending) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  const displayName = folder?.displayName || folderName

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <div>
            <p className="text-sm text-muted-foreground">
              <Link
                to="/orgs/$orgName/settings"
                params={{ orgName }}
                className="hover:underline"
              >
                {orgName}
              </Link>
              {' / '}
              <Link
                to="/orgs/$orgName/folders"
                params={{ orgName }}
                className="hover:underline"
              >
                Folders
              </Link>
              {' / '}
              {folderName}
            </p>
            <CardTitle className="mt-1">{displayName}</CardTitle>
          </div>
          <Link
            to="/folders/$folderName/settings"
            params={{ folderName }}
            aria-label="Settings"
          >
            <Button variant="outline" size="sm">
              <Settings className="h-4 w-4 mr-1" />
              Settings
            </Button>
          </Link>
        </CardHeader>
      </Card>

      <TemplatesSection
        folderName={folderName}
        templates={templatesQuery.data}
        isPending={templatesQuery.isPending}
        error={templatesQuery.error as Error | null}
      />
      <TemplatePoliciesSection
        folderName={folderName}
        policies={policiesQuery.data}
        isPending={policiesQuery.isPending}
        error={policiesQuery.error as Error | null}
      />
      <ProjectsSection
        orgName={orgName}
        projects={projectsQuery.data}
        isPending={projectsQuery.isPending}
        error={projectsQuery.error as Error | null}
      />
    </div>
  )
}

interface SectionCardProps {
  title: string
  count: number | undefined
  isPending: boolean
  error: Error | null
  emptyText: string
  viewAll: React.ReactNode
  children: React.ReactNode
}

function SectionCard({
  title,
  count,
  isPending,
  error,
  emptyText,
  viewAll,
  children,
}: SectionCardProps) {
  return (
    <Card>
      <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
        <CardTitle className="flex items-center gap-2">
          {title}
          {typeof count === 'number' && (
            <Badge variant="secondary" aria-label={`${count} total`}>
              {count}
            </Badge>
          )}
        </CardTitle>
        {viewAll}
      </CardHeader>
      <CardContent>
        {isPending ? (
          <div className="space-y-2" data-testid={`${title.toLowerCase().replace(/\s+/g, '-')}-loading`}>
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : error ? (
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        ) : count === 0 ? (
          <p className="text-sm text-muted-foreground">{emptyText}</p>
        ) : (
          children
        )}
      </CardContent>
    </Card>
  )
}

function TemplatesSection({
  folderName,
  templates,
  isPending,
  error,
}: {
  folderName: string
  templates: Template[] | undefined
  isPending: boolean
  error: Error | null
}) {
  const preview = (templates ?? []).slice(0, SECTION_PREVIEW_LIMIT)
  return (
    <SectionCard
      title="Templates"
      count={templates?.length}
      isPending={isPending}
      error={error}
      emptyText="No templates in this folder."
      viewAll={
        <Link
          to="/folders/$folderName/templates"
          params={{ folderName }}
        >
          <Button variant="outline" size="sm">
            View all
          </Button>
        </Link>
      }
    >
      <ul className="divide-y divide-border">
        {preview.map((t) => (
          <li
            key={t.name}
            className="flex items-center justify-between gap-3 py-2"
          >
            <Link
              to="/folders/$folderName/templates/$templateName"
              params={{ folderName, templateName: t.name }}
              className="font-medium hover:underline"
            >
              {t.displayName || t.name}
            </Link>
            {!t.enabled && (
              <Badge variant="outline" className="text-muted-foreground">
                Disabled
              </Badge>
            )}
          </li>
        ))}
      </ul>
    </SectionCard>
  )
}

function TemplatePoliciesSection({
  folderName,
  policies,
  isPending,
  error,
}: {
  folderName: string
  policies: TemplatePolicy[] | undefined
  isPending: boolean
  error: Error | null
}) {
  const preview = (policies ?? []).slice(0, SECTION_PREVIEW_LIMIT)
  return (
    <SectionCard
      title="Template Policies"
      count={policies?.length}
      isPending={isPending}
      error={error}
      emptyText="No template policies in this folder."
      viewAll={
        <Link
          to="/folders/$folderName/template-policies"
          params={{ folderName }}
        >
          <Button variant="outline" size="sm">
            View all
          </Button>
        </Link>
      }
    >
      <ul className="divide-y divide-border">
        {preview.map((p) => (
          <li
            key={p.name}
            className="flex items-center justify-between gap-3 py-2"
          >
            <Link
              to="/folders/$folderName/template-policies/$policyName"
              params={{ folderName, policyName: p.name }}
              className="font-medium hover:underline"
            >
              {p.name}
            </Link>
          </li>
        ))}
      </ul>
    </SectionCard>
  )
}

function ProjectsSection({
  orgName,
  projects,
  isPending,
  error,
}: {
  orgName: string
  projects: Project[] | undefined
  isPending: boolean
  error: Error | null
}) {
  const preview = (projects ?? []).slice(0, SECTION_PREVIEW_LIMIT)
  return (
    <SectionCard
      title="Projects"
      count={projects?.length}
      isPending={isPending}
      error={error}
      emptyText="No projects in this folder."
      viewAll={
        <Link to="/orgs/$orgName/projects" params={{ orgName }}>
          <Button variant="outline" size="sm">
            View all
          </Button>
        </Link>
      }
    >
      <ul className="divide-y divide-border">
        {preview.map((p) => (
          <li
            key={p.name}
            className="flex items-center justify-between gap-3 py-2"
          >
            <Link
              to="/projects/$projectName"
              params={{ projectName: p.name }}
              className="font-medium hover:underline"
            >
              {p.displayName || p.name}
            </Link>
          </li>
        ))}
      </ul>
    </SectionCard>
  )
}

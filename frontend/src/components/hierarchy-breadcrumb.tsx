import { Link } from '@tanstack/react-router'
import { ChevronRight } from 'lucide-react'

export interface BreadcrumbItem {
  /** Human-readable label for this breadcrumb segment. */
  label: string
  /** If provided, the breadcrumb item is rendered as a clickable link. */
  href?: string
  /** TanStack Router `to` path (used alongside `params`). */
  to?: string
  /** TanStack Router route params. */
  params?: Record<string, string>
}

export interface HierarchyBreadcrumbProps {
  items: BreadcrumbItem[]
  className?: string
}

/**
 * HierarchyBreadcrumb renders an ancestor chain as clickable breadcrumb links.
 *
 * Usage example (Org > Folder > Project > current page):
 *
 * ```tsx
 * <HierarchyBreadcrumb
 *   items={[
 *     { label: 'my-org', to: '/orgs/$orgName/settings/', params: { orgName: 'my-org' } },
 *     { label: 'payments', to: '/orgs/$orgName/folders/$folderName', params: { orgName: 'my-org', folderName: 'payments' } },
 *     { label: 'checkout', to: '/projects/$projectName/secrets', params: { projectName: 'checkout' } },
 *     { label: 'Secrets' },  // current page — no link
 *   ]}
 * />
 * ```
 */
export function HierarchyBreadcrumb({ items, className }: HierarchyBreadcrumbProps) {
  if (items.length === 0) return null

  return (
    <nav aria-label="breadcrumb" className={className}>
      <ol className="flex items-center flex-wrap gap-1 text-sm text-muted-foreground">
        {items.map((item, index) => {
          const isLast = index === items.length - 1
          return (
            <li key={index} className="flex items-center gap-1">
              {item.to ? (
                <Link
                  to={item.to}
                  params={item.params}
                  className="hover:text-foreground transition-colors"
                >
                  {item.label}
                </Link>
              ) : item.href ? (
                <a href={item.href} className="hover:text-foreground transition-colors">
                  {item.label}
                </a>
              ) : (
                <span className={isLast ? 'text-foreground font-medium' : undefined}>
                  {item.label}
                </span>
              )}
              {!isLast && <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}

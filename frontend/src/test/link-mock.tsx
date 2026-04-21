// linkMock is a test-only TanStack Router Link stub shared across index-page
// tests. It renders an <a> whose href is the `to` pattern with $param tokens
// replaced by the corresponding `params` values. All props forwarded by
// resource / folder / template index pages (title, className, aria-label) are
// preserved so asserting on them works without per-file duplication.
import React from 'react'

export function LinkMock({
  children,
  to,
  params,
  title,
  className,
  'aria-label': ariaLabel,
}: {
  children: React.ReactNode
  to: string
  params?: Record<string, string>
  title?: string
  className?: string
  'aria-label'?: string
}) {
  let href = to
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      href = href.replace(`$${k}`, v)
    }
  }
  return (
    <a href={href} title={title} className={className} aria-label={ariaLabel}>
      {children}
    </a>
  )
}

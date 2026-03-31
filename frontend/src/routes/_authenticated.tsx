import { createFileRoute, Outlet } from '@tanstack/react-router'
import { useAuth } from '@/lib/auth'
import { useEffect, useRef, useState } from 'react'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { AppSidebar } from '@/components/app-sidebar'
import { OrgProvider } from '@/lib/org-context'
import { ProjectProvider } from '@/lib/project-context'
import { Separator } from '@/components/ui/separator'

export const Route = createFileRoute('/_authenticated')({
  component: AuthenticatedLayout,
})

export function AuthenticatedLayout() {
  const { isAuthenticated, isLoading, refreshTokens, login } = useAuth()
  const silentRenewAttempted = useRef(false)
  const [silentRenewPending, setSilentRenewPending] = useState(false)

  // Attempt silent token renewal once. If it succeeds, isAuthenticated flips
  // to true and we re-render with the sidebar. If it fails, redirect the user
  // through the OIDC auth flow so they land back at the same URL after login.
  useEffect(() => {
    if (!isLoading && !isAuthenticated && !silentRenewAttempted.current) {
      silentRenewAttempted.current = true
      setSilentRenewPending(true)
      refreshTokens()
        .catch(() => {
          login(window.location.pathname + window.location.search).catch(() => {})
        })
        .finally(() => setSilentRenewPending(false))
    }
  }, [isLoading, isAuthenticated, refreshTokens, login])

  if (isLoading || silentRenewPending || !isAuthenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>
    )
  }

  return (
    <OrgProvider>
      <ProjectProvider>
      <SidebarProvider>
        <AppSidebar />
        <SidebarInset>
          <header className="flex h-12 items-center gap-2 border-b px-4 md:hidden">
            <SidebarTrigger />
            <Separator orientation="vertical" className="h-4" />
            <span className="font-semibold">Holos Console</span>
          </header>
          <main className="flex-1 p-4 md:p-6">
            <Outlet />
          </main>
        </SidebarInset>
      </SidebarProvider>
      </ProjectProvider>
    </OrgProvider>
  )
}

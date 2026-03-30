import { createFileRoute, Outlet } from '@tanstack/react-router'
import { useAuth } from '@/lib/auth'
import { useEffect, useRef } from 'react'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { AppSidebar } from '@/components/app-sidebar'
import { OrgProvider } from '@/lib/org-context'
import { ProjectProvider } from '@/lib/project-context'
import { Separator } from '@/components/ui/separator'

export const Route = createFileRoute('/_authenticated')({
  component: AuthenticatedLayout,
})

export function AuthenticatedLayout() {
  const { isAuthenticated, isLoading, refreshTokens } = useAuth()
  const silentRenewAttempted = useRef(false)

  // Attempt silent token renewal once in the background.
  // If it succeeds, isAuthenticated flips to true and we re-render with sidebar.
  // If it fails, child routes handle auth (e.g., profile page shows "Sign In").
  useEffect(() => {
    if (!isLoading && !isAuthenticated && !silentRenewAttempted.current) {
      silentRenewAttempted.current = true
      refreshTokens().catch(() => {})
    }
  }, [isLoading, isAuthenticated, refreshTokens])

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return (
      <main className="flex-1 p-4 md:p-6">
        <Outlet />
      </main>
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

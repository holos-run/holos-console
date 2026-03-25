import { createFileRoute, Outlet } from '@tanstack/react-router'
import { useAuth } from '@/lib/auth'
import { useEffect, useRef, useState } from 'react'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { AppSidebar } from '@/components/app-sidebar'
import { OrgProvider } from '@/lib/org-context'
import { Separator } from '@/components/ui/separator'

export const Route = createFileRoute('/_authenticated')({
  component: AuthenticatedLayout,
})

export function AuthenticatedLayout() {
  const { isAuthenticated, isLoading, login, refreshTokens } = useAuth()
  const [isRefreshing, setIsRefreshing] = useState(false)
  const silentRenewAttempted = useRef(false)

  useEffect(() => {
    if (!isLoading && !isAuthenticated && !silentRenewAttempted.current) {
      silentRenewAttempted.current = true
      setIsRefreshing(true)
      refreshTokens()
        .catch(() => {
          login(window.location.pathname)
        })
        .finally(() => {
          setIsRefreshing(false)
        })
    }
  }, [isLoading, isAuthenticated, login, refreshTokens])

  if (isLoading || isRefreshing) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return null
  }

  return (
    <OrgProvider>
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
    </OrgProvider>
  )
}

import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { User } from 'oidc-client-ts'
import { getUserManager } from './userManager'
import { tokenRef } from '../client'

export interface AuthContextValue {
  // Current authenticated user, or null if not logged in
  user: User | null
  // True while checking initial auth state
  isLoading: boolean
  // Error from auth operations
  error: Error | null
  // True if user is authenticated
  isAuthenticated: boolean
  // Redirect to login page. Optional returnTo path for post-login redirect.
  login: (returnTo?: string) => Promise<void>
  // Log out and redirect
  logout: () => Promise<void>
  // Get the current access token (for API calls)
  getAccessToken: () => string | null
  // Trigger manual token refresh
  refreshTokens: () => Promise<void>
  // Last silent renew result
  lastRefreshStatus: 'idle' | 'success' | 'error'
  lastRefreshTime: Date | null
  lastRefreshError: Error | null
}

export const AuthContext = createContext<AuthContextValue | null>(null)

interface AuthProviderProps {
  children: ReactNode
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const [lastRefreshStatus, setLastRefreshStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [lastRefreshTime, setLastRefreshTime] = useState<Date | null>(null)
  const [lastRefreshError, setLastRefreshError] = useState<Error | null>(null)

  const userManager = useMemo(() => getUserManager(), [])

  // Check for existing session on mount
  useEffect(() => {
    const checkAuth = async () => {
      try {
        const existingUser = await userManager.getUser()
        if (existingUser && !existingUser.expired) {
          setUser(existingUser)
        }
      } catch (err) {
        console.error('Error checking auth state:', err)
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setIsLoading(false)
      }
    }

    checkAuth()
  }, [userManager])

  // Listen for user changes (e.g., token refresh)
  useEffect(() => {
    const handleUserLoaded = (loadedUser: User) => {
      setUser(loadedUser)
      setError(null)
      setLastRefreshStatus('success')
      setLastRefreshTime(new Date())
      setLastRefreshError(null)
    }

    const handleUserUnloaded = () => {
      setUser(null)
    }

    const handleSilentRenewError = (err: Error) => {
      console.error('Silent renew error:', err)
      setError(err)
      setLastRefreshStatus('error')
      setLastRefreshTime(new Date())
      setLastRefreshError(err)
    }

    userManager.events.addUserLoaded(handleUserLoaded)
    userManager.events.addUserUnloaded(handleUserUnloaded)
    userManager.events.addSilentRenewError(handleSilentRenewError)

    return () => {
      userManager.events.removeUserLoaded(handleUserLoaded)
      userManager.events.removeUserUnloaded(handleUserUnloaded)
      userManager.events.removeSilentRenewError(handleSilentRenewError)
    }
  }, [userManager])

  // Keep the shared tokenRef in sync so the transport interceptor can inject
  // the Authorization header on every outgoing RPC.
  useEffect(() => {
    tokenRef.current = user?.access_token ?? null
  }, [user])

  const login = useCallback(
    async (returnTo?: string) => {
      try {
        setError(null)
        const targetPath = returnTo ?? window.location.pathname
        await userManager.signinRedirect({ state: { returnTo: targetPath } })
      } catch (err) {
        console.error('Login error:', err)
        setError(err instanceof Error ? err : new Error(String(err)))
        throw err
      }
    },
    [userManager],
  )

  const logout = useCallback(async () => {
    try {
      setError(null)
      await userManager.signoutRedirect()
    } catch (err) {
      console.error('Logout error:', err)
      setError(err instanceof Error ? err : new Error(String(err)))
      throw err
    }
  }, [userManager])

  const getAccessToken = useCallback(() => {
    return user?.access_token ?? null
  }, [user])

  const refreshTokens = useCallback(async () => {
    try {
      setLastRefreshStatus('idle')
      const refreshedUser = await userManager.signinSilent()
      if (refreshedUser) {
        setUser(refreshedUser)
        setLastRefreshStatus('success')
        setLastRefreshTime(new Date())
        setLastRefreshError(null)
      }
    } catch (err) {
      setLastRefreshStatus('error')
      setLastRefreshTime(new Date())
      const error = err instanceof Error ? err : new Error(String(err))
      setLastRefreshError(error)
      throw error
    }
  }, [userManager])

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      isLoading,
      error,
      isAuthenticated: !!user && !user.expired,
      login,
      logout,
      getAccessToken,
      refreshTokens,
      lastRefreshStatus,
      lastRefreshTime,
      lastRefreshError,
    }),
    [user, isLoading, error, login, logout, getAccessToken, refreshTokens, lastRefreshStatus, lastRefreshTime, lastRefreshError],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

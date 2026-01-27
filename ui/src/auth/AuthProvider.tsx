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
import { isBFFMode, BFF_ENDPOINTS } from './config'

// BFF mode user type (simpler than oidc-client-ts User)
export interface BFFUser {
  user: string
  email: string
  groups?: string[]
}

export interface AuthContextValue {
  // Current authenticated user, or null if not logged in
  user: User | null
  // BFF mode user info (null in development mode)
  bffUser: BFFUser | null
  // True if running in BFF mode (behind oauth2-proxy)
  isBFF: boolean
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
  // Get the current access token (for API calls, development mode only)
  getAccessToken: () => string | null
  // Trigger manual token refresh (development mode only)
  refreshTokens: () => Promise<void>
  // Last silent renew result (development mode only)
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
  const [bffUser, setBffUser] = useState<BFFUser | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const [lastRefreshStatus, setLastRefreshStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [lastRefreshTime, setLastRefreshTime] = useState<Date | null>(null)
  const [lastRefreshError, setLastRefreshError] = useState<Error | null>(null)

  // Check BFF mode once on mount
  const [isBFF] = useState(() => isBFFMode())

  // Use shared UserManager singleton (only in non-BFF mode)
  const userManager = useMemo(() => (isBFF ? null : getUserManager()), [isBFF])

  // Check for existing session on mount
  useEffect(() => {
    const checkAuth = async () => {
      try {
        if (isBFF) {
          // BFF mode: check /api/userinfo
          const response = await fetch(BFF_ENDPOINTS.userInfo, {
            credentials: 'include', // Include cookies
          })
          if (response.ok) {
            const data = await response.json()
            setBffUser(data)
          }
        } else {
          // Development mode: use oidc-client-ts
          const existingUser = await userManager!.getUser()
          if (existingUser && !existingUser.expired) {
            setUser(existingUser)
          }
        }
      } catch (err) {
        console.error('Error checking auth state:', err)
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setIsLoading(false)
      }
    }

    checkAuth()
  }, [isBFF, userManager])

  // Listen for user changes (e.g., token refresh) - development mode only
  useEffect(() => {
    if (isBFF || !userManager) return

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
  }, [isBFF, userManager])

  const login = useCallback(
    async (returnTo?: string) => {
      try {
        setError(null)
        if (isBFF) {
          // BFF mode: redirect to oauth2-proxy login endpoint
          const returnUrl = returnTo ?? window.location.pathname
          window.location.href = `${BFF_ENDPOINTS.login}?rd=${encodeURIComponent(returnUrl)}`
        } else {
          // Development mode: use oidc-client-ts
          const targetPath = returnTo ?? window.location.pathname
          await userManager!.signinRedirect({ state: { returnTo: targetPath } })
        }
      } catch (err) {
        console.error('Login error:', err)
        setError(err instanceof Error ? err : new Error(String(err)))
        throw err
      }
    },
    [isBFF, userManager],
  )

  const logout = useCallback(async () => {
    try {
      setError(null)
      if (isBFF) {
        // BFF mode: redirect to oauth2-proxy logout endpoint
        window.location.href = BFF_ENDPOINTS.logout
      } else {
        // Development mode: use oidc-client-ts
        await userManager!.signoutRedirect()
      }
    } catch (err) {
      console.error('Logout error:', err)
      setError(err instanceof Error ? err : new Error(String(err)))
      throw err
    }
  }, [isBFF, userManager])

  const getAccessToken = useCallback(() => {
    // In BFF mode, tokens are managed server-side
    if (isBFF) return null
    return user?.access_token ?? null
  }, [isBFF, user])

  const refreshTokens = useCallback(async () => {
    if (isBFF) {
      // BFF mode: no manual refresh needed, proxy handles it
      console.warn('Manual refresh not available in BFF mode')
      return
    }

    try {
      setLastRefreshStatus('idle')
      const refreshedUser = await userManager!.signinSilent()
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
  }, [isBFF, userManager])

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      bffUser,
      isBFF,
      isLoading,
      error,
      isAuthenticated: isBFF ? !!bffUser : (!!user && !user.expired),
      login,
      logout,
      getAccessToken,
      refreshTokens,
      lastRefreshStatus,
      lastRefreshTime,
      lastRefreshError,
    }),
    [user, bffUser, isBFF, isLoading, error, login, logout, getAccessToken, refreshTokens, lastRefreshStatus, lastRefreshTime, lastRefreshError],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

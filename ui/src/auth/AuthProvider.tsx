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

export interface AuthContextValue {
  // Current authenticated user, or null if not logged in
  user: User | null
  // True while checking initial auth state
  isLoading: boolean
  // Error from auth operations
  error: Error | null
  // True if user is authenticated
  isAuthenticated: boolean
  // Redirect to login page
  login: () => Promise<void>
  // Log out and redirect
  logout: () => Promise<void>
  // Get the current access token (for API calls)
  getAccessToken: () => string | null
}

export const AuthContext = createContext<AuthContextValue | null>(null)

interface AuthProviderProps {
  children: ReactNode
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Use shared UserManager singleton
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
    }

    const handleUserUnloaded = () => {
      setUser(null)
    }

    const handleSilentRenewError = (err: Error) => {
      console.error('Silent renew error:', err)
      setError(err)
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

  const login = useCallback(async () => {
    try {
      setError(null)
      await userManager.signinRedirect()
    } catch (err) {
      console.error('Login error:', err)
      setError(err instanceof Error ? err : new Error(String(err)))
      throw err
    }
  }, [userManager])

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

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      isLoading,
      error,
      isAuthenticated: !!user && !user.expired,
      login,
      logout,
      getAccessToken,
    }),
    [user, isLoading, error, login, logout, getAccessToken],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

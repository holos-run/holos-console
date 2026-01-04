import { useContext } from 'react'
import { AuthContext, type AuthContextValue } from './AuthProvider'

/**
 * Hook to access authentication state and methods.
 *
 * @example
 * ```tsx
 * function LoginButton() {
 *   const { isAuthenticated, login, logout, user } = useAuth()
 *
 *   if (isAuthenticated) {
 *     return (
 *       <button onClick={logout}>
 *         Logout {user?.profile.name}
 *       </button>
 *     )
 *   }
 *
 *   return <button onClick={login}>Login</button>
 * }
 * ```
 */
export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}

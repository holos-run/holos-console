import { UserManager } from 'oidc-client-ts'
import { getOIDCSettings } from './config'

let userManagerInstance: UserManager | null = null

/**
 * Get the singleton UserManager instance.
 * Creates the instance on first call (lazy initialization).
 *
 * Using a singleton ensures AuthProvider and Callback share the same
 * UserManager, so events like userLoaded propagate correctly.
 */
export function getUserManager(): UserManager {
  if (!userManagerInstance) {
    userManagerInstance = new UserManager(getOIDCSettings())
  }
  return userManagerInstance
}

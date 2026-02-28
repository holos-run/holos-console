import { UserManager } from 'oidc-client-ts'
import { getOIDCSettings } from './config'

let userManagerInstance: UserManager | null = null

export function getUserManager(): UserManager {
  if (!userManagerInstance) {
    userManagerInstance = new UserManager(getOIDCSettings())
  }
  return userManagerInstance
}

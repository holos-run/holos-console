import type { UserManagerSettings } from 'oidc-client-ts'
import { WebStorageStateStore } from 'oidc-client-ts'

interface OIDCConfig {
  authority: string
  client_id: string
  redirect_uri: string
  post_logout_redirect_uri: string
}

declare global {
  interface Window {
    __OIDC_CONFIG__?: OIDCConfig
  }
}

function getConfig(): OIDCConfig {
  if (window.__OIDC_CONFIG__) {
    return window.__OIDC_CONFIG__
  }

  // Fallback for edge cases (should not happen in normal operation)
  console.warn('OIDC config not injected, using origin-based fallback')
  const origin = window.location.origin
  return {
    authority: `${origin}/dex`,
    client_id: 'holos-console',
    redirect_uri: `${origin}/pkce/verify`,
    post_logout_redirect_uri: `${origin}/`,
  }
}

export function getOIDCSettings(): UserManagerSettings {
  const config = getConfig()

  return {
    authority: config.authority,
    client_id: config.client_id,
    redirect_uri: config.redirect_uri,
    post_logout_redirect_uri: config.post_logout_redirect_uri,
    response_type: 'code',
    scope: 'openid profile email groups offline_access',
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
    automaticSilentRenew: true,
    loadUserInfo: true,
  }
}

import type { UserManagerSettings } from 'oidc-client-ts'
import { WebStorageStateStore } from 'oidc-client-ts'

// OIDC configuration for the holos-console SPA.
// Uses the embedded Dex OIDC provider by default.

// BFF (Backend For Frontend) mode detection and endpoints.
// When running behind oauth2-proxy, authentication is handled by the proxy
// and tokens are managed server-side via cookies.

/**
 * Check if running behind oauth2-proxy (BFF mode).
 * oauth2-proxy sets an _oauth2_proxy cookie when user is authenticated.
 */
export function isBFFMode(): boolean {
  return document.cookie.includes('_oauth2_proxy')
}

/**
 * BFF mode endpoints (oauth2-proxy standard paths)
 */
export const BFF_ENDPOINTS = {
  // Initiate login - redirects to OIDC provider
  login: '/oauth2/start',
  // Logout - clears session and optionally redirects to OIDC logout
  logout: '/oauth2/sign_out',
  // Get user info from forwarded headers (requires backend endpoint)
  userInfo: '/api/userinfo',
} as const

// Read config from window.__OIDC_CONFIG__ if injected by server,
// otherwise use development defaults.
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
  // Config must be injected by server (production) or Vite plugin (development)
  if (window.__OIDC_CONFIG__) {
    return window.__OIDC_CONFIG__
  }

  // Fallback for edge cases (should not happen in normal operation)
  console.warn('OIDC config not injected, using origin-based fallback')
  const origin = window.location.origin
  return {
    authority: `${origin}/dex`,
    client_id: 'holos-console',
    redirect_uri: `${origin}/ui/callback`,
    post_logout_redirect_uri: `${origin}/ui`,
  }
}

export function getOIDCSettings(): UserManagerSettings {
  const config = getConfig()

  return {
    authority: config.authority,
    client_id: config.client_id,
    redirect_uri: config.redirect_uri,
    post_logout_redirect_uri: config.post_logout_redirect_uri,

    // No silent_redirect_uri: oidc-client-ts uses refresh tokens (from the
    // offline_access scope) instead of iframes for silent renewal. See ADR 004.

    // PKCE is required for public clients (SPAs)
    response_type: 'code',

    // Request openid, profile, email, groups, and offline_access scopes
    // groups scope requests group memberships from the identity provider
    // offline_access requests a refresh token for silent renewal
    scope: 'openid profile email groups offline_access',

    // Use session storage to survive page refreshes but not browser restarts
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),

    // Automatically renew tokens before they expire
    automaticSilentRenew: true,

    // Load user info from userinfo endpoint
    loadUserInfo: true,
  }
}

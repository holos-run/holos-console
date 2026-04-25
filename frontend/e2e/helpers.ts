import type { Page } from '@playwright/test'

// Default credentials for embedded Dex OIDC provider
export const DEFAULT_USERNAME = 'admin'
export const DEFAULT_PASSWORD = 'verysecret'

// Persona email constants matching TestUsers in console/oidc/config.go.
export const ADMIN_EMAIL = 'admin@localhost'
export const PLATFORM_ENGINEER_EMAIL = 'platform@localhost'
export const PRODUCT_ENGINEER_EMAIL = 'product@localhost'
export const SRE_EMAIL = 'sre@localhost'

/** Response from the POST /api/dev/token endpoint. */
interface TokenExchangeResponse {
  id_token: string
  email: string
  groups: string[]
  expires_in: number
}

/**
 * Build a Dex OIDC authorize URL with PKCE parameters.
 */
export function buildAuthorizeUrl(): string {
  const url = new URL('/dex/auth', 'https://localhost:5173')
  url.searchParams.set('client_id', 'holos-console')
  url.searchParams.set('redirect_uri', 'https://localhost:5173/pkce/verify')
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', 'openid profile email')
  url.searchParams.set('state', 'test_state')
  url.searchParams.set('code_challenge', 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
  url.searchParams.set('code_challenge_method', 'S256')
  return url.toString()
}

/**
 * Navigate past the Dex connector selection page if present.
 * Call this after landing on /dex/.
 */
async function navigatePastConnectorSelection(page: Page): Promise<void> {
  const connectorLink = page.locator('a[href*="connector"]').first()
  if ((await connectorLink.count()) > 0) {
    await connectorLink.click()
    await page.waitForLoadState('networkidle')
  }
}

/**
 * Navigate to the Dex authorize endpoint and wait for the login page.
 * Returns true if the Dex login form is shown, false if Dex auto-completed
 * (e.g., due to an existing server-side session).
 */
export async function navigateToDexLogin(page: Page): Promise<boolean> {
  await page.goto(buildAuthorizeUrl())
  await page.waitForURL(/\/dex\/|\/pkce\/verify/, { timeout: 5000 })

  if (!page.url().includes('/dex/')) {
    return false
  }

  await navigatePastConnectorSelection(page)
  return true
}

/**
 * Complete the full login flow via the profile page: navigate to /profile,
 * wait for the automatic OIDC redirect to Dex, fill credentials, and wait
 * for redirect back.
 *
 * After PR #230 the auth layout no longer shows a Sign In button — unauthenticated
 * users are automatically redirected through the OIDC flow. The auth layout
 * shows a spinner, attempts a silent token refresh, then calls login() which
 * triggers a full browser navigation to Dex.
 *
 * Handles two cases:
 * 1. Dex has no session: shows login form, fill credentials, submit
 * 2. Dex has existing session: auto-completes auth, redirects back immediately
 */
export async function loginViaProfilePage(page: Page): Promise<void> {
  await page.goto('/profile')
  // Wait for the OIDC redirect to Dex (or pkce/verify if Dex auto-completes).
  // Do NOT match /profile here — we're already at /profile and waitForURL
  // would resolve immediately without waiting for the Dex redirect.
  await page.waitForURL(/\/dex\/|\/pkce\/verify/, { timeout: 15000 })

  // If we landed on the Dex login form, fill credentials and submit
  if (page.url().includes('/dex/')) {
    await navigatePastConnectorSelection(page)

    await page.locator('input[name="login"]').fill(DEFAULT_USERNAME)
    await page.locator('input[name="password"]').fill(DEFAULT_PASSWORD)
    await page.locator('button[type="submit"]').click()
  }

  // Wait for redirect back to profile
  await page.waitForURL(/\/profile/, { timeout: 15000 })
}

/**
 * Extract the OIDC access token and user email from sessionStorage.
 */
async function getRpcAuth(page: Page): Promise<{ token: string; email: string }> {
  return page.evaluate(() => {
    const key = Object.keys(sessionStorage).find((k) => k.startsWith('oidc.user:'))
    if (!key) throw new Error('No OIDC session found in sessionStorage')
    const data = JSON.parse(sessionStorage.getItem(key)!) as {
      access_token?: string
      profile?: { email?: string }
    }
    if (!data?.access_token) throw new Error('No access_token in OIDC session')
    return { token: data.access_token, email: data.profile?.email ?? '' }
  })
}

/**
 * Create an organization via the RPC API.
 * The current user is added as owner.
 */
export async function apiCreateOrg(page: Page, name: string): Promise<void> {
  const { token, email } = await getRpcAuth(page)
  await page.evaluate(
    async ({ name, email, token }) => {
      const resp = await fetch('/holos.console.v1.OrganizationService/CreateOrganization', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name,
          displayName: name,
          userGrants: [{ principal: email, role: 3 }],
          roleGrants: [],
        }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`CreateOrganization failed (${resp.status}): ${text}`)
      }
    },
    { name, email, token },
  )
}

/**
 * Delete an organization via the RPC API.
 */
export async function apiDeleteOrg(page: Page, name: string): Promise<void> {
  const { token } = await getRpcAuth(page)
  await page.evaluate(
    async ({ name, token }) => {
      const resp = await fetch('/holos.console.v1.OrganizationService/DeleteOrganization', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`DeleteOrganization failed (${resp.status}): ${text}`)
      }
    },
    { name, token },
  )
}

/**
 * Create a project via the RPC API.
 * The current user is added as owner.
 */
export async function apiCreateProject(
  page: Page,
  name: string,
  organization: string,
): Promise<void> {
  const { token, email } = await getRpcAuth(page)
  await page.evaluate(
    async ({ name, organization, email, token }) => {
      const resp = await fetch('/holos.console.v1.ProjectService/CreateProject', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name,
          displayName: name,
          organization,
          userGrants: [{ principal: email, role: 3 }],
          roleGrants: [],
        }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`CreateProject failed (${resp.status}): ${text}`)
      }
    },
    { name, organization, email, token },
  )
}

/**
 * Delete a project via the RPC API.
 */
export async function apiDeleteProject(page: Page, name: string): Promise<void> {
  const { token } = await getRpcAuth(page)
  await page.evaluate(
    async ({ name, token }) => {
      const resp = await fetch('/holos.console.v1.ProjectService/DeleteProject', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`DeleteProject failed (${resp.status}): ${text}`)
      }
    },
    { name, token },
  )
}

/**
 * Create a folder via the RPC API.
 * parentType: 1 = ORGANIZATION, 2 = FOLDER
 */
export async function apiCreateFolder(
  page: Page,
  name: string,
  organization: string,
  parentType: 1 | 2,
  parentName: string,
): Promise<void> {
  const { token, email } = await getRpcAuth(page)
  await page.evaluate(
    async ({ name, organization, parentType, parentName, email, token }) => {
      const resp = await fetch('/holos.console.v1.FolderService/CreateFolder', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name,
          displayName: name,
          organization,
          parentType,
          parentName,
          userGrants: [{ principal: email, role: 3 }],
          roleGrants: [],
        }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`CreateFolder failed (${resp.status}): ${text}`)
      }
    },
    { name, organization, parentType, parentName, email, token },
  )
}

/**
 * Delete a folder via the RPC API.
 */
export async function apiDeleteFolder(
  page: Page,
  name: string,
  organization: string,
): Promise<void> {
  const { token } = await getRpcAuth(page)
  await page.evaluate(
    async ({ name, organization, token }) => {
      const resp = await fetch('/holos.console.v1.FolderService/DeleteFolder', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Connect-Protocol-Version': '1',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name, organization }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(`DeleteFolder failed (${resp.status}): ${text}`)
      }
    },
    { name, organization, token },
  )
}

/**
 * Select an org via the /organizations page. We navigate there, filter the
 * table by org name, and click the matching row. This both sets the org in
 * OrgContext and navigates to the org-scoped Projects listing.
 */
export async function selectOrg(page: Page, orgName: string): Promise<void> {
  await page.goto('/organizations')
  await page.waitForLoadState('networkidle')

  const search = page.getByPlaceholder(/search organizations/i)
  await search.waitFor({ timeout: 5000 })
  await search.fill(orgName)

  await page
    .getByRole('row')
    .filter({ hasText: orgName })
    .first()
    .click()

  await page.waitForURL(new RegExp(`/organizations/${orgName}/projects`), { timeout: 5000 })
}

/**
 * Get a valid ID token for a test persona via the dev token endpoint.
 * The backend must be running with --enable-insecure-dex for this endpoint
 * to be available.
 */
async function getPersonaToken(
  page: Page,
  email: string,
): Promise<TokenExchangeResponse> {
  return page.evaluate(async (email) => {
    const resp = await fetch('/api/dev/token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email }),
    })
    if (!resp.ok) {
      const text = await resp.text()
      throw new Error(`Token exchange failed (${resp.status}): ${text}`)
    }
    return resp.json()
  }, email)
}

/**
 * Inject a persona's OIDC token into sessionStorage so the app treats the
 * browser as authenticated for that user. This constructs an oidc-client-ts
 * compatible User object keyed by `oidc.user:{authority}:{client_id}`.
 *
 * Call page.reload() after this to pick up the new token.
 */
async function injectPersonaSession(
  page: Page,
  tokenData: TokenExchangeResponse,
): Promise<void> {
  await page.evaluate((data) => {
    // Discover the OIDC storage key pattern from existing session (if any),
    // or construct from the well-known defaults.
    const existingKey = Object.keys(sessionStorage).find((k) =>
      k.startsWith('oidc.user:'),
    )

    // Parse authority and client_id from the existing key, or use defaults.
    let authority: string
    let clientId: string
    if (existingKey) {
      const parts = existingKey.replace('oidc.user:', '').split(':')
      // Key format: oidc.user:<authority>:<client_id>
      // authority may contain colons (https://...), client_id is the last segment.
      clientId = parts.pop()!
      authority = parts.join(':')
    } else {
      authority = `${window.location.origin}/dex`
      clientId = 'holos-console'
    }

    // Clear all existing OIDC sessions
    Object.keys(sessionStorage)
      .filter((k) => k.startsWith('oidc.user:'))
      .forEach((k) => sessionStorage.removeItem(k))

    // Build an oidc-client-ts User-compatible object.
    // The token is an ID token from Dex; we use it as both id_token and
    // access_token since the backend verifies JWTs on the access_token header.
    const now = Math.floor(Date.now() / 1000)
    const user = {
      id_token: data.id_token,
      access_token: data.id_token,
      token_type: 'Bearer',
      scope: 'openid profile email groups',
      expires_at: now + data.expires_in,
      profile: {
        sub: '', // Will be filled by the token itself
        email: data.email,
        email_verified: true,
        groups: data.groups,
        iss: authority,
        aud: clientId,
        iat: now,
        exp: now + data.expires_in,
      },
    }

    const key = `oidc.user:${authority}:${clientId}`
    sessionStorage.setItem(key, JSON.stringify(user))
  }, tokenData)
}

/**
 * Switch the current browser session to a different persona.
 * Clears the existing OIDC session, fetches a new token via the dev endpoint,
 * injects it into sessionStorage, and reloads the page.
 *
 * The page must have already loaded the app (any route) so that
 * fetch('/api/dev/token') can reach the backend.
 */
async function switchPersona(page: Page, email: string): Promise<void> {
  const tokenData = await getPersonaToken(page, email)
  await injectPersonaSession(page, tokenData)
  await page.reload()
  await page.waitForLoadState('networkidle')
}

/**
 * Perform initial login as a specific persona. Navigates to /profile to
 * trigger the auto-login OIDC flow (which authenticates as admin by default),
 * then immediately switches to the requested persona via token exchange.
 *
 * Use this at the start of a test to begin authenticated as a specific persona.
 */
export async function loginAsPersona(page: Page, email: string): Promise<void> {
  // First, establish a session via the normal OIDC flow (logs in as admin).
  await loginViaProfilePage(page)
  // If the requested persona is admin, we are already done.
  if (email === ADMIN_EMAIL) return
  // Switch to the desired persona.
  await switchPersona(page, email)
}

/**
 * Grant a user a specific role on an organization via the RPC API.
 * Role values: 1 = VIEWER, 2 = EDITOR, 3 = OWNER.
 * Preserves existing grants by fetching the current sharing state first.
 */
export async function apiGrantOrgAccess(
  page: Page,
  orgName: string,
  principal: string,
  role: number,
): Promise<void> {
  const { token } = await getRpcAuth(page)
  await page.evaluate(
    async ({ orgName, principal, role, token }) => {
      // Fetch current org to get existing grants
      const getResp = await fetch(
        '/holos.console.v1.OrganizationService/GetOrganization',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Connect-Protocol-Version': '1',
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({ name: orgName }),
        },
      )
      if (!getResp.ok) {
        const text = await getResp.text()
        throw new Error(`GetOrganization failed (${getResp.status}): ${text}`)
      }
      const orgData = await getResp.json()
      const org = orgData.organization

      // Build updated user grants list: replace or add the target principal.
      const existingUserGrants: Array<{ principal: string; role: number }> =
        org?.userGrants ?? []
      const filtered = existingUserGrants.filter(
        (g: { principal: string }) => g.principal !== principal,
      )
      filtered.push({ principal, role })

      const resp = await fetch(
        '/holos.console.v1.OrganizationService/UpdateOrganizationSharing',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Connect-Protocol-Version': '1',
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({
            name: orgName,
            userGrants: filtered,
            roleGrants: org?.roleGrants ?? [],
          }),
        },
      )
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(
          `UpdateOrganizationSharing failed (${resp.status}): ${text}`,
        )
      }
    },
    { orgName, principal, role, token },
  )
}

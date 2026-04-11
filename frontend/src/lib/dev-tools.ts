/**
 * Dev Tools helpers: persona definitions and token exchange client.
 *
 * These are only used when devToolsEnabled is true in the console config
 * (controlled by the --enable-dev-tools server flag).
 */

/** A test persona available for switching in the dev tools panel. */
export interface Persona {
  /** Short identifier (e.g. "platform"). */
  id: string
  /** Human-readable label shown in the UI. */
  label: string
  /** Email address used for token exchange. */
  email: string
  /** OIDC groups assigned to this persona. */
  groups: string[]
  /** Role badge text derived from the groups. */
  role: 'Owner' | 'Editor' | 'Viewer'
}

/**
 * Static list of test personas matching the TestUsers defined in
 * console/oidc/config.go. The admin user is omitted because the persona
 * switcher focuses on the three workflow roles.
 */
export const personas: Persona[] = [
  {
    id: 'platform',
    label: 'Platform Engineer',
    email: 'platform@localhost',
    groups: ['owner'],
    role: 'Owner',
  },
  {
    id: 'product',
    label: 'Product Engineer',
    email: 'product@localhost',
    groups: ['editor'],
    role: 'Editor',
  },
  {
    id: 'sre',
    label: 'SRE',
    email: 'sre@localhost',
    groups: ['viewer'],
    role: 'Viewer',
  },
]

/** Response from the POST /api/dev/token endpoint. */
export interface TokenExchangeResponse {
  id_token: string
  email: string
  groups: string[]
  expires_in: number
}

/**
 * Exchange an email for a signed OIDC token via the dev token endpoint.
 * Throws on network or server errors.
 */
export async function exchangeToken(email: string): Promise<TokenExchangeResponse> {
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
}

/**
 * Derive a role label from OIDC groups. Returns the highest-privilege role
 * found in the groups array.
 */
export function roleFromGroups(groups: string[]): 'Owner' | 'Editor' | 'Viewer' {
  if (groups.includes('owner')) return 'Owner'
  if (groups.includes('editor')) return 'Editor'
  return 'Viewer'
}

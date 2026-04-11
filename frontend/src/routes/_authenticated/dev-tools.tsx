import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Copy, Eye, EyeOff } from 'lucide-react'
import { toast } from 'sonner'
import { useAuth } from '@/lib/auth'
import { getConsoleConfig } from '@/lib/console-config'
import { personas, exchangeToken, roleFromGroups, type Persona } from '@/lib/dev-tools'
import { getUserManager } from '@/lib/auth'
import { tokenRef } from '@/lib/transport'

export const Route = createFileRoute('/_authenticated/dev-tools')({
  component: DevToolsPage,
})

export function DevToolsPage() {
  const { devToolsEnabled } = getConsoleConfig()
  const { user, isAuthenticated, isLoading, login } = useAuth()
  const [switching, setSwitching] = useState<string | null>(null)
  const [tokenRevealed, setTokenRevealed] = useState(false)

  if (!devToolsEnabled) {
    return (
      <Card>
        <CardContent className="pt-6">
          <p className="text-muted-foreground">Dev tools are not enabled. Start the server with <code className="font-mono">--enable-dev-tools</code> to use this page.</p>
        </CardContent>
      </Card>
    )
  }

  if (isLoading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <span className="text-sm">Loading...</span>
        </CardContent>
      </Card>
    )
  }

  if (!isAuthenticated || !user) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <p className="text-muted-foreground">Sign in to use dev tools.</p>
          <Button onClick={() => login('/dev-tools')}>Sign In</Button>
        </CardContent>
      </Card>
    )
  }

  const profile = user.profile as Record<string, unknown> | undefined
  const email = profile?.email ? String(profile.email) : 'unknown'
  const sub = profile?.sub ? String(profile.sub) : 'unknown'
  const groups = Array.isArray(profile?.groups) ? (profile!.groups as string[]) : []
  const role = roleFromGroups(groups)

  const idToken = user.id_token ?? ''

  const handleSwitchPersona = async (persona: Persona) => {
    if (persona.email === email) return
    setSwitching(persona.id)
    try {
      const resp = await exchangeToken(persona.email)

      // Build a synthetic OIDC User object and store it in sessionStorage
      // under the key oidc-client-ts uses, then reload to pick up the new identity.
      const userManager = getUserManager()
      const settings = userManager.settings
      const storeKey = `oidc.user:${settings.authority}:${settings.client_id}`

      const syntheticUser = {
        id_token: resp.id_token,
        access_token: resp.id_token,
        token_type: 'Bearer',
        scope: 'openid profile email groups offline_access',
        expires_at: Math.floor(Date.now() / 1000) + resp.expires_in,
        profile: {
          iss: settings.authority,
          sub: `dev-${persona.id}`,
          aud: settings.client_id,
          exp: Math.floor(Date.now() / 1000) + resp.expires_in,
          iat: Math.floor(Date.now() / 1000),
          email: resp.email,
          email_verified: true,
          groups: resp.groups,
          name: persona.label,
        },
      }

      sessionStorage.setItem(storeKey, JSON.stringify(syntheticUser))
      tokenRef.current = resp.id_token
      window.location.reload()
    } catch (err) {
      toast.error(`Failed to switch persona: ${err instanceof Error ? err.message : String(err)}`)
      setSwitching(null)
    }
  }

  const handleCopyToken = () => {
    navigator.clipboard.writeText(idToken)
    toast.success('Copied to clipboard')
  }

  const origin = typeof window !== 'undefined' ? window.location.origin : 'https://localhost:8443'

  const curlCmd =
    `curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \\\n` +
    `  ${origin}/holos.console.v1.OrganizationService/ListOrganizations \\\n` +
    `  -H "Content-Type: application/json" \\\n` +
    `  -H "Connect-Protocol-Version: 1" \\\n` +
    `  -H "Authorization: Bearer ${idToken}" \\\n` +
    `  -d '{}'`

  const handleCopyCurl = () => {
    navigator.clipboard.writeText(curlCmd)
    toast.success('Copied to clipboard')
  }

  return (
    <div className="space-y-4">
      <CurrentIdentityCard
        email={email}
        sub={sub}
        groups={groups}
        role={role}
      />
      <PersonaSwitcherCard
        currentEmail={email}
        switching={switching}
        onSwitch={handleSwitchPersona}
      />
      <QuickTokenCard
        idToken={idToken}
        tokenRevealed={tokenRevealed}
        onToggleReveal={() => setTokenRevealed((v) => !v)}
        onCopyToken={handleCopyToken}
        curlCmd={curlCmd}
        onCopyCurl={handleCopyCurl}
      />
    </div>
  )
}

function CurrentIdentityCard({
  email,
  sub,
  groups,
  role,
}: {
  email: string
  sub: string
  groups: string[]
  role: string
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Current Identity</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground">Email</p>
          <p className="font-mono">{email}</p>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground">Subject</p>
          <p className="font-mono">{sub}</p>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground">Groups</p>
          <div className="flex flex-wrap gap-1 mt-1">
            {groups.length > 0 ? (
              groups.map((g) => (
                <Badge key={g} variant="outline">
                  {g}
                </Badge>
              ))
            ) : (
              <span className="text-muted-foreground">None</span>
            )}
          </div>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground">Role</p>
          <Badge variant="default">{role}</Badge>
        </div>
      </CardContent>
    </Card>
  )
}

function PersonaSwitcherCard({
  currentEmail,
  switching,
  onSwitch,
}: {
  currentEmail: string
  switching: string | null
  onSwitch: (persona: Persona) => void
}) {
  const roleBadgeVariant = (role: string): 'default' | 'secondary' | 'outline' => {
    if (role === 'Owner') return 'default'
    if (role === 'Editor') return 'secondary'
    return 'outline'
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Persona Switcher</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid gap-3 sm:grid-cols-3">
          {personas.map((persona) => {
            const isCurrent = persona.email === currentEmail
            const isSwitching = switching === persona.id
            return (
              <button
                key={persona.id}
                type="button"
                disabled={isCurrent || switching !== null}
                onClick={() => onSwitch(persona)}
                className={`rounded-lg border p-4 text-left transition-colors ${
                  isCurrent
                    ? 'border-primary bg-primary/10'
                    : 'border-border hover:border-primary/50 hover:bg-accent'
                } ${switching !== null && !isSwitching ? 'opacity-50' : ''}`}
              >
                <div className="flex items-center justify-between mb-2">
                  <span className="font-medium">{persona.label}</span>
                  <Badge variant={roleBadgeVariant(persona.role)}>{persona.role}</Badge>
                </div>
                <p className="text-sm text-muted-foreground font-mono">{persona.email}</p>
                {isCurrent && (
                  <p className="text-xs text-primary mt-2">Current</p>
                )}
                {isSwitching && (
                  <p className="text-xs text-muted-foreground mt-2">Switching...</p>
                )}
              </button>
            )
          })}
        </div>
      </CardContent>
    </Card>
  )
}

function QuickTokenCard({
  idToken,
  tokenRevealed,
  onToggleReveal,
  onCopyToken,
  curlCmd,
  onCopyCurl,
}: {
  idToken: string
  tokenRevealed: boolean
  onToggleReveal: () => void
  onCopyToken: () => void
  curlCmd: string
  onCopyCurl: () => void
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Quick Token</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">ID Token</p>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              aria-label={tokenRevealed ? 'Hide token' : 'Reveal token'}
              onClick={onToggleReveal}
            >
              {tokenRevealed ? (
                <>
                  <EyeOff className="h-3.5 w-3.5 mr-1" />
                  Hide
                </>
              ) : (
                <>
                  <Eye className="h-3.5 w-3.5 mr-1" />
                  Reveal
                </>
              )}
            </Button>
            <Button
              variant="outline"
              size="sm"
              aria-label="Copy token"
              onClick={onCopyToken}
            >
              <Copy className="h-3.5 w-3.5 mr-1" />
              Copy
            </Button>
          </div>
          <pre className="rounded-md bg-muted p-4 text-xs font-mono overflow-auto whitespace-pre break-all">
            {tokenRevealed ? idToken : '••••••••••••••••••••'}
          </pre>
        </div>

        <div className="space-y-2">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">
            curl example (Connect protocol)
          </p>
          <div className="relative">
            <pre className="rounded-md bg-muted p-4 text-xs font-mono overflow-auto whitespace-pre">
              {curlCmd}
            </pre>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Copy curl command"
              className="absolute top-2 right-2 h-7 w-7"
              onClick={onCopyCurl}
            >
              <Copy className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

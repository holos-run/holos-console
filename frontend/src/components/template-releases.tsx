import { useState, useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Plus, Tag } from 'lucide-react'
import { useListReleases, useCreateRelease } from '@/queries/templates'
import type { Release } from '@/queries/templates'

// Semver validation regex: major.minor.patch
const SEMVER_RE = /^\d+\.\d+\.\d+$/

// parseSemver extracts major, minor, patch from a version string.
function parseSemver(v: string): [number, number, number] | null {
  const m = SEMVER_RE.exec(v)
  if (!m) return null
  const parts = v.split('.')
  return [parseInt(parts[0], 10), parseInt(parts[1], 10), parseInt(parts[2], 10)]
}

// suggestVersions returns patch, minor, and major bump candidates from the latest version.
function suggestVersions(latest: string): { patch: string; minor: string; major: string } {
  const parsed = parseSemver(latest)
  if (!parsed) return { patch: '0.0.1', minor: '0.1.0', major: '1.0.0' }
  const [maj, min, pat] = parsed
  return {
    patch: `${maj}.${min}.${pat + 1}`,
    minor: `${maj}.${min + 1}.0`,
    major: `${maj + 1}.0.0`,
  }
}

// isMajorBump returns true when the new version is a major bump relative to the latest.
function isMajorBump(latest: string, next: string): boolean {
  const lp = parseSemver(latest)
  const np = parseSemver(next)
  if (!lp || !np) return false
  return np[0] > lp[0]
}

// truncateText truncates text to maxLen characters with ellipsis.
function truncateText(text: string, maxLen = 120): string {
  if (text.length <= maxLen) return text
  return text.slice(0, maxLen) + '...'
}

// formatDate formats a protobuf Timestamp into a locale date string.
function formatDate(ts: Release['createdAt']): string {
  if (!ts) return ''
  const ms = Number(ts.seconds) * 1000
  return new Date(ms).toLocaleDateString()
}

interface TemplateReleasesProps {
  namespace: string
  templateName: string
  canWrite: boolean
  /** Current CUE template source for creating a release from current state. */
  currentCueTemplate?: string
  /** Current template defaults for creating a release from current state. */
  currentDefaults?: Release['defaults']
}

export function TemplateReleases({ namespace, templateName, canWrite, currentCueTemplate, currentDefaults }: TemplateReleasesProps) {
  const { data: releases, isPending, error } = useListReleases(namespace, templateName)
  const createMutation = useCreateRelease(namespace, templateName)

  const [dialogOpen, setDialogOpen] = useState(false)
  const [version, setVersion] = useState('')
  const [versionBump, setVersionBump] = useState<'patch' | 'minor' | 'major' | 'custom'>('patch')
  const [changelog, setChangelog] = useState('')
  const [upgradeAdvice, setUpgradeAdvice] = useState('')
  const [validationError, setValidationError] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)

  // Sorted releases in descending version order (highest first).
  const sortedReleases = useMemo(() => {
    if (!releases) return []
    return [...releases].sort((a, b) => {
      const ap = parseSemver(a.version)
      const bp = parseSemver(b.version)
      if (!ap || !bp) return 0
      if (ap[0] !== bp[0]) return bp[0] - ap[0]
      if (ap[1] !== bp[1]) return bp[1] - ap[1]
      return bp[2] - ap[2]
    })
  }, [releases])

  const latestVersion = sortedReleases.length > 0 ? sortedReleases[0].version : ''
  const suggestions = suggestVersions(latestVersion)
  const existingVersions = new Set((releases ?? []).map((r) => r.version))

  // Determine whether to show upgrade advice (major bump relative to latest).
  const showUpgradeAdvice = latestVersion ? isMajorBump(latestVersion, version) : false

  const handleOpenDialog = () => {
    setVersion(suggestions.patch)
    setVersionBump('patch')
    setChangelog('')
    setUpgradeAdvice('')
    setValidationError(null)
    setSubmitError(null)
    setDialogOpen(true)
  }

  const handleBumpChange = (bump: 'patch' | 'minor' | 'major' | 'custom') => {
    setVersionBump(bump)
    if (bump === 'patch') setVersion(suggestions.patch)
    else if (bump === 'minor') setVersion(suggestions.minor)
    else if (bump === 'major') setVersion(suggestions.major)
    setValidationError(null)
  }

  const handleVersionChange = (v: string) => {
    setVersion(v)
    // If the typed version matches one of the suggestions, select that radio
    if (v === suggestions.patch) setVersionBump('patch')
    else if (v === suggestions.minor) setVersionBump('minor')
    else if (v === suggestions.major) setVersionBump('major')
    else setVersionBump('custom')
    setValidationError(null)
  }

  const handlePublish = async () => {
    setValidationError(null)
    setSubmitError(null)

    if (!SEMVER_RE.test(version)) {
      setValidationError('Version must be valid semver (e.g. 1.2.3)')
      return
    }
    if (existingVersions.has(version)) {
      setValidationError(`Version ${version} already exists`)
      return
    }

    try {
      await createMutation.mutateAsync({
        version,
        changelog,
        upgradeAdvice: showUpgradeAdvice ? upgradeAdvice : '',
        cueTemplate: currentCueTemplate ?? '',
        defaults: currentDefaults,
      })
      setDialogOpen(false)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">Releases</h3>
        {canWrite && (
          <Button variant="outline" size="sm" onClick={handleOpenDialog}>
            <Plus className="h-3.5 w-3.5 mr-1.5" />
            Create Release
          </Button>
        )}
      </div>
      <Separator />

      {isPending && <p className="text-sm text-muted-foreground">Loading releases...</p>}

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      )}

      {!isPending && !error && sortedReleases.length === 0 && (
        <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
          <Tag className="h-8 w-8 mb-2 opacity-50" />
          <p className="text-sm">No releases published yet.</p>
        </div>
      )}

      {sortedReleases.length > 0 && (
        <ul className="space-y-2">
          {sortedReleases.map((release, idx) => (
            <li
              key={release.version}
              className={`flex items-start gap-3 p-3 rounded-md border ${idx === 0 ? 'border-primary/40 bg-primary/5' : 'border-border'}`}
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium font-mono">{release.version}</span>
                  {idx === 0 && (
                    <Badge variant="secondary" className="text-xs">
                      Latest
                    </Badge>
                  )}
                  {release.createdAt && (
                    <span className="text-xs text-muted-foreground">{formatDate(release.createdAt)}</span>
                  )}
                </div>
                {release.changelog && (
                  <p className="text-xs text-muted-foreground mt-1">{truncateText(release.changelog)}</p>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Create Release</DialogTitle>
            <DialogDescription>
              Publish a new versioned release of this template.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            {/* Version bump radio options */}
            {latestVersion && (
              <fieldset className="space-y-2">
                <legend className="text-sm font-medium">Version bump</legend>
                <div className="flex gap-4">
                  <label className="flex items-center gap-1.5 text-sm cursor-pointer">
                    <input
                      type="radio"
                      name="version-bump"
                      value="patch"
                      checked={versionBump === 'patch'}
                      onChange={() => handleBumpChange('patch')}
                    />
                    {suggestions.patch} (patch)
                  </label>
                  <label className="flex items-center gap-1.5 text-sm cursor-pointer">
                    <input
                      type="radio"
                      name="version-bump"
                      value="minor"
                      checked={versionBump === 'minor'}
                      onChange={() => handleBumpChange('minor')}
                    />
                    {suggestions.minor} (minor)
                  </label>
                  <label className="flex items-center gap-1.5 text-sm cursor-pointer">
                    <input
                      type="radio"
                      name="version-bump"
                      value="major"
                      checked={versionBump === 'major'}
                      onChange={() => handleBumpChange('major')}
                    />
                    {suggestions.major} (major)
                  </label>
                </div>
              </fieldset>
            )}

            <div className="space-y-2">
              <Label htmlFor="release-version">Version</Label>
              <Input
                id="release-version"
                aria-label="Version"
                value={version}
                onChange={(e) => handleVersionChange(e.target.value)}
                placeholder="1.0.0"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="release-changelog">Changelog</Label>
              <Textarea
                id="release-changelog"
                aria-label="Changelog"
                value={changelog}
                onChange={(e) => setChangelog(e.target.value)}
                placeholder="Describe what changed in this release..."
                rows={4}
              />
            </div>

            {showUpgradeAdvice && (
              <div className="space-y-2">
                <Label htmlFor="release-upgrade-advice">Upgrade Advice</Label>
                <Textarea
                  id="release-upgrade-advice"
                  aria-label="Upgrade Advice"
                  value={upgradeAdvice}
                  onChange={(e) => setUpgradeAdvice(e.target.value)}
                  placeholder="Provide guidance for consumers upgrading to this major version..."
                  rows={3}
                />
              </div>
            )}

            {validationError && (
              <Alert variant="destructive">
                <AlertDescription>{validationError}</AlertDescription>
              </Alert>
            )}
            {submitError && (
              <Alert variant="destructive">
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handlePublish} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Publishing...' : 'Publish'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useVersion } from '@/queries/version'

export const Route = createFileRoute('/_authenticated/version')({
  component: VersionPage,
})

function formatValue(value: string) {
  return value && value.length > 0 ? value : 'unknown'
}

function VersionPage() {
  const { data, isLoading, error } = useVersion()

  return (
    <Card>
      <CardHeader>
        <CardTitle>Server Version</CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <p className="text-sm">Loading version info...</p>
        ) : error ? (
          <p className="text-sm text-destructive">
            Failed to load version info: {error.message}
          </p>
        ) : (
          <div className="space-y-3">
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Version</p>
              <p>{formatValue(data?.version ?? '')}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Git Commit</p>
              <p>{formatValue(data?.gitCommit ?? '')}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Git Tree State</p>
              <p>{formatValue(data?.gitTreeState ?? '')}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Build Date</p>
              <p>{formatValue(data?.buildDate ?? '')}</p>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

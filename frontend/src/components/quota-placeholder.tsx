import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

// Placeholder bars. The values encode the visual share of the bar so the
// graphic reads as real data at a glance, but the caption below makes it
// unambiguous that resource tracking is not yet wired up (HOL-609).
const PLACEHOLDER_BARS: { label: string; used: number; limit: string }[] = [
  { label: 'CPU', used: 0.32, limit: '2 cores' },
  { label: 'Memory', used: 0.58, limit: '8 GiB' },
  { label: 'Storage', used: 0.12, limit: '100 GiB' },
  { label: 'Deployments', used: 0.45, limit: '20 max' },
]

export function QuotaPlaceholder() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Usage / Quota / Limits</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3" data-testid="quota-placeholder-bars">
          {PLACEHOLDER_BARS.map((bar) => (
            <div key={bar.label}>
              <div className="flex items-baseline justify-between text-sm">
                <span className="font-medium">{bar.label}</span>
                <span className="text-muted-foreground tabular-nums">
                  {bar.limit}
                </span>
              </div>
              <div
                className="mt-1 h-2 w-full rounded-full bg-muted"
                role="progressbar"
                aria-label={`${bar.label} usage (placeholder)`}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(bar.used * 100)}
              >
                <div
                  className="h-full rounded-full bg-primary/60"
                  style={{ width: `${bar.used * 100}%` }}
                />
              </div>
            </div>
          ))}
        </div>
        <p className="mt-4 text-xs text-muted-foreground">
          Planned — resource tracking not yet implemented. Bar values shown
          above are illustrative and do not reflect real usage.
        </p>
      </CardContent>
    </Card>
  )
}

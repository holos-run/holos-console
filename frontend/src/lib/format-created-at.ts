// formatCreatedAt formats an RFC3339 timestamp string as "YYYY-MM-DD (N days ago)".
// The relative suffix uses plain English: "today", "1 day ago", or "N days ago".
// Callers should handle an empty string by returning an empty string (no timestamp available).
export function formatCreatedAt(ts: string): string {
  if (!ts) return ''
  const date = new Date(ts)
  if (Number.isNaN(date.getTime())) return ''

  const isoDate = date.toISOString().slice(0, 10)
  const now = new Date()

  // Compute calendar-day difference in the user's local timezone by comparing
  // midnight-aligned date values. Using UTC midnight for both avoids DST skew.
  const msPerDay = 86_400_000
  const startOfToday = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  const startOfCreated = Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate())
  const daysDiff = Math.round((startOfToday - startOfCreated) / msPerDay)

  let relative: string
  if (daysDiff === 0) {
    relative = 'today'
  } else if (daysDiff === 1) {
    relative = '1 day ago'
  } else {
    relative = `${daysDiff} days ago`
  }

  return `${isoDate} (${relative})`
}

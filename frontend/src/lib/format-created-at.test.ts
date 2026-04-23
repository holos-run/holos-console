import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { formatCreatedAt } from './format-created-at'

// Pin "now" to 2026-04-23T12:00:00Z so relative-time assertions are stable.
const FIXED_NOW = new Date('2026-04-23T12:00:00Z').getTime()

describe('formatCreatedAt', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns empty string for an empty input', () => {
    expect(formatCreatedAt('')).toBe('')
  })

  it('returns empty string for an invalid timestamp', () => {
    expect(formatCreatedAt('not-a-date')).toBe('')
  })

  it('formats a timestamp from today as "YYYY-MM-DD (today)"', () => {
    expect(formatCreatedAt('2026-04-23T08:00:00Z')).toBe('2026-04-23 (today)')
  })

  it('formats a timestamp from yesterday as "YYYY-MM-DD (1 day ago)"', () => {
    expect(formatCreatedAt('2026-04-22T10:00:00Z')).toBe('2026-04-22 (1 day ago)')
  })

  it('formats a timestamp from 2 days ago as "YYYY-MM-DD (2 days ago)"', () => {
    expect(formatCreatedAt('2026-04-21T00:00:00Z')).toBe('2026-04-21 (2 days ago)')
  })

  it('formats a timestamp from 30 days ago correctly', () => {
    expect(formatCreatedAt('2026-03-24T06:00:00Z')).toBe('2026-03-24 (30 days ago)')
  })

  it('formats a timestamp from 365 days ago correctly', () => {
    expect(formatCreatedAt('2025-04-23T12:00:00Z')).toBe('2025-04-23 (365 days ago)')
  })
})

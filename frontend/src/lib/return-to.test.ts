import { describe, it, expect } from 'vitest'
import { isValidReturnTo, resolveReturnTo, buildReturnTo } from './return-to'

// ---------------------------------------------------------------------------
// isValidReturnTo
// ---------------------------------------------------------------------------

describe('isValidReturnTo', () => {
  // Valid in-app paths
  it.each([
    ['/'],
    ['/organizations'],
    ['/folders/my-folder'],
    ['/projects/my-project/settings'],
    ['/organizations?page=1'],
    ['/folders/my-folder?tab=secrets'],
    ['/resource-manager'],
  ])('accepts valid in-app path: %s', (path) => {
    expect(isValidReturnTo(path)).toBe(true)
  })

  // Reject protocol-relative URLs (open-redirect risk)
  it('rejects protocol-relative URL starting with //', () => {
    expect(isValidReturnTo('//evil.example.com')).toBe(false)
  })

  it('rejects protocol-relative URL with path', () => {
    expect(isValidReturnTo('//evil.example.com/steal?token=1')).toBe(false)
  })

  // Reject absolute URLs
  it.each([
    ['https://evil.example.com'],
    ['https://evil.example.com/steal'],
    ['http://evil.example.com'],
  ])('rejects absolute URL: %s', (url) => {
    expect(isValidReturnTo(url)).toBe(false)
  })

  // Reject javascript: and other dangerous schemes
  it('rejects javascript: URL', () => {
    expect(isValidReturnTo('javascript:alert(1)')).toBe(false)
  })

  it('rejects javascript: wrapped in a path prefix attempt', () => {
    // Does not start with '/', so rejected at first check
    expect(isValidReturnTo('javascript:void(0)')).toBe(false)
  })

  it('rejects data: URL', () => {
    expect(isValidReturnTo('data:text/html,<script>alert(1)</script>')).toBe(false)
  })

  // Reject backslashes (path traversal tricks)
  it('rejects path containing backslash', () => {
    expect(isValidReturnTo('/organizations\\..\\admin')).toBe(false)
  })

  it('rejects path with backslash at start of segment', () => {
    expect(isValidReturnTo('/\\')).toBe(false)
  })

  // Reject empty and non-string values
  it('rejects empty string', () => {
    expect(isValidReturnTo('')).toBe(false)
  })

  it('rejects null', () => {
    expect(isValidReturnTo(null)).toBe(false)
  })

  it('rejects undefined', () => {
    expect(isValidReturnTo(undefined)).toBe(false)
  })

  it('rejects number', () => {
    expect(isValidReturnTo(42)).toBe(false)
  })

  // Reject paths that do not start with /
  it('rejects relative path without leading slash', () => {
    expect(isValidReturnTo('organizations')).toBe(false)
  })

  // Reject invalid percent-encoded sequences
  it('rejects string with invalid percent encoding', () => {
    expect(isValidReturnTo('/path/%GG')).toBe(false)
  })

  // Colon in the first path segment (scheme-like)
  it('rejects /something:with-colon in first segment', () => {
    expect(isValidReturnTo('/something:with-colon')).toBe(false)
  })

  it('allows colon in a later path segment', () => {
    // A colon after the first slash separator is fine (e.g. a resource name)
    expect(isValidReturnTo('/projects/my:project')).toBe(true)
  })
})

// ---------------------------------------------------------------------------
// resolveReturnTo
// ---------------------------------------------------------------------------

describe('resolveReturnTo', () => {
  const FALLBACK = '/organizations'

  it('returns a valid returnTo path', () => {
    expect(resolveReturnTo('/folders', FALLBACK)).toBe('/folders')
  })

  it('returns a valid returnTo path with query string', () => {
    expect(resolveReturnTo('/folders?tab=secrets', FALLBACK)).toBe('/folders?tab=secrets')
  })

  it('falls back when returnTo is undefined', () => {
    expect(resolveReturnTo(undefined, FALLBACK)).toBe(FALLBACK)
  })

  it('falls back when returnTo is null', () => {
    expect(resolveReturnTo(null, FALLBACK)).toBe(FALLBACK)
  })

  it('falls back when returnTo is empty string', () => {
    expect(resolveReturnTo('', FALLBACK)).toBe(FALLBACK)
  })

  it('falls back when returnTo is an absolute URL', () => {
    expect(resolveReturnTo('https://evil.example.com', FALLBACK)).toBe(FALLBACK)
  })

  it('falls back when returnTo is protocol-relative', () => {
    expect(resolveReturnTo('//evil.example.com', FALLBACK)).toBe(FALLBACK)
  })

  it('falls back when returnTo is javascript:', () => {
    expect(resolveReturnTo('javascript:alert(1)', FALLBACK)).toBe(FALLBACK)
  })
})

// ---------------------------------------------------------------------------
// buildReturnTo
// ---------------------------------------------------------------------------

describe('buildReturnTo', () => {
  it('returns pathname only when no search string', () => {
    expect(buildReturnTo({ pathname: '/organizations' })).toBe('/organizations')
  })

  it('appends search string when present', () => {
    expect(buildReturnTo({ pathname: '/folders', search: '?tab=secrets' })).toBe(
      '/folders?tab=secrets',
    )
  })

  it('omits bare "?" as search', () => {
    expect(buildReturnTo({ pathname: '/folders', search: '?' })).toBe('/folders')
  })

  it('omits empty-string search', () => {
    expect(buildReturnTo({ pathname: '/folders', search: '' })).toBe('/folders')
  })

  it('omits undefined search', () => {
    expect(buildReturnTo({ pathname: '/folders', search: undefined })).toBe('/folders')
  })

  it('returns empty string when pathname is empty', () => {
    expect(buildReturnTo({ pathname: '' })).toBe('')
  })

  it('produces a value that passes isValidReturnTo for a normal path', () => {
    const value = buildReturnTo({ pathname: '/folders/my-folder', search: '?page=2' })
    expect(isValidReturnTo(value)).toBe(true)
  })
})

import { describe, it, expect } from 'vitest'
import { isSafeHttpUrl } from './url'

describe('isSafeHttpUrl', () => {
  it('returns true for https URL', () => {
    expect(isSafeHttpUrl('https://example.com')).toBe(true)
  })

  it('returns true for http URL', () => {
    expect(isSafeHttpUrl('http://example.com')).toBe(true)
  })

  it('returns false for javascript: URL', () => {
    expect(isSafeHttpUrl('javascript:alert(1)')).toBe(false)
  })

  it('returns false for data: URL', () => {
    expect(isSafeHttpUrl('data:text/html,<script>alert(1)</script>')).toBe(false)
  })

  it('returns false for vbscript: URL', () => {
    expect(isSafeHttpUrl('vbscript:alert(1)')).toBe(false)
  })

  it('returns false for file: URL', () => {
    expect(isSafeHttpUrl('file:///etc/passwd')).toBe(false)
  })

  it('returns false for empty string', () => {
    expect(isSafeHttpUrl('')).toBe(false)
  })

  it('returns false for malformed URL', () => {
    expect(isSafeHttpUrl('not a url')).toBe(false)
  })
})

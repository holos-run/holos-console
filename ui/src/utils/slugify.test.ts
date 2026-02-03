import { describe, it, expect } from 'vitest'
import { slugify } from './slugify'

describe('slugify', () => {
  it('lowercases input', () => {
    expect(slugify('Hello World')).toBe('hello-world')
  })

  it('replaces spaces with hyphens', () => {
    expect(slugify('foo bar baz')).toBe('foo-bar-baz')
  })

  it('replaces underscores with hyphens', () => {
    expect(slugify('my_cool_org')).toBe('my-cool-org')
  })

  it('strips non-alphanumeric/hyphen characters', () => {
    expect(slugify('test@#$org')).toBe('testorg')
  })

  it('collapses consecutive hyphens', () => {
    expect(slugify('foo---bar')).toBe('foo-bar')
  })

  it('trims leading and trailing hyphens', () => {
    expect(slugify('--leading-trailing--')).toBe('leading-trailing')
  })

  it('handles empty string', () => {
    expect(slugify('')).toBe('')
  })

  it('handles mixed special characters', () => {
    expect(slugify('Test @#$ Org!!!')).toBe('test-org')
  })

  it('handles leading/trailing spaces', () => {
    expect(slugify(' --leading trailing-- ')).toBe('leading-trailing')
  })
})

import { describe, it, expect } from 'vitest'
import { toSlug } from './slug'

describe('toSlug', () => {
  it.each([
    ['Test Project', 'test-project'],
    ['  Hello World  ', 'hello-world'],
    ['My Org 2', 'my-org-2'],
    ['already-a-slug', 'already-a-slug'],
    ['Special! Ch@rs', 'special-ch-rs'],
    ['', ''],
    ['---', ''],
    ['  Multiple   Spaces  ', 'multiple-spaces'],
    ['UPPERCASE', 'uppercase'],
    ['with123numbers', 'with123numbers'],
  ])('toSlug(%j) === %j', (input, expected) => {
    expect(toSlug(input)).toBe(expected)
  })
})

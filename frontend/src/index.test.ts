import { readFileSync } from 'fs'
import { resolve } from 'path'

describe('index.html', () => {
  it('has correct tab title', () => {
    const html = readFileSync(resolve(__dirname, '../index.html'), 'utf-8')
    expect(html).toContain('<title>Holos Console</title>')
  })
})

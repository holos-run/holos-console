import { describe, it, expect, afterEach } from 'vitest'
import { getConsoleConfig } from './console-config'

describe('getConsoleConfig', () => {
  afterEach(() => {
    // Clean up global between tests
    delete window.__CONSOLE_CONFIG__
  })

  it('returns injected config when window.__CONSOLE_CONFIG__ is set', () => {
    window.__CONSOLE_CONFIG__ = { devToolsEnabled: true }
    const config = getConsoleConfig()
    expect(config.devToolsEnabled).toBe(true)
  })

  it('returns devToolsEnabled false when injected as false', () => {
    window.__CONSOLE_CONFIG__ = { devToolsEnabled: false }
    const config = getConsoleConfig()
    expect(config.devToolsEnabled).toBe(false)
  })

  it('returns default config with devToolsEnabled false when global is not set', () => {
    const config = getConsoleConfig()
    expect(config.devToolsEnabled).toBe(false)
  })
})

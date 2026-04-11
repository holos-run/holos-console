/**
 * Console configuration injected by the Go server into index.html
 * via window.__CONSOLE_CONFIG__.
 */
interface ConsoleConfig {
  devToolsEnabled: boolean
}

declare global {
  interface Window {
    __CONSOLE_CONFIG__?: ConsoleConfig
  }
}

/**
 * Returns the console configuration, falling back to safe defaults
 * when the global is not injected (e.g., during tests or static preview).
 */
export function getConsoleConfig(): ConsoleConfig {
  return window.__CONSOLE_CONFIG__ ?? { devToolsEnabled: false }
}

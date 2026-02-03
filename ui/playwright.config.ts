import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E test configuration for holos-console.
 *
 * Tests run against the full application stack (Go backend + React frontend).
 * Playwright automatically starts both servers via the webServer config.
 *
 * Run tests with: make test-e2e (or: cd ui && npm run test:e2e)
 *
 * For manual debugging, start servers separately and tests will reuse them:
 *   Terminal 1: make run     (Go backend on https://localhost:8443)
 *   Terminal 2: make dev     (Vite dev server on https://localhost:5173)
 *   Terminal 3: npm run test:e2e
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  // Use list reporter for console output (CI-friendly)
  // HTML reporter opens a browser which blocks non-interactive execution
  reporter: 'list',
  use: {
    // Base URL for the Vite dev server (proxies to Go backend)
    baseURL: 'https://localhost:5173',

    // Accept self-signed certificates in development
    ignoreHTTPSErrors: true,

    // Collect trace on first retry
    trace: 'on-first-retry',

    // Screenshot on failure
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'mobile-chrome',
      use: { ...devices['iPhone 13'] },
    },
  ],

  // Server orchestration - Playwright manages lifecycle
  webServer: [
    {
      // Go backend - must be built first (make build or make test-e2e)
      // Use exec to ensure signals reach the Go binary directly
      command: 'exec ../bin/holos-console --enable-insecure-dex --cert ../certs/tls.crt --key ../certs/tls.key',
      url: 'https://localhost:8443/ui',
      timeout: 30_000,
      reuseExistingServer: !process.env.CI,
      ignoreHTTPSErrors: true,
      stdout: 'pipe',
      stderr: 'pipe',
    },
    {
      // Vite dev server - depends on Go backend for proxy
      command: 'npm run dev',
      url: 'https://localhost:5173/ui',
      timeout: 30_000,
      reuseExistingServer: !process.env.CI,
      ignoreHTTPSErrors: true,
      stdout: 'pipe',
      stderr: 'pipe',
    },
  ],
})

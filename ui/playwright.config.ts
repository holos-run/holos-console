import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E test configuration for holos-console.
 *
 * These tests run against the full application stack (Go backend + React frontend).
 * Before running E2E tests, start both servers:
 *
 *   Terminal 1: make run     (Go backend on https://localhost:8443)
 *   Terminal 2: npm run dev  (Vite dev server on https://localhost:5173)
 *
 * Then run tests with: npm run test:e2e
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
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
  ],

  // No webServer config - tests expect servers to be running already
  // This allows for more flexible test execution and debugging
})

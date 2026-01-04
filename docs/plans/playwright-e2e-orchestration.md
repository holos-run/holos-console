# Plan: Playwright E2E Test Orchestration

> **Status:** APPROVED
>
> This plan has been reviewed and approved for implementation.

## Overview

This plan addresses the current friction in running Playwright E2E tests, which requires a human to manually start the Go backend in one terminal, the Vite dev server in another, and then run the tests in a third. This workflow is:

1. **Not agent-friendly** - Claude Code cannot orchestrate multi-terminal workflows
2. **Error-prone** - Humans forget to start servers or leave orphan processes
3. **CI-unfriendly** - Requires manual intervention or complex scripting

We propose using Playwright's built-in `webServer` configuration with a unified orchestration script that starts both servers, runs tests, and ensures clean shutdown without orphan processes.

## Design Decisions

| Topic | Decision | Rationale |
|-------|----------|-----------|
| Orchestration approach | Playwright `webServer` array | Native Playwright feature, handles startup/shutdown, no custom code |
| Process management | Single parent script with process groups | Kill entire process tree on exit, prevents orphans |
| Server startup | Go binary directly (not `make run`) | Simpler process tree, easier to manage |
| Health checks | HTTP probe before tests start | Playwright's `url` option provides this automatically |
| Cleanup | SIGTERM on exit, SIGKILL after timeout | Graceful shutdown with fallback |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Test Orchestration                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────┐      ┌──────────────────────────────────┐ │
│  │  Playwright      │      │         webServer config         │ │
│  │  Test Runner     │      │                                  │ │
│  │                  │      │  Server 1: Go backend (:8443)    │ │
│  │  - Starts        │─────▶│    command: ./bin/holos-console  │ │
│  │    webServers    │      │    url: https://localhost:8443   │ │
│  │  - Runs tests    │      │                                  │ │
│  │  - Stops         │      │  Server 2: Vite dev (:5173)      │ │
│  │    webServers    │      │    command: npm run dev          │ │
│  │                  │      │    url: https://localhost:5173   │ │
│  └──────────────────┘      └──────────────────────────────────┘ │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Key Behaviors

1. **Playwright manages server lifecycle** - `webServer` starts servers before tests, stops after
2. **Health checks prevent race conditions** - Tests only start when servers respond to health probes
3. **Automatic cleanup** - Playwright sends SIGTERM to server processes on completion
4. **Reuseability** - If servers are already running, `reuseExistingServer: true` skips startup
5. **Timeout handling** - Configurable startup timeout prevents hanging builds

## Current State

The current implementation:
- Requires manual server startup in separate terminals
- Tests skip gracefully if servers aren't running (via `test.beforeAll` check)
- `playwright.config.ts` has no `webServer` configuration
- `make test-e2e` assumes servers are already running

## Solution: Playwright webServer Configuration

Use Playwright's native `webServer` array to manage both servers. This is the recommended approach because:

- Zero custom code for process management
- Playwright handles SIGTERM/cleanup automatically
- Built-in health checking via `url` option
- Well-documented, battle-tested approach
- Works in CI without modification

Trade-offs:
- Go backend must be built before tests run (add `make build` dependency)
- Both servers start fresh each test run (slower than manual approach, mitigated by `reuseExistingServer`)

### Implementation Details

#### 1. Update `playwright.config.ts`

```typescript
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'list',
  use: {
    baseURL: 'https://localhost:5173',
    ignoreHTTPSErrors: true,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Server orchestration - Playwright manages lifecycle
  webServer: [
    {
      // Go backend - must be built first
      command: 'exec ../bin/holos-console --cert ../certs/tls.crt --key ../certs/tls.key',
      url: 'https://localhost:8443/ui',
      timeout: 30_000,
      reuseExistingServer: !process.env.CI,
      ignoreHTTPSErrors: true,
      // stdout/stderr go to console for debugging
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
```

#### 2. Update Makefile

```makefile
.PHONY: test-e2e
test-e2e: build ## Run Playwright E2E tests (orchestrates servers automatically).
	cd ui && npm run test:e2e
```

Note: The `build` dependency ensures the Go binary exists before Playwright tries to start it.

#### 3. Remove Skip Logic from Tests

The `test.beforeAll` server check in `e2e/auth.spec.ts` becomes unnecessary since Playwright guarantees servers are running. However, keeping it provides a better error message if configuration is wrong.

#### 4. CI Considerations

In CI environments (`process.env.CI`), set `reuseExistingServer: false` to ensure clean state. The current config already handles this.

### Process Cleanup Details

Playwright's `webServer` handles cleanup as follows:

1. **Normal exit** - Sends SIGTERM to server processes
2. **Test timeout** - Same as normal exit
3. **Ctrl+C** - Propagates signal, servers receive SIGTERM
4. **Crash** - OS cleans up child processes (Playwright is parent)

For robustness, server commands should handle SIGTERM gracefully. The Go `http.Server.Shutdown()` method already does this.

### Preventing Orphan Processes

Several safeguards prevent orphan processes:

1. **Process groups** - Playwright spawns servers as direct children
2. **Signal propagation** - SIGTERM reaches all children
3. **Timeout fallback** - Playwright has internal timeouts
4. **CI cleanup** - Most CI systems kill process trees on job completion

If additional safety is needed, wrap server commands:

```typescript
// Use 'exec' to replace shell with actual process
command: 'exec ../bin/holos-console --cert ...',
```

This ensures signals reach the Go binary directly, not a shell wrapper.

## File Changes

### Modify

- `ui/playwright.config.ts` - Add `webServer` array configuration
- `Makefile` - Add `build` dependency to `test-e2e` target
- `ui/e2e/auth.spec.ts` - Update/remove server availability check (optional)

### No Changes Needed

- Go server code (already handles graceful shutdown)
- Vite configuration (already works as dev server)
- CI configuration (Playwright handles everything)

## Testing the Implementation

1. **Clean state test**
   ```bash
   # Kill any running servers
   pkill -f holos-console || true
   pkill -f vite || true

   # Run tests - should start servers automatically
   make test-e2e

   # Verify cleanup - no orphan processes
   pgrep -f holos-console && echo "FAIL: orphan process" || echo "OK: clean"
   pgrep -f vite && echo "FAIL: orphan process" || echo "OK: clean"
   ```

2. **Ctrl+C test**
   ```bash
   make test-e2e
   # Press Ctrl+C during test run

   # Verify cleanup
   sleep 2
   pgrep -f holos-console && echo "FAIL" || echo "OK"
   ```

3. **Reuse test (local development)**
   ```bash
   # Start servers manually
   make run &
   make dev &

   # Run tests - should reuse existing servers
   make test-e2e  # Should not start new servers
   ```

## Implementation Phases

### Phase 1: Basic Orchestration

- [x] 1.1: Update `playwright.config.ts` with `webServer` array
- [x] 1.2: Update Makefile `test-e2e` target to depend on `build`
- [x] 1.3: Test locally and verify cleanup

### Phase 2: Robustness

- [x] 2.1: Add `exec` prefix to commands if needed for signal handling
- [x] 2.2: Test Ctrl+C cleanup behavior
- [x] 2.3: Verify no orphan processes in various scenarios

### Phase 3: CI Verification

- [ ] 3.1: Run tests in CI environment
- [ ] 3.2: Verify CI cleanup works correctly
- [ ] 3.3: Add CI-specific timeout tuning if needed

### Phase 4: Documentation

- [x] 4.1: Update CONTRIBUTING.md with new test workflow
- [x] 4.2: Remove manual server startup instructions from test files
- [x] 4.3: Document how to debug with servers running separately

## Success Criteria

1. `make test-e2e` works without manual server startup
2. No orphan processes after test completion (normal or Ctrl+C)
3. Tests still work with manually started servers (local dev)
4. CI runs work without modification
5. Claude Code can run `make test-e2e` as a single command

---

## Alternatives Considered

### Custom Orchestration Script

Create `scripts/test-e2e` that manages processes manually.

**Why not chosen:**
- More code to maintain
- Must handle signal propagation manually
- Risk of orphan processes if script crashes

**Would provide:**
- More control over startup sequence
- Custom health checks
- Server reuse between runs

### Hybrid Approach

Use Playwright `webServer` for Vite only, require Go backend to be pre-started.

**Why not chosen:**
- Still requires manual intervention for backend
- Inconsistent experience between local and CI

**Would provide:**
- Go backend can be debugged separately
- Faster iteration on frontend-only changes

### globalSetup/globalTeardown

If `webServer` proves insufficient, Playwright also supports `globalSetup` and `globalTeardown` scripts. These provide more control but require manual process management:

```typescript
// playwright.config.ts
export default defineConfig({
  globalSetup: require.resolve('./e2e/global-setup'),
  globalTeardown: require.resolve('./e2e/global-teardown'),
})

// e2e/global-setup.ts
import { spawn } from 'child_process'

export default async function globalSetup() {
  const backend = spawn('../bin/holos-console', ['--cert', '...'], {
    detached: false,  // Die with parent
    stdio: 'pipe',
  })

  // Store PID for teardown
  process.env.BACKEND_PID = String(backend.pid)

  // Wait for health check
  await waitForServer('https://localhost:8443')
}

// e2e/global-teardown.ts
export default async function globalTeardown() {
  const pid = parseInt(process.env.BACKEND_PID || '')
  if (pid) {
    process.kill(pid, 'SIGTERM')
  }
}
```

**Why not chosen:** More complex and requires manual process management. Only consider if `webServer` has issues.

---

## Appendix: Playwright webServer Reference

From Playwright documentation:

```typescript
interface WebServerConfig {
  // Command to start the server
  command: string

  // URL to probe for server readiness
  url?: string

  // Timeout for server to become ready (ms)
  timeout?: number

  // Reuse existing server if already running
  reuseExistingServer?: boolean

  // Ignore HTTPS certificate errors
  ignoreHTTPSErrors?: boolean

  // Working directory for command
  cwd?: string

  // Environment variables
  env?: Record<string, string>

  // Port to use (alternative to url)
  port?: number

  // Stdout handling: 'pipe' | 'ignore'
  stdout?: 'pipe' | 'ignore'

  // Stderr handling: 'pipe' | 'ignore'
  stderr?: 'pipe' | 'ignore'
}
```

Key behaviors:
- Multiple servers in array start in parallel
- All must be ready before tests run
- All are terminated when tests complete
- `reuseExistingServer` checks if URL is already accessible

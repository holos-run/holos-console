# Plan: Fix CGO_ENABLED for Race Detector Tests

> **Status:** PROPOSED
>
> This plan addresses the `make test` failure when CGO is disabled.

## Overview

Running `make test` fails with the error:

```
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
```

The Go race detector (`-race` flag) requires CGO to be enabled, but the environment has `CGO_ENABLED=0` by default.

## Root Cause

**Location:** [Makefile:78](../../../Makefile#L78)

**Problem:** The `test-go` target runs:

```makefile
test-go: ## Run Go tests.
	go test -race -coverprofile=coverage.out $(TEST_LDFLAGS) ./...
```

The `-race` flag requires the C compiler toolchain because the race detector is implemented using ThreadSanitizer, which requires CGO. When `CGO_ENABLED=0` (which can happen in certain environments, containers, or when no C compiler is available), the test command fails.

## Solution Options

### Option 1: Explicitly Enable CGO (Recommended)

Modify the `test-go` target to explicitly set `CGO_ENABLED=1`:

```makefile
test-go: ## Run Go tests.
	CGO_ENABLED=1 go test -race -coverprofile=coverage.out $(TEST_LDFLAGS) ./...
```

**Pros:**
- Simple one-line fix
- Maintains race detection capability
- Makes the test behavior explicit and reproducible

**Cons:**
- Requires C compiler toolchain in the build environment
- May fail in minimal container images without gcc/clang

### Option 2: Conditional Race Detection

Add logic to conditionally enable race detection:

```makefile
RACE_FLAG := $(shell CGO_ENABLED=1 go env CGO_ENABLED 2>/dev/null | grep -q 1 && echo "-race" || echo "")

test-go: ## Run Go tests.
	go test $(RACE_FLAG) -coverprofile=coverage.out $(TEST_LDFLAGS) ./...
```

**Pros:**
- Gracefully degrades in environments without CGO
- Tests still run, just without race detection

**Cons:**
- More complex
- Silently disables an important safety check
- May hide race conditions in CI environments

### Option 3: Separate Targets

Create separate targets for race and non-race testing:

```makefile
test-go: ## Run Go tests with race detector.
	CGO_ENABLED=1 go test -race -coverprofile=coverage.out $(TEST_LDFLAGS) ./...

test-go-no-race: ## Run Go tests without race detector.
	go test -coverprofile=coverage.out $(TEST_LDFLAGS) ./...
```

**Pros:**
- Maximum flexibility
- Clear intent for each target

**Cons:**
- Additional target to maintain
- Default `make test` still requires CGO

## Recommendation

**Option 1** is recommended because:

1. Race detection is valuable for catching concurrency bugs
2. The fix is minimal and explicit
3. Development environments typically have CGO available
4. CI environments can easily include gcc/clang

## Implementation Plan

### Phase 1: Fix Makefile

#### 1.1 Add CGO_ENABLED=1 to test-go target

**File:** `Makefile`

Change line 78 from:
```makefile
	go test -race -coverprofile=coverage.out $(TEST_LDFLAGS) ./...
```

To:
```makefile
	CGO_ENABLED=1 go test -race -coverprofile=coverage.out $(TEST_LDFLAGS) ./...
```

### Phase 2: Verify Fix

#### 2.1 Run make test and verify all tests pass

```bash
make test
```

Expected: All Go and UI tests pass without the CGO_ENABLED error.

## TODO (Implementation Checklist)

### Phase 1: Fix Makefile
- [ ] 1.1: Add `CGO_ENABLED=1` prefix to `go test` command in `test-go` target

### Phase 2: Verify Fix
- [ ] 2.1: Run `make test` and verify all tests pass

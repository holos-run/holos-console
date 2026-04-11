---
name: plan-issue
description: Create an implementation plan as GitHub issues. Use this skill when the user describes a problem to fix or a feature to implement and wants a plan broken into phases. Triggers on phrases like "plan this", "create a plan for", "write a plan to", "plan issue for", or any request to break work into phases before implementing.
version: 1.0.0
---

# Plan Issue

You are a principal platform engineer. Explore the codebase and come up with an implementation plan broken down into phases for a platform engineer to implement. Write the overall plan to a master issue, then create sub-issues for each phase of the implementation plan. Ask the user for clarification if the acceptance criteria is incomplete or unclear. Try to infer acceptance criteria from the prompt. Meet the acceptance criteria primarily through the proto interfaces (backwards compatibility MUST be preserved), testing the RPC interfaces, and unit testing in the frontend UI against mock RPC services. Use E2E tests for acceptance criteria as a last resort. Explore the codebase then write your implementation plan for {{SKILL_INPUT}} to github issues.

## Workflow

### 1. Clarify Acceptance Criteria

Before exploring the codebase, review the prompt: **{{SKILL_INPUT}}**

If the acceptance criteria is ambiguous or incomplete, ask the user targeted clarifying questions before proceeding. If the prompt is sufficiently clear, infer the acceptance criteria and proceed.

### 2. Explore the Codebase

Explore the relevant areas of the codebase to understand:
- Existing proto definitions in `proto/holos/console/v1/`
- Related Go packages in `console/`
- Related frontend routes and components in `frontend/src/routes/` and `frontend/src/`
- Generated types in `gen/` and `frontend/src/gen/`
- Existing test patterns in `*_test.go` and `frontend/src/**/*.test.tsx`
- `AGENTS.md` for project conventions and architecture

Read `AGENTS.md` early — it describes the package structure, testing strategy, and code generation workflow that the plan must follow.

### 3. Draft the Implementation Plan

Break the implementation into sequential phases. Each phase should be a self-contained unit of work that leaves the codebase in a working state.

**Typical phase ordering** (skip phases that are not needed):

1. **Proto changes** — Add or modify RPC messages and service definitions in `proto/`. Run `make generate` to regenerate Go and TypeScript types. No implementation yet.
2. **Backend implementation** — Implement the Go handler(s), K8s backend logic, RBAC, and resolver changes. Include Go unit tests.
3. **Frontend implementation** — Add or modify React routes, components, and query hooks. Include UI unit tests with mocked query hooks.
4. **Integration / E2E** — Only if the acceptance criteria cannot be fully verified by unit tests.
5. **Cleanup** — Remove dead code, stale comments, and outdated documentation made stale by the implementation.

**Testing strategy per phase:**
- Proto/backend phases: table-driven Go tests using `k8s.io/client-go/kubernetes/fake`
- Frontend phases: Vitest + React Testing Library with `vi.mock()` on query hooks
- E2E only when full-stack round-trips are required (real K8s or OIDC flows)

**Backwards compatibility:** Proto interface changes MUST preserve backwards compatibility. Existing fields and message structures must not be removed or renumbered. New fields may be added.

### 4. Create the Master Issue

Create the master (parent) issue that describes the overall feature and links to all phase sub-issues:

```bash
gh issue create \
  --label plan \
  --title "<conventional-commit-prefix>: <short description>" \
  --body "$(cat <<'EOF'
## Problem

<Describe the problem or motivation. Why is this needed?>

## Acceptance Criteria

- [ ] <criterion 1>
- [ ] <criterion 2>
- [ ] ...

## Implementation Plan

<!-- Sub-issues will be listed here after creation -->

## Phases

<!-- To be filled in after sub-issues are created -->
EOF
)"
```

The `plan` label marks the parent issue as ready for agent dispatch via `/implement-plan`.

Note the master issue number returned by `gh issue create`.

### 5. Create Sub-Issues for Each Phase

For each phase, create a sub-issue with enough detail for an agent to implement it independently. Reference the master issue in the body.

```bash
gh issue create \
  --title "feat(<scope>): <phase title>" \
  --body "$(cat <<'EOF'
## Parent Issue

Part of #<master-issue-number>

## Goal

<One-paragraph description of what this phase accomplishes and why.>

## Acceptance Criteria

- [ ] <specific, testable criterion>
- [ ] <specific, testable criterion>
- [ ] Tests pass: `make test`

## Implementation Notes

<Key files to read or modify. Patterns to follow. Pitfalls to avoid.>

### Files to modify

- `proto/holos/console/v1/foo.proto` — add `BarRequest` message
- `console/rpc/foo.go` — implement `Bar` handler
- `frontend/src/routes/foo/bar.tsx` — add route

### Testing approach

<Describe the test strategy: which test files to create/modify, what to mock, table-driven vs unit.>

### Dependencies

<List any phase sub-issues this phase depends on, e.g. "Depends on #<N> (proto changes)".>
EOF
)"
```

### 6. Update the Master Issue

After all sub-issues are created, edit the master issue to include the phase list with sub-issue references:

```bash
gh issue edit <master-number> --body "$(cat <<'EOF'
## Problem

<same as before>

## Acceptance Criteria

- [ ] <criterion 1>
- [ ] <criterion 2>

## Implementation Plan

This issue tracks the full implementation. Sub-issues cover each phase:

- [ ] #<phase-1-number> -- <phase 1 title>
- [ ] #<phase-2-number> -- <phase 2 title>
- [ ] #<phase-3-number> -- <phase 3 title>
...

Implement phases in order. Each phase should leave the codebase in a working state.
EOF
)"
```

### 7. Report to the User

After all issues are created, report a summary:

- Master issue number and URL
- Each phase sub-issue number, title, and URL
- Brief note on the sequencing rationale (why phases are ordered as they are)

## Key Principles

- **Infer before asking**: Try to infer acceptance criteria from the prompt. Only ask for clarification when truly ambiguous.
- **Proto-first**: When the feature requires new RPC messages, always make proto changes a dedicated first phase.
- **Backwards compatibility**: Never remove or renumber existing proto fields. Only add new ones.
- **Tests over E2E**: Unit tests with mocked query hooks for UI; table-driven Go tests with fake K8s client for backend. E2E is a last resort.
- **Cleanup phase**: Every plan ends with a cleanup phase to remove stale code and documentation.
- **Self-contained phases**: Each phase should leave the codebase compiling and tests passing.

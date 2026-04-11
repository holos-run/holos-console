# Plan, Implement, Review: Agent-Assisted Development Cycle

**Version:** 0.1.0
**Last Updated:** 2026-04-11

This document describes how features are planned, implemented, and reviewed in this repository using an agent-assisted development cycle. The cycle combines Claude Opus for planning and implementation with OpenAI Codex for code review, all running locally on engineer laptops.

This guide is a living document. The tooling, prompts, and patterns described here will evolve as we learn what works. Version bumps track significant changes. If something described here doesn't match reality, update the guide.

## Overview

The development cycle has three phases:

```
/plan-issue  →  /implement-issue  →  Codex review (local)
                                          │
                                          ├── clean → merge
                                          ├── non-critical findings → merge + follow-up issue
                                          └── critical findings → fix loop (max 2 cycles)
                                                                    └── escalate if unresolved
```

Each phase is backed by a Claude Code skill. The plan-issue and implement-issue skills already exist. The review step is integrated into implement-issue via a local Codex CLI invocation.

## Constraints

These constraints shape every design decision in the cycle.

**1. Local execution only.** The entire workflow runs from Claude, Codex, and other tools on engineer laptops. No GitHub Actions, no CI/CD workflow integration. GitHub workflows are too much friction to maintain and cannot be easily customized or tailored to each engineer's workflow. The tools you run locally are under your direct control — tune prompts, swap models, adjust review criteria, and iterate without waiting on CI pipeline changes or repo-level secrets provisioning.

**2. Artifacts on GitHub.** While execution is local, all outputs (issues, PRs, review comments) are posted to GitHub. GitHub is the record of work, not the orchestrator of it. This means anyone on the team can audit what happened after the fact by reading the issue, PR, and review threads.

**3. Good enough, fully automated, fix-forward.** The cycle optimizes for throughput over perfection. As long as there are no critical security vulnerabilities or structural design errors, it is acceptable to merge the PR and fix-forward. Only two categories block progress during review:

- **Security** — vulnerabilities that could be exploited (injection, auth bypass, credential exposure)
- **Reliability** — structural errors that would cause data loss, outages, or cascading failures

Everything else — style, naming, minor refactoring opportunities, non-critical edge cases — merges now with a follow-up issue. The goal is an unattended cycle that completes without human intervention. Blocking on cosmetic findings defeats the purpose.

## Prerequisites

| Tool | Purpose | Install |
|------|---------|---------|
| `claude` | Planning and implementation (Opus) | `npm install -g @anthropic-ai/claude-code` |
| `codex` | Code review | `npm install -g @openai/codex` |
| `gh` | GitHub API interaction | `brew install gh` |
| `make` | Build and test | Already in this repo |

## Phase 1: Planning — `/plan-issue`

The plan-issue skill takes a feature description and produces a structured GitHub issue hierarchy.

### How to use it

```
/plan-issue Add Data and Resource views to the folder settings page
```

### What it produces

A master issue (labeled `plan`) containing:

- **Problem statement** — what gap this feature fills
- **Acceptance criteria** — checkboxes defining "done"
- **Implementation plan** — ordered sub-issues, each independently implementable
- **Sequencing rationale** — why the phases are in this order

Each sub-issue contains:

- **Goal** — what this phase accomplishes
- **Acceptance criteria** — always includes `make test` passing
- **Implementation notes** — files to modify, testing approach, dependencies
- **Parent reference** — links back to the master issue

### Phase ordering convention

1. Proto changes (always first — generates type scaffolding everything depends on)
2. Backend implementation (Go)
3. Frontend implementation (React)
4. Integration/E2E tests (only if unit tests are insufficient)
5. Cleanup (always last — sweep stale code and docs)

### Examples

- [#670](https://github.com/holos-run/holos-console/issues/670) — feat: default folder, sidebar reorder, and resource reparenting (7 phases)
- [#678](https://github.com/holos-run/holos-console/issues/678) — feat: add Data and Resource views to folder settings page (2 phases)
- [#406](https://github.com/holos-run/holos-console/issues/406) — feat: auto-deploy on Harbor image push via NATS JetStream (6 phases)

## Phase 2: Implementation — `/implement-issue`

The implement-issue skill takes a GitHub issue number and runs end-to-end implementation.

### How to use it

```
/implement-issue 670
```

### What it does

For a master issue (has sub-issues):
1. Dispatches each sub-issue sequentially to a sub-agent
2. Pulls main between each sub-issue
3. Closes the master issue when all sub-issues are done

For a leaf issue (no sub-issues):
1. Creates branch `feat/<number>-<slug>`
2. Implements using RED-GREEN (write failing test first, then make it pass)
3. Opens PR with `Closes #N` and a rationale comment
4. Runs CI loop (`gh pr checks --watch`) until green
5. Invokes Codex review locally (see Phase 3)
6. Merges with `--merge` (never squash/rebase)

### Branch naming

`feat/<issue-number>-<slug>` — slug derived from issue title, lowercased, hyphens only, truncated to ~40 chars.

### PR format

```markdown
## Summary
- <bullet points>

Closes #<issue-number>

## Test plan
- [ ] <verification steps>

Generated with [Claude Code](https://claude.com/claude-code)
```

### Session management

Each sub-issue gets its own sub-agent with fresh context. This is intentional — agent performance degrades after ~35 minutes of continuous work because errors compound. By giving each sub-issue a clean session that reads its input from GitHub (the issue body) and writes output to GitHub (the PR), the cycle avoids context drift. A 3-hour unattended session isn't one long session — it's a sequence of 20-30 minute sessions orchestrated by the master issue checklist.

## Phase 3: Review — Local Codex

After CI passes and before merge, the implement-issue skill invokes Codex CLI locally to review the PR diff. This is cross-model review: Claude implements, Codex evaluates. The separation matters because a different model brings genuinely independent judgment rather than confirming its own work.

### How it works

1. **Fetch the diff** — `gh pr diff <N>`
2. **Fetch linked issue** — parse `Closes #M` from PR body, fetch acceptance criteria
3. **Invoke Codex locally** — run `codex --quiet` with the diff, acceptance criteria, and review instructions
4. **Classify findings** — each finding is either critical (security/reliability) or non-critical (everything else)
5. **Post review** — post findings as a GitHub review via `gh api`

### Review criteria

The reviewer focuses on:

| Category | What to check |
|----------|---------------|
| **Correctness** | Logic errors, off-by-one, nil/null handling, race conditions |
| **Acceptance criteria** | Does the PR satisfy the linked issue's acceptance criteria? |
| **Tests** | Are new code paths tested? Do tests cover edge cases? |
| **Proto compatibility** | Backwards-compatible field additions, no removed fields |
| **Security** | Input validation, authentication checks, injection |
| **Error handling** | Errors propagated correctly, no swallowed errors |

The reviewer does **not** comment on: style preferences, comment formatting, naming opinions, or suggestions beyond the issue's acceptance criteria.

### Review outcomes

**No findings** — post APPROVE, merge immediately. This is the common case.

**Non-critical findings only** — post a COMMENT review (not REQUEST_CHANGES), merge the PR, and create a follow-up issue linking the review comments. Non-critical findings are not worth blocking the automated cycle.

**Critical findings (security or reliability)** — enter the fix loop:

1. Claude reads the findings, fixes each issue, commits with `fix: address review finding — <summary>`
2. Push fixes, re-invoke Codex
3. If clean after re-review, merge
4. Maximum 2 cycles — after that, unresolved critical findings escalate

**Escalation** — if critical findings remain after 2 fix cycles, the PR gets a `needs-human-review` label and is not merged. The agent moves to the next sub-issue. This keeps the pipeline moving — one stuck PR doesn't block the entire plan.

### Why 2 cycles

After 2 rounds, if Claude and Codex still disagree on a critical finding, it's almost certainly a judgment call that requires human context — not a clear-cut bug. Continuing to cycle wastes compute without converging.

### Why local is better

The local review loop completes in under 5 minutes. The same loop via GitHub Actions takes 20+ minutes due to webhook latency, CI queue time, and polling delays. Speed matters because review must not become a bottleneck in the automated cycle.

## Running the Full Cycle

### Unattended execution

Start a plan and walk away:

```bash
# Plan the feature
claude "/plan-issue <feature description>"

# Implement all phases (runs 1-3+ hours unattended)
claude "/implement-issue <master-issue-number>"
```

### What you'll find when you come back

- Master issue closed with all sub-issues checked off
- One merged PR per sub-issue
- Review comments on each PR (from Codex)
- Follow-up issues for any non-critical review findings
- Any PRs labeled `needs-human-review` if critical findings were unresolved

### Dispatch via tmux

For background execution:

```bash
make dispatch ISSUE=670
```

This creates a worktree, opens a tmux window, and runs Claude with the implement-issue skill.

## Customization

### Tuning review behavior

The review prompt lives in `.claude/skills/review-pr.md`. Edit it to:
- Adjust what counts as critical vs. non-critical
- Add repo-specific review criteria (e.g., proto backwards compatibility rules)
- Change the Codex model (`codex-mini` for speed, larger models for depth)
- Modify the review output format

### Swapping review models

Replace `codex` with any CLI-based review tool:
- `claude` with a review-specific prompt (Claude Code Review)
- `codex` with a different model flag
- Any tool that can read a diff and produce structured findings

The skill file defines the integration — changing it is a one-file edit.

## Design Rationale

### Why cross-model review

Self-review (Claude reviewing its own PRs) produces reviews that confirm the implementation rather than challenge it. Using Codex — a different model family with different training, biases, and failure modes — provides genuinely independent evaluation.

### Why fix-forward

Blocking the automated cycle on non-critical findings (style, naming, minor edge cases) defeats the purpose of unattended execution. The cost of shipping a naming inconsistency is low; the cost of human-gating every PR is high (breaks the automated cycle, requires synchronous attention). Critical security and reliability issues are the exception because the cost of shipping those bugs is genuinely high.

### Why local execution

GitHub Actions cannot be customized per engineer. Local tools can. An engineer who wants stricter review adds criteria to their local prompt. An engineer who wants faster cycles swaps to a lighter model. The workflow adapts to the person, not the other way around.

### Why session handoff

Agent performance degrades after ~35 minutes of continuous work. Dispatching each sub-issue to a fresh sub-agent avoids this entirely. Each sub-agent starts with clean context loaded from GitHub (the issue body, the repo), not from accumulated conversational memory. The master issue checklist is the coordination mechanism between sessions.

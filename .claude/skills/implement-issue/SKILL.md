---
name: implement-issue
description: Implement a single GitHub issue end-to-end. Use this skill when the user provides a GitHub issue URL and asks to implement it, work on it, fix it, or resolve it. Triggers on phrases like "implement issue", "work on this issue", "fix this issue", or when given a GitHub issue URL alone. Handles the full workflow: fetch, branch, comment, implement, and open a PR. For parent issues with sub-issues, use /implement-plan instead.
version: 9.0.0
---

# Implement Issue

Full workflow for implementing a single GitHub issue: fetch the issue, create a feature branch, announce on the issue, implement using repository conventions, open a PR, and post a summary comment with wall clock timing.

For parent issues with sub-issues, use the `/implement-plan` skill instead -- it iterates over sub-issues, implements each one via this skill, runs code review/fix loops, and merges.

## Workflow

### 0. Start Wall Clock Timer

Record the start time immediately so total elapsed time can be reported at the end:

```bash
ISSUE_START_TIME=$(date +%s)
```

### 1. Fetch the Issue

Fetch the issue details using the gh CLI:

```bash
gh issue view <issue-number> --repo <owner/repo> --json number,title,body,labels
```

Parse the issue URL to extract the repo and issue number. URL formats:
- `https://github.com/owner/repo/issues/123` -> repo=`owner/repo`, number=`123`

If the issue body contains sub-issue references (task-list lines like `- [ ] #123`), this is a parent issue. **Stop and inform the user** to use `/implement-plan` instead. Do not attempt to implement a parent issue directly.

### 2. Name the Session

Rename the current session so the human operator can see what this agent is working on:

```
/rename #<number> <issue title>
```

For example, if implementing issue #42 "Add Playwright E2E test infrastructure":
```
/rename #42 Add Playwright E2E test infrastructure
```

### 3. Fetch Origin and Create Branch

```bash
git fetch origin
git checkout main
git pull origin main
git checkout -b <branch-name>
```

**Branch naming**: `feat/<number>-<slug>` where `<slug>` is the issue title converted to lowercase with spaces replaced by hyphens, truncated to ~40 chars. Examples:
- Issue #42 "Add Playwright E2E test infrastructure" -> `feat/42-add-playwright-e2e-test-infrastructure`
- Issue #7 "Fix token expiry handling" -> `feat/7-fix-token-expiry-handling`
- Issue #173 "Add role-based access control for secrets" -> `feat/173-add-role-based-access-control-for-secrets`

Strip special characters from the slug (keep only alphanumeric and hyphens).

### 4. Comment on the Issue

Post a comment announcing which agent is working on this issue:

```bash
gh issue comment <number> --repo <owner/repo> --body "$(cat <<'EOF'
Working on this issue.

- **Agent path**: <absolute path of working directory>
- **Hostname**: <hostname from `hostname` command>
- **Branch**: <branch-name>
EOF
)"
```

Get the values:
```bash
pwd          # working directory
hostname     # machine hostname
```

### 5. Read and Understand the Codebase

Before implementing:
1. Read `AGENTS.md` (or `CLAUDE.md`) for project conventions
2. Read `CONTRIBUTING.md` if it exists for commit message requirements
3. Explore the relevant code areas mentioned in the issue
4. Understand existing patterns before writing new code

### 6. Implement Using RED GREEN Approach

Follow the RED GREEN pattern from the repository's conventions:

1. **RED** -- Write failing tests first that define the expected behavior
2. **GREEN** -- Write the minimum implementation to make the tests pass
3. Make regular commits as you work -- each commit should be a logical unit

Commit message format (check CONTRIBUTING.md for the specific format):
```
<type>(<scope>): <short description>

<longer explanation if needed>
```

Run the relevant test commands to verify your implementation:
- `make test` -- all tests
- `make test-go` -- Go tests
- `make test-ui` -- UI unit tests
- `make generate` -- regenerate code if proto/schema files changed

**Always run `make generate` before committing** if any generated files might be affected.

#### When to run `make test-e2e` locally

Before opening the PR, assess whether your changes warrant a local E2E test run. Use the E2E relevance decision table in the `implement-plan` skill (Step 6a) to decide -- that table is the single source of truth for which file patterns are E2E-relevant.

**Local E2E is optional and best-effort.** Running `make test-e2e` requires `make certs` and a k3d cluster. If the environment does not support E2E (e.g., no cluster available), skip the local run and note this in the PR description so the CI E2E check covers it. Example:

```
> Local E2E was not run (no k3d cluster available). Relying on CI E2E check.
```

### 7. Final Cleanup Phase

Before opening the PR, scan for:
- Dead code introduced or made stale by the implementation
- Obsolete comments or outdated references
- Unused imports
- Stale documentation

Commit cleanup separately with a message explaining what was removed and why.

### 8. Open the PR

The PR must close the **specific issue being implemented**. When implementing a sub-issue dispatched from a parent issue, close the sub-issue number -- not the parent.

```bash
gh pr create --title "<concise title under 70 chars>" --body "$(cat <<'EOF'
## Summary
- <bullet points describing changes>

Closes #<issue-number>

## Test plan
- [ ] <specific things to verify>

Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

The `Closes #<number>` line automatically closes the issue when the PR is merged. Use the issue number you are implementing (the sub-issue number when dispatched from a parent).

### 9. Post Summary Comment

After opening the PR, calculate elapsed time and post a summary comment to the issue:

```bash
ISSUE_END_TIME=$(date +%s)
ELAPSED=$((ISSUE_END_TIME - ISSUE_START_TIME))
MINUTES=$((ELAPSED / 60))
SECONDS=$((ELAPSED % 60))

REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
BRANCH=$(git rev-parse --abbrev-ref HEAD)
PR_NUMBER=$(gh pr list --state open --head "$BRANCH" --json number --jq '.[0].number')

gh issue comment <number> --repo $REPO --body "$(cat <<EOF
## Implementation Complete

- **PR**: #${PR_NUMBER}
- **Branch**: \`${BRANCH}\`
- **Wall clock time**: ${MINUTES}m ${SECONDS}s

The PR is open and ready for review.
EOF
)"
```

**Stop here.** Do not loop on CI checks, capture screenshots, or merge. The PR is open and ready for review. The `/implement-plan` skill handles the review-fix-merge cycle via `/review-pr` when orchestrating sub-issues. When `/implement-issue` is invoked standalone, a human or separate agent should review before merging.

## Key Conventions

- **No backwards compatibility**: This code is not yet released; breaking changes are fine
- **RED GREEN**: Write tests before implementation
- **Regular commits**: Commit logical units as you go, not one giant commit at the end
- **make generate**: Always run before committing if proto or generated files are involved
- **E2E decision-making**: Assess whether `make test-e2e` is warranted using the E2E relevance heuristic (see Step 6). Run it locally when relevant and the environment supports it; otherwise note the skip in the PR description
- **Cleanup phase**: Every implementation ends with a cleanup commit
- **Wall clock timing**: Record start time at step 0, post elapsed time in the summary comment at step 9
- **Stop at PR**: Do not loop on CI, capture screenshots, or merge -- stop after opening the PR. The `/implement-plan` skill handles review and merge
- **Single issues only**: If the issue has sub-issues, stop and direct the user to `/implement-plan`
- **Close the right issue**: PRs close the specific issue being worked on (`Closes #<sub-issue>` for sub-issues, not the parent)

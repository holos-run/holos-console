---
name: implement-issue
description: Implement a GitHub issue end-to-end. Use this skill when the user provides a GitHub issue URL and asks to implement it, work on it, fix it, or resolve it. Triggers on phrases like "implement issue", "work on this issue", "fix this issue", or when given a GitHub issue URL alone. Handles the full workflow: fetch, branch, comment, implement, open PR, fix CI, capture screenshots, and merge.
version: 3.0.0
---

# Implement Issue

Full workflow for implementing a GitHub issue: fetch the issue, create a feature branch, announce on the issue, implement using repository conventions, open a PR, loop on CI until green, capture screenshots for frontend changes, and merge.

## Workflow

### 1. Fetch the Issue

Fetch the issue details using the gh CLI:

```bash
gh issue view <issue-number> --repo <owner/repo> --json number,title,body,labels
```

Parse the issue URL to extract the repo and issue number. URL formats:
- `https://github.com/owner/repo/issues/123` → repo=`owner/repo`, number=`123`

### 1b. Check for Sub-Issues

After fetching the issue, check its body for sub-issues. Sub-issues are indicated by task-list lines referencing other issues in the same repo, matching patterns like:

- `- [ ] #123 — description`
- `- [x] #123 — description`
- `- [ ] #123`

Extract all referenced issue numbers from these patterns using a regex like `#(\d+)`.

**If sub-issues are found**, do NOT proceed with the normal implementation workflow. Instead:

1. For each sub-issue number **in order** (sequentially, not in parallel):
   a. Launch an Agent tool call with `subagent_type: "general-purpose"` and `model: "sonnet"`.
   b. The agent prompt must instruct it to invoke the `/implement-issue` skill with the sub-issue number as the argument. Include the repo context (owner/repo) and working directory so the agent has full context.
   c. **Wait for the agent to complete** before starting the next sub-issue. Each sub-issue may depend on the prior one (e.g., proto changes before backend, backend before frontend).
   d. After each agent completes, pull main to pick up the merged changes: `git checkout main && git pull origin main`.

2. After all sub-issues are implemented and merged, close the parent issue:
   ```bash
   gh issue close <parent-number> --repo <owner/repo> --comment "All sub-issues implemented and merged."
   ```

3. **Stop here** — do not continue to step 2 or beyond. The parent issue has no implementation of its own; the sub-issues cover all the work.

**If no sub-issues are found**, continue with step 2 below (normal single-issue workflow).

### 2. Fetch Origin and Create Branch

```bash
git fetch origin
git checkout main
git pull origin main
git checkout -b <branch-name>
```

**Branch naming**: `feat/<number>-<slug>` where `<slug>` is the issue title converted to lowercase with spaces replaced by hyphens, truncated to ~40 chars. Examples:
- Issue #42 "Add Playwright E2E test infrastructure" → `feat/42-add-playwright-e2e-test-infrastructure`
- Issue #7 "Fix token expiry handling" → `feat/7-fix-token-expiry-handling`
- Issue #173 "Add role-based access control for secrets" → `feat/173-add-role-based-access-control-for-secrets`

Strip special characters from the slug (keep only alphanumeric and hyphens).

### 3. Comment on the Issue

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

### 4. Read and Understand the Codebase

Before implementing:
1. Read `AGENTS.md` (or `CLAUDE.md`) for project conventions
2. Read `CONTRIBUTING.md` if it exists for commit message requirements
3. Explore the relevant code areas mentioned in the issue
4. Understand existing patterns before writing new code

### 5. Implement Using RED GREEN Approach

Follow the RED GREEN pattern from the repository's conventions:

1. **RED** — Write failing tests first that define the expected behavior
2. **GREEN** — Write the minimum implementation to make the tests pass
3. Make regular commits as you work — each commit should be a logical unit

Commit message format (check CONTRIBUTING.md for the specific format):
```
<type>(<scope>): <short description>

<longer explanation if needed>
```

Run the relevant test commands to verify your implementation:
- `make test` — all tests
- `make test-go` — Go tests
- `make test-ui` — UI unit tests
- `make generate` — regenerate code if proto/schema files changed

**Always run `make generate` before committing** if any generated files might be affected.

### 6. Final Cleanup Phase

Before opening the PR, scan for:
- Dead code introduced or made stale by the implementation
- Obsolete comments or outdated references
- Unused imports
- Stale documentation

Commit cleanup separately with a message explaining what was removed and why.

### 7. Open the PR

```bash
gh pr create --title "<concise title under 70 chars>" --body "$(cat <<'EOF'
## Summary
- <bullet points describing changes>

Closes: #<issue-number>

## Test plan
- [ ] <specific things to verify>

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

The `Closes: #<number>` line automatically closes the issue when the PR is merged.

After creating the PR, post a comment explaining the rationale and motivation for the implementation approach, including alternatives considered and why they were rejected.

### 8. Loop on CI Checks Until Green

After pushing and creating the PR, watch CI and fix failures in a loop:

```
repeat:
  1. Watch CI checks:  gh pr checks <N> --watch
  2. If all checks pass → break out of loop
  3. If any check fails:
     a. Read the failed check logs:  gh run view <run-id> --log-failed
     b. Diagnose and fix the failure
     c. Commit the fix with a descriptive message
     d. Push and loop back to step 1
```

**Important**: Do not give up after one failure. Keep iterating until all checks are green. Common CI failures include:
- Lint errors (formatting, vet issues)
- Test failures in other packages affected by the change
- E2E test flakes (retry once before investigating)

### 9. Visual Verification for Frontend Changes

**After CI is green**, check whether the PR includes frontend changes (files under `frontend/src/`, changes to UI components, routes, or styles). If it does, screenshots are **required** before the PR is complete.

#### Determine if screenshots are needed

Frontend changes are indicated by modifications to:
- `frontend/src/**/*.tsx` or `frontend/src/**/*.ts` (excluding test files)
- `frontend/src/**/*.css`
- Proto files that affect UI-visible types

If none of these are changed, skip to step 10.

#### Capture screenshots

1. **Write a capture script** at `scripts/pr-<N>/capture` (for complex cases with multiple pages or K8s fixtures), or use `--url` for simple single-page captures.

2. **Run the capture**:
   ```bash
   # Simple case (single page):
   scripts/browser-capture-pr <N> --url /path/to/affected/page

   # Complex case (capture script handles navigation):
   scripts/browser-capture-pr <N>
   ```

3. **Commit screenshots** to the feature branch:
   ```bash
   git add docs/screenshots/pr-<N>/
   git commit -m "docs: add visual verification screenshots for PR #<N>"
   git push
   ```

4. **Post screenshots to the PR** using the commit SHA for stable URLs:
   ```bash
   SHA=$(git rev-parse HEAD)
   gh pr comment <N> --body "$(cat <<EOF
   ## Visual Verification

   ![screenshot description](https://raw.githubusercontent.com/<owner>/<repo>/${SHA}/docs/screenshots/pr-<N>/screenshot.png)

   Captured with \`scripts/browser-capture-pr <N>\`.
   EOF
   )"
   ```

5. **Loop on CI again** after the screenshot commit — the push may trigger another CI run. Watch and fix if needed.

### 10. Merge the PR

Once all CI checks are green (and screenshots are posted if applicable):

```bash
gh pr merge <N> --merge
```

Use `--merge` (not squash or rebase) so that commit SHAs referenced in screenshot URLs remain reachable in the main branch history.

## Key Conventions

- **No backwards compatibility**: This code is not yet released; breaking changes are fine
- **RED GREEN**: Write tests before implementation
- **Regular commits**: Commit logical units as you go, not one giant commit at the end
- **make generate**: Always run before committing if proto or generated files are involved
- **Cleanup phase**: Every implementation ends with a cleanup commit
- **CI loop**: Never leave a PR with failing checks — fix until green
- **Screenshots for frontend PRs**: Required before the PR is considered complete
- **Merge commits**: Always merge with `--merge`, never squash or rebase

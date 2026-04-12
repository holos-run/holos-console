---
name: implement-issue
description: Implement a single GitHub issue end-to-end. Use this skill when the user provides a GitHub issue URL and asks to implement it, work on it, fix it, or resolve it. Triggers on phrases like "implement issue", "work on this issue", "fix this issue", or when given a GitHub issue URL alone. Handles the full workflow: fetch, branch, comment, implement, open a PR, run code review, fix findings, wait for CI, and merge. For parent issues with sub-issues, use /implement-plan instead.
version: 10.0.0
---

# Implement Issue

Full workflow for implementing a single GitHub issue: fetch the issue, create a feature branch, announce on the issue, implement using repository conventions, open a PR, run code review (before waiting for CI), fix findings, wait for CI checks, and merge.

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

### 9. Review the PR (Round 1)

Run the `/review-pr` skill immediately after opening the PR -- do NOT wait for CI checks first. Code review runs in parallel with CI to minimize wall clock time.

Detect the PR number first:

```bash
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
BRANCH=$(git rev-parse --abbrev-ref HEAD)
PR_NUMBER=$(gh pr list --state open --head "$BRANCH" --json number --jq '.[0].number')
```

Then invoke the review skill:

```
/review-pr $PR_NUMBER
```

After the review completes, parse the result for:
- **Verdict**: APPROVE or REQUEST_CHANGES
- **Critical count**: Number of `[CRITICAL]` findings
- **Important count**: Number of `[IMPORTANT]` findings
- **Style count**: Number of `[STYLE]` findings

**If APPROVE (no findings):** Skip to step 11 (wait for CI).

**If any findings exist (any severity):** Proceed to step 10 (fix findings).

### 10. Fix-First Review Loop

The review follows a **fix-first** model. Round 1 findings are fixed directly in the PR -- no follow-up issues are created after round 1. Follow-up issues are only created for findings that persist after round 2.

#### 10a. Fix ALL Findings (Round 1)

Read the review output:

```bash
REVIEW_FILE="tmp/review-pr/pr-${PR_NUMBER}/round-1.md"
```

Fix **all** findings -- critical, important, and style. For each finding:
1. Read the cited file and line
2. Understand the issue
3. Apply the concrete fix suggested (or a better one if you disagree -- leave a comment on the PR explaining why)
4. Run relevant tests to verify the fix

After all fixes:
- Run `make generate && make test` to ensure nothing is broken
- Commit: `fix: address codex review round 1 findings`
- Push the fixes

#### 10b. Re-Review (Round 2)

Run the review skill again:

```
/review-pr $PR_NUMBER
```

Parse the result:

- **If APPROVE (no findings):** Proceed to step 11 (wait for CI).
- **If style-only findings remain (no CRITICAL or IMPORTANT):** Proceed to step 11 (wait for CI). Create a follow-up issue for the remaining style findings after merge (step 12).
- **If critical or important findings remain:** Proceed to step 10c (escalation).

#### 10c. Escalation After Round 2

If critical or important findings remain after round 2:

1. Post a summary comment on the PR listing the unresolved findings:

```bash
gh pr comment $PR_NUMBER --body "$(cat <<'EOF'
## Unresolved Critical/Important Findings

After 2 review rounds, the following critical or important findings remain unresolved:

<list each unresolved critical/important finding with file, line, and description>

This PR requires human review before merge.
EOF
)"
```

2. Add the `needs-human-review` label:

```bash
gh pr edit $PR_NUMBER --add-label "needs-human-review"
```

3. Do NOT merge the PR. Skip to step 13 (post summary comment) with result ESCALATED.

### 11. Wait for CI Checks and Fix Failures

After code review is complete (and findings are fixed), wait for CI checks to pass.

#### 11a. Assess E2E Relevance

Examine the PR's changed files to determine whether E2E tests are relevant:

```bash
CHANGED_FILES=$(gh pr diff $PR_NUMBER --name-only)
```

Apply the following decision table:

| Changed files match | E2E relevant? | Reason |
|---------------------|---------------|--------|
| `frontend/src/routes/**` | YES | UI routes affect user-facing behavior |
| `frontend/src/components/**` | YES | Shared UI components may affect rendered pages |
| `frontend/src/lib/**` | YES | Auth, transport, or query logic affects runtime |
| `console/oidc/**` | YES | OIDC provider affects the login flow |
| Existing files in `console/rpc/**` | YES | Existing RPC handler changes may affect UI |
| `console/console.go` | YES | Server setup or route registration affects E2E |
| `frontend/e2e/**` | YES | E2E tests themselves should run E2E |
| `frontend/package.json`, `frontend/tsconfig*.json` | YES | Package deps and TS config affect runtime |
| `proto/**` (new messages only) | NO | New proto definitions don't affect existing UI |
| `gen/**`, `frontend/src/gen/**` | NO | Generated code, not hand-edited |
| New Go files (new packages, new handlers) | NO | No existing UI integration to break |
| `docs/**`, `*_test.go`, `*.test.ts`, `*.test.tsx` | NO | Docs and test-only changes |
| `.claude/**`, `.github/**`, `Makefile`, `*.md` | NO | Tooling and config |

**Tie-breaking**: If any changed file matches a YES row, E2E is relevant.

#### 11b. Wait for CI Checks

**If E2E is relevant**, wait for all checks:

```bash
gh pr checks $PR_NUMBER --watch --fail-level all
```

**If E2E is NOT relevant**, poll until test and lint checks reach a terminal state:

```bash
CI_PASSED=false
API_ERRORS=0
while true; do
  CHECKS=$(gh pr checks $PR_NUMBER --json name,bucket 2>/dev/null)
  if [ $? -ne 0 ] || [ -z "$CHECKS" ]; then
    API_ERRORS=$((API_ERRORS + 1))
    echo "gh pr checks failed (attempt $API_ERRORS)"
    if [ "$API_ERRORS" -ge 5 ]; then
      echo "Too many consecutive API errors ($API_ERRORS) -- treating as CI failure"
      break
    fi
    sleep 30
    continue
  fi
  API_ERRORS=0

  TEST_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "Unit Tests") | .bucket' 2>/dev/null)
  LINT_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "Lint") | .bucket' 2>/dev/null)

  is_terminal() { [ "$1" = "pass" ] || [ "$1" = "fail" ] || [ "$1" = "cancel" ] || [ "$1" = "skipping" ]; }

  if is_terminal "$TEST_BUCKET" && is_terminal "$LINT_BUCKET"; then
    if [ "$TEST_BUCKET" = "pass" ] && [ "$LINT_BUCKET" = "pass" ]; then
      echo "test and lint passed -- proceeding without waiting for e2e"
      CI_PASSED=true
      break
    else
      echo "CI failed: test=$TEST_BUCKET lint=$LINT_BUCKET"
      break
    fi
  fi

  sleep 30
done
```

#### 11c. Fix CI Failures

If CI failed, diagnose and fix the failures:

1. Run `gh pr checks $PR_NUMBER` to see which checks failed
2. Read the CI logs to understand the failure
3. Fix the issue locally, run `make generate && make test` to verify
4. Commit and push the fix

Re-check CI after the fix. If CI still fails after one fix attempt, add the `needs-human-review` label and skip to step 13 with result ESCALATED.

### 12. Merge the PR

Before merging, handle any remaining style-only findings from round 2:

**If style-only findings remain after round 2** (no CRITICAL or IMPORTANT), create a follow-up issue:

```bash
FOLLOW_UP=$(gh issue create \
  --title "fix: address review findings from PR #${PR_NUMBER}" \
  --body "$(cat <<EOF
## Context

PR #${PR_NUMBER} was merged with style-only review findings remaining after round 2 fixes.

## Findings

<paste remaining style findings from round 2 review output>

## Source

Review output: \`tmp/review-pr/pr-${PR_NUMBER}/round-2.md\`
EOF
)")
```

**Merge the PR:**

```bash
# Mark PR as ready (remove draft status)
gh pr ready $PR_NUMBER

# Merge with merge commit (preserves commit SHAs)
gh pr merge $PR_NUMBER --merge --delete-branch
```

If the merge fails due to conflicts, rebase and retry:

```bash
git fetch origin
git rebase origin/main
git push --force-with-lease
gh pr merge $PR_NUMBER --merge --delete-branch
```

After successful merge, verify the issue was closed:

```bash
gh issue view <issue-number> --json state -q .state
```

### 13. Post Summary Comment

Calculate elapsed time and post a summary comment to the issue:

```bash
ISSUE_END_TIME=$(date +%s)
ELAPSED=$((ISSUE_END_TIME - ISSUE_START_TIME))
MINUTES=$((ELAPSED / 60))
SECONDS=$((ELAPSED % 60))

gh issue comment <number> --repo $REPO --body "$(cat <<EOF
## Implementation Complete

- **PR**: #${PR_NUMBER}
- **Branch**: \`${BRANCH}\`
- **Result**: <MERGED | ESCALATED>
- **Review rounds**: <count>
- **Wall clock time**: ${MINUTES}m ${SECONDS}s
<if follow-up issue>
- **Follow-up**: #<follow-up-number> (style-only review findings)
</if>
EOF
)"
```

## Key Conventions

- **No backwards compatibility**: This code is not yet released; breaking changes are fine
- **RED GREEN**: Write tests before implementation
- **Regular commits**: Commit logical units as you go, not one giant commit at the end
- **make generate**: Always run before committing if proto or generated files are involved
- **E2E decision-making**: Assess whether `make test-e2e` is warranted using the E2E relevance heuristic (see Step 6). Run it locally when relevant and the environment supports it; otherwise note the skip in the PR description
- **Cleanup phase**: Every implementation ends with a cleanup commit
- **Wall clock timing**: Record start time at step 0, post elapsed time in the summary comment at step 13
- **Review before CI**: Run code review immediately after opening the PR, before waiting for CI checks. This minimizes wall clock time by running review in parallel with CI
- **Fix-first model**: Fix ALL findings (critical, important, style) in-PR after round 1. Only create follow-up issues for style-only findings that persist after round 2
- **Merge authority**: Merge after review is clean and CI passes. Escalate with `needs-human-review` if critical/important findings persist after round 2 or CI fails after one fix attempt
- **Single issues only**: If the issue has sub-issues, stop and direct the user to `/implement-plan`
- **Close the right issue**: PRs close the specific issue being worked on (`Closes #<sub-issue>` for sub-issues, not the parent)

## Merge Authority

| Situation | Action |
|-----------|--------|
| Clean review (APPROVE, no findings), CI green | Merge immediately |
| All findings fixed, clean re-review, CI green | Merge after clean re-review |
| Style-only findings remain after round 2, CI green | Merge, create follow-up issue |
| Critical or important findings unresolved after round 2 | Do NOT merge, add `needs-human-review` label |
| CI failures unresolved after 1 fix attempt | Do NOT merge, add `needs-human-review` label |

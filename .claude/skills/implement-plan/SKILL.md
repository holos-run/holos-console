---
name: implement-plan
description: Execute a full implementation plan from a parent GitHub issue with sub-issues. Iterates over each sub-issue, implements it with a Claude Opus sub-agent, runs two code review/fix loops using the review-pr skill, and merges or escalates. Triggers on phrases like "implement plan", "execute plan", "run the plan", "implement parent issue", or when given a parent issue URL with sub-issues.
version: 2.0.0
---

# Implement Plan

Automated implementation cycle for a parent GitHub issue containing sub-issues. Each sub-issue is implemented by an Opus sub-agent, reviewed by the `/review-pr` skill (Codex backend) in a sub-agent, fixed by an Opus sub-agent if needed, and merged or escalated -- all without human intervention unless critical findings persist.

## Arguments

`{{ARGUMENTS}}` is a GitHub issue number or URL:

- **`<number>`** -- e.g., `/implement-plan 42`
- **`<url>`** -- e.g., `/implement-plan https://github.com/owner/repo/issues/42`

## Workflow

### 1. Resolve the Parent Issue

Parse `{{ARGUMENTS}}` to extract the repo and issue number.

```bash
# Determine repo from git remote
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)

# Fetch parent issue
gh issue view <number> --repo $REPO --json number,title,body,state
```

### 2. Name the Session

Rename the current session so the human operator can see what this agent is working on:

```
/rename Plan #<number> <parent issue title>
```

For example, if executing plan issue #42 "Add RBAC for secrets":
```
/rename Plan #42 Add RBAC for secrets
```

### 3. Extract Sub-Issues

Parse the parent issue body for sub-issue references. Sub-issues appear as task-list lines:

```
- [ ] #123 -- description
- [x] #456 -- description (already done)
- [ ] #789
```

Extract all referenced issue numbers using a regex like `#(\d+)`. Determine checked status from `[x]` vs `[ ]`.

Build an ordered list of sub-issues. Skip any that are already checked off (`[x]`).

If no sub-issues are found, treat the issue as a single issue and delegate directly to `/implement-issue`.

If all sub-issues are already checked off, comment on the parent issue that all work is complete and stop.

### 4. Iterate Over Sub-Issues

For each unchecked sub-issue, execute the full implement-review-merge cycle (steps 5-9). Process sub-issues **sequentially** in the order they appear in the parent issue body.

Before starting each sub-issue, update the session name:

```
/rename #<sub-number> <sub-issue title> (plan #<parent-number>)
```

After each sub-issue completes (merged or escalated), move to the next one.

### 5. Implement the Sub-Issue

Launch an Opus sub-agent to implement the sub-issue. The sub-agent invokes `/implement-issue` which handles branching, coding, testing, and opening a draft PR.

```
Agent(
  description: "Implement issue #<N>",
  model: "opus",
  prompt: "You are working in <working-directory>. Invoke the /implement-issue skill with argument '<N>' to implement GitHub issue #<N> for the repo <owner/repo>. Follow all repository conventions in AGENTS.md. Do not merge the PR -- stop after opening it."
)
```

**Wait for the sub-agent to complete before proceeding.**

After the sub-agent finishes, detect the PR number by filtering on the current branch name to avoid selecting an unrelated PR:

```bash
# The sub-agent created a branch and opened a PR
# Detect the branch name, then find the PR for that specific branch
BRANCH=$(git rev-parse --abbrev-ref HEAD)
PR_NUMBER=$(gh pr list --state open --head "$BRANCH" --json number --jq '.[0].number')
```

If no PR was found, the implementation sub-agent may have failed. Fall back to searching by sub-issue reference in PR bodies:

```bash
# Fallback: search for a PR that closes the sub-issue
PR_NUMBER=$(gh pr list --state open --json number,body --jq '.[] | select(.body | test("Closes:?\\s*#<SUB_ISSUE_NUMBER>")) | .number' | head -1)
```

If still not found, comment on the sub-issue that implementation failed and continue to the next sub-issue.

### 6. Wait for CI

After the PR is open, assess whether E2E tests are relevant to this change, then wait for the appropriate CI checks.

#### 6a. Assess E2E Relevance

Examine the PR's changed files to determine whether E2E tests are relevant:

```bash
CHANGED_FILES=$(gh pr diff $PR_NUMBER --name-only)
```

Apply the following decision table to classify the change:

| Changed files match | E2E relevant? | Reason |
|---------------------|---------------|--------|
| `frontend/src/routes/**` | YES | Changes to existing UI routes affect user-facing behavior |
| `frontend/src/components/**` | YES | Changes to shared UI components may affect rendered pages |
| `frontend/src/lib/**` | YES | Changes to auth, transport, or query logic affect runtime behavior |
| `console/oidc/**` | YES | Changes to OIDC provider affect the login flow tested by E2E |
| Existing files in `console/rpc/**` | YES | Changes to existing RPC handlers may affect UI data flow |
| `console/console.go` | YES | Changes to server setup or route registration affect E2E |
| `frontend/e2e/**` | YES | Changes to E2E tests themselves should run E2E |
| `proto/**` (new messages only, no field changes to existing messages) | NO | New proto definitions do not affect existing UI behavior |
| `gen/**`, `frontend/src/gen/**` | NO | Generated code from proto changes; not hand-edited |
| New Go files (new packages, new handlers) | NO | New backend code has no existing UI integration to break |
| `docs/**` | NO | Documentation does not affect runtime behavior |
| `*_test.go`, `*.test.ts`, `*.test.tsx` | NO | Test-only changes do not affect runtime behavior |
| `.claude/**`, `.github/**` | NO | Tooling and CI config do not affect runtime behavior |
| `Makefile`, `*.md`, `*.json` (config files) | NO | Build and config files do not affect E2E behavior |

**Tie-breaking rule**: If any changed file matches a YES row, E2E is relevant. The heuristic errs on the side of waiting -- when uncertain, treat E2E as relevant. A false positive (waiting when not needed) costs 15 minutes; a false negative (skipping when needed) risks merging broken code.

Log the assessment reasoning so operators can audit the decision:

```bash
echo "E2E relevance assessment for PR #$PR_NUMBER:"
echo "Changed files:"
echo "$CHANGED_FILES"
echo ""
echo "Decision: E2E_RELEVANT=<yes|no>"
echo "Reason: <one-line explanation>"
```

#### 6b. Wait for CI Checks (Conditional)

**If E2E is relevant**, wait for all checks (existing behavior):

```bash
gh pr checks $PR_NUMBER --watch --fail-level all
```

**If E2E is NOT relevant**, wait only for `test` and `lint` checks, ignoring the `e2e` check:

```bash
# Poll until test and lint checks have completed (pass or fail)
while true; do
  CHECKS=$(gh pr checks $PR_NUMBER --json name,state,conclusion 2>/dev/null || echo "[]")

  TEST_STATE=$(echo "$CHECKS" | jq -r '.[] | select(.name == "test") | .state' 2>/dev/null)
  LINT_STATE=$(echo "$CHECKS" | jq -r '.[] | select(.name == "lint") | .state' 2>/dev/null)

  # Both must have completed (state is no longer empty or "pending"/"queued")
  if [ "$TEST_STATE" = "COMPLETED" ] && [ "$LINT_STATE" = "COMPLETED" ]; then
    TEST_CONCLUSION=$(echo "$CHECKS" | jq -r '.[] | select(.name == "test") | .conclusion' 2>/dev/null)
    LINT_CONCLUSION=$(echo "$CHECKS" | jq -r '.[] | select(.name == "lint") | .conclusion' 2>/dev/null)

    if [ "$TEST_CONCLUSION" = "SUCCESS" ] && [ "$LINT_CONCLUSION" = "SUCCESS" ]; then
      echo "test and lint passed -- proceeding without waiting for e2e"
      break
    else
      echo "CI failed: test=$TEST_CONCLUSION lint=$LINT_CONCLUSION"
      break
    fi
  fi

  sleep 30
done
```

#### 6c. Fix CI Failures

If CI fails (either all-check mode or test/lint-only mode), launch an Opus sub-agent to fix the failures:

```
Agent(
  description: "Fix CI for PR #<PR_NUMBER>",
  model: "opus",
  prompt: "You are working in <working-directory> on branch <branch>. PR #<PR_NUMBER> has failing CI checks. Run `gh pr checks <PR_NUMBER>` to see failures, then fix them. Run `make generate && make test` to verify locally before pushing. Commit fixes and push."
)
```

Re-check CI after the fix. If CI still fails after one fix attempt, add the `needs-human-review` label and continue to the review step anyway (the review may catch the root cause).

**Note on skipped E2E**: If E2E was assessed as not relevant and later the `e2e` check fails independently, the CI Fix sub-agent should still attempt to fix it if encountered, but E2E failure alone does not block the review cycle when E2E was assessed as not relevant.

### 7. Review the PR (Round 1)

Launch a sub-agent to run the `/review-pr` skill. The review runs in a sub-agent to preserve the main agent's context window and to ensure the Codex review process is isolated.

```
Agent(
  description: "Review PR #<PR_NUMBER> round 1",
  prompt: "You are working in <working-directory> on the branch for PR #<PR_NUMBER>. Run the /review-pr skill with argument '<PR_NUMBER>'. Report the verdict (APPROVE or REQUEST_CHANGES), the count of critical/important/style findings, and the path to the review output file."
)
```

**Wait for the review sub-agent to complete.** Parse its response for:
- **Verdict**: APPROVE or REQUEST_CHANGES
- **Critical count**: Number of `[CRITICAL]` findings
- **Important count**: Number of `[IMPORTANT]` findings
- **Style count**: Number of `[STYLE]` findings

**If APPROVE (no findings):** Skip to step 9 (merge).

**If non-critical findings only (no CRITICAL):** Skip to step 9 (merge with follow-up).

**If critical findings exist:** Proceed to step 8 (fix).

### 8. Fix-Review Loop (Up to 2 Rounds)

Execute up to 2 fix-review rounds for critical findings. Each round:

#### 7a. Fix Critical Findings

Read the review output to understand what needs fixing:

```bash
REVIEW_FILE="tmp/review-pr/pr-${PR_NUMBER}/round-<R>.md"
```

Launch an Opus sub-agent to fix the critical findings:

```
Agent(
  description: "Fix review findings PR #<PR_NUMBER> round <R>",
  model: "opus",
  prompt: "You are working in <working-directory> on the branch for PR #<PR_NUMBER>.

The Codex code review (round <R>) found critical issues that must be fixed before merge. The review is at: <REVIEW_FILE>

Read the review file, then fix each [CRITICAL] finding. For each fix:
1. Read the cited file and line
2. Understand the issue
3. Apply the concrete fix suggested (or a better one if you disagree -- leave a comment on the PR explaining why)
4. Run relevant tests to verify the fix

After all critical fixes:
- Run `make generate && make test` to ensure nothing is broken
- Commit: 'fix: address codex review round <R> critical findings'
- Push the fixes

Do NOT fix [IMPORTANT] or [STYLE] findings -- those are tracked separately."
)
```

**Wait for the fix sub-agent to complete.**

#### 7b. Re-Review

Launch another review sub-agent:

```
Agent(
  description: "Review PR #<PR_NUMBER> round <R+1>",
  prompt: "You are working in <working-directory> on the branch for PR #<PR_NUMBER>. Run the /review-pr skill with argument '<PR_NUMBER>'. This is a re-review after fixes were applied. Report the verdict, finding counts, and output file path."
)
```

Parse the result. If APPROVE or non-critical only, proceed to merge. If critical findings remain and this was round 2, proceed to escalation.

#### 7c. Escalation After 2 Rounds

If critical findings remain after 2 fix-review rounds:

1. Post a summary comment on the PR listing the unresolved critical findings:

```bash
gh pr comment $PR_NUMBER --body "$(cat <<'EOF'
## Unresolved Critical Findings

After 2 review rounds, the following critical findings remain unresolved:

<list each unresolved critical finding with file, line, and description>

This PR requires human review before merge.
EOF
)"
```

2. Add the `needs-human-review` label:

```bash
gh pr edit $PR_NUMBER --add-label "needs-human-review"
```

3. Do NOT merge the PR.
4. Continue to the next sub-issue.

### 9. Merge the PR

Before merging, handle any non-critical findings:

**If non-critical findings exist**, create a follow-up issue:

```bash
gh issue create \
  --title "fix: address non-critical review findings from PR #${PR_NUMBER}" \
  --body "$(cat <<'EOF'
## Context

PR #<PR_NUMBER> was merged with non-critical review findings that should be addressed in a follow-up.

## Findings

<paste non-critical findings from the review output>

## Source

Review output: `tmp/review-pr/pr-<PR_NUMBER>/round-<R>.md`

Parent plan: #<parent-issue-number>
EOF
)"
```

**Merge the PR:**

```bash
# Mark PR as ready (remove draft status)
gh pr ready $PR_NUMBER

# Merge with merge commit (not squash -- preserves commit SHAs for screenshot URLs)
gh pr merge $PR_NUMBER --merge --delete-branch
```

Wait for the merge to complete. If it fails (e.g., merge conflict from another PR that landed), rebase and retry:

```bash
git fetch origin
git rebase origin/main
git push --force-with-lease
gh pr merge $PR_NUMBER --merge --delete-branch
```

After successful merge, update the parent issue to check off this sub-issue (the `Closes #<N>` in the PR body handles this automatically via GitHub, but verify):

```bash
# Verify the sub-issue was closed
gh issue view <sub-issue-number> --json state -q .state
```

### 10. Reset for Next Sub-Issue

After merge, prepare the working directory for the next sub-issue:

```bash
git checkout main
git pull origin main
```

Return to step 4 to process the next unchecked sub-issue.

### 11. Completion

After all sub-issues have been processed (merged or escalated), post a summary comment on the parent issue:

```bash
gh issue comment <parent-number> --body "$(cat <<'EOF'
## Plan Execution Complete

All sub-issues have been processed:

<for each sub-issue>
- #<N> <title>: <MERGED | ESCALATED (needs-human-review) | FAILED>
  - PR: #<PR_NUMBER>
  - Review rounds: <count>
  - Non-critical follow-up: #<follow-up-issue> (if any)
</for each>

<if any escalated>
**Action required**: Some PRs have the `needs-human-review` label and were not merged. Please review the critical findings and merge manually.
</if>
EOF
)"
```

If all sub-issues were successfully merged, close the parent issue if it's still open:

```bash
# Only close if all sub-issues are done
OPEN_SUBS=$(gh issue view <parent-number> --json body -q .body | grep -c '\- \[ \]' || true)
if [ "$OPEN_SUBS" = "0" ]; then
  gh issue close <parent-number> --comment "All phases implemented and merged."
fi
```

## Sub-Agent Isolation Model

Each sub-agent runs in the same working directory but is isolated by purpose:

| Sub-Agent | Model | Purpose | Reads | Writes |
|-----------|-------|---------|-------|--------|
| Implement | Opus | Code the sub-issue | Issue body, codebase | Branch, commits, PR |
| Review | Default | Run `/review-pr` (Codex) | PR diff, conventions | Review file, GitHub review |
| Fix | Opus | Fix critical findings | Review file, codebase | Commits, push |
| CI Fix | Opus | Fix CI failures | CI output, codebase | Commits, push |

Using sub-agents preserves the orchestrator's context window. The orchestrator only tracks issue state, PR numbers, and verdicts -- not the full implementation or review details.

## Guardrails

### Merge Authority

| Situation | Action |
|-----------|--------|
| Clean review (APPROVE, no findings) | Merge immediately |
| Non-critical findings only | Merge, create follow-up issue |
| Critical findings resolved within 2 rounds | Merge after clean re-review |
| Critical findings unresolved after 2 rounds | Do NOT merge, add `needs-human-review` label |
| CI failures unresolved after 1 fix attempt | Add `needs-human-review` label, continue to review |

### Safety

- **One sub-issue at a time**: Sub-issues are processed sequentially. No parallel PRs.
- **No force merges**: If a merge fails due to conflicts, rebase and retry once. If it fails again, escalate.
- **Review isolation**: The Codex review runs in a separate sub-agent process with no influence from the implementing agent. This ensures independent cross-model review.
- **Fix scope**: Fix sub-agents address only critical findings. Non-critical findings are tracked in follow-up issues, not fixed inline.
- **Escalation is permanent**: Once a PR gets `needs-human-review`, the agent does not re-attempt it. A human must intervene.

### Commit Conventions

- Implementation commits follow CONTRIBUTING.md format
- Fix commits: `fix: address codex review round <R> critical findings`
- Each review round is tracked in `tmp/review-pr/pr-<N>/round-<R>.md`

## Prerequisites

- **Claude Code**: With Agent tool and Opus model access
- **Codex CLI**: Required by the `/review-pr` skill (`npm install -g @openai/codex`)
- **GitHub CLI**: `gh` authenticated with repo access
- **Git**: Clean working directory on `main` branch
- **make**: Build toolchain for `make generate`, `make test`

## Examples

```
# Execute a plan with 3 sub-issues
/implement-plan 42

# Execute from a URL
/implement-plan https://github.com/holos-run/holos-console/issues/42
```

The skill will:
1. Fetch issue #42, find sub-issues #43, #44, #45
2. Implement #43 -> PR #50 -> review -> fix -> merge
3. Implement #44 -> PR #51 -> review -> approve -> merge
4. Implement #45 -> PR #52 -> review -> critical findings persist -> escalate
5. Post summary on #42 with results

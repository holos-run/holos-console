---
name: implement-plan
description: Execute a full implementation plan from a parent GitHub issue with sub-issues. Iterates over each sub-issue, implements it with a Claude Opus sub-agent, runs a fix-first code review loop using the review-pr skill, and merges or escalates. Implements follow-up issues created during review. Posts wall clock timing summaries to each issue. Triggers on phrases like "implement plan", "execute plan", "run the plan", "implement parent issue", or when given a parent issue URL with sub-issues.
version: 3.0.0
---

# Implement Plan

Automated implementation cycle for a parent GitHub issue containing sub-issues. Each sub-issue is implemented by an Opus sub-agent, reviewed by the `/review-pr` skill (Codex backend) in a sub-agent, fixed in-PR if findings exist, and merged or escalated -- all without human intervention unless critical findings persist. After all sub-issues are processed, the plan re-reads the parent issue for follow-up issues created during review and implements those too. Wall clock timing is tracked per sub-issue and overall.

## Arguments

`{{ARGUMENTS}}` is a GitHub issue number or URL:

- **`<number>`** -- e.g., `/implement-plan 42`
- **`<url>`** -- e.g., `/implement-plan https://github.com/owner/repo/issues/42`

## Workflow

### 1. Start Wall Clock Timer and Resolve the Parent Issue

Record the plan start time and parse `{{ARGUMENTS}}` to extract the repo and issue number.

```bash
PLAN_START_TIME=$(date +%s)

# Determine repo from git remote
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)

# Fetch parent issue
gh issue view <number> --repo $REPO --json number,title,body,state
```

Initialize a tracking structure for per-sub-issue timing and results. For each sub-issue processed, record:
- `SUB_START_TIME` -- when work began on this sub-issue
- `SUB_END_TIME` -- when the sub-issue was merged/escalated/failed
- `SUB_RESULT` -- MERGED, ESCALATED, or FAILED
- `SUB_PR` -- the PR number (if any)
- `FOLLOW_UP_ISSUE` -- follow-up issue number (if any)

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

Before starting each sub-issue, record the start time and update the session name:

```bash
SUB_START_TIME=$(date +%s)
```

```
/rename #<sub-number> <sub-issue title> (plan #<parent-number>)
```

After each sub-issue completes (merged or escalated), record the end time and result before moving to the next one:

```bash
SUB_END_TIME=$(date +%s)
SUB_ELAPSED=$((SUB_END_TIME - SUB_START_TIME))
```

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
| `Makefile`, `*.md` | NO | Build and config files do not affect E2E behavior |
| `*.json` outside `frontend/` (e.g., `.claude/*.json`, root config) | NO | Non-frontend JSON config does not affect E2E behavior |
| `frontend/package.json`, `frontend/tsconfig*.json` | YES | Package deps and TypeScript config can affect runtime behavior |

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
# Poll until test and lint checks reach a terminal bucket (pass, fail, cancel, skipping)
# Supported gh pr checks --json fields: bucket, completedAt, description, event, link, name, startedAt, state, workflow
while true; do
  CHECKS=$(gh pr checks $PR_NUMBER --json name,bucket 2>/dev/null || echo "[]")

  TEST_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "test") | .bucket' 2>/dev/null)
  LINT_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "lint") | .bucket' 2>/dev/null)

  # Terminal buckets: pass, fail, cancel, skipping
  # Non-terminal: pending, queued, "" (not yet reported)
  is_terminal() { [ "$1" = "pass" ] || [ "$1" = "fail" ] || [ "$1" = "cancel" ] || [ "$1" = "skipping" ]; }

  if is_terminal "$TEST_BUCKET" && is_terminal "$LINT_BUCKET"; then
    if [ "$TEST_BUCKET" = "pass" ] && [ "$LINT_BUCKET" = "pass" ]; then
      echo "test and lint passed -- proceeding without waiting for e2e"
      break
    else
      echo "CI failed: test=$TEST_BUCKET lint=$LINT_BUCKET"
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

**If any findings exist (any severity):** Proceed to step 8 (fix all findings in-PR).

### 8. Fix-First Review Loop

The review follows a **fix-first** model. Round 1 findings are fixed directly in the PR — no follow-up issues are created after round 1. Follow-up issues are only created for findings that persist after round 2.

#### 8a. Fix ALL Findings (Round 1)

Read the review output to understand what needs fixing:

```bash
REVIEW_FILE="tmp/review-pr/pr-${PR_NUMBER}/round-1.md"
```

Launch an Opus sub-agent to fix **all** findings — critical, important, and style:

```
Agent(
  description: "Fix review findings PR #<PR_NUMBER> round 1",
  model: "opus",
  prompt: "You are working in <working-directory> on the branch for PR #<PR_NUMBER>.

The Codex code review (round 1) found issues that should be fixed in this PR. The review is at: <REVIEW_FILE>

Read the review file, then fix ALL findings — [CRITICAL], [IMPORTANT], and [STYLE]. For each fix:
1. Read the cited file and line
2. Understand the issue
3. Apply the concrete fix suggested (or a better one if you disagree -- leave a comment on the PR explaining why)
4. Run relevant tests to verify the fix

After all fixes:
- Run `make generate && make test` to ensure nothing is broken
- Commit: 'fix: address codex review round 1 findings'
- Push the fixes

Fix ALL findings, not just critical ones. The goal is to resolve everything in this PR."
)
```

**Wait for the fix sub-agent to complete.**

#### 8b. Re-Review (Round 2)

Launch another review sub-agent:

```
Agent(
  description: "Review PR #<PR_NUMBER> round 2",
  prompt: "You are working in <working-directory> on the branch for PR #<PR_NUMBER>. Run the /review-pr skill with argument '<PR_NUMBER>'. This is a re-review after fixes were applied. Report the verdict, finding counts, and output file path."
)
```

Parse the result:

- **If APPROVE (no findings):** Proceed to step 9 (merge).
- **If non-critical findings only (no CRITICAL):** Proceed to step 9 (merge with follow-up issue).
- **If critical findings remain:** Proceed to step 8c (escalation).

#### 8c. Escalation After Round 2

If critical findings remain after round 2:

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

Before merging, handle any remaining non-critical findings from round 2:

**If non-critical findings remain after round 2**, create a follow-up issue **attached to the parent issue**:

```bash
FOLLOW_UP=$(gh issue create \
  --title "fix: address review findings from PR #${PR_NUMBER}" \
  --body "$(cat <<EOF
## Context

PR #${PR_NUMBER} was merged with non-critical review findings that remain after round 2 fixes. These should be addressed in a follow-up.

## Findings

<paste remaining non-critical findings from round 2 review output>

## Source

Review output: \`tmp/review-pr/pr-${PR_NUMBER}/round-2.md\`

Parent plan: #<parent-issue-number>
EOF
)" | grep -oP '\d+$')

# Add as a sub-issue on the parent by editing the parent issue body
# Append the follow-up issue reference to the parent's task list
gh issue view <parent-issue-number> --json body -q .body > /tmp/parent-body.md
echo "- [ ] #${FOLLOW_UP} -- follow-up: review findings from PR #${PR_NUMBER}" >> /tmp/parent-body.md
gh issue edit <parent-issue-number> --body-file /tmp/parent-body.md
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

After successful merge, verify the sub-issue was closed:

```bash
gh issue view <sub-issue-number> --json state -q .state
```

### 10. Reset for Next Sub-Issue

After merge, prepare the working directory for the next sub-issue:

```bash
git checkout main
git pull origin main
```

Return to step 4 to process the next unchecked sub-issue.

### 11. Re-Read Parent for Follow-Up Issues

After all original sub-issues have been processed, re-read the parent issue to discover any follow-up issues that were added during the review cycle (step 9):

```bash
gh issue view <parent-number> --repo $REPO --json number,title,body
```

Parse the parent issue body again for unchecked sub-issues. Compare against the original sub-issue list. Any **new** unchecked issues (not in the original list) are follow-up issues that were created during the review cycle.

For each follow-up issue found, execute the same implement-review-merge cycle (steps 4-10). Track timing and results the same way as original sub-issues.

Update the session name when processing follow-ups:

```
/rename #<follow-up-number> <title> (follow-up, plan #<parent-number>)
```

If no follow-up issues are found, proceed directly to step 12.

### 12. Completion — Post Summary with Wall Clock Timing

After all sub-issues and follow-up issues have been processed, calculate total elapsed time and post a comprehensive summary comment on the parent issue:

```bash
PLAN_END_TIME=$(date +%s)
PLAN_ELAPSED=$((PLAN_END_TIME - PLAN_START_TIME))
PLAN_MINUTES=$((PLAN_ELAPSED / 60))
PLAN_SECONDS=$((PLAN_ELAPSED % 60))
```

Post the closing summary:

```bash
gh issue comment <parent-number> --body "$(cat <<EOF
## Plan Execution Complete

**Total wall clock time**: ${PLAN_MINUTES}m ${PLAN_SECONDS}s

### Sub-Issues

<for each sub-issue>
- #<N> <title>: **<MERGED | ESCALATED | FAILED>**
  - PR: #<PR_NUMBER>
  - Review rounds: <count>
  - Wall clock time: <minutes>m <seconds>s
  - Follow-up: #<follow-up-issue> (if any)
</for each>

### Follow-Up Issues

<for each follow-up issue implemented>
- #<N> <title>: **<MERGED | ESCALATED | FAILED>**
  - PR: #<PR_NUMBER>
  - Wall clock time: <minutes>m <seconds>s
</for each>

<if none>
No follow-up issues were created during review.
</if>

<if any escalated>
**Action required**: Some PRs have the \`needs-human-review\` label and were not merged. Please review the critical findings and merge manually.
</if>
EOF
)"
```

If all sub-issues and follow-ups were successfully merged, close the parent issue if it's still open:

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
| All findings fixed in round 1, clean re-review | Merge after clean re-review |
| Non-critical findings remain after round 2 | Merge, create follow-up issue attached to parent |
| Critical findings unresolved after round 2 | Do NOT merge, add `needs-human-review` label |
| CI failures unresolved after 1 fix attempt | Add `needs-human-review` label, continue to review |

### Safety

- **One sub-issue at a time**: Sub-issues are processed sequentially. No parallel PRs.
- **No force merges**: If a merge fails due to conflicts, rebase and retry once. If it fails again, escalate.
- **Review isolation**: The Codex review runs in a separate sub-agent process with no influence from the implementing agent. This ensures independent cross-model review.
- **Fix-first model**: Round 1 fix sub-agents address ALL findings (critical, important, and style) in the PR directly. Follow-up issues are only created for findings that remain after round 2.
- **Follow-up attachment**: Follow-up issues are added to the parent issue's task list so the plan can discover and implement them.
- **Follow-up sweep**: After all original sub-issues are processed, the plan re-reads the parent issue and implements any follow-up issues that were added during review.
- **Escalation is permanent**: Once a PR gets `needs-human-review`, the agent does not re-attempt it. A human must intervene.
- **Wall clock timing**: Every sub-issue and the overall plan track wall clock time. The closing summary on the parent issue includes timing for each item.

### Commit Conventions

- Implementation commits follow CONTRIBUTING.md format
- Fix commits: `fix: address codex review round <R> findings`
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
2. Implement #43 -> PR #50 -> review (findings) -> fix all in-PR -> re-review (clean) -> merge
3. Implement #44 -> PR #51 -> review -> approve -> merge
4. Implement #45 -> PR #52 -> review (findings) -> fix in-PR -> re-review (non-critical remain) -> merge + follow-up #60
5. Re-read #42, find follow-up #60 -> implement #60 -> PR #53 -> review -> merge
6. Post summary with wall clock timing on #42

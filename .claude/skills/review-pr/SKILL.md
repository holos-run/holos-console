---
name: review-pr
description: Review a pull request using a cross-model code review. Use this skill to review a PR before merge. Triggers on phrases like "review PR", "review this PR", "review PR #N", "code review", "review-pr", or "/review-pr". Accepts an optional PR number argument; if omitted, detects the PR for the current branch.
version: 2.0.0
---

# Review PR

Cross-model code review of a pull request. Reviews the PR diff against project conventions and acceptance criteria, posts findings as a GitHub review, and reports a structured verdict. Designed for the plan-implement-review cycle where Claude implements and a separate model reviews independently.

Currently uses the OpenAI Codex CLI as the review backend. The backend is swappable — the rest of the skill (PR context gathering, finding classification, GitHub review posting, fix-review loop) is backend-agnostic.

## Arguments

`{{ARGUMENTS}}` is an optional PR number:

- **`<number>`** -- Review PR #`<number>` (e.g., `/review-pr 42`)
- **No argument** -- Detect the PR for the current branch. If the current branch has no open PR, abort with a clear message.

## Workflow

### 1. Preflight

Verify codex is available using the helper script:

```bash
eval "$(scripts/check-codex)"
```

If the script exits non-zero, **abort the skill** -- codex is not installed.

Log the codex version for debugging:

```bash
$CODEX --version 2>&1 || true
```

### 2. Resolve the PR

Determine the PR number from `{{ARGUMENTS}}` or the current branch.

**If `{{ARGUMENTS}}` is a number**, use it directly as the PR number.

**If `{{ARGUMENTS}}` is empty or not a number**, detect the PR for the current branch:

```bash
PR_NUMBER=$(gh pr view --json number -q .number 2>/dev/null || echo "")
```

If `PR_NUMBER` is empty, **abort the skill** with this message:

```
No open PR found for the current branch. Either:
  1. Provide a PR number: /review-pr 42
  2. Push the current branch and open a PR first
```

### 3. Fetch PR Context

Fetch PR metadata, the diff, and linked issue context in parallel:

```bash
# PR metadata
gh pr view $PR_NUMBER --json number,title,body,headRefName,baseRefName,additions,deletions,files

# PR diff
gh pr diff $PR_NUMBER > /tmp/pr-${PR_NUMBER}.diff

# Extract linked issue number from "Closes #N" or "Closes: #N" in PR body
ISSUE_NUMBER=$(gh pr view $PR_NUMBER --json body -q .body | grep -oP '(?i)closes:?\s*#\K\d+' | head -1)
```

If a linked issue is found, fetch it for acceptance criteria context:

```bash
if [ -n "$ISSUE_NUMBER" ]; then
  gh issue view $ISSUE_NUMBER --json number,title,body
fi
```

Record the PR base branch for the diff scope:

```bash
BASE_BRANCH=$(gh pr view $PR_NUMBER --json baseRefName -q .baseRefName)
```

### 4. Determine Review Round

```bash
mkdir -p tmp/review-pr/pr-${PR_NUMBER}
ROUND=$(ls tmp/review-pr/pr-${PR_NUMBER}/round-*.md 2>/dev/null | wc -l | tr -d ' ')
ROUND=$((ROUND + 1))
OUTPUT_FILE="tmp/review-pr/pr-${PR_NUMBER}/round-${ROUND}.md"
```

### 5. Build the Review Prompt

Assemble the review prompt. Include acceptance criteria from the linked issue when available.

The prompt must contain:
- Scope instruction: review the PR diff (the branch diff against the base branch)
- Project context: holos-console is a Go HTTPS server with React frontend and ConnectRPC
- Acceptance criteria from the linked issue (if found)
- The review checklist (critical, important, style categories)
- The structured output format for findings

### 6. Run the Review

Use `codex exec review` with the prompted mode. The `--base` flag and `[PROMPT]` are mutually exclusive in the codex CLI, so embed scope instructions in the prompt.

```bash
# $CODEX was set by scripts/check-codex in step 1

# Build ACCEPTANCE_CRITERIA section
ACCEPTANCE_CRITERIA=""
if [ -n "$ISSUE_NUMBER" ]; then
  ACCEPTANCE_CRITERIA="=== Acceptance Criteria (from issue #${ISSUE_NUMBER}) ===
$(gh issue view $ISSUE_NUMBER --json body -q .body)

"
fi

timeout 300 $CODEX exec review \
  --ephemeral \
  -o "${OUTPUT_FILE}" \
  "Review this pull request. Run \`git diff ${BASE_BRANCH}...HEAD\` to see the changes.

You are reviewing PR #${PR_NUMBER} for holos-console, a Go HTTPS server with a React frontend that serves a web console UI and exposes ConnectRPC services. The built UI is embedded into the Go binary via go:embed.

${ACCEPTANCE_CRITERIA}=== Review Checklist ===

Check each convention below. Report ONLY violations you actually find in the diff. Do not report passing checks.

--- Critical (must fix before merge) ---

1. **Security**: No hardcoded credentials, secrets in code, command injection, XSS, SQL injection, or insecure defaults. Input validation at system boundaries.
2. **Reliability**: No data loss paths, cascading failures, resource leaks, race conditions, nil/null dereferences, or off-by-one errors.
3. **TLS example guardrail**: No \`curl -k\`, \`curl --insecure\`, or \`grpcurl -insecure\` in code, tests, docs, or comments.
4. **Generated code not edited**: Files under \`gen/\` and \`frontend/src/gen/\` must not be manually edited.
5. **Template field guardrail**: New fields on \`CreateDeploymentRequest\`, \`DeploymentDefaults\`, or template proto messages must also appear in \`api/v1alpha2/types.go\`, \`console/deployments/render.go\`, the frontend template editor preview, and \`console/templates/defaults.go\`.

--- Important (should fix) ---

6. **Acceptance criteria**: Does the PR satisfy the linked issue's acceptance criteria? Are any requirements missed?
7. **Test coverage**: New behavior should have tests. Prefer unit tests (Vitest + RTL for frontend, table-driven Go tests with fake K8s client) over E2E.
8. **RED GREEN pattern**: Tests should define expected behavior. Look for tests that always pass (tautologies) or never exercise the new code path.
9. **Terminology**: Use \"platform template\" not \"system template\" for org-level or folder-level CUE templates.
10. **Code generation consistency**: If proto files or CUE schemas changed, generated code should have corresponding changes.
11. **Error handling**: Errors propagated correctly, no swallowed errors.

--- Style (optional, nice to have) ---

12. **UI conventions**: Semantic CSS tokens (\`bg-background\`, \`text-foreground\`), not hardcoded colors. \`Combobox\` for dynamic collections.
13. **Dead code**: Unused imports, unreachable code paths, stale comments referencing removed behavior.

--- NOT in scope ---

Do NOT comment on: style preferences beyond the list above, comment formatting, naming opinions, or suggestions that would constitute scope creep beyond the issue's acceptance criteria. This code is unreleased -- do not flag breaking changes, removed exports, or renamed APIs as issues.

=== Output Format ===

Structure your review as markdown:

## Review Summary

**PR**: #${PR_NUMBER}
**Round**: ${ROUND}
**Scope**: Branch diff against ${BASE_BRANCH}
**Verdict**: APPROVE | REQUEST_CHANGES

## Findings

### [CRITICAL] <title>
- **File**: <path>:<line>
- **Category**: security | reliability | tls-guardrail | generated-code | template-field
- **Issue**: <what is wrong>
- **Fix**: <concrete suggestion>

### [IMPORTANT] <title>
- **File**: <path>:<line>
- **Category**: acceptance-criteria | tests | red-green | terminology | codegen | error-handling
- **Issue**: <what is wrong>
- **Fix**: <concrete suggestion>

### [STYLE] <title>
- **File**: <path>:<line>
- **Category**: ui-conventions | dead-code
- **Issue**: <what is wrong>
- **Fix**: <concrete suggestion>

If no issues found, output:

## Review Summary

**PR**: #${PR_NUMBER}
**Round**: ${ROUND}
**Scope**: Branch diff against ${BASE_BRANCH}
**Verdict**: APPROVE

No issues found. The changes follow project conventions.

IMPORTANT: Be specific and concise. Cite exact file paths and line numbers. Give concrete fix suggestions. Only report actual issues found in the diff, not hypothetical concerns."
```

The `--ephemeral` flag prevents codex from persisting session files. The `-o` flag writes only the final review message to the output file. The `timeout 300` caps execution at 5 minutes.

### 7. Read and Parse Results

After codex finishes, read the output file with the Read tool.

If the output file is empty or missing, codex may have failed. Report the error and suggest checking:
- `codex login status` -- authentication
- Network connectivity to OpenAI API
- The stderr output from the command

If the output file has content, extract the verdict and count findings by severity:
- Search for `**Verdict**: APPROVE` or `**Verdict**: REQUEST_CHANGES`
- Count `### [CRITICAL]`, `### [IMPORTANT]`, and `### [STYLE]` headings

### 8. Classify Findings

Classify each finding for the fix-forward decision:

| Classification | Criteria | Action |
|----------------|----------|--------|
| **Critical (blocks merge)** | Security vulnerabilities, reliability issues (data loss, cascading failures, resource leaks), TLS guardrail violations | Must fix before merge |
| **Non-critical (fix-forward)** | Test coverage gaps, terminology, code generation consistency, error handling improvements, style issues | Merge now, track in follow-up issue |

Findings tagged `[CRITICAL]` by Codex are critical. Findings tagged `[IMPORTANT]` or `[STYLE]` are non-critical.

### 9. Post GitHub Review

Post the review findings to the PR using `gh api`. This creates a visible review on the PR that persists as an audit trail.

**If verdict is APPROVE** (no findings):

```bash
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
gh api repos/${REPO}/pulls/${PR_NUMBER}/reviews \
  --method POST \
  --field body="Codex review round ${ROUND}: no findings. LGTM." \
  --field event="APPROVE"
```

**If findings exist**, build an inline comments array and post a review:

For each finding with a file and line number, create an inline comment. Post as `REQUEST_CHANGES` if any `[CRITICAL]` findings exist, otherwise as `COMMENT`.

```bash
# Example: posting a review with inline comments
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)

# Build the comments JSON array from parsed findings
# Each comment: {"path": "file.go", "line": 42, "body": "finding text"}
COMMENTS='[{"path":"console/rpc/auth.go","line":42,"body":"[CRITICAL] ..."}]'

# Determine event type
EVENT="COMMENT"  # default for non-critical only
# If any [CRITICAL] findings exist: EVENT="REQUEST_CHANGES"

gh api repos/${REPO}/pulls/${PR_NUMBER}/reviews \
  --method POST \
  --field body="Codex review round ${ROUND}: <N> findings (<breakdown>)" \
  --field event="${EVENT}" \
  --input <(echo "{\"comments\": ${COMMENTS}}")
```

If inline comment posting fails (e.g., line numbers don't map to the diff), fall back to posting the full review as a single body comment.

### 10. Report Summary

Print a concise summary:

```
Codex review round <ROUND> for PR #<PR_NUMBER> complete.
- Output: tmp/review-pr/pr-<PR_NUMBER>/round-<ROUND>.md
- Verdict: APPROVE | REQUEST_CHANGES
- Critical: <count>
- Important: <count>
- Style: <count>
- GitHub review: posted as <APPROVE|REQUEST_CHANGES|COMMENT>
```

Then provide guidance based on the classification:

| Situation | Guidance |
|-----------|----------|
| APPROVE (no findings) | "Review is clean. Ready to merge." |
| Non-critical findings only | "Non-critical findings only. Safe to merge and create a follow-up issue for the findings." |
| Critical findings exist | "Critical findings must be addressed. Fix and re-run `/review-pr <PR_NUMBER>` (round <N+1>)." |

## Fix-Review Loop

When used during implementation (integrated into the implement-plan workflow):

```
1. Implementation complete, PR open, CI green
2. Run /review-pr <PR_NUMBER>
3. If APPROVE -> merge
4. If non-critical findings only -> merge, create follow-up issue
5. If critical findings:
   a. Read tmp/review-pr/pr-<N>/round-<R>.md
   b. Fix each critical finding
   c. Commit: "fix: address codex review round <R> critical findings"
   d. Push fixes
   e. Re-run /review-pr <PR_NUMBER> (round R+1)
   f. Maximum 2 rounds on critical findings
6. After 2 rounds with unresolved critical findings:
   - Post summary comment listing unresolved findings
   - Add label: needs-human-review
   - Do NOT merge
   - Move to next sub-issue if applicable
```

### Merge Authority Under Fix-Forward

| Situation | Action |
|-----------|--------|
| Clean review (no findings) | Merge immediately |
| Findings resolved within 2 cycles | Merge after clean re-review |
| Non-critical findings unresolved | Merge, create follow-up issue linking findings |
| Critical findings unresolved after 2 cycles | Do NOT merge. Add `needs-human-review` label |

### Handling Disagreements

When Claude disagrees with a critical Codex finding:
1. Reply to the specific review comment on GitHub with a rationale
2. If the same finding persists after re-review, it counts toward the cycle limit
3. After 2 cycles, disagreement escalates to human review

For non-critical disagreements, merge and create the follow-up issue. The human decides at their own pace.

### Resetting Review Rounds

To start fresh (e.g., after significant rework):

```bash
rm -rf tmp/review-pr/pr-${PR_NUMBER}/
```

## Prerequisites

- **Codex CLI**: Install with `npm install -g @openai/codex`
- **Authentication**: Run `codex login` once to authenticate with OpenAI
- **GitHub CLI**: `gh` must be authenticated with repo access
- **Git repository**: Must be inside a git repo with a remote

## Technical Notes

- Uses `codex exec review` (not `codex review`) to access `-o` and `--ephemeral` flags
- The `--base` flag and `[PROMPT]` are mutually exclusive in codex CLI -- the prompted mode embeds scope instructions in the prompt instead
- Output captured via `-o` flag writes only the final agent message (no progress noise)
- The `tmp/` directory is gitignored -- review artifacts are not committed
- Each round creates a separate file for cross-round comparison
- Review runs in read-only sandbox mode -- codex does not modify files
- Cross-model review (GPT reviewing Claude's work) provides independent perspective -- Codex runs as a separate process with no influence from Claude
- Override the model with `-c model="gpt-5.4"` if needed
- Review findings are posted to GitHub as a proper review (not just comments), creating an audit trail
- The review prompt is version-controlled in this skill file -- fully visible and customizable

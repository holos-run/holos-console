# Visual Verification

Visual verification (screenshots, capture scripts) is **not required by default**. Include it only when:

1. The implementation plan (GitHub issue) explicitly calls for visual verification.
2. A human reviewer requests it via a comment on the pull request.

When visual verification is requested, the PR should include:

1. A PR-specific capture script at `scripts/pr-<N>/capture` that takes screenshots of the affected pages.
2. Screenshots committed to `docs/screenshots/pr-<N>/` and referenced in the PR.

## Launcher

The generic launcher `scripts/browser-capture-pr <N>` handles the full agent-dev lifecycle (build, start backend, login, SIGPIPE cleanup) and calls the PR-specific capture script with these environment variables:

- `HOLOS_BACKEND_PORT` — the backend port
- `HOLOS_BACKEND_URL` — `https://localhost:$HOLOS_BACKEND_PORT`
- `PR_SCREENSHOT_DIR` — `docs/screenshots/pr-<N>/` (already created)

For simple cases (single page, no K8s fixtures needed), pass `--url` to skip writing a capture script:

```bash
scripts/browser-capture-pr <N> --url /profile
```

This navigates to the given path and saves `$PR_SCREENSHOT_DIR/screenshot.png` automatically.

For complex cases (multiple pages, K8s fixtures, multiple screenshots), write `scripts/pr-<N>/capture`:

- Apply any required K8s fixtures
- Use `agent-browser` to navigate and capture screenshots to `$PR_SCREENSHOT_DIR`
- The Go backend serves the built frontend — do not use Vite

## Workflow

1. **Add E2E tests** in `frontend/e2e/` asserting the new/changed behavior.
2. **Write the capture script** at `scripts/pr-<N>/capture` (or use `--url` for simple pages).
3. **Run it** after the PR is created to capture screenshots:
   ```bash
   scripts/browser-capture-pr <N>
   # or for simple pages:
   scripts/browser-capture-pr <N> --url /profile
   ```
4. **Commit images** to the feature branch:
   ```bash
   git add docs/screenshots/pr-<N>/ && git commit -m "Add visual verification screenshots for PR #<N>"
   git push
   ```
5. **Reference in PR** using the **commit SHA** in raw GitHub URLs so images remain accessible after the branch is deleted on merge:
   ```bash
   SHA=$(git rev-parse HEAD)
   REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
   gh pr comment <N> --body "![description](https://raw.githubusercontent.com/${REPO}/${SHA}/docs/screenshots/pr-<N>/filename.png)"
   ```
   Using the commit SHA (not the branch name) is the conventional approach — the SHA is immutable and resolves correctly both before and after merge. **Important**: PRs with screenshot references must be merged using a **merge commit** (not squash), so the referenced commit SHA survives in the target branch history.
6. **Annotate**: Include a brief caption describing what the screenshot shows and which script produced it.

## Related

- [Browser Automation](browser-automation.md) — Browser scripts and configuration
- [Per-Agent Dev Servers](per-agent-dev-servers.md) — How `scripts/browser-capture-pr` starts the backend
- [Implementing Plans](implementing-plans.md) — Merge commit requirement for screenshot SHA preservation

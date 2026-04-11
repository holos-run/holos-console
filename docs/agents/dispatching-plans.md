# Dispatching Plans

After drafting a plan as a GitHub issue, dispatch it to a Claude Code agent in a new worktree:

```
scripts/dispatch <issue-number>
```

This creates a git worktree at `../holos-console-<N>`, opens a new tmux window named `i<N>`, and starts a Claude Code agent that reads the issue and implements the plan. The script returns immediately so the main agent can continue planning.

Prerequisite: must be run inside a tmux session.

## Related

- [Implementing Plans](implementing-plans.md) — How agents execute dispatched plans
- [Build Commands](build-commands.md) — `make dispatch ISSUE=N` alternative

---
name: reset-agent
description: Reset the current agent worktree to a clean state on origin/main. Use when the user says "reset this agent", "reset agent", "start fresh", or wants to discard all local changes and return to main. Runs the reset-agent script which determines the agent slot from PWD, fetches origin, resets the feature branch to origin/main, and cleans the working directory.
version: 1.0.0
---

# Reset Agent

Reset the current agent worktree to a clean state aligned with origin/main.

## Workflow

Run the reset script:

```bash
scripts/reset-agent
```

The script:
1. Determines the agent slot from the working directory (e.g. `agent-3`)
2. Fetches origin
3. Switches to or creates the `feat/agent-N` branch
4. Hard-resets the branch to `origin/main`
5. Sets the branch to track `origin/main`
6. Cleans untracked files
7. Exits with code 1 to signal the reset is complete

After the script runs, report the final state (branch, commit SHA) and stop. Do not continue with any prior task context -- the reset means any previous work is discarded.

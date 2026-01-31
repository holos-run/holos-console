## Summary

Wire up a Slack integration so team members can prompt named Claude Code agents running on dedicated VMs directly from Slack channels. This enables "multiplayer mode" where multiple people collaborate with multiple agents in shared channels.

## Context

People have been using [OpenClaw](https://github.com/openclaw/openclaw) (formerly Clawdbot) to connect Claude with chat services. We want a similar but more targeted capability: prompt specific Claude Code agents from Slack, where each agent runs in an isolated Debian 13 sandbox VM with full `gh` CLI access to the `holos-run` GitHub org.

Slack thread: https://openinfrastructure.slack.com/archives/C0AC6KFAKKL/p1769884109352349

## Requirements

- **Multiplayer**: Multiple team members (Jeff, Andy, Gary, Nate, etc.) can all prompt agents from the same Slack channel
- **Named agents per VM**: Jeff's VM hosts multiple named agents; Andy's VM hosts a separate set. Each agent has its own working directory / repo checkout
- **Agent routing**: Address a specific agent by name in Slack (e.g. `@console-agent fix the build` or `@infra-agent deploy staging`)
- **Existing infrastructure**: Debian 13 VMs with Claude Code CLI already installed, `gh` CLI authenticated, paid Slack workspace, public-facing Kubernetes cluster available for webhook routing if needed

## Evaluated Options

### Option A: `mpociot/claude-code-slack-bot` (Recommended starting point)

**Repository**: https://github.com/mpociot/claude-code-slack-bot

A Node.js Slack bot that uses the Claude Code SDK (now [Claude Agent SDK](https://www.npmjs.com/package/@anthropic-ai/claude-agent-sdk)) to bridge Slack messages to a local Claude Code agent. Key properties:

- **Self-hosted**: Runs on our VMs, not Anthropic's cloud
- **Socket Mode**: Uses Slack Socket Mode (outbound WebSocket), so no inbound webhooks or public endpoints needed
- **Thread context**: Maintains conversation context within Slack threads
- **Working directory scoping**: Can scope each agent to a specific repo checkout
- **MCP server support**: Can attach MCP servers (filesystem, GitHub, etc.) for extended capabilities
- **Streaming responses**: Real-time updates in Slack as Claude works

**Multiplayer adaptation needed**: The stock bot is single-agent. For our multi-agent setup, we'd either:
1. Run one bot instance per named agent (each with its own Slack app, working directory, and VM assignment), or
2. Fork and add agent-name routing so a single bot dispatches to multiple Claude Code SDK sessions based on the `@agent-name` mention

### Option B: Official Claude Code in Slack

**Docs**: https://code.claude.com/docs/en/slack

Anthropic's official integration routes `@Claude` mentions to Claude Code sessions on `claude.ai/code`. This is cloud-hosted — sessions run on Anthropic's infrastructure, not on our VMs. It also:
- Requires Claude Pro/Max/Team/Enterprise plans with Claude Code web access
- Only supports GitHub repos connected to claude.ai/code
- Runs sessions under individual user accounts (no shared agent identity)
- Cannot run against local codebases or custom tooling on our VMs

**Verdict**: Does not meet requirements. We need agents executing locally on our VMs with access to local tooling, `gh` CLI auth, and Kubernetes access.

### Option C: OpenClaw (formerly Clawdbot)

**Repository**: https://github.com/openclaw/openclaw

General-purpose AI assistant that connects to Slack, Telegram, WhatsApp, etc. Runs as a daemon on the host machine with full system access.

**Pros**: Mature Slack integration, daemon mode, skills ecosystem (700+ community skills)
**Cons**:
- Significant security concerns (credentials stored unencrypted, [Shodan exposure incidents](https://en.ara.cat/media/openclaw-the-viral-ai-that-controls-your-computer-and-opens-huge-cybersecurity-hole_1_5633694.html))
- Much broader scope than we need (calendar, email, smart home, etc.)
- Not specifically designed for Claude Code SDK integration / agentic coding
- Overkill for our use case of "prompt a coding agent from Slack"

**Verdict**: Too broad and too many security concerns for a focused coding-agent use case.

### Option D: Custom bot using Claude Agent SDK directly

Build a minimal bot from scratch using:
- [Slack Bolt](https://slack.dev/bolt-js/) for Slack event handling (Socket Mode)
- [@anthropic-ai/claude-agent-sdk](https://www.npmjs.com/package/@anthropic-ai/claude-agent-sdk) `query()` function for programmatic agent sessions
- Agent name routing based on Slack mentions

**Pros**: Full control, minimal dependencies, exactly fits our requirements
**Cons**: More upfront work than forking `mpociot/claude-code-slack-bot`

## Proposed Architecture

```
┌─────────────────────────────────────────┐
│             Slack Workspace             │
│  #holos-dev channel                     │
│                                         │
│  User: @console-agent fix the build     │
│  User: @infra-agent deploy staging      │
└──────────┬──────────────┬───────────────┘
           │ Socket Mode  │ Socket Mode
           ▼              ▼
┌──────────────────┐  ┌──────────────────┐
│   Jeff's VM      │  │   Andy's VM      │
│   (Debian 13)    │  │   (Debian 13)    │
│                  │  │                  │
│ ┌──────────────┐ │  │ ┌──────────────┐ │
│ │console-agent │ │  │ │deploy-agent  │ │
│ │  cwd: holos- │ │  │ │  cwd: holos- │ │
│ │  console/    │ │  │ │  infra/      │ │
│ └──────────────┘ │  │ └──────────────┘ │
│ ┌──────────────┐ │  │ ┌──────────────┐ │
│ │infra-agent   │ │  │ │docs-agent    │ │
│ │  cwd: holos/ │ │  │ │  cwd: holos- │ │
│ │              │ │  │ │  docs/       │ │
│ └──────────────┘ │  │ └──────────────┘ │
│                  │  │                  │
│ gh cli ✓         │  │ gh cli ✓         │
│ claude code ✓    │  │ claude code ✓    │
└──────────────────┘  └──────────────────┘
```

Each named agent is either:
- A separate Slack app (simplest — one bot token per agent, each appears as a distinct `@name` in Slack), or
- A single Slack app per VM that parses agent names from message text

## Implementation Plan

### Phase 1: Single-agent proof of concept
- [ ] Deploy `mpociot/claude-code-slack-bot` (or minimal custom bot) on one VM
- [ ] Create a Slack app with Socket Mode enabled
- [ ] Configure working directory to a `holos-run` repo checkout
- [ ] Verify team members can prompt the agent from a shared channel
- [ ] Verify thread context and streaming responses work

### Phase 2: Multi-agent per VM
- [ ] Decide on routing strategy (multiple Slack apps vs. single app with name parsing)
- [ ] Deploy multiple named agents on Jeff's VM, each scoped to a different repo
- [ ] Test concurrent prompts from different users to different agents

### Phase 3: Multi-VM
- [ ] Replicate setup on Andy's VM
- [ ] Document the agent provisioning process
- [ ] Establish naming conventions (e.g. `jeff-console-agent`, `andy-infra-agent`)

### Phase 4: Hardening
- [ ] Add permission controls (who can prompt which agents)
- [ ] Add cost/rate-limit guardrails
- [ ] Set up systemd service for daemon persistence
- [ ] Add monitoring/alerting for agent health
- [ ] Document runbook for adding new agents and VMs

## Open Questions

1. **Slack app per agent vs. single app with routing?** Multiple apps is simpler but creates more Slack app overhead. Single app needs custom routing logic.
2. **Claude Agent SDK vs. Claude Code CLI subprocess?** The SDK (`query()`) is cleaner for programmatic use. Shelling out to `claude` CLI is simpler but harder to manage streaming.
3. **Agent isolation**: Should agents within a VM share a Claude API key / subscription, or have separate credentials?
4. **Notification preferences**: Should agents post results back to the thread, or also DM the requester?

## References

- [mpociot/claude-code-slack-bot](https://github.com/mpociot/claude-code-slack-bot) — Self-hosted Slack bot using Claude Code SDK
- [Official Claude Code in Slack docs](https://code.claude.com/docs/en/slack) — Anthropic's cloud-hosted integration
- [OpenClaw](https://github.com/openclaw/openclaw) — General-purpose AI assistant (formerly Clawdbot)
- [Claude Agent SDK (npm)](https://www.npmjs.com/package/@anthropic-ai/claude-agent-sdk) — Programmatic SDK for Claude Code
- [Slack Bolt for JavaScript](https://slack.dev/bolt-js/) — Slack app framework
- [Building an agentic Slackbot with Claude Code](https://medium.com/@dotdc/building-an-agentic-slackbot-with-claude-code-eba0e472d8f4) — Community tutorial

+++
title   = "Codex research: sandbox, approvals, hooks"
touches = ["internal/agent/codex.go"]
source  = "T260"
+++
## Decided
- Codex runs with its own sandbox (workspace-write) and approval policy on-request; tm detects capabilities from codex --help, never by version.
- Codex hooks are not used: its notify program reports turn ends.

## Watch out
- Codex resets the sandbox when it is started outside a git repository.

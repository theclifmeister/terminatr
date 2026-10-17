+++
title  = "How the user wants the work run"
standing = true
source = "user, 2026-10-06 to 2026-10-17"
+++
## Decided
- Momentum: run independent work in parallel; don't stop for confirmation on routine steps. The user's "start Tn" is the go-ahead.
- Models: sonnet for small, well-specified work; opus for large or subtle work.
- Names: show the short title next to every id ("T9 (sidebar thread ids)").
- Merging: green and mergeable thread PRs without asking. After every merge batch, check main's CI before the next batch.
- PRs: threads push and report; the coordinator opens every PR. PR titles are Conventional Commits written for the release notes.
- Releases: release-please opens the release PR; the user merges it.
- CI is metered: push once per round, never poll CI in a loop, one rerun to tell a flake.
- Generalize: when a fix only helps this project's own setup, propose the version that helps every user.

## Ruled out
- Hand-archiving tasks, threads, inbox or journal: the ticker does it.

## Watch out
- Docs are written for end users: never mention task or thread ids in README, site or docs.

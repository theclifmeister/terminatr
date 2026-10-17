+++
title   = "CI and flaky tests"
touches = [".github/workflows/", "internal/e2e/"]
source  = "T301"
+++
## Decided
- CI runs on Linux and macOS runners; macOS jobs are the slow ones (about 12 minutes).
- A flaky test gets one rerun to tell a flake from a failure; a test that flakes twice gets a task.
- The e2e suite runs with -race on Linux only.

## Ruled out
- Retrying whole workflows automatically: it hides real failures.

## Watch out
- Timing-based waits in e2e tests flake on loaded macOS runners: wait on the condition (testwait), never sleep.

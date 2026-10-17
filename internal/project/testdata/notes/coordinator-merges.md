+++
title    = "Merge batches and thread budget"
audience = "coordinator"
source   = "user, 2026-10-12"
+++
## Decided
- At most six threads run at once; merge green PRs in batches of three, then check main's CI.
- Threads are never told about the merge order or the budget.

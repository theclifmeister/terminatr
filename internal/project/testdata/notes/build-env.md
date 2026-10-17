+++
title    = "Building and testing in a thread worktree"
touches  = ["Makefile", "go.mod"]
standing = true
source   = "worktree-builds"
+++
## Decided
- Worktrees have no .build: eval "$(make -s env)" reuses the main checkout's libghostty. Never create .build/ in a worktree. CGO_ENABLED=0 for pure packages.
- In the sandbox: GOCACHE=$TMPDIR/gocache, GOPROXY=off. Run tui, server and e2e tests unsandboxed.
- Push with git push origin HEAD, once per round.

## Watch out
- Tests must not resolve the real home: set TERMINATR_HOME.
- $TMPDIR and git stash are shared by every session and worktree.

+++
title   = "Git guard gotchas"
touches = ["internal/guard/", "internal/gitx/"]
source  = "T377"
+++
## Decided
- The guard refuses force-push, branch deletion and merges from threads; it reads the command line, not the shell.
- git worktree commands are checked against the thread's own worktree path.

## Ruled out
- Wrapping git in a shim binary on the PATH: agents call git by absolute path.

## Watch out
- A pushurl in .git/config can point a push elsewhere: the guard resolves the remote first.

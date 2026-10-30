Azure DevOps' REST answers as tm asks them, `az rest --method get
--resource 499b84ac-1321-427f-aa17-267ca6975798 --url <url> --headers
Accept=application/json --output json`, captured from a live
organization on 2026-10-07 (T120) and scrubbed: the organization,
project, repo, branch and people renamed (acme, Shop, web, feature/…,
ada@example.com), every GUID but the well-known policy type ids mapped
to a made-up one, the same everywhere; commit ids kept. A build
validation policy (blocking) on main runs a YAML pipeline that fails
when a file FAIL exists.

- pr-active.json: PR 1, active, the user a required reviewer without a
  vote, after main moved (its build policy's build expired).
- pr-failed.json: PR 2, active, its build failed, a vote of -5.
- pr-draft.json: PR 3, a draft.
- pr-abandoned.json: PR 4, abandoned (no mergeStatus).
- pr-completed.json: PR 5, completed by hand, merge (no fast-forward),
  "Merged PR 5: …".
- pr-conflicts.json: PR 6, into feature/base, mergeStatus conflicts.
- pr-list.json: the repo's PRs, `searchCriteria.status=all`;
  pr-list-branch.json: by `searchCriteria.sourceRefName=refs/heads/feature/pass`.
- policies.json: PR 2's policy evaluations (`artifactId` of the project
  id and the PR, 7.1-preview.1): the build policy rejected, its context
  naming build 6; policies-approved.json: PR 1's, queued with an
  expired build; policies-draft.json: PR 3's, queued without a build.
- statuses.json: PR 1's statuses (7.1-preview.1): lint pending, pending,
  succeeded, and the Azure Pipelines Test Service's codecoverage, whose
  queued status has no state; statuses-failed.json: PR 2's, lint failed.
- builds.json: `_apis/build/builds?branchName=refs/pull/2/merge&resultFilter=failed`.
- timeline.json: build 6's timeline: the Check task failed (log 7), its
  Job, Phase and Stage rollups too.
- buildlog.txt: log 7's lines (`{"count":n,"value":[lines]}` as asked).

Captured on 2026-10-30 (T877), scrubbed the same way, from a YAML
pipeline (ci, definition 2) that fails when FAIL exists:

- pr-branch-ci.json: PR 7, active, from feature/ci into main, which has
  no build validation policy; builds-branch.json: its branch's builds
  (`branchName=refs/heads/feature/ci`), build 8 failed at the PR's head.
  Its statuses and policy evaluations are empty lists.
- pr-policy-ci.json: PR 8, from feature/policy into feature/base, which
  has a blocking build validation policy; policies-ci.json: its
  evaluation, approved, naming build 7; builds-merge.json:
  `branchName=refs/pull/8/merge`, build 7 succeeded at the merge commit.

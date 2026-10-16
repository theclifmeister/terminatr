// A code host mod (mods design §2, tier 5): GitLab, through glab's REST
// calls (glab api). tm sends each codehost.* method the repo's remote
// ({kind, host, path}) and what it asks about; the handler answers in
// tm's fixed words for a pull request, and tm drops anything else.
//
// What is destructive stays tm's: it never removes a branch or worktree
// on this mod's word (git checks the commits are on the remote), it
// finds merge commits in git itself, and tm thread merge makes its own
// checks (open, green, mergeable) before it asks for merge, with the
// head it checked. merge passes that head to GitLab as sha, so GitLab
// refuses it once the branch moved, and it never removes the source
// branch. It merges by the project's merge_method: "merge" and "squash"
// (the merge request's squash flag); GitLab's API has no per-merge
// rebase, so "rebase" is refused (the project's own GitLab merge method
// decides between a merge commit and a fast-forward).
//
// glab runs on its own login (glab auth login), with no token in its
// environment: the commands grant runs it with a scrubbed one.
import type { TM } from "./types/tm";
declare const $: TM;

type Remote = TM.CodeHostArgs["remote"];

interface MR {
  iid: number;
  web_url: string;
  state: string; // opened, closed, locked, merged
  source_branch: string;
  target_branch: string;
  source_project_id?: number;
  target_project_id?: number;
  sha: string | null;
  merge_commit_sha: string | null;
  squash_commit_sha: string | null;
  merged_at: string | null;
  has_conflicts?: boolean;
  detailed_merge_status?: string;
  head_pipeline?: { id: number; status: string } | null;
}

/** A glab call that failed; notFound for GitLab's 404. */
class GlabError extends Error {
  constructor(message: string, readonly notFound: boolean) {
    super(message);
  }
}

/** glab api on the remote's server: its output, raw. */
async function glab(r: Remote, endpoint: string, fields: string[] = [], method = "GET"): Promise<string> {
  const argv = ["glab", "api", "--hostname", r.host];
  if (method !== "GET") argv.push("--method", method);
  for (const f of fields) argv.push("--raw-field", f);
  argv.push(endpoint);
  const res = await $.process.run(argv, { timeout_ms: 30000 });
  if (res.code !== 0) {
    const first = (res.stderr || res.stdout).trim().split("\n")[0] || `exit status ${res.code}`;
    throw new GlabError(`glab api ${endpoint}: ${first}`, /\b404\b/.test(first));
  }
  return res.stdout;
}

async function api<T>(r: Remote, endpoint: string, fields: string[] = [], method = "GET"): Promise<T> {
  return JSON.parse(await glab(r, endpoint, fields, method)) as T;
}

const project = (r: Remote) => `projects/${encodeURIComponent(r.path)}`;
const mrPath = (r: Remote, iid: number) => `${project(r)}/merge_requests/${iid}`;

/** The merge request by number, null when GitLab has none. */
async function byNumber(r: Remote, iid: number): Promise<MR | null> {
  try {
    return await api<MR>(r, mrPath(r, iid));
  } catch (e) {
    if (e instanceof GlabError && e.notFound) return null;
    throw e;
  }
}

/** The newest merge request from branch, in any state; null for none. */
async function byBranch(r: Remote, branch: string): Promise<MR | null> {
  const q = `source_branch=${encodeURIComponent(branch)}&state=all&order_by=updated_at&sort=desc&per_page=1`;
  const list = await api<MR[]>(r, `${project(r)}/merge_requests?${q}`);
  return list.length > 0 ? list[0] : null;
}

function state(mr: MR): "OPEN" | "CLOSED" | "MERGED" {
  switch (mr.state) {
    case "opened":
      return "OPEN";
    case "merged":
      return "MERGED";
  }
  return "CLOSED"; // closed, locked
}

/** A pipeline's status as tm's checks: "" with no pipeline. */
function checks(mr: MR): "pass" | "fail" | "pending" | "" {
  switch (mr.head_pipeline?.status) {
    case undefined:
      return "";
    case "success":
    case "skipped":
      return "pass";
    case "failed":
    case "canceled":
      return "fail";
  }
  return "pending"; // created, waiting_for_resource, preparing, pending, running, scheduled, manual
}

/** Not yet computed: GitLab says checking or unchecked until it knows. */
const computing = ["checking", "unchecked", "preparing", "approvals_syncing"];

async function review(r: Remote, mr: MR): Promise<"APPROVED" | "CHANGES_REQUESTED" | "REVIEW_REQUIRED" | ""> {
  if (mr.detailed_merge_status === "requested_changes") return "CHANGES_REQUESTED";
  try {
    const a = await api<{ approved?: boolean; approvals_left?: number; approved_by?: unknown[] }>(r, `${mrPath(r, mr.iid)}/approvals`);
    if (a.approvals_left && a.approvals_left > 0) return "REVIEW_REQUIRED";
    if (a.approved && (a.approved_by?.length ?? 0) > 0) return "APPROVED";
  } catch (e) {
    $.log(`approvals of !${mr.iid}: ${(e as Error).message}`); // a review we can't read is no review
  }
  return "";
}

async function pr(r: Remote, mr: MR): Promise<TM.CodeHostPR> {
  const st = state(mr);
  const out: TM.CodeHostPR = {
    number: mr.iid,
    url: mr.web_url,
    state: st,
    checks: checks(mr),
    review: st === "OPEN" ? await review(r, mr) : "",
    base: mr.target_branch,
    head: mr.sha ?? undefined,
    mergeable: "UNKNOWN",
  };
  if (mr.source_project_id === mr.target_project_id) out.branch = mr.source_branch; // not a fork's
  if (out.checks === "fail") out.failed = 1; // one pipeline
  if (st === "MERGED") {
    out.merge = mr.merge_commit_sha ?? mr.squash_commit_sha ?? undefined;
    out.merged_at = mr.merged_at ?? undefined;
  }
  if (st === "OPEN") {
    const ds = mr.detailed_merge_status ?? "";
    if (mr.has_conflicts || ds === "conflict") {
      out.mergeable = "CONFLICTING";
      out.merge_state = "DIRTY";
    } else if (!computing.includes(ds)) {
      out.mergeable = "MERGEABLE";
      if (ds === "need_rebase") out.merge_state = "BEHIND";
    }
  }
  return out;
}

$.codehost.handle("pr", async (a) => {
  let mr: MR | null = null;
  if (a.number) mr = await byNumber(a.remote, a.number);
  else if (a.branch) {
    const found = await byBranch(a.remote, a.branch);
    // The list leaves out the pipeline and the merge status.
    mr = found && (await byNumber(a.remote, found.iid));
  }
  return mr && pr(a.remote, mr);
});

$.codehost.handle("prState", async (a): Promise<TM.CodeHost["prState"]> => {
  const mr = a.branch ? await byBranch(a.remote, a.branch) : null;
  return { state: mr ? state(mr) : "" };
});

$.codehost.handle("prHead", async (a): Promise<TM.CodeHost["prHead"]> => {
  const mr = a.number ? await byNumber(a.remote, a.number) : null;
  if (!mr) return { state: "", number: 0, head: "" };
  return { state: state(mr), number: mr.iid, head: mr.source_branch };
});

$.codehost.handle("failedLog", async (a) => {
  const mr = a.number ? await byNumber(a.remote, a.number) : null;
  const pipeline = mr?.head_pipeline;
  if (!mr || !pipeline || (a.head && mr.sha !== a.head)) return { job: "", log: "" };
  const jobs = await api<{ id: number; name: string; stage: string }[]>(
    a.remote,
    `${project(a.remote)}/pipelines/${pipeline.id}/jobs?scope[]=failed&per_page=1`,
  );
  if (jobs.length === 0) return { job: "", log: "" };
  const log = await glab(a.remote, `${project(a.remote)}/jobs/${jobs[0].id}/trace`);
  return { job: `${jobs[0].stage}: ${jobs[0].name}`, log };
});

$.codehost.handle("merge", async (a) => {
  if (!a.number || !a.head || !/^[0-9a-f]{40}$/.test(a.head)) throw new Error("merge needs the number and head tm checked");
  const method = a.method ?? "merge";
  if (method !== "merge" && method !== "squash") return { refused: "method", allowed: ["merge", "squash"] };
  // At exactly this head: GitLab answers 409 when the head moved, and
  // 405 or 422 when it can't merge.
  await api(a.remote, `${mrPath(a.remote, a.number)}/merge`,
    [`sha=${a.head}`, `squash=${method === "squash"}`, "should_remove_source_branch=false"], "PUT");
  return {};
});

$.codehost.handle("doctor", async (a) => {
  const ver = await $.process.run(["glab", "version"], { timeout_ms: 10000 }).catch(() => null);
  if (!ver || ver.code !== 0) {
    return { checks: [{ name: "glab", ok: false, detail: "not found or not working: install glab (PR follow-up and tm thread merge need it)" }] };
  }
  const args = ["glab", "auth", "status"];
  if (a.remote.host) args.push("--hostname", a.remote.host);
  const auth = await $.process.run(args, { timeout_ms: 10000 });
  return {
    checks: [
      { name: "glab", ok: true, detail: "found" },
      auth.code === 0
        ? { name: "glab auth", ok: true, detail: "logged in" }
        : { name: "glab auth", ok: false, detail: "not logged in: run glab auth login" },
    ],
  };
});

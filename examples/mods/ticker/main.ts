// The ticker (sidebar sections design §8, §11.8): when the ticker last
// polled a project's PRs and synced its repos, and whether its PR host
// answers, as key/value rows whose times are ago and until spans, so the
// console moves them by itself. The view ticker is a section of the
// coordinator's info panel, the view band one of the sidebar's band; the
// place setting picks which show, and a view it leaves out gets a null
// tree, which draws nothing, not even a header: for all projects from
// its start, and for each project it opens for. The mod publishes on
// view.opened and on ticker.changed, once per poll at most; it never
// polls itself. Core warns of a failing PR host whatever this mod does.
import type { TM } from "./types/tm";
declare const $: TM;

const TICKER = "ticker";
const BAND = "band";

// The projects each view shows for, as view.opened and view.closed say.
const open = new Map<string, Set<string>>([[TICKER, new Set()], [BAND, new Set()]]);

// wanted says whether the place setting shows view: right is the
// coordinator's panel, sidebar the band, both both. A settings change
// restarts the mod, so reading it once per draw is enough.
function wanted(view: string): boolean {
  const place = String($.settings().place ?? "right");
  return view === BAND ? place !== "right" : place !== "sidebar";
}

// rows are the ticker's rows, as core's TICKER block drew them: PRs
// with the next poll, synced, and the PR host (none, failing, or ok once
// it answered).
function rows(tk: TM.WatchTicker): TM.Value[] {
  const pairs: TM.Value[] = [];
  if (tk.pr_checked) {
    const value: TM.Span[] = [{ ago: tk.pr_checked, role: "faint" }];
    if (tk.pr_poll_seconds > 0) {
      const next = new Date(Date.parse(tk.pr_checked) + tk.pr_poll_seconds * 1000).toISOString();
      value.push({ text: " · next ", role: "faint" }, { until: next, role: "faint" });
    }
    pairs.push({ key: "PRs", value });
  }
  if (tk.synced) pairs.push({ key: "synced", value: [{ ago: tk.synced, role: "faint" }] });
  if (tk.no_pr_host) pairs.push({ key: "PR host", value: [{ text: "none", role: "faint" }] });
  else if (tk.gh_failing) pairs.push({ key: "PR host", value: [{ text: "failing", role: "bad" }] });
  else if (tk.pr_checked) pairs.push({ key: "PR host", value: [{ text: "ok", role: "faint" }] });
  return pairs;
}

// paint publishes view for project from its ticker: null when the place
// setting leaves the view out or the ticker has nothing to say yet.
async function paint(view: string, project: string, tk?: TM.WatchTicker): Promise<void> {
  const pairs = tk && wanted(view) ? rows(tk) : [];
  if (pairs.length === 0) {
    await $.ui.view.set(view, null, { project });
    return;
  }
  const opts: TM.ViewOptions = { project, height: pairs.length };
  // Folded, the header still says the PR host fails.
  if (tk?.gh_failing && !tk.no_pr_host) opts.badge = [{ text: "PR host ", role: "bad" }, { icon: "blocked", role: "bad" }];
  await $.ui.view.set(view, { kind: "kv", pairs }, opts);
}

// At the start, a view the place setting leaves out is cleared for all
// projects, so no console waits for it to draw (its header would show).
$.on("mod.ready", async () => {
  for (const view of open.keys()) {
    if (!wanted(view)) await $.ui.view.set(view, null);
  }
});

$.on("view.opened", async (v) => {
  const projects = open.get(v.view);
  if (!projects || !v.project) return;
  projects.add(v.project);
  await paint(v.view, v.project, (await $.project.get(v.project)).ticker);
});

$.on("view.closed", async (v) => {
  const projects = open.get(v.view);
  if (!projects || !v.project) return;
  projects.delete(v.project);
  // A view keeps at most 64 trees: drop the project's once it's gone.
  await $.ui.view.set(v.view, null, { project: v.project });
});

$.on("ticker.changed", async (t) => {
  for (const [view, projects] of open) {
    if (projects.has(t.project)) await paint(view, t.project, t.ticker);
  }
});

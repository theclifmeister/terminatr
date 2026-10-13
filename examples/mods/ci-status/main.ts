// CI status (sidebar sections design §9): a sidebar.project.section
// under each project's threads lists its pull requests with their checks
// as the ticker last saw them, failing first, at most 4 rows; its badge
// counts the failing ones (else the passing ones). A project without a
// PR gets a null tree, so no section shows under it. The section names
// the PRs tab as its companion (opens): enter on its header, or a click,
// sends ui.click for the section's project, and the mod opens the tab,
// which the console shows in that project's popup. The mod draws on
// view.opened and on pr.changed, thread.started and thread.resolved for
// the projects it shows; it never asks for a PR itself: the ticker polls.
import type { TM } from "./types/tm";
declare const $: TM;

const SECTION = "ci";
const TAB = "prs";
// The most rows a per-project section shows (the slot's cap).
const ROWS = 4;

// The projects each view shows for, as view.opened and view.closed say.
const open = new Map<string, Set<string>>([[SECTION, new Set()], [TAB, new Set()]]);

const look: Record<string, TM.Span> = {
  fail: { icon: "ci.fail", role: "bad" },
  pending: { icon: "ci.run", role: "faint" },
  pass: { icon: "ci.pass", role: "good" },
};
// Failing checks first, then running ones, passing ones, and none.
const rank: Record<string, number> = { fail: 0, pending: 1, pass: 2 };

// prs are the project's threads with a pull request, failing first.
function prs(p: TM.ProjectWatch): TM.WatchThread[] {
  return p.threads
    .filter((t) => t.pr && !t.done)
    .sort((a, b) => (rank[a.pr_checks ?? ""] ?? 3) - (rank[b.pr_checks ?? ""] ?? 3));
}

// number is the PR's number from the ticker's words ("#412 open, …").
function number(t: TM.WatchThread): string {
  return /#\d+/.exec(t.pr ?? "")?.[0] ?? "PR";
}

function row(t: TM.WatchThread, words: boolean): TM.Value {
  const name = t.task ? `${t.task.id} ${t.task.title}` : `${t.id} ${t.title}`;
  const spans: TM.Span[] = [look[t.pr_checks ?? ""] ?? { text: "·", role: "faint" }, { text: ` ${number(t)} `, role: "faint" }, { text: name }];
  if (words) spans.push({ text: `  ${t.pr}`, role: t.pr_bad ? "bad" : "faint" });
  return { id: t.id, spans };
}

// badge counts the failing checks, else the passing ones.
function badge(list: TM.WatchThread[]): TM.Span[] | undefined {
  const fail = list.filter((t) => t.pr_checks === "fail").length;
  if (fail > 0) return [{ text: `${fail} `, role: "bad" }, look.fail];
  const pass = list.filter((t) => t.pr_checks === "pass").length;
  if (pass > 0) return [{ text: `${pass} `, role: "good" }, look.pass];
  return undefined;
}

async function paint(view: string, p: TM.ProjectWatch): Promise<void> {
  const list = prs(p);
  if (view === TAB) {
    const rows = list.map((t) => row(t, true));
    await $.ui.view.set(TAB, { kind: "list", id: "prs", rows, empty: "No thread of this project has a pull request." }, { project: p.project, count: rows.length });
    return;
  }
  if (list.length === 0) {
    await $.ui.view.set(SECTION, null, { project: p.project });
    return;
  }
  const rows = list.slice(0, ROWS).map((t) => row(t, false));
  await $.ui.view.set(SECTION, { kind: "list", id: "prs", rows }, { project: p.project, height: rows.length, badge: badge(list) });
}

// redraw paints every view shown for project.
async function redraw(project: string): Promise<void> {
  const views = [...open].filter(([, projects]) => projects.has(project)).map(([view]) => view);
  if (views.length === 0) return;
  const p = await $.project.get(project);
  for (const view of views) await paint(view, p);
}

$.on("view.opened", async (v) => {
  const projects = open.get(v.view);
  if (!projects || !v.project) return;
  projects.add(v.project);
  await paint(v.view, await $.project.get(v.project));
});

$.on("view.closed", async (v) => {
  const projects = open.get(v.view);
  if (!projects || !v.project) return;
  projects.delete(v.project);
  // A view keeps at most 64 trees: drop the project's once it's gone.
  await $.ui.view.set(v.view, null, { project: v.project });
});

$.on("pr.changed", (e) => redraw(e.project));
$.on("thread.started", (e) => redraw(e.project));
$.on("thread.resolved", (e) => redraw(e.project));

// Enter on (or a click on) a section's header: its project's PRs tab.
$.on("ui.click", async (e) => {
  if (e.view === SECTION) await $.ui.view.open(TAB);
});

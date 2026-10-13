// Report glance: panel sections (mods design §12, Phase 2). A
// details.section shows at the bottom of the dashboard's details panel
// for the row it shows, and beside a thread's pane for that thread:
// view.opened names the project and the task or thread, and the mod
// publishes for exactly that one, then clears it on view.closed. A
// coordinator.section shows at the bottom of the coordinator's info
// panel, for its project (a layout can place both among the panels'
// blocks). Their bodies show; they take no keys. A row the mod draws nothing for (here, a project's own row) gets
// no section at all.
import type { TM } from "./types/tm";
declare const $: TM;

const LAST = "last";
const REPORTS = "reports";

// The rows on show, by "project/task/thread": what report.stored redraws.
const shown = new Map<string, TM.ViewEvent>();

function key(v: TM.ViewEvent): string {
  return `${v.project ?? ""}/${v.task ?? ""}/${v.thread ?? ""}`;
}

// The options a tree for v's row is published with.
function target(v: TM.ViewEvent): TM.ViewOptions {
  const o: TM.ViewOptions = { project: v.project };
  if (v.task) o.task = v.task;
  if (v.thread) o.thread = v.thread;
  return o;
}

// firstLine is the report's first line, short.
function firstLine(text: string): string {
  const l = text.split("\n").find((s) => s.trim() !== "") ?? "";
  return l.length > 80 ? l.slice(0, 79) + "…" : l;
}

function reportTree(r: TM.Report): TM.Widget {
  const children: TM.Widget[] = [
    { kind: "text", lines: [[{ text: `report ${r.report}`, role: "head" }, { text: "  " + firstLine(r.sections.report) }]] },
  ];
  if (r.sections.next.length > 0) {
    children.push({ kind: "list", id: "next", rows: r.sections.next.map((n, i) => ({ id: String(i), spans: [{ text: "next  ", role: "faint" }, { text: n }] })) });
  }
  const checks = r.sections.check?.length ?? 0;
  children.push(checks > 0
    ? { kind: "text", text: `${checks} way${checks === 1 ? "" : "s"} to check it`, role: "good" }
    : { kind: "text", text: "no ## Check", role: "warn" });
  return { kind: "stack", children };
}

// drawLast publishes the latest report of v's thread, or of its task's
// thread; nothing for a row without one.
async function drawLast(v: TM.ViewEvent): Promise<void> {
  if (!v.project) return;
  let thread = v.thread;
  if (!thread && v.task) {
    thread = (await $.tasks.list(v.project)).find((t) => t.id === v.task)?.thread;
  }
  if (!thread) return;
  const r = await $.report.get(v.project, thread);
  if (r) await $.ui.view.set(LAST, reportTree(r), target(v));
}

// drawReports publishes the project's threads that reported, most first.
async function drawReports(slug: string): Promise<void> {
  const p = await $.project.get(slug);
  const rows = p.threads.filter((t) => (t.reports ?? 0) > 0).sort((a, b) => (b.reports ?? 0) - (a.reports ?? 0));
  const tree: TM.Widget = rows.length === 0
    ? { kind: "text", text: "No reports yet.", role: "faint" }
    : { kind: "list", id: "threads", rows: rows.map((t) => ({ id: t.id, spans: [{ text: t.id, role: "head" }, { text: ` ${t.reports} · ` }, { text: t.title, role: "faint" }] })) };
  await $.ui.view.set(REPORTS, tree, { project: slug });
}

async function draw(v: TM.ViewEvent): Promise<void> {
  if (v.view === LAST) await drawLast(v);
  else if (v.view === REPORTS && v.project) await drawReports(v.project);
}

$.on("view.opened", async (v) => {
  shown.set(v.view + ":" + key(v), v);
  await draw(v);
});

$.on("view.closed", async (v) => {
  shown.delete(v.view + ":" + key(v));
  // A view keeps at most 64 trees: drop the row's once it's gone.
  if (v.view === LAST) await $.ui.view.set(LAST, null, target(v));
});

// A new report redraws the sections on show for its project.
$.on("report.stored", async (r) => {
  for (const v of shown.values()) {
    if (v.project === r.project) await draw(v);
  }
});

// The project popup's Tasks tab (mods design §11 and §12 Phase 2):
// the board's tasks grouped as tm groups them (needs you, in motion, on
// deck, backlog, done), the done ones folded until b. Each row names the
// core actions its keys run: enter shows the task, D delegates, A
// accepts, x sends back, c opens the coordinator; + (a new task) is
// bound in the whole view, as no row stands for it. The mod only names
// them. The key, its word, the check against the live board, the
// confirmation and the run are tm's, and the mod hears action.done
// afterwards. The cursor, the filter, the scroll and the selected task's
// steps (a row's more) are the core's too, so moving never waits on the
// mod, and a task picked elsewhere (t, NEEDS YOU) gets the cursor once
// its row is listed.
import type { TM } from "./types/tm";
declare const $: TM;

const VIEW = "tasks";
const TABLE = "tasks";

// The board's sections, in order, and the statuses in each (tasks.GroupOf).
const GROUPS: { id: string; title: string; statuses: string[] }[] = [
  { id: "needs-you", title: "NEEDS YOU", statuses: ["blocked", "review"] },
  { id: "in-motion", title: "IN MOTION", statuses: ["started"] },
  { id: "on-deck", title: "ON DECK", statuses: ["ready"] },
  { id: "backlog", title: "BACKLOG", statuses: ["open"] },
  { id: "done", title: "DONE", statuses: ["done"] },
];

function groupOf(status: string): string {
  for (const g of GROUPS) if (g.statuses.includes(status)) return g.id;
  return "on-deck";
}

// What a project's tab shows.
interface Tab {
  tasks: TM.Task[];
  doneAll: boolean;
  error: string;
}

const tabs = new Map<string, Tab>();

// The core actions a task's row binds, as tm's own Tasks tab offers
// them: an ask (delegate, accept, send back) only while nothing already
// asks the coordinator about the task, as tm checks again at the press.
function actions(project: string, t: TM.Task): { id: string; target: Record<string, string> }[] {
  const task = { project, task: t.id };
  const out = [{ id: "task.show", target: task }];
  const ask = (id: string) => {
    if (!t.asked) out.push({ id, target: task });
  };
  switch (t.status) {
    case "review":
      ask("task.accept");
      ask("task.send-back");
      break;
    case "done":
      ask("task.send-back");
      break;
    case "blocked":
      out.push({ id: "task.coordinator", target: task });
      ask("task.delegate");
      break;
    case "open":
    case "ready":
      ask("task.delegate");
      break;
  }
  return out;
}

// status is a task's status cell: its icon and word, as tm's own task
// rows draw them.
function status(s: string): TM.Span[] {
  const look: Record<string, [TM.Icon, TM.Role | undefined]> = {
    started: ["working", "good"],
    blocked: ["blocked", "bad"],
    review: ["review", "warn"],
    done: ["done", "good"],
    open: ["idle", undefined],
    ready: ["idle", undefined],
  };
  const [icon, role]: [TM.Icon, TM.Role | undefined] = look[s] ?? ["other", "faint"];
  return [
    { icon, role },
    { text: " " + s, role },
  ];
}

function row(project: string, t: TM.Task): TM.Value {
  const rest: TM.Span[] = [];
  if (t.asked) rest.push({ text: "asked the coordinator", role: "warn" });
  const more = [t.steps_total > 0 ? `${t.steps_done}/${t.steps_total}` : "", t.thread ?? ""].filter((s) => s !== "").join(" ");
  if (more) rest.push({ text: (rest.length > 0 ? "  " : "") + more, role: "faint" });
  const r: Record<string, TM.Value> = {
    id: t.id,
    cells: [[{ text: t.id }], [{ text: t.title }], status(t.status), rest] as TM.Value,
    actions: actions(project, t) as TM.Value,
  };
  // The selected task's steps, under it, unless it is done.
  if (t.status !== "done" && (t.steps ?? []).length > 0) {
    r.more = t.steps.map((s) => [
      { text: "      " },
      s.done ? { icon: "todo.done", role: "good" } : { icon: "todo.open", role: "faint" },
      { text: " " + s.text },
    ]) as TM.Value;
  }
  return r as TM.Value;
}

// shown are the tasks listed, by section: the done ones last, newest
// first (by updated date, then id), so x can still send one back; left
// out while folded.
function shown(t: Tab): TM.Task[] {
  const out: TM.Task[] = [];
  for (const g of GROUPS) {
    if (g.id === "done" && !t.doneAll) continue;
    const in_ = t.tasks.filter((x) => groupOf(x.status) === g.id);
    if (g.id === "done") {
      in_.sort((a, b) => (b.updated ?? "").localeCompare(a.updated ?? "") || num(b.id) - num(a.id));
    }
    out.push(...in_);
  }
  return out;
}

function num(id: string): number {
  return Number(id.slice(1)) || 0;
}

function tree(project: string, t: Tab): TM.Widget {
  if (t.error) {
    return { kind: "text", text: `error: ${t.error}`, role: "bad" };
  }
  const rows: TM.Value[] = [];
  let group = "";
  for (const task of shown(t)) {
    const g = groupOf(task.status);
    if (g !== group) {
      group = g;
      const title = GROUPS.find((x) => x.id === g)!.title;
      rows.push({ id: "g:" + g, head: true, spans: [{ text: title, role: g === "needs-you" ? "warn" : "head" }] });
    }
    rows.push(row(project, task));
  }
  const done = t.tasks.filter((x) => x.status === "done").length;
  const folded = !t.doneAll && done > 0;
  if (folded) rows.push({ id: "g:done", head: true, spans: [{ text: "DONE", role: "head" }] });
  const children: TM.Value[] = [
    {
      kind: "table",
      id: TABLE,
      empty: "no tasks",
      columns: [{ title: "" }, { title: "" }, { title: "" }, { title: "" }],
      rows,
    },
  ];
  if (folded) children.push({ kind: "text", text: `… ${done} done (b shows them)`, role: "faint" });
  // b is the mod's own key; + runs tm's new task, from a template or none.
  const keys = t.doneAll || done > 0 ? [{ key: "b", label: t.doneAll ? "hide done" : "done" }] : [];
  children.push({ kind: "keys", id: "keys", keys, actions: [{ id: "task.new", target: { project } }] });
  return { kind: "stack", children };
}

async function load(project: string, t: Tab): Promise<void> {
  t.error = "";
  try {
    t.tasks = await $.tasks.list(project);
  } catch (e: any) {
    t.error = String(e?.message ?? e);
  }
}

async function publish(project: string, t: Tab): Promise<void> {
  try {
    await $.ui.view.set(VIEW, tree(project, t), { project });
  } catch (e: any) {
    const why = String(e?.message ?? e);
    $.log(`tasks-tab: ${project}: ${why}`);
    await $.ui.view.set(VIEW, { kind: "text", text: `can't draw the tasks: ${why}`, role: "bad" }, { project });
  }
}

// refresh redraws project's tab, if it shows.
async function refresh(project: string | undefined): Promise<void> {
  const t = project ? tabs.get(project) : undefined;
  if (!project || !t) return;
  await load(project, t);
  await publish(project, t);
}

// The tab shows. When it was opened to show a task (e.task) that is
// done, the done tasks are listed, so its row is there for the cursor.
$.on("view.opened", async (e) => {
  if (e.view !== VIEW || !e.project) return;
  const t: Tab = tabs.get(e.project) ?? { tasks: [], doneAll: false, error: "" };
  tabs.set(e.project, t);
  await load(e.project, t);
  if (e.task && t.tasks.some((x) => x.id === e.task && x.status === "done")) t.doneAll = true;
  await publish(e.project, t);
});

$.on("view.closed", async (e) => {
  if (e.view !== VIEW || !e.project) return;
  tabs.delete(e.project);
});

// Anything the tab shows changed: a task's title, status or steps, a
// task added, an ask made or handled.
$.on("tasks.changed", async (e) => refresh(e.project));

$.on("ui.key", async (e) => {
  const project = e.target?.project;
  if (e.view !== VIEW || !project || e.key !== "b") return;
  const t = tabs.get(project);
  if (!t) return;
  t.doneAll = !t.doneAll;
  await publish(project, t);
});

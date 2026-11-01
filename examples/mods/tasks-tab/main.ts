// The project popup's Tasks tab (mods design §11 and §12 Phase 2):
// the board's tasks grouped as tm groups them (needs you, in motion, on
// deck, backlog, done, dropped), the done and dropped ones folded until b,
// each section by priority (must, should, could, none) as tm lists it,
// and p narrows the list to one priority. Each row names the
// core actions its keys run: enter shows the task, D delegates, A
// accepts, x sends back, P sets the priority, c opens the coordinator; + (a new task) is
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
  { id: "dropped", title: "DROPPED", statuses: ["dropped"] },
];

// closed are the groups b folds: the tasks the user closed.
const closed = (g: string) => g === "done" || g === "dropped";

function groupOf(status: string): string {
  for (const g of GROUPS) if (g.statuses.includes(status)) return g.id;
  return "on-deck";
}

// What a project's tab shows.
interface Tab {
  tasks: TM.Task[];
  doneAll: boolean;
  // prio is the priority p shows only ("none" for the tasks without
  // one), "" for all.
  prio: string;
  error: string;
}

// PRIOS are what p steps through, all first.
const PRIOS = ["", "must", "should", "could", "none"];

const tabs = new Map<string, Tab>();

// The core actions a task's row binds, as tm's own Tasks tab offers
// them: an ask (delegate, accept, send back) only while nothing already
// asks the coordinator about the task, as tm checks again at the press.
function actions(project: string, t: TM.Task): { id: string; target: Record<string, string> }[] {
  const task = { project, task: t.id };
  const out = [{ id: "task.show", target: task }];
  if (!closed(groupOf(t.status))) out.push({ id: "task.priority", target: task });
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

// priority is a task's priority cell: must stands out, could is faint.
function priority(p: string | undefined): TM.Span[] {
  if (!p) return [];
  const role: Record<string, TM.Role | undefined> = { must: "warn", should: undefined, could: "faint" };
  return [{ text: p, role: role[p] }];
}

// row is task t's row; prio says the list has a priority column.
function row(project: string, t: TM.Task, prio: boolean): TM.Value {
  const rest: TM.Span[] = [];
  if (t.asked) rest.push({ text: "asked the coordinator", role: "warn" });
  const more = [t.steps_total > 0 ? `${t.steps_done}/${t.steps_total}` : "", t.thread ?? ""].filter((s) => s !== "").join(" ");
  if (more) rest.push({ text: (rest.length > 0 ? "  " : "") + more, role: "faint" });
  const r: Record<string, TM.Value> = {
    id: t.id,
    cells: [[{ text: t.id }], ...(prio ? [priority(t.priority)] : []), [{ text: t.title }], status(t.status), rest] as TM.Value,
    actions: actions(project, t) as TM.Value,
  };
  // The selected task's steps, under it, unless it is done or dropped.
  if (!closed(groupOf(t.status)) && (t.steps ?? []).length > 0) {
    r.more = t.steps.map((s) => [
      { text: "      " },
      s.done ? { icon: "todo.done", role: "good" } : { icon: "todo.open", role: "faint" },
      { text: " " + s.text },
    ]) as TM.Value;
  }
  return r as TM.Value;
}

// shown are the tasks listed, by section, each by priority then id
// (tasks.list's board order): the done and dropped ones last, newest
// first (by updated date, then id), so x can still send a done one
// back; left out while folded. With p, only one priority's.
function shown(t: Tab): TM.Task[] {
  const out: TM.Task[] = [];
  for (const g of GROUPS) {
    if (closed(g.id) && !t.doneAll) continue;
    const in_ = t.tasks.filter((x) => groupOf(x.status) === g.id && wanted(t, x));
    if (!closed(g.id)) {
      in_.sort((a, b) => rank(a.priority) - rank(b.priority) || num(a.id) - num(b.id));
    }
    if (closed(g.id)) {
      in_.sort((a, b) => (b.updated ?? "").localeCompare(a.updated ?? "") || num(b.id) - num(a.id));
    }
    out.push(...in_);
  }
  return out;
}

// wanted says whether p's filter lets task x through.
function wanted(t: Tab, x: TM.Task): boolean {
  return t.prio === "" || (x.priority ?? "none") === t.prio;
}

// rank orders priorities: must, should, could, then none.
function rank(p: string | undefined): number {
  const i = ["must", "should", "could"].indexOf(p ?? "");
  return i < 0 ? 3 : i;
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
  const list = shown(t);
  // The priority column, only while a listed task has one.
  const prio = list.some((x) => !!x.priority);
  for (const task of list) {
    const g = groupOf(task.status);
    if (g !== group) {
      group = g;
      const title = GROUPS.find((x) => x.id === g)!.title;
      rows.push({ id: "g:" + g, head: true, spans: [{ text: title, role: g === "needs-you" ? "warn" : "head" }] });
    }
    rows.push(row(project, task, prio));
  }
  const done = t.tasks.filter((x) => x.status === "done" && wanted(t, x)).length;
  const dropped = t.tasks.filter((x) => x.status === "dropped" && wanted(t, x)).length;
  const folded = !t.doneAll && done + dropped > 0;
  if (folded && done > 0) rows.push({ id: "g:done", head: true, spans: [{ text: "DONE", role: "head" }] });
  if (folded && dropped > 0) rows.push({ id: "g:dropped", head: true, spans: [{ text: "DROPPED", role: "head" }] });
  const children: TM.Value[] = [
    {
      kind: "table",
      id: TABLE,
      empty: t.prio ? `no ${t.prio === "none" ? "tasks without a priority" : t.prio + " tasks"}` : "no tasks",
      columns: Array.from({ length: prio ? 5 : 4 }, () => ({ title: "" })),
      rows,
    },
  ];
  if (folded) {
    const counts = [done > 0 ? `${done} done` : "", dropped > 0 ? `${dropped} dropped` : ""].filter((s) => s !== "").join(", ");
    children.push({ kind: "text", text: `… ${counts} (b shows them)`, role: "faint" });
  }
  if (t.prio) {
    const which = t.prio === "none" ? "the tasks without a priority" : `the ${t.prio} tasks`;
    children.push({ kind: "text", text: `only ${which} (p for the next)`, role: "faint" });
  }
  // b and p are the mod's own keys; + runs tm's new task, from a template or none.
  const keys: { key: string; label: string }[] = t.doneAll || done + dropped > 0 ? [{ key: "b", label: t.doneAll ? "hide done" : "done" }] : [];
  // p's label says what the next press lists.
  const next = PRIOS[(PRIOS.indexOf(t.prio) + 1) % PRIOS.length];
  keys.push({ key: "p", label: next === "" ? "all priorities" : next === "none" ? "no priority only" : `${next} only` });
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
// done or dropped, those are listed, so its row is there for the cursor.
$.on("view.opened", async (e) => {
  if (e.view !== VIEW || !e.project) return;
  const t: Tab = tabs.get(e.project) ?? { tasks: [], doneAll: false, prio: "", error: "" };
  tabs.set(e.project, t);
  await load(e.project, t);
  if (e.task && t.tasks.some((x) => x.id === e.task && closed(groupOf(x.status)))) t.doneAll = true;
  await publish(e.project, t);
});

$.on("view.closed", async (e) => {
  if (e.view !== VIEW || !e.project) return;
  tabs.delete(e.project);
  // A view keeps at most 64 trees: drop the project's once it's gone.
  await $.ui.view.set(VIEW, null, { project: e.project });
});

// Anything the tab shows changed: a task's title, status or steps, a
// task added, an ask made or handled.
$.on("tasks.changed", async (e) => refresh(e.project));

$.on("ui.key", async (e) => {
  const project = e.target?.project;
  if (e.view !== VIEW || !project || (e.key !== "b" && e.key !== "p")) return;
  const t = tabs.get(project);
  if (!t) return;
  if (e.key === "b") t.doneAll = !t.doneAll;
  else t.prio = PRIOS[(PRIOS.indexOf(t.prio) + 1) % PRIOS.length];
  await publish(project, t);
});

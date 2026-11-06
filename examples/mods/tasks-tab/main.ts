// The project popup's Tasks tab (mods design §11 and §12 Phase 2):
// the board's tasks grouped as tm groups them (needs you, in motion, on
// deck, backlog, done, dropped), the done and dropped ones folded until b,
// each section by priority (must, should, could, none) as tm lists it,
// done ones newest first by when they finished, each saying when instead
// of its priority, and p narrows the list to one priority. Each row names the
// core actions its keys run: enter shows the task (an archived one too,
// the only thing a row of the archive binds), D delegates, A
// accepts, x sends back, P sets the priority, c opens the coordinator; + (a new task) is
// bound in the whole view, as no row stands for it. The mod only names
// them. The key, its word, the check against the live board, the
// confirmation and the run are tm's, and the mod hears action.done
// afterwards. The cursor, the filter, the scroll and the selected task's
// steps (a row's more) are the core's too, so moving never waits on the
// mod, and a task picked elsewhere (t, NEEDS YOU) gets the cursor once
// its row is listed. Shown with b, Done is by day: the newest 7 days with
// work open (at most 30 rows), older work folded by week, then by month
// (tasks.done), and enter on a heading opens or folds it, on "… more" reads
// on. The mod draws what tm groups, so an empty week has no heading. The
// Pulse (history.get) tops the list once tm says it appears, and [ and ]
// filter it to one of its bars: the tasks that finished then, nothing else.
import type { TM } from "./types/tm";
declare const $: TM;

const VIEW = "tasks";
const TABLE = "tasks";

// PAGE is how many rows a fold reads at a time.
const PAGE = 50;

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
  // The Done groups (tasks.done), read while done tasks show; a fold's
  // rows, by its key, as far as they were read; the folds the user
  // opened or closed from how they came; the done task to reveal.
  groups: TM.DoneGroup[];
  total: number;
  rows: Map<string, Fold>;
  toggled: Set<string>;
  reveal: string;
  // The project's numbers and what they were read at; the bar [ and ]
  // picked, by its first day ("" for none), and its rows as far as read.
  history: TM.History | null;
  historyAt: string;
  pick: string;
  picked: Fold;
}

// Fold is the rows read of a group that opened.
interface Fold {
  tasks: TM.Task[];
  next: number;
}

const key = (g: TM.DoneGroup) => `${g.from}:${g.to}`;

// PRIOS are what p steps through, all first.
const PRIOS = ["", "must", "should", "could", "none"];

const tabs = new Map<string, Tab>();

// The core actions a task's row binds, as tm's own Tasks tab offers
// them: an ask (delegate, accept, send back) only while nothing already
// asks the coordinator about the task, as tm checks again at the press.
function actions(project: string, t: TM.Task): { id: string; target: Record<string, string> }[] {
  const task = { project, task: t.id };
  const out = [{ id: "task.show", target: task }];
  if (t.archived) return out;
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
function status(s: string, finished?: string): TM.Span[] {
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
    { text: " " + ((s === "done" && finished) || s), role },
  ];
}

// timeOf is the time a done label ends with ("5 Nov 10:10"), "" for a
// label with only a day.
function timeOf(label: string | undefined): string {
  const last = (label ?? "").split(" ").pop() ?? "";
  return last.includes(":") ? last : "";
}

// priority is a task's priority cell: must stands out, could is faint.
function priority(p: string | undefined): TM.Span[] {
  if (!p) return [];
  const role: Record<string, TM.Role | undefined> = { must: "warn", should: undefined, could: "faint" };
  return [{ text: p, role: role[p] }];
}

// waitWords is what a row says of the after links, tm's WaitWords: the
// first two unmet links and "+N" for more, then how many tasks wait for it.
function waitWords(t: TM.Task): string {
  const refs = t.waits_for ?? [];
  const out: string[] = [];
  if (refs.length > 0) out.push("after " + refs.slice(0, 2).join(", ") + (refs.length > 2 ? ` +${refs.length - 2}` : ""));
  if ((t.waiters ?? 0) > 0) out.push(`${t.waiters} wait`);
  return out.join(" · ");
}

// row is task t's row; prio says the list has a priority column.
// timeOnly: its section's heading says the day, so it says the time alone.
function row(project: string, t: TM.Task, prio: boolean, timeOnly = false): TM.Value {
  const rest: TM.Span[] = [];
  if (t.asked) rest.push({ text: "asked the coordinator", role: "warn" });
  // Waits-for (mod API 1.15): "after T9" while a link is unmet, "2 wait"
  // on the task they wait for; nothing once the link is met.
  const waits = waitWords(t);
  if (waits) rest.push({ text: (rest.length > 0 ? "  " : "") + waits, role: "faint" });
  const more = [t.steps_total > 0 ? `${t.steps_done}/${t.steps_total}` : "", t.thread ?? ""].filter((s) => s !== "").join(" ");
  if (more) rest.push({ text: (rest.length > 0 ? "  " : "") + more, role: "faint" });
  const r: Record<string, TM.Value> = {
    id: t.id,
    cells: [[{ text: t.id }], ...(prio ? [t.status === "done" ? [] : priority(t.priority)] : []), [{ text: t.title }], status(t.status, timeOnly ? timeOf(t.done_label) : t.done_label), rest] as TM.Value,
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
// first (a done one by when it finished, else by updated date, then
// id), so x can still send a done one back; left out while folded. With p, only one priority's.
function shown(t: Tab): TM.Task[] {
  const out: TM.Task[] = [];
  for (const g of GROUPS) {
    if (closed(g.id) && !t.doneAll) continue;
    if (g.id === "done" && byDay(t)) continue;
    const in_ = t.tasks.filter((x) => groupOf(x.status) === g.id && wanted(t, x));
    if (!closed(g.id)) {
      in_.sort((a, b) => rank(a.priority) - rank(b.priority) || num(a.id) - num(b.id));
    }
    if (closed(g.id)) {
      const when = (x: TM.Task) => x.done_at ?? x.updated ?? "";
      in_.sort((a, b) => (when(a) === when(b) ? 0 : when(a) < when(b) ? 1 : -1) || num(b.id) - num(a.id));
    }
    out.push(...in_);
  }
  return out;
}

// byDay says whether Done lists by day: b shows it and p narrows nothing
// (with p, the board's done tasks of that priority, newest first).
function byDay(t: Tab): boolean {
  return t.doneAll && t.prio === "";
}

// isOpen says whether group g shows its rows: as it came, unless the user
// pressed enter on it.
function isOpen(t: Tab, g: TM.DoneGroup): boolean {
  return g.open !== t.toggled.has(key(g));
}

// foldRow is a heading of Done that enter opens or folds.
function foldRow(t: Tab, g: TM.DoneGroup): TM.Value {
  const mark = isOpen(t, g) ? "" : "  ▸";
  return { id: "f:" + key(g), spans: [{ text: g.title, role: isOpen(t, g) ? undefined : "faint" }, { text: ` · ${g.count}${mark}`, role: "faint" }] };
}

// doneRows are Done by day: each group's heading, an open one's rows and,
// while it has more to read, a row that reads them.
function doneRows(project: string, t: Tab, prio: boolean): TM.Value[] {
  const rows: TM.Value[] = [{ id: "g:done", head: true, spans: [{ text: `DONE · ${t.total}`, role: "head" }, { text: "  newest first   b hides   ⏎ opens", role: "faint" }] }];
  for (const g of t.groups) {
    rows.push(foldRow(t, g));
    if (!isOpen(t, g)) continue;
    const f = t.rows.get(key(g));
    if (!f) continue;
    for (const task of f.tasks) rows.push(row(project, task, prio, g.kind === "day"));
    if (f.next > 0) rows.push({ id: "m:" + key(g), spans: [{ text: `  … ${g.count - f.tasks.length} more`, role: "faint" }] });
  }
  return rows;
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

// pickedBar is the bar [ and ] picked, while there is one.
function pickedBar(t: Tab): TM.PulseBar | undefined {
  return t.pick ? t.history?.pulse?.bars.find((b) => b.from === t.pick) : undefined;
}

// band is the Pulse: the bars, the dots when there are releases, and what
// they add up to in words. Without releases the dots and their words are
// left out. Reading wider than the tab, tm draws the words under the bars.
function band(t: Tab): TM.Widget | null {
  const h = t.history;
  const p = h?.pulse;
  if (!h || !p) return null;
  const facts = [`${h.done} done in ${p.span}${p.unit === "day" ? "" : `, by ${p.unit}`}`];
  if (p.releases) facts.push(p.releases);
  if (p.unit !== "day" && h.typical) facts.push(`typical ${h.typical}`);
  const dots = p.bars.some((b) => b.released);
  const pick = p.bars.findIndex((b) => b.from === t.pick) + 1;
  return {
    kind: "band",
    values: p.bars.map((b) => b.done),
    labels: p.bars.map((b) => b.label),
    ...(dots ? { dots: p.bars.map((b) => !!b.released) } : {}),
    ...(pick > 0 ? { pick } : {}),
    beside: [[{ text: facts.join(" · ") }], ...(dots ? [[{ text: `releases, each dot a ${p.unit}`, role: "faint" as TM.Role }]] : [])],
  };
}

// pickedRows are the tasks of the bar picked, with what reads on.
function pickedRows(project: string, t: Tab, bar: TM.PulseBar): TM.Value[] {
  const list = t.picked.tasks.filter((x) => wanted(t, x));
  const rows: TM.Value[] = [{ id: "g:done", head: true, spans: [{ text: `DONE · ${bar.done}`, role: "head" }, { text: `  ${bar.tip.split(":")[0]}`, role: "faint" }] }];
  for (const task of list) rows.push(row(project, task, false, t.history?.pulse?.unit === "day"));
  if (t.picked.next > 0) rows.push({ id: "m:pick", spans: [{ text: `  … ${bar.done - t.picked.tasks.length} more`, role: "faint" }] });
  return rows;
}

function tree(project: string, t: Tab): TM.Widget {
  if (t.error) {
    return { kind: "text", text: `error: ${t.error}`, role: "bad" };
  }
  const bar = pickedBar(t);
  const pulse = band(t);
  const rows: TM.Value[] = bar ? pickedRows(project, t, bar) : [];
  let group = "";
  const list = bar ? [] : shown(t);
  // The priority column, only while a listed task shows one.
  const prio = list.some((x) => !!x.priority && x.status !== "done");
  const days = !bar && byDay(t) && t.groups.length > 0;
  for (const task of list) {
    const g = groupOf(task.status);
    if (g !== group) {
      // Done by day goes where its section would, before the dropped.
      if (g === "dropped" && days) rows.push(...doneRows(project, t, prio));
      group = g;
      const title = GROUPS.find((x) => x.id === g)!.title;
      rows.push({ id: "g:" + g, head: true, spans: [{ text: title, role: g === "needs-you" ? "warn" : "head" }] });
    }
    rows.push(row(project, task, prio));
  }
  if (days && group !== "dropped") rows.push(...doneRows(project, t, prio));
  // The board's, or with no filter and none left there, the archive's.
  const onBoard = t.tasks.filter((x) => x.status === "done" && wanted(t, x)).length;
  // Under the band, all of them, as it says.
  const all = t.history?.pulse ? t.history.done : onBoard === 0 ? t.total : onBoard;
  const done = t.prio === "" ? all : onBoard;
  const dropped = t.tasks.filter((x) => x.status === "dropped" && wanted(t, x)).length;
  const folded = !bar && !t.doneAll && done + dropped > 0;
  if (folded && done > 0) rows.push({ id: "g:done", head: true, spans: [{ text: "DONE", role: "head" }] });
  if (folded && dropped > 0) rows.push({ id: "g:dropped", head: true, spans: [{ text: "DROPPED", role: "head" }] });
  const children: TM.Value[] = [
    ...(pulse ? [pulse, { kind: "text", lines: [[]] }] : []),
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
  if (bar) children.push({ kind: "text", text: bar.tip, role: "faint" });
  if (t.prio) {
    const which = t.prio === "none" ? "the tasks without a priority" : `the ${t.prio} tasks`;
    children.push({ kind: "text", text: `only ${which} (p for the next)`, role: "faint" });
  }
  // b and p are the mod's own keys; + runs tm's new task, from a template or none.
  const keys: { key: string; label: string }[] = [];
  if (bar) keys.push({ key: "b", label: "all tasks" });
  else if (t.doneAll || done + dropped > 0) keys.push({ key: "b", label: t.doneAll ? "hide done" : "done" });
  // enter is the core's on a task's row; on a heading of Done it is this.
  if (days) keys.push({ key: "enter", label: "open or fold" });
  // p's label says what the next press lists.
  const next = PRIOS[(PRIOS.indexOf(t.prio) + 1) % PRIOS.length];
  keys.push({ key: "p", label: next === "" ? "all priorities" : next === "none" ? "no priority only" : `${next} only` });
  // [ and ] step through the bars with work; ] past the newest ends it.
  const unit = t.history?.pulse?.unit;
  if (unit) {
    keys.push({ key: "[", label: bar ? "earlier" : `filter by ${unit}` });
    if (bar) keys.push({ key: "]", label: "later" });
  }
  children.push({ kind: "keys", id: "keys", keys, actions: [{ id: "task.new", target: { project } }] });
  return { kind: "stack", children };
}

async function load(project: string, t: Tab): Promise<void> {
  t.error = "";
  try {
    t.tasks = await $.tasks.list(project);
    await loadHistory(project, t);
    await loadDone(project, t);
    await loadPicked(project, t);
    // No done task left on the board: is there one in the archive?
    if (!t.doneAll && !t.tasks.some((x) => x.status === "done")) {
      t.total = (await $.tasks.done(project, { groups: true }))?.total ?? 0;
    }
  } catch (e: any) {
    t.error = String(e?.message ?? e);
  }
}

// loadHistory reads the project's numbers when the done count or the day
// changed, as nothing the band shows moves otherwise. Failing, the board
// has no band, and nothing else changes.
async function loadHistory(project: string, t: Tab): Promise<void> {
  const at = `${t.tasks.filter((x) => x.status === "done").length} ${new Date().toDateString()}`;
  if (t.history && t.historyAt === at) return;
  try {
    t.history = await $.history.get(project);
    t.historyAt = at;
  } catch {
    t.history = null;
  }
}

// loadPicked reads the bar picked again, as far as it was read, and ends
// the pick when tm has no such bar with work any more. The numbers are in
// tm's time zone, as tasks.done reads.
async function loadPicked(project: string, t: Tab): Promise<void> {
  if (!t.pick) return;
  const bar = pickedBar(t);
  if (!bar || bar.done === 0) {
    t.pick = "";
    t.picked = { tasks: [], next: 0 };
    return;
  }
  t.picked = await readBar(project, bar, 0, Math.max(t.picked.tasks.length, PAGE));
}

// readBar reads limit of bar's rows from offset, the archive's too.
async function readBar(project: string, bar: TM.PulseBar, offset: number, limit: number): Promise<Fold> {
  const res = await $.tasks.done(project, { from: bar.from, to: bar.to, archived: true, offset, limit });
  return { tasks: res.tasks, next: res.next };
}

// step moves the pick: [ to the bar with work before it (from none, the
// newest), ] to the one after it, past the newest to none.
function step(t: Tab, k: string): void {
  const bars = (t.history?.pulse?.bars ?? []).filter((b) => b.done > 0);
  const at = bars.findIndex((b) => b.from === t.pick);
  if (k === "[") t.pick = (at < 0 ? bars[bars.length - 1] : bars[Math.max(at - 1, 0)])?.from ?? "";
  else t.pick = at < 0 ? "" : (bars[at + 1]?.from ?? "");
  t.picked = { tasks: [], next: 0 };
}

// loadDone reads Done's groups while they show, and again each fold that
// is open as far as it was read. The archive is read only for a fold that
// holds archived tasks. Nothing is read while Done is folded.
async function loadDone(project: string, t: Tab): Promise<void> {
  if (!byDay(t)) return;
  const res = await $.tasks.done(project, { groups: true, reveal: t.reveal });
  t.groups = res.groups;
  t.total = res.total;
  const was = t.rows;
  t.rows = new Map();
  for (const g of t.groups) {
    if (!isOpen(t, g)) continue;
    const read = was.get(key(g))?.tasks.length ?? 0;
    if (g.open && read <= (g.tasks ?? []).length) {
      t.rows.set(key(g), { tasks: g.tasks ?? [], next: g.next });
    } else {
      t.rows.set(key(g), await readFold(project, g, 0, Math.max(read, PAGE)));
    }
  }
}

// readFold reads limit of g's rows from offset.
async function readFold(project: string, g: TM.DoneGroup, offset: number, limit: number): Promise<Fold> {
  const res = await $.tasks.done(project, { from: g.from, to: g.to, archived: g.archived > 0, offset, limit });
  return { tasks: res.tasks, next: res.next };
}

// press handles enter or a click on a row of Done: a heading opens or
// folds, "… more" reads on. It says whether row was one of those.
async function press(project: string, t: Tab, row: string): Promise<boolean> {
  if (row === "m:pick") {
    const bar = pickedBar(t);
    try {
      t.error = "";
      if (bar && t.picked.next > 0) {
        const more = await readBar(project, bar, t.picked.next, PAGE);
        t.picked = { tasks: [...t.picked.tasks, ...more.tasks], next: more.next };
      }
    } catch (e: any) {
      t.error = String(e?.message ?? e);
    }
    return true;
  }
  const fold = row.startsWith("f:");
  if (!fold && !row.startsWith("m:")) return false;
  const k = row.slice(2);
  const g = t.groups.find((x) => key(x) === k);
  if (!g) return false;
  try {
    t.error = "";
    if (fold) {
      if (t.toggled.has(k)) t.toggled.delete(k);
      else t.toggled.add(k);
      if (isOpen(t, g) && !t.rows.has(k)) {
        t.rows.set(k, g.open ? { tasks: g.tasks ?? [], next: g.next } : await readFold(project, g, 0, PAGE));
      }
    } else {
      const f = t.rows.get(k);
      if (f && f.next > 0) {
        const more = await readFold(project, g, f.next, PAGE);
        t.rows.set(k, { tasks: [...f.tasks, ...more.tasks], next: more.next });
      }
    }
  } catch (e: any) {
    t.error = String(e?.message ?? e);
  }
  return true;
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
  const t: Tab = tabs.get(e.project) ?? { tasks: [], doneAll: false, prio: "", error: "", groups: [], total: 0, rows: new Map(), toggled: new Set(), reveal: "", history: null, historyAt: "", pick: "", picked: { tasks: [], next: 0 } };
  tabs.set(e.project, t);
  // A task shown from elsewhere needs its row, whatever day is picked.
  if (e.task) t.pick = "";
  await load(e.project, t);
  if (e.task && t.tasks.some((x) => x.id === e.task && closed(groupOf(x.status)))) {
    t.doneAll = true;
    t.reveal = e.task;
    await load(e.project, t);
  }
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
  if (e.view !== VIEW || !project) return;
  const t = tabs.get(project);
  if (!t) return;
  if (e.key === "enter") {
    if (await press(project, t, e.row ?? "")) await publish(project, t);
    return;
  }
  if (e.key === "[" || e.key === "]") {
    step(t, e.key);
    await load(project, t);
    await publish(project, t);
    return;
  }
  if (e.key !== "b" && e.key !== "p") return;
  // b ends a pick, the board as it was.
  if (e.key === "b" && t.pick) t.pick = "";
  else if (e.key === "b") t.doneAll = !t.doneAll;
  else t.prio = PRIOS[(PRIOS.indexOf(t.prio) + 1) % PRIOS.length];
  await load(project, t);
  await publish(project, t);
});

$.on("ui.click", async (e) => {
  const project = e.target?.project;
  if (e.view !== VIEW || !project) return;
  const t = tabs.get(project);
  if (t && (await press(project, t, e.row ?? ""))) await publish(project, t);
});

// A thread timeline (mods design §13, example 6): a full view with each
// thread of the project as a row of the states it went through (oldest
// first, one mark each), how long ago it last changed, and a chart of
// the changes per hour over the last 12 hours. The mod records the
// events in its own store, so the timeline starts when the mod does and
// survives restarts; terminatr keeps no history for it.
//
// The view takes the dashboard's body; the header, the sidebar with its
// needs-you marks and NEEDS YOU's count stay, and esc returns. Enter on
// a thread with a task shows the task: the row binds task.show, which
// the core runs.
import type { TM } from "./types/tm";
declare const $: TM;

const VIEW = "timeline";
const LOG = "log";
const MARKS = 16; // state changes kept per thread: a cell holds 16 spans
const HOURS = 12;
const KEEP_RESOLVED_DAYS = 7; // a resolved thread's row stays this long; the store holds 1 MiB

interface Change {
  at: string; // RFC 3339
  state: string;
}
interface Thread {
  task?: string;
  changes: Change[];
  resolved?: boolean;
}

// By project, then thread.
let log: Record<string, Record<string, Thread>> = {};
const shown = new Set<string>();

// How each state is drawn: one mark per change.
const marks: Record<string, TM.Span> = {
  started: { icon: "starting", role: "faint" },
  working: { icon: "working", role: "good" },
  idle: { icon: "idle", role: "faint" },
  blocked: { icon: "blocked", role: "warn" },
  waiting: { icon: "blocked", role: "warn" },
  review: { icon: "review", role: "info" },
  resolved: { icon: "done", role: "faint" },
};
const mark = (state: string): TM.Span => marks[state] ?? { icon: "other", role: "faint" };
const latest = (t: Thread): Change | undefined => t.changes[t.changes.length - 1];

function perHourOf(threads: Record<string, Thread>, now: Date): number[] {
  const perHour = new Array(HOURS).fill(0);
  for (const t of Object.values(threads)) {
    for (const c of t.changes) {
      const h = Math.floor((now.getTime() - Date.parse(c.at)) / 3600000);
      if (h >= 0 && h < HOURS) perHour[HOURS - 1 - h]++;
    }
  }
  return perHour;
}

function view(project: string, now: Date): TM.Widget {
  // The dashboard lists no project: the view has none to show.
  const threads = log[project] ?? {};
  const ids = Object.keys(threads).sort((a, b) => {
    const la = latest(threads[a])?.at ?? "";
    const lb = latest(threads[b])?.at ?? "";
    return la < lb ? 1 : la > lb ? -1 : a < b ? -1 : 1;
  });
  const rows = ids.map((id) => {
    const t = threads[id];
    const last = latest(t);
    return {
      id,
      cells: [
        [{ text: id, role: t.resolved ? ("faint" as TM.Role) : undefined }],
        [{ text: t.task ?? "" }],
        t.changes.map((c) => mark(c.state)),
        last ? [{ text: `${last.state} ` }, { ago: last.at, role: "faint" as TM.Role }] : [],
      ],
      actions: t.task ? [{ id: "task.show", target: { project, task: t.task } }] : [],
    };
  });
  const perHour = perHourOf(threads, now);
  return {
    kind: "stack",
    children: [
      {
        kind: "table",
        id: "threads",
        title: `${project}: threads, oldest change first`,
        columns: [{ title: "Thread" }, { title: "Task" }, { title: "States" }, { title: "Last", right: true }],
        rows,
        empty: "No thread has changed state since the mod started.",
      },
      {
        kind: "bars",
        title: `State changes per hour, the last ${HOURS} hours`,
        values: perHour,
        labels: perHour.map((_, i) => (i === HOURS - 1 ? "now" : `-${HOURS - 1 - i}h`)),
      },
    ],
  };
}

// The chart each shown project was last drawn with.
const drawn = new Map<string, string>();

async function draw(project: string, now: Date) {
  if (!shown.has(project)) return;
  drawn.set(project, perHourOf(log[project] ?? {}, now).join());
  await $.ui.view.set(VIEW, view(project, now), project ? { project } : {});
}

// prune drops the threads resolved more than KEEP_RESOLVED_DAYS before
// now, so the store doesn't grow with every thread the project ever had.
function prune(now: Date) {
  const cutoff = now.getTime() - KEEP_RESOLVED_DAYS * 86400000;
  for (const threads of Object.values(log)) {
    for (const [id, t] of Object.entries(threads)) {
      const last = latest(t);
      if (t.resolved && last && Date.parse(last.at) < cutoff) delete threads[id];
    }
  }
}

// save keeps the log in the store. A store that refuses it (full) isn't
// fatal: the timeline goes on from memory.
async function save() {
  try {
    await $.store.set(LOG, log);
  } catch (e) {
    $.log("the store refused the log:", String(e));
  }
}

async function record(project: string, thread: string, state: string, task: string | undefined, now: Date) {
  const threads = (log[project] ??= {});
  const t = (threads[thread] ??= { changes: [] });
  if (task) t.task = task;
  if (latest(t)?.state === state) return;
  t.changes.push({ at: now.toISOString(), state });
  t.changes = t.changes.slice(-MARKS);
  t.resolved = state === "resolved";
  prune(now);
  await save();
  await draw(project, now);
}

$.on("mod.ready", async (e) => {
  const stored = await $.store.get(LOG);
  log = stored && typeof stored === "object" && !Array.isArray(stored) ? (stored as typeof log) : {};
  // A project the mod no longer runs for keeps no history.
  const projects = new Set(e.projects.map((p) => p.project));
  for (const p of Object.keys(log)) if (!projects.has(p)) delete log[p];
  prune(new Date());
});

$.on("thread.started", async (e, meta) => {
  await record(e.project, e.thread, "started", e.task, new Date(meta.time));
});

$.on("thread.state", async (e, meta) => {
  await record(e.project, e.thread, e.state, e.task, new Date(meta.time));
});

$.on("task.changed", async (e, meta) => {
  if (e.thread && e.to === "review") await record(e.project, e.thread, "review", e.id, new Date(meta.time));
});

$.on("thread.resolved", async (e, meta) => {
  await record(e.project, e.thread, "resolved", e.task, new Date(meta.time));
});

$.on("view.opened", async (e, meta) => {
  if (e.view !== VIEW) return;
  shown.add(e.project ?? "");
  await draw(e.project ?? "", new Date(meta.time));
});

$.on("view.closed", async (e) => {
  if (e.view !== VIEW) return;
  shown.delete(e.project ?? "");
  drawn.delete(e.project ?? "");
  // A view keeps at most 64 trees: drop the project's once it's gone.
  await $.ui.view.set(VIEW, null, e.project ? { project: e.project } : {});
});

// The "Last" column ages by itself (an ago span); the chart only moves
// when a change passes into the next hour, so draw again only then.
$.on("clock.minute", async (e) => {
  const now = new Date(e.now);
  const stale = [...shown].filter((p) => drawn.get(p) !== perHourOf(log[p] ?? {}, now).join());
  await Promise.all(stale.map((p) => draw(p, now)));
});

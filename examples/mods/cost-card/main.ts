// A cost card (mods design §13, example 4): what each thread has used,
// in tokens and in the dollars its agent reported (an estimate, as exact
// as the agent's price table), with today's and the last 7 days' totals
// and a sparkline of the days. It folds with z and opens with enter, as
// every card does.
//
// session.state carries a thread's totals so far (tokens, cost_usd). The
// mod keeps each thread's last totals in its store, and adds what grew
// to the day it grew on, so the days count only what was spent while
// the mod ran. A thread that started while it ran (thread.started)
// counts from nothing, so its first turn, spent before its first
// session.state, counts too; one that was running already when the mod
// first hears of it sets a baseline, its earlier spend unknown.
import type { TM } from "./types/tm";
declare const $: TM;

const CARD = "cost";
const THREADS = "threads";
const DAYS = "days";
const FRESH = "fresh";
const KEEP_DAYS = 14;

interface Used {
  project: string;
  thread: string;
  title: string;
  tokens: number;
  cost: number;
  resolved?: string; // RFC 3339, once the thread is resolved
}
interface Day {
  tokens: number;
  cost: number;
}

let threads: Record<string, Used> = {}; // by "<project>/<thread>"
let days: Record<string, Day> = {}; // by "YYYY-MM-DD"
let fresh: Record<string, true> = {}; // threads started while the mod ran, not heard of since
const shown = new Set<string>(); // projects whose card shows ("" for all)

const money = (usd: number) => `$${usd.toFixed(2)}`;

function compact(n: number): string {
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
  return String(n);
}

// day is the date d falls on, n days earlier.
function day(d: Date, n = 0): string {
  return new Date(d.getTime() - n * 86400000).toISOString().slice(0, 10);
}

function card(project: string, now: Date): TM.Widget {
  const week = [6, 5, 4, 3, 2, 1, 0].map((n) => days[day(now, n)]?.cost ?? 0);
  const today = days[day(now)] ?? { tokens: 0, cost: 0 };
  const mine = Object.values(threads)
    .filter((u) => project === "" || u.project === project)
    .sort((a, b) => b.cost - a.cost || b.tokens - a.tokens);
  const rows = mine.slice(0, Number($.settings().rows) || 5).map((u) => ({
    id: `${u.project}/${u.thread}`,
    cells: [
      [{ text: project ? u.title : `${u.project} ${u.title}` }],
      [{ text: compact(u.tokens), role: "faint" as TM.Role }],
      [{ text: money(u.cost) }],
    ],
  }));
  return {
    kind: "stack",
    children: [
      {
        kind: "kv",
        pairs: [
          { key: "today", value: [{ text: money(today.cost), role: "accent" }, { text: `  ${compact(today.tokens)} tokens`, role: "faint" }] },
          { key: "7 days", value: [{ text: money(week.reduce((a, b) => a + b, 0)) }] },
        ],
      },
      { kind: "bars", values: week, spark: true },
      {
        kind: "table",
        id: "threads",
        columns: [{ title: "Thread" }, { title: "Tokens", right: true }, { title: "Cost", right: true }],
        rows,
        empty: "No thread has reported any use yet.",
      },
    ],
  };
}

// prune drops the threads resolved more than KEEP_DAYS before now, so
// the store doesn't grow with every thread ever seen (it holds 1 MiB).
function prune(now: Date) {
  const cutoff = now.getTime() - KEEP_DAYS * 86400000;
  for (const [k, u] of Object.entries(threads)) if (u.resolved && Date.parse(u.resolved) < cutoff) delete threads[k];
}

// save keeps a value in the store. A store that refuses it (full) isn't
// fatal: the card goes on from memory.
async function save(key: string, value: unknown) {
  try {
    await $.store.set(key, value);
  } catch (e) {
    $.log(`the store refused ${key}:`, String(e));
  }
}

// record is a stored map, or an empty one when the store has none or
// something else under its key.
function record<T>(stored: unknown): Record<string, T> {
  return stored && typeof stored === "object" && !Array.isArray(stored) ? (stored as Record<string, T>) : {};
}

async function draw(now: Date) {
  await Promise.all(
    [...shown].map((p) => $.ui.view.set(CARD, card(p, now), { project: p || undefined, height: 6 + Math.min(Object.keys(threads).length, Number($.settings().rows) || 5) })),
  );
}

$.on("mod.ready", async () => {
  threads = record<Used>(await $.store.get(THREADS));
  days = record<Day>(await $.store.get(DAYS));
  fresh = record<true>(await $.store.get(FRESH));
  prune(new Date());
});

$.on("thread.resolved", async (e, meta) => {
  const k = `${e.project}/${e.thread}`;
  if (threads[k]) threads[k].resolved = new Date(meta.time).toISOString();
  delete fresh[k];
  prune(new Date(meta.time));
  await save(THREADS, threads);
  await save(FRESH, fresh);
});

$.on("thread.started", async (e) => {
  fresh[`${e.project}/${e.thread}`] = true;
  await save(FRESH, fresh);
});

$.on("session.state", async (e, meta) => {
  if (!e.project || !e.thread || e.tokens === undefined) return;
  const k = `${e.project}/${e.thread}`;
  // A thread started while the mod ran spent from nothing.
  const was: Used | undefined = threads[k] ?? (fresh[k] ? { project: e.project, thread: e.thread, title: "", tokens: 0, cost: 0 } : undefined);
  const tokens = e.tokens ?? 0;
  const cost = e.cost_usd ?? 0;
  if (was && was.tokens === tokens && was.cost === cost) return;
  const now = new Date(meta.time);
  if (was && tokens >= was.tokens && cost >= was.cost) {
    const d = (days[day(now)] ??= { tokens: 0, cost: 0 });
    d.tokens += tokens - was.tokens;
    d.cost += cost - was.cost;
    for (const old of Object.keys(days)) if (old < day(now, KEEP_DAYS)) delete days[old];
    await save(DAYS, days);
  }
  // The thread's title, once: its id until the project says.
  const title = was?.title || (await $.thread.get(e.project, e.thread))?.title || e.thread;
  threads[k] = { project: e.project, thread: e.thread, title, tokens, cost };
  prune(now);
  await save(THREADS, threads);
  if (fresh[k]) {
    delete fresh[k];
    await save(FRESH, fresh);
  }
  await draw(now);
});

$.on("view.opened", async (e, meta) => {
  if (e.view !== CARD) return;
  shown.add(e.project ?? "");
  await draw(new Date(meta.time));
});

$.on("view.closed", async (e) => {
  if (e.view !== CARD) return;
  shown.delete(e.project ?? "");
  // A view keeps at most 64 trees: drop the project's once it's gone.
  await $.ui.view.set(CARD, null, { project: e.project || undefined });
});

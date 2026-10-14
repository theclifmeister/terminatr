// Each thread's changes since its base in a sidebar column (mod API
// 1.8): "+12 -3", in the good and bad roles, with a dot while the thread
// has uncommitted work. At the start it asks $.thread.changes for each
// thread of mod.ready's snapshot; after that the thread.changes event
// carries the same totals whenever a live thread edits, commits or
// pushes. A thread without a repo, or resolved, has no cell.
import type { TM } from "./types/tm";
declare const $: TM;

const COLUMN = "changes";

interface Totals {
  dirty: boolean;
  add: number;
  del: number;
  files: number;
}

// spans draws totals in at most 10 cells; none for a thread with no
// changes at all.
function spans(t: Totals | null): TM.Span[] | null {
  if (!t || (t.files === 0 && !t.dirty)) return null;
  const out: TM.Span[] = [];
  if (t.dirty) out.push({ text: "• ", role: "warn" });
  out.push({ text: `+${short(t.add)}`, role: "good" });
  out.push({ text: ` -${short(t.del)}`, role: "bad" });
  return out;
}

// short keeps a count within 3 cells: 999, 12k.
function short(n: number): string {
  if (n < 1000) return String(n);
  if (n < 100000) return `${Math.floor(n / 1000)}k`;
  return "99k";
}

function draw(project: string, thread: string, t: Totals | null) {
  return $.ui.cell.set(COLUMN, { project, thread }, spans(t));
}

$.on("mod.ready", async (e) => {
  const sets: Promise<void>[] = [];
  for (const p of e.projects) {
    for (const t of p.threads) {
      const cs = await $.thread.changes(p.project, t.id);
      sets.push(draw(p.project, t.id, cs.state === "none" ? null : { dirty: cs.state === "dirty", ...cs.totals }));
    }
  }
  await Promise.all(sets);
});

$.on("thread.changes", async (e) => {
  await draw(e.project, e.thread, e);
});

$.on("thread.resolved", async (e) => {
  await draw(e.project, e.thread, null);
});

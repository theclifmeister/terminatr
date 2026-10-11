// The sidebar's PR/CI column (mods design §11, proof 1): each thread's
// pull request checks as the ticker last saw them, ✓ passing, ✗ failing,
// ● running, in the console's icon set. It draws from mod.ready's
// snapshot and pr.changed, and clears a thread's cell when the thread is
// resolved. It never asks for a PR itself: the ticker polls.
import type { TM } from "./types/tm";
declare const $: TM;

const COLUMN = "ci";
// The targets this mod has a cell for, kept in its store so a restart
// can clear the cells of threads resolved while it wasn't running (the
// server shows the last cells again before the mod runs).
const DRAWN = "drawn";

const look: Record<string, TM.Span[]> = {
  pass: [{ icon: "ci.pass", role: "good" }],
  fail: [{ icon: "ci.fail", role: "bad" }],
  pending: [{ icon: "ci.run", role: "faint" }],
};

let drawn = new Set<string>(); // "<project>/<thread>"
let saved = "";
// The cell sets a handler made, awaited together at its end.
let sets: Promise<void>[] = [];

const key = (project: string, thread: string) => `${project}/${thread}`;

function draw(project: string, thread: string, checks: string | undefined) {
  const spans = look[checks ?? ""] ?? null;
  const k = key(project, thread);
  if (!spans && !drawn.has(k)) return;
  sets.push($.ui.cell.set(COLUMN, { project, thread }, spans));
  if (spans) drawn.add(k);
  else drawn.delete(k);
}

// done waits for the handler's cell sets and stores the targets drawn
// when they changed.
async function done() {
  const pending = sets;
  sets = [];
  await Promise.all(pending);
  const now = JSON.stringify([...drawn].sort());
  if (now !== saved) {
    saved = now;
    await $.store.set(DRAWN, [...drawn].sort());
  }
}

// paint draws every thread of the projects given; any target drawn
// before in those projects that isn't among their threads now is
// cleared.
function paint(projects: TM.ProjectWatch[]) {
  const seen = new Set<string>();
  for (const p of projects) {
    for (const t of p.threads) {
      seen.add(key(p.project, t.id));
      draw(p.project, t.id, t.pr_checks);
    }
  }
  const slugs = new Set(projects.map((p) => p.project));
  for (const k of [...drawn]) {
    const [project, thread] = k.split("/");
    if (slugs.has(project) && !seen.has(k)) draw(project, thread, undefined);
  }
}

$.on("mod.ready", async (e) => {
  const stored = await $.store.get(DRAWN);
  drawn = new Set(Array.isArray(stored) ? (stored as string[]) : []);
  saved = JSON.stringify([...drawn].sort());
  paint(e.projects);
  // A project no longer among the mod's keeps no cells.
  const slugs = new Set(e.projects.map((p) => p.project));
  for (const k of [...drawn]) {
    const [project, thread] = k.split("/");
    if (!slugs.has(project)) draw(project, thread, undefined);
  }
  await done();
});

$.on("pr.changed", async (e, meta) => {
  if (meta.dropped > 0) {
    // The queue dropped events: look again rather than trust the gap.
    paint([await $.project.get(e.project)]);
  } else {
    draw(e.project, e.thread, e.checks);
  }
  await done();
});

$.on("thread.resolved", async (e) => {
  draw(e.project, e.thread, undefined);
  await done();
});

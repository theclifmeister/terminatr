// The mods design's example B (§10), against the generated types: the
// test copies tm.d.ts to types/tm.d.ts beside it.
import type { TM } from "./types/tm";
declare const $: TM;

const since = new Map<string, number>(); // task key → when it entered review
const key = (p: string, id: string) => `${p}/${id}`;

$.on("mod.ready", async () => {
  for (const p of await $.projects.list())
    for (const t of await $.tasks.list(p.slug))
      if (t.status === "review") since.set(key(p.slug, t.id), (await $.store.get(key(p.slug, t.id))) ?? Date.now());
  paint();
});

$.on("task.changed", async (e) => {
  const k = key(e.project, e.id);
  if (e.to === "review") { since.set(k, Date.now()); await $.store.set(k, Date.now()); }
  else if (e.from === "review") { since.delete(k); await $.store.del(k); $.ui.cell.set("age", { project: e.project, task: e.id }, null); }
  paint();
});

$.on("clock.minute", paint);

function paint() {
  const warn = $.settings().warn_after_hours * 3600e3;
  for (const [k, t0] of since) {
    const [project, task] = k.split("/");
    const ms = Date.now() - t0;
    const text = ms < 3600e3 ? `${Math.floor(ms / 60e3)}m` : ms < 86400e3 ? `${Math.floor(ms / 3600e3)}h` : `${Math.floor(ms / 86400e3)}d`;
    $.ui.cell.set("age", { project, task }, [{ text, role: ms > warn ? "bad" : "faint" }]);
  }
}

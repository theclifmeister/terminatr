// The mods design's example C (§10), against the generated types: the
// test copies tm.d.ts to types/tm.d.ts beside it. The report's PR is
// e.pr (the design's sketch had e.sections.pr).
import type { TM } from "./types/tm";

type Report = TM.Events["report.stored"];
const RULES: [string, (e: Report) => boolean, TM.Role][] = [
  ["docs",      e => /\b(README|docs?\/|SPEC\.md)\b/i.test(e.sections.report), "info"],
  ["no-check",  e => (e.sections.check ?? []).length === 0,                    "warn"],
  ["needs-you", e => /\b(blocked|waiting on|needs? a decision)\b/i.test(e.sections.report), "bad"],
];

$.on("report.stored", async (e) => {
  if (!e.task) return;
  const labels = RULES.filter(([, test]) => test(e)).map(([text, , role]) => ({ text, role }));
  await $.annotate({ project: e.project, task: e.task }, "labels", labels.length ? labels : null);
  if (labels.some(l => l.text === "no-check") && e.pr)
    await $.inbox.raise(e.project, { subject: e.task, summary: `${e.task} report has no ## Check; ask the thread how to verify` });
});

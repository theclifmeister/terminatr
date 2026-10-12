// Notifications (mods design §13, example 5): an OS notification when a
// thread or a coordinator asks you something, and when a task reaches
// review. A button names a core action: the question's options for a
// question menu (question.answer), Accept and Send back for a review,
// Show for a question in a thread's status (the task's popup, where the
// thread and its question are). Pressing one brings terminatr forward
// and opens the core's own confirmation (Answer t-0012's question
// "Which port?" with "8080"?, Accept T12?) or the send-back note,
// checked against the live menu or board; nothing is accepted, sent
// back or answered by the notification, and the mod hears of a press
// only as action.done. The console draws the notification under its own
// title, terminatr · mod: notify-me.
import type { TM } from "./types/tm";
declare const $: TM;

// What a thread or coordinator waits on, by "<project>/<thread>" (or
// "<project>/coordinator"), so the same question notifies once.
const ASKED = "asked";

let asked: Record<string, string> = {};

const on = (setting: string) => $.settings()[setting] !== false;

async function notify(n: TM.Notification) {
  try {
    await $.ui.notify(n);
  } catch (err) {
    // Over the rate (6 a minute), the grant taken back, or the menu gone
    // already: the board still shows it all.
    $.log(`not notified: ${(err as Error).message}`);
  }
}

// ask records question as what key waits on; false when it already
// did, so it isn't told again.
async function ask(key: string, question: string): Promise<boolean> {
  if ((asked[key] ?? "") === question) return false;
  if (question) asked[key] = question;
  else delete asked[key];
  await $.store.set(ASKED, asked);
  return question !== "";
}

$.on("mod.ready", async (e) => {
  const stored = await $.store.get(ASKED);
  asked = stored && typeof stored === "object" ? (stored as Record<string, string>) : {};
  // What needs you now was there before the mod started: it isn't news,
  // but it isn't to be told again either.
  for (const p of e.projects) {
    for (const t of p.threads) {
      if (t.needs_you) asked[`${p.project}/${t.id}`] = t.needs_you;
    }
  }
  await $.store.set(ASKED, asked);
});

// A question menu opened on a thread's or a coordinator's session: its
// options are the buttons (at most three), each the core's answer.
$.on("session.question", async (e) => {
  const who = e.thread || "coordinator";
  const q = e.question;
  if (!(await ask(`${e.project}/${who}`, q?.question ?? "")) || !q || !on("needs_you")) return;
  // Several options at once are picked in the session itself.
  const options = q.multi_select ? [] : q.options.slice(0, 3);
  const more = q.multi_select ? " (pick them in the session)" : q.options.length > 3 ? " (more in the session)" : "";
  await notify({
    title: `${e.thread ?? e.project + " coordinator"} asks`,
    text: `${e.project}: ${q.question}${more}`,
    level: "warn",
    actions: options.map((o) => ({ id: "question.answer", target: { session: e.session, question: q.question, option: o.label } })),
  });
});

// A thread's status needs you. The question of a menu came as
// session.question already, and is told once.
$.on("thread.state", async (e) => {
  if (!(await ask(`${e.project}/${e.thread}`, e.needs_you ?? "")) || !on("needs_you")) return;
  const target = { project: e.project, task: e.task };
  await notify({
    title: `${e.task ?? e.thread} needs you`,
    text: `${e.project}: ${e.needs_you}`,
    level: "warn",
    actions: e.task ? [{ id: "task.show", target }] : [],
  });
});

$.on("task.changed", async (e) => {
  if (e.to !== "review" || e.from === "review" || !on("review")) return;
  const target = { project: e.project, task: e.id };
  await notify({
    title: `${e.id} is ready for review`,
    text: `${e.project}: ${e.title}`,
    actions: [
      { id: "task.accept", target },
      { id: "task.send-back", target },
      { id: "task.show", target },
    ],
  });
});

$.on("thread.resolved", async (e) => {
  const k = `${e.project}/${e.thread}`;
  if (!(k in asked)) return;
  delete asked[k];
  await $.store.set(ASKED, asked);
});

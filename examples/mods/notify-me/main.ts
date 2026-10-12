// Notifications (mods design §13, example 5): an OS notification when a
// thread starts to need you, and when a task reaches review. A button
// names a core action on the task: Accept and Send back for a review,
// Show for a question (the task's popup, where the thread and its
// question are). Pressing one brings terminatr forward and opens the
// core's own confirmation (Accept T12?) or the send-back note, checked
// against the live board; nothing is accepted, sent back or answered by
// the notification, and the mod hears nothing of it. The console draws
// the notification under its own title, terminatr · mod: notify-me.
import type { TM } from "./types/tm";
declare const $: TM;

// What a thread waits on, by "<project>/<thread>", so the same question
// notifies once.
const ASKED = "asked";

let asked: Record<string, string> = {};

const on = (setting: string) => $.settings()[setting] !== false;

async function notify(n: TM.Notification) {
  try {
    await $.ui.notify(n);
  } catch (err) {
    // Over the rate (6 a minute) or the grant taken back: the board
    // still shows it all.
    $.log(`not notified: ${(err as Error).message}`);
  }
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

$.on("thread.state", async (e) => {
  const k = `${e.project}/${e.thread}`;
  const question = e.needs_you ?? "";
  if ((asked[k] ?? "") === question) return;
  if (question) asked[k] = question;
  else delete asked[k];
  await $.store.set(ASKED, asked);
  if (!question || !on("needs_you")) return;
  const target = { project: e.project, task: e.task };
  await notify({
    title: `${e.task ?? e.thread} needs you`,
    text: `${e.project}: ${question}`,
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

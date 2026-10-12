// Standup: a command (mods design §2 tier 4). The human runs it with S
// on the dashboard, from the ≡ menu or with tm mod run; the mod gets
// command.run with the project shown. A console's command may open one
// of the mod's views (a human input), so it opens its full view, which
// it draws when shown (view.opened). tm mod run can't open a view: the
// open is refused, and the mod says the same in one line, as a toast.
import type { TM } from "./types/tm";
declare const $: TM;

const VIEW = "standup";

// The board's statuses, in the order the view lists them.
const GROUPS: [string, string, TM.Role][] = [
  ["blocked", "Blocked", "bad"],
  ["review", "In review", "warn"],
  ["started", "Started", "accent"],
  ["ready", "Ready", "info"],
];

// project is the project the last command was for, which the view
// shows; shown says it shows.
let project = "";
let shown = false;

function line(t: TM.Task): TM.Span[] {
  const out: TM.Span[] = [{ text: `${t.id} ${t.title}` }];
  if (t.steps_total > 0) out.push({ text: `  ${t.steps_done}/${t.steps_total}`, role: "faint" });
  if (t.thread) out.push({ text: `  ${t.thread}`, role: "faint" });
  return out;
}

// counts is "1 blocked · 2 started · 3 ready", the toast's line.
function counts(tasks: TM.Task[]): string {
  const parts: string[] = [];
  for (const [status, label] of GROUPS) {
    const n = tasks.filter((t) => t.status === status).length;
    if (n > 0) parts.push(`${n} ${label.toLowerCase()}`);
  }
  return parts.length > 0 ? parts.join(" · ") : "nothing open";
}

async function draw(slug: string): Promise<void> {
  const tasks = await $.tasks.list(slug);
  const children: TM.Widget[] = [{ kind: "text", lines: [[{ text: slug, role: "head" }, { text: "  " + counts(tasks), role: "faint" }]] }];
  for (const [status, label, role] of GROUPS) {
    const some = tasks.filter((t) => t.status === status);
    if (some.length === 0) continue;
    children.push({ kind: "text", text: `${label} (${some.length})`, role });
    children.push({ kind: "list", id: status, rows: some.map((t) => ({ id: t.id, spans: line(t) })) });
  }
  await $.ui.view.set(VIEW, { kind: "stack", children }, { project: slug });
}

$.on("command.run", async (cmd) => {
  if (!cmd.project) {
    await $.ui.toast("Standup: show a project first (or tm mod run standup show --project <slug>)", "warn");
    return;
  }
  project = cmd.project;
  try {
    await $.ui.view.open(VIEW);
  } catch {
    // tm mod run: no console to open it on.
    await $.ui.toast(`${cmd.project}: ${counts(await $.tasks.list(cmd.project))}`);
  }
});

$.on("view.opened", async (v) => {
  shown = true;
  project = v.project || project;
  if (project) await draw(project);
});

$.on("view.closed", () => {
  shown = false;
});

// While it shows, the view follows the board.
$.on("task.changed", async (t) => {
  if (shown && t.project === project) await draw(project);
});

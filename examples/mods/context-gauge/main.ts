// A context gauge (mods design §13, example 3): each session's use of its
// context window as a cell on its row, faint below the warning, then
// warn, then bad from 95%. A dashboard card lists the sessions past the
// warning; each row binds the core actions session.clear (C) and
// session.attach (enter, or the keys the console's keymap gives them).
// C asks first, in tm's own words, then has the agent start a fresh
// conversation once it is idle; the project's files keep its state. The
// mod only names the actions: the core checks them against live state
// and runs them, and the mod hears action.done.
//
// session.state carries the context use; it needs no grant. The mod
// keeps what it saw in its store, since mod.ready's snapshot has no
// session's context.
import type { TM } from "./types/tm";
declare const $: TM;

const COLUMN = "ctx";
const CARD = "full";
const SEEN = "sessions";
const FULL = 95;

interface Seen {
  project: string;
  thread: string;
  role: string;
  context: number;
}

let sessions: Record<string, Seen> = {};
// The projects whose card a console shows ("" for the card of all).
const shown = new Set<string>();

const warn = () => Number($.settings().warn) || 80;

function role(pct: number): TM.Role {
  return pct >= FULL ? "bad" : pct >= warn() ? "warn" : "faint";
}

function cell(pct: number): TM.Span[] | null {
  return pct < 0 ? null : [{ text: `${pct}%`, role: role(pct) }];
}

function who(s: Seen): string {
  return s.role === "coordinator" || !s.thread ? `${s.project} coordinator` : `${s.project} ${s.thread}`;
}

function card(project: string): TM.Widget {
  const rows = Object.keys(sessions)
    .filter((id) => sessions[id].context >= warn() && (project === "" || sessions[id].project === project))
    .sort((a, b) => sessions[b].context - sessions[a].context || (a < b ? -1 : 1))
    .map((id) => ({
      id,
      spans: [{ text: `${sessions[id].context}%`.padStart(4), role: role(sessions[id].context) }, { text: "  " + who(sessions[id]) }],
      actions: [
        { id: "session.clear", target: { session: id } },
        { id: "session.attach", target: { session: id } },
      ],
    }));
  return {
    kind: "stack",
    children: [
      { kind: "list", id: "sessions", rows, empty: `No session is past ${warn()}% of its context.` },
      { kind: "text", role: "faint", text: "Clear starts a fresh conversation: the project's state is in its files, so nothing is lost." },
    ],
  };
}

async function drawCards() {
  await Promise.all([...shown].map((p) => $.ui.view.set(CARD, card(p), p ? { project: p } : {})));
}

async function save() {
  await $.store.set(SEEN, sessions);
}

$.on("mod.ready", async () => {
  const stored = await $.store.get(SEEN);
  sessions = stored && typeof stored === "object" ? (stored as Record<string, Seen>) : {};
  // A settings change restarts the mod, so the warning may have moved:
  // redraw every cell and card with it.
  await Promise.all(Object.keys(sessions).map((id) => $.ui.cell.set(COLUMN, { session: id }, cell(sessions[id].context))));
  await drawCards();
});

$.on("session.state", async (e) => {
  if (!e.project) return; // a session outside the projects has no row of the mod's
  const old = sessions[e.session];
  if (e.state === "exited") {
    if (!old) return;
    delete sessions[e.session];
    await $.ui.cell.set(COLUMN, { session: e.session }, null);
  } else {
    if (old && old.context === e.context) return;
    sessions[e.session] = { project: e.project, thread: e.thread ?? "", role: e.role, context: e.context };
    await $.ui.cell.set(COLUMN, { session: e.session }, cell(e.context));
  }
  await save();
  await drawCards();
});

$.on("view.opened", async (e) => {
  if (e.view !== CARD) return;
  shown.add(e.project ?? "");
  await drawCards();
});

$.on("view.closed", async (e) => {
  shown.delete(e.project ?? "");
});

$.on("action.done", async (e) => {
  if (!e.ok) $.log(`${e.action} on ${e.target.session} didn't run`);
});

// The project popup's Memory tab (mods design §11, proof 2): the
// project's memory files (CONTEXT.md, MEMORY.md and the notes in
// memory/) as a list, and a text viewer for the one opened, a note's
// front matter as one line; t shows the template of a new note. The
// core keeps the cursor, the filter and the scroll; only enter, n, p,
// t and clicks on a row reach the mod. A note's row, and the viewer of
// one, bind the core's memory.edit (e: the human's editor, then back
// here) and memory.rm (d: tm asks first); the list binds memory.new
// (n). tm runs them, as tm memory edit, new and rm do, and the mod
// hears action.done, then reads the files again. It draws when the tab
// is shown (view.opened) and reads the files again then, so the tab
// never shows older memory than when it was opened.
import type { TM } from "./types/tm";
declare const $: TM;

const VIEW = "memory";
const LIST = "files";

// Limits of the widget set (internal/widget): a text widget's bytes
// and lines, the widgets of a tree, with room for the stack, the head,
// the keys and the cut-short line, and the tree's JSON (1 MB) with room
// for escapes.
const MAX_TEXT = 60000;
const MAX_LINES = 1900;
const MAX_BLOCKS = 250;
const MAX_TREE = 800000;

// TEMPLATE is a new memory note with the schema: tm's own
// (internal/project.NoteTemplate, which tm memory template prints).
const TEMPLATE = `+++
# Rules, not stories: no how-we-found-out.
# No task ids, dates or (user, date) provenance in the text, except on
# a user decision someone may question.
# Each fact once, in the note whose touches fit; others link [[note]].
# Point to a repo file instead of copying its steps.
# Terse one-line imperative bullets, no hedging. Keep stop words (only,
# not): matching ignores them anyway.
# Never store what the code, git or docs already say.
# Never note a way around tm's sandbox, guard or access (run something
# unsandboxed, dodge a refusal, have the coordinator run it for a
# thread): report it, for a fix in tm or an access rule set through tm.
# How a sandbox works, kept for building tm, is fine.
title    = "One line: what was decided"
touches  = ["path/in/repo.go", "repo:<name>"]
standing = false
audience = "everyone"
source   = "T<n>"
+++
## Decided
What holds, and why.

## Ruled out
What was tried or proposed and dropped, and why.

## Watch out
What bites whoever changes this next.
`;

// noteActions are the core actions a note binds: edit it, delete it.
function noteActions(project: string, path: string): TM.Value {
  return [
    { id: "memory.edit", target: { project, path } },
    { id: "memory.rm", target: { project, path } },
  ];
}

function isNote(path: string): boolean {
  return path.indexOf("memory/") === 0;
}

// What a project's tab shows: the files, and the one open in the viewer
// ("" for the list), or the note template.
interface Tab {
  files: TM.MemoryFile[];
  open: string;
  text: string;
  error: string;
  template: boolean;
}

const tabs = new Map<string, Tab>();

// bytes is s's length in UTF-8, which the widget limits count.
function bytes(s: string): number {
  let n = 0;
  for (const ch of s) {
    const c = ch.codePointAt(0) ?? 0;
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return n;
}

function size(n: number): string {
  if (n < 1024) return `${n} B`;
  return `${Math.round(n / 102.4) / 10} KB`;
}

// front splits a note's TOML front matter (between +++ lines) from its
// body, and says it as one line: touches, standing, audience, source.
// A value is read as JSON, which TOML's strings, arrays of strings and
// booleans are; anything else shows as written. Without a closing +++ there is
// no front matter, as tm reads it.
function front(md: string): { meta: string; body: string } {
  const lines = md.replace(/\r\n?/g, "\n").split("\n");
  if (lines[0] !== "+++") return { meta: "", body: md };
  const end = lines.indexOf("+++", 1);
  if (end < 0) return { meta: "", body: md };
  const vals = new Map<string, unknown>();
  for (const l of lines.slice(1, end)) {
    const m = /^\s*([A-Za-z_]+)\s*=\s*(.*?)\s*$/.exec(l);
    if (!m) continue;
    try {
      vals.set(m[1], JSON.parse(m[2]));
    } catch {
      vals.set(m[1], m[2]);
    }
  }
  const meta: string[] = [];
  const touches = vals.get("touches");
  if (Array.isArray(touches) && touches.length > 0) meta.push(`touches ${touches.join(", ")}`);
  else if (typeof touches === "string" && touches) meta.push(`touches ${touches}`);
  if (vals.get("standing") === true) meta.push("standing");
  const audience = vals.get("audience");
  if (audience && audience !== "everyone") meta.push(`for the ${audience} only`);
  const source = vals.get("source");
  if (source) meta.push(`from ${source}`);
  return { meta: meta.join(" · "), body: lines.slice(end + 1).join("\n") };
}

// blocks is markdown as text widgets: each heading in the head role (the
// file's own title, its first "# " line, left out: the viewer's head
// says it), the text between them plain, a link as its text. Text runs
// are cut to the widget set's limits; past MAX_BLOCKS widgets or
// MAX_TREE bytes the file is cut short, and says so. A wide glyph (an
// emoji) is the core's to draw: it shows as ?.
function blocks(md: string): TM.Widget[] {
  const out: TM.Widget[] = [];
  let run: string[] = [];
  let runBytes = 0;
  let total = 0;
  let first = true;
  const flush = () => {
    while (run.length > 0 && run[run.length - 1] === "") run.pop();
    while (run.length > 0 && run[0] === "") run.shift();
    if (run.length > 0) out.push({ kind: "text", text: run.join("\n") });
    run = [];
    runBytes = 0;
  };
  for (let line of md.replace(/\r\n?/g, "\n").split("\n")) {
    if (out.length >= MAX_BLOCKS || total > MAX_TREE) break;
    line = line.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1").replace(/\s+$/, "");
    const h = /^#{1,6}\s+(.*)$/.exec(line);
    if (h) {
      if (!(first && line.startsWith("# "))) {
        flush();
        out.push({ kind: "text", text: h[1].slice(0, 200), role: "head" });
      }
      first = false;
      continue;
    }
    if (line !== "") first = false;
    if (line === "" && (run.length === 0 || run[run.length - 1] === "")) continue;
    // A line longer than a widget's text takes widgets of its own, cut
    // on a code point (16000 UTF-16 units are at most 48000 bytes).
    while (bytes(line) > MAX_TEXT && out.length < MAX_BLOCKS) {
      flush();
      const hi = line.charCodeAt(15999);
      const end = hi >= 0xd800 && hi <= 0xdbff ? 15999 : 16000;
      const cut = line.slice(0, end);
      out.push({ kind: "text", text: cut });
      line = line.slice(cut.length);
      total += bytes(cut);
    }
    const n = bytes(line) + 1;
    if (runBytes + n > MAX_TEXT || run.length >= MAX_LINES) flush();
    run.push(line);
    runBytes += n;
    total += n;
  }
  const cut = out.length >= MAX_BLOCKS || total > MAX_TREE;
  if (!cut) flush();
  if (cut) out.push({ kind: "text", text: "… the rest of the file doesn't fit the tab.", role: "faint" });
  return out;
}

// tree is project's tab's widget tree: the list, the open file or the
// template.
function tree(project: string, t: Tab): TM.Widget {
  if (t.error) {
    return { kind: "text", text: `error: ${t.error}`, role: "bad" };
  }
  if (t.template) {
    return {
      kind: "stack",
      children: [
        { kind: "text", lines: [[{ text: "New note", role: "accent" }, { text: "  memory/<name>.md · tm memory template", role: "faint" }]] },
        { kind: "text", text: TEMPLATE.replace(/\s+$/, "") },
        {
          kind: "text",
          text: "All of it is optional; a free-form note still works. touches are paths in a repo (a folder ends with /) or repo:<name>; standing notes are for every thread; audience = \"coordinator\" keeps a note from threads. tm lists the note in MEMORY.md with its touches.",
          role: "faint",
        },
        { kind: "keys", keys: [{ key: "enter", label: "files" }], actions: [{ id: "memory.new", target: { project } }] },
      ],
    };
  }
  const file = t.files.find((f) => f.path === t.open);
  if (!file) {
    return {
      kind: "stack",
      children: [
        {
          kind: "list",
          id: LIST,
          empty: "no memory yet",
          rows: t.files.map((f) => ({
            id: f.path,
            spans: [
              { text: f.title },
              { text: `  ${f.path} · ${size(f.bytes)}${f.touches?.length ? ` · ${f.touches.join(", ")}` : ""}`, role: "faint" },
            ],
            ...(isNote(f.path) ? { actions: noteActions(project, f.path) } : {}),
          })),
        },
        {
          kind: "text",
          text: "Every thread is told these. e edits a note in your editor, n writes a new one, d deletes one; tm lists the notes in MEMORY.md and tells the coordinator.",
          role: "faint",
        },
        {
          kind: "keys",
          keys: [
            { key: "enter", label: "read" },
            { key: "t", label: "note template" },
          ],
          actions: [{ id: "memory.new", target: { project } }],
        },
      ],
    };
  }
  const note = file.path.startsWith("memory/") ? front(t.text) : { meta: "", body: t.text };
  const body = blocks(note.body);
  return {
    kind: "stack",
    children: [
      { kind: "text", lines: [[{ text: file.title, role: "accent" }, { text: `  ${file.path}`, role: "faint" }]] },
      ...(note.meta ? [{ kind: "text", text: note.meta.slice(0, 1000), role: "faint" } as TM.Widget] : []),
      ...(body.length > 0 ? body : [{ kind: "text", text: "empty", role: "faint" }]),
      {
        kind: "keys",
        keys: [
          { key: "enter", label: "files" },
          { key: "n", label: "next" },
          { key: "p", label: "previous" },
        ],
        ...(isNote(file.path) ? { actions: noteActions(project, file.path) } : {}),
      },
    ],
  };
}

// load reads project's files again, and the open one's text.
async function load(project: string, t: Tab) {
  t.error = "";
  try {
    t.files = await $.memory.list(project);
    if (t.open && !t.files.some((f) => f.path === t.open)) t.open = "";
    t.text = t.open ? await $.memory.read(project, t.open) : "";
  } catch (e) {
    t.error = String((e as Error)?.message ?? e);
  }
}

// publish draws project's tab; a tree the widget set refuses (a file
// too full of escapes for its limit) says why instead.
async function publish(project: string, t: Tab) {
  try {
    await $.ui.view.set(VIEW, tree(project, t), { project });
  } catch (e) {
    const why = String((e as Error)?.message ?? e);
    $.log(`memory-tab: ${project}: ${why}`);
    await $.ui.view.set(VIEW, { kind: "text", text: `can't draw ${t.open || "the files"}: ${why}`, role: "bad" }, { project });
  }
}

function tab(project: string): Tab {
  let t = tabs.get(project);
  if (!t) {
    t = { files: [], open: "", text: "", error: "", template: false };
    tabs.set(project, t);
  }
  return t;
}

$.on("view.opened", async (e) => {
  if (e.view !== VIEW || !e.project) return;
  const t = tab(e.project);
  await load(e.project, t);
  await publish(e.project, t);
});

$.on("view.closed", async (e) => {
  if (e.view !== VIEW || !e.project) return;
  // Opened again, the tab starts at the list, read afresh.
  tabs.delete(e.project);
  // A view keeps at most 64 trees: drop the project's once it's gone.
  await $.ui.view.set(VIEW, null, { project: e.project });
});

// open shows file path (in the list) of project's tab; "" goes back to
// the list.
async function open(project: string, path: string) {
  const t = tab(project);
  t.open = path;
  t.template = false;
  await load(project, t);
  await publish(project, t);
}

$.on("ui.key", async (e) => {
  const project = e.target?.project;
  if (e.view !== VIEW || !project) return;
  const t = tab(project);
  const at = t.files.findIndex((f) => f.path === t.open);
  switch (e.key) {
    case "t":
      if (!t.open && !t.template) {
        t.template = true;
        await publish(project, t);
      }
      break;
    case "enter":
      await open(project, t.open ? "" : e.widget === LIST ? e.row ?? "" : "");
      break;
    case "n":
    case "p":
      if (!t.template && at >= 0 && t.files.length > 0) {
        const step = e.key === "n" ? 1 : -1;
        await open(project, t.files[(at + step + t.files.length) % t.files.length].path);
      }
      break;
  }
});

// A note edited, made or deleted from the tab: read the files again (a
// deleted note's viewer goes back to the list).
$.on("action.done", async (e) => {
  const project = e.target?.project;
  if (!project || e.action.indexOf("memory.") !== 0 || !tabs.has(project)) return;
  const t = tab(project);
  if (e.action === "memory.new") t.template = false;
  await load(project, t);
  await publish(project, t);
});

$.on("ui.click", async (e) => {
  const project = e.target?.project;
  if (e.view !== VIEW || !project || e.widget !== LIST || !e.row) return;
  await open(project, e.row);
});

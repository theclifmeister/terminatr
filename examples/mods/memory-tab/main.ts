// The project popup's Memory tab (mods design §11, proof 2): the
// project's memory files (CONTEXT.md, MEMORY.md and the notes in
// memory/) as a list, and a text viewer for the one opened, read-only.
// The core keeps the cursor, the filter and the scroll; only enter, n, p
// and clicks on a row reach the mod. It draws when the tab is shown
// (view.opened) and reads the files again then, so the tab never shows
// older memory than when it was opened.
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

// What a project's tab shows: the files, and the one open in the viewer
// ("" for the list).
interface Tab {
  files: TM.MemoryFile[];
  open: string;
  text: string;
  error: string;
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

// tree is the tab's widget tree: the list, or the open file.
function tree(t: Tab): TM.Widget {
  if (t.error) {
    return { kind: "text", text: `error: ${t.error}`, role: "bad" };
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
            spans: [{ text: f.title }, { text: `  ${f.path} · ${size(f.bytes)}`, role: "faint" }],
          })),
        },
        { kind: "text", text: "Read-only: the coordinator keeps these; every thread is told them.", role: "faint" },
        { kind: "keys", keys: [{ key: "enter", label: "read" }] },
      ],
    };
  }
  const body = blocks(t.text);
  return {
    kind: "stack",
    children: [
      { kind: "text", lines: [[{ text: file.title, role: "accent" }, { text: `  ${file.path}`, role: "faint" }]] },
      ...(body.length > 0 ? body : [{ kind: "text", text: "empty", role: "faint" }]),
      {
        kind: "keys",
        keys: [
          { key: "enter", label: "files" },
          { key: "n", label: "next" },
          { key: "p", label: "previous" },
        ],
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
    await $.ui.view.set(VIEW, tree(t), { project });
  } catch (e) {
    const why = String((e as Error)?.message ?? e);
    $.log(`memory-tab: ${project}: ${why}`);
    await $.ui.view.set(VIEW, { kind: "text", text: `can't draw ${t.open || "the files"}: ${why}`, role: "bad" }, { project });
  }
}

function tab(project: string): Tab {
  let t = tabs.get(project);
  if (!t) {
    t = { files: [], open: "", text: "", error: "" };
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
  await load(project, t);
  await publish(project, t);
}

$.on("ui.key", async (e) => {
  const project = e.target?.project;
  if (e.view !== VIEW || !project) return;
  const t = tab(project);
  const at = t.files.findIndex((f) => f.path === t.open);
  switch (e.key) {
    case "enter":
      await open(project, t.open ? "" : e.widget === LIST ? e.row ?? "" : "");
      break;
    case "n":
    case "p":
      if (at >= 0 && t.files.length > 0) {
        const step = e.key === "n" ? 1 : -1;
        await open(project, t.files[(at + step + t.files.length) % t.files.length].path);
      }
      break;
  }
});

$.on("ui.click", async (e) => {
  const project = e.target?.project;
  if (e.view !== VIEW || !project || e.widget !== LIST || !e.row) return;
  await open(project, e.row);
});

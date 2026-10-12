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

// Limits of the widget set (internal/widget): one string's bytes, a text
// widget's lines, and the widgets of a tree, with room for the head and
// the keys.
const MAX_TEXT = 4000;
const MAX_LINES = 400;
const MAX_BLOCKS = 56;

// What a project's tab shows: the files, and the one open in the viewer
// ("" for the list).
interface Tab {
  files: TM.MemoryFile[];
  open: string;
  text: string;
  error: string;
}

const tabs = new Map<string, Tab>();

// narrow keeps text the widget set accepts: it refuses a glyph wider than
// one cell (an emoji, a CJK character), whose width differs between
// terminals. Such a glyph shows as "?". strict keeps printable ASCII,
// Latin and the common symbol blocks only, for when a wide glyph slipped
// past the ranges below.
function narrow(s: string, strict = false): string {
  let out = "";
  for (const ch of s) {
    const c = ch.codePointAt(0) ?? 0;
    if (strict ? !safe(c) : wide(c)) out += "?";
    else if (c === 0xfe0f || c === 0x200d) continue; // emoji presentation, joiner
    else out += ch;
  }
  return out;
}

function wide(c: number): boolean {
  return (
    (c >= 0x1100 && c <= 0x115f) ||
    c === 0x231a || c === 0x231b || c === 0x2329 || c === 0x232a ||
    (c >= 0x23e9 && c <= 0x23ec) || c === 0x23f0 || c === 0x23f3 ||
    c === 0x25fd || c === 0x25fe ||
    (c >= 0x2614 && c <= 0x2615) || (c >= 0x2648 && c <= 0x2653) ||
    c === 0x267f || c === 0x2693 || c === 0x26a1 || c === 0x26aa || c === 0x26ab ||
    c === 0x26bd || c === 0x26be || c === 0x26c4 || c === 0x26c5 || c === 0x26ce ||
    c === 0x26d4 || c === 0x26ea || c === 0x26f2 || c === 0x26f3 || c === 0x26f5 ||
    c === 0x26fa || c === 0x26fd || c === 0x2705 || c === 0x270a || c === 0x270b ||
    c === 0x2728 || c === 0x274c || c === 0x274e || (c >= 0x2753 && c <= 0x2755) ||
    c === 0x2757 || (c >= 0x2795 && c <= 0x2797) || c === 0x27b0 || c === 0x27bf ||
    c === 0x2b1b || c === 0x2b1c || c === 0x2b50 || c === 0x2b55 ||
    (c >= 0x2e80 && c <= 0x303e) || (c >= 0x3041 && c <= 0x33ff) ||
    (c >= 0x3400 && c <= 0x4dbf) || (c >= 0x4e00 && c <= 0x9fff) ||
    (c >= 0xa000 && c <= 0xa4cf) || (c >= 0xa960 && c <= 0xa97f) ||
    (c >= 0xac00 && c <= 0xd7a3) || (c >= 0xf900 && c <= 0xfaff) ||
    (c >= 0xfe10 && c <= 0xfe19) || (c >= 0xfe30 && c <= 0xfe6f) ||
    (c >= 0xff00 && c <= 0xff60) || (c >= 0xffe0 && c <= 0xffe6) ||
    (c >= 0x16fe0 && c <= 0x18aff) || (c >= 0x1b000 && c <= 0x1b2ff) ||
    (c >= 0x1f000 && c <= 0x1faff) || (c >= 0x20000 && c <= 0x3fffd)
  );
}

function safe(c: number): boolean {
  return (
    c === 0x0a || (c >= 0x20 && c <= 0x7e) || (c >= 0xa0 && c <= 0x24f) ||
    (c >= 0x2010 && c <= 0x2027) || (c >= 0x2190 && c <= 0x21ff) ||
    (c >= 0x2500 && c <= 0x25ff) || (c >= 0x2713 && c <= 0x2718)
  );
}

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
// are cut to the widget set's limits; past MAX_BLOCKS widgets the file
// is cut short, and says so.
function blocks(md: string): TM.Widget[] {
  const out: TM.Widget[] = [];
  let run: string[] = [];
  let runBytes = 0;
  let first = true;
  const flush = () => {
    while (run.length > 0 && run[run.length - 1] === "") run.pop();
    while (run.length > 0 && run[0] === "") run.shift();
    if (run.length > 0) out.push({ kind: "text", text: run.join("\n") });
    run = [];
    runBytes = 0;
  };
  for (let line of md.replace(/\r\n?/g, "\n").split("\n")) {
    if (out.length >= MAX_BLOCKS) break;
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
    // on a code point (1000 UTF-16 units are at most 3000 bytes).
    while (bytes(line) > MAX_TEXT && out.length < MAX_BLOCKS) {
      flush();
      const hi = line.charCodeAt(999);
      const end = hi >= 0xd800 && hi <= 0xdbff ? 999 : 1000;
      const cut = line.slice(0, end);
      out.push({ kind: "text", text: cut });
      line = line.slice(cut.length);
    }
    const n = bytes(line) + 1;
    if (runBytes + n > MAX_TEXT || run.length >= MAX_LINES) flush();
    run.push(line);
    runBytes += n;
  }
  const cut = out.length >= MAX_BLOCKS;
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

// publish draws project's tab. A tree the widget set refuses for a wide
// glyph the ranges missed is drawn again with strict text.
async function publish(project: string, t: Tab) {
  const w = tree(t);
  try {
    await $.ui.view.set(VIEW, JSON.parse(narrow(JSON.stringify(w))), { project });
  } catch (e) {
    $.log(`memory-tab: ${project}: ${String((e as Error)?.message ?? e)}; drawing it in plain text`);
    await $.ui.view.set(VIEW, JSON.parse(narrow(JSON.stringify(w), true)), { project });
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

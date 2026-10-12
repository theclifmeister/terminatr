#!/usr/bin/env python3
"""Render tm's golden screen captures as coloured HTML terminal frames.

The screens on terminatr.dev are real tm screens: the end-to-end tests'
golden captures (internal/e2e/testdata/golden). This fills in their masks
(<duration>, <session>, ...) with neutral values, keeps every column where
tm put it, colours the glyphs and writes each frame into site/index.html
between its <!-- screen:NAME --> and <!-- /screen:NAME --> markers, and
the README's images, as SVG, into docs/images/.

    python3 site/tools/screens.py        # from the repository root

Run it again after a golden changes. The site itself has no build step:
the output is committed.
"""

import html
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
GOLDEN = ROOT / "internal/e2e/testdata/golden"
INDEX = ROOT / "site/index.html"

# Masked values. A value is padded or trimmed so that the
# border after it keeps the column it has on the line above.
MASKS = {
    "<duration>": "4m",
    "<session>": "s-3",
    "<tmp>": "~/src/demo",
    "<host>": "this machine",
    "<worktree>": "…/worktrees/demo/t-0001-fix-the-login",
}

SCREENS = {
    "split": "dashboard-split.txt",
    "thread": "dashboard-thread.txt",
    "needs": "dashboard-needs-you.txt",
    "tasks": "popup-tasks.txt",
    "overview": "popup-overview.txt",
    "inbox": "dashboard-inbox.txt",
    "sidebar": "sidebar-unicode.txt",
    "coordinator": "infopanel-coordinator",
    "threadpanel": "infopanel-thread",
    "mods-dashboard": "mods-dashboard.txt",
    "mods-standup": "mods-standup.txt",
    "mods-menu": "mods-menu.txt",
    "mods-settings": "mods-settings.txt",
    "mods-memory": "mods-memory.txt",
    "session": "session-sidebar.txt",
}

# The README's images: the same screens as SVG terminal frames, written
# to docs/images/<name>.svg, with the title they carry for screen readers.
IMAGES = {
    "dashboard": ("dashboard-split.txt", "tm dashboard"),
    "session": ("session-sidebar.txt", "tm · a thread's session"),
    "tasks": ("popup-tasks.txt", "tm · project popup, Tasks tab"),
    "mods": ("mods-dashboard.txt", "tm · dashboard with mods"),
    "mods-settings": ("mods-settings.txt", "tm · Settings, Mods tab"),
}
IMAGE_DIR = ROOT / "docs/images"

BORDERS = "│╭╮╰╯"


def border_cols(line):
    return [i for i, c in enumerate(line) if c in BORDERS]


def unmask(lines):
    out = []
    for n, line in enumerate(lines):
        if "<" not in line:
            out.append(line)
            continue
        ref = border_cols(out[-1]) if out else []
        for mask, value in MASKS.items():
            while mask in line:
                at = line.index(mask)
                after = line[at + len(mask):]
                if after.startswith("  ") and not any(c in BORDERS for c in after):
                    # A column follows and no border to align on: keep its place.
                    value = value.ljust(len(mask))
                line = line[:at] + value + line[at + len(mask):]
                # The first border after the value goes back to its column.
                nxt = [i for i, c in enumerate(line) if c in BORDERS and i > at]
                want = [c for c in ref if c > at]
                if nxt and want:
                    gap = want[0] - nxt[0]
                    end = at + len(value)
                    if gap > 0:
                        line = line[:end] + " " * gap + line[end:]
                    elif gap < 0:
                        # Take the surplus from the spaces after the value,
                        # else from the text before the border.
                        cut = -gap
                        tail = line[end:nxt[0]]
                        spaces = len(tail) - len(tail.lstrip(" "))
                        take = min(cut, max(spaces - 1, 0))
                        line = line[:end] + line[end + take:]
                        cut -= take
                        if cut:
                            b = nxt[0] - take
                            line = line[:b - cut - 1] + "…" + line[b:]
        out.append(line)
    return out


# Token colouring, applied to escaped text. Order matters: the first rule
# that matches a stretch of text wins.
RULES = [
    (r"● server ok", "ok"),
    (r"[╭╮╰╯│─└┘├┌┐]+", "dim"),
    (r"\b(PROJECTS|NEEDS YOU|IN MOTION|ON DECK|OTHER SESSIONS|THREADS|STEPS|THREAD|REPOSITORIES)\b", "head"),
    (r"tm dashboard", "title"),
    (r"mod: [a-z-]+", "mod"),
    (r"◆ review", "warn"),
    (r"▲ blocked", "bad"),
    (r"⚑", "bad"),
    (r"● started", "ok"),
    (r"● (?=[A-Z])", "ok"),
    (r"☐", "idle"),
    (r"▁+", "accent"),
    (r"\$\d+\.\d\d", "accent"),
    (r"▷ running", "ok"),
    (r"○ (idle|ready)", "idle"),
    (r"✓", "ok"),
    (r"[◐▸]", "warn"),
    (r"▰+", "accent"),
    (r"▱+", "dim"),
    (r"■", "accent"),
    (r"PR #\d+|https://github\.com/\S+", "link"),
    (r"\bT\d+\b|\bt-\d{4}\b(?!-)", "task"),
    (r"report new|\d inbox\b|inbox 1", "warn"),
    (r"\d+%", "accent"),
    (r"(≡ menu|prefix\+(?:tab\b|[a-zA-Z?](?![a-zA-Z]))|\benter\b|\besc\b|← →)", "key"),
]
PATTERN = re.compile("|".join(f"(?P<g{i}>{p})" for i, (p, _) in enumerate(RULES)))


def colour(line):
    parts, pos = [], 0
    for m in PATTERN.finditer(line):
        parts.append(html.escape(line[pos:m.start()], quote=False))
        cls = RULES[int(m.lastgroup[1:])][1]
        parts.append(f'<span class="{cls}">{html.escape(m.group(0), quote=False)}</span>')
        pos = m.end()
    parts.append(html.escape(line[pos:], quote=False))
    return "".join(parts)


def screen_lines(name):
    raw = (GOLDEN / name).read_text().rstrip("\n").split("\n")
    lines = unmask(raw)
    leak = [l for l in lines if re.search(r"<\w+>|/Users/|/home/", l)]
    if leak:
        sys.exit(f"{name}: left unmasked: {leak!r}")
    return lines


def render(name):
    lines = screen_lines(name)
    cols = max(len(l) for l in lines)
    body = "\n".join(colour(l.rstrip()) for l in lines)
    return f'<pre class="screen" style="--cols:{cols}" data-golden="{name}">{body}</pre>'


# SVG frames. Every coloured run, and every stretch of text after two
# spaces, starts at its own column, so the borders stay in line whatever
# monospace font the viewer falls back to.
SVG_COLOURS = {
    None: "#c8d0f0", "dim": "#4e5578", "head": "#bb9af7", "title": "#c8d0f0",
    "ok": "#9ece6a", "warn": "#e0af68", "bad": "#f7768e", "idle": "#8189ad",
    "accent": "#7dcfff", "link": "#7aa2f7", "task": "#7dcfff", "key": "#7aa2f7",
    "mod": "#bb9af7",
}
SVG_BOLD = {"head", "title", "bad", "task"}
CW, LH, FS, PAD, BAR = 8.4, 17, 14, 16, 34


def runs(line):
    out, pos = [], 0
    for m in PATTERN.finditer(line):
        out.append((pos, line[pos:m.start()], None))
        out.append((m.start(), m.group(0), RULES[int(m.lastgroup[1:])][1]))
        pos = m.end()
    out.append((pos, line[pos:], None))
    for at, text, cls in out:
        for m in re.finditer(r"\S+(?: \S+)*", text):
            yield at + m.start(), m.group(0), cls


def svg(name, title):
    lines = [l.rstrip() for l in screen_lines(name)]
    cols = max(len(l) for l in lines)
    w = round(cols * CW + 2 * PAD)
    h = round(len(lines) * LH + 2 * PAD + BAR)
    out = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" role="img" aria-labelledby="t">',
        f"<title id=\"t\">{html.escape(title)}</title>",
        "<style>text{font:%dpx ui-monospace,SFMono-Regular,Menlo,Consolas,'DejaVu Sans Mono','Liberation Mono',monospace;white-space:pre}</style>" % FS,
        f'<rect width="{w}" height="{h}" rx="10" fill="#11131c"/>',
        f'<rect width="{w}" height="{BAR}" rx="10" fill="#161925"/><rect y="{BAR - 10}" width="{w}" height="10" fill="#161925"/>',
        f'<line x1="0" y1="{BAR}" x2="{w}" y2="{BAR}" stroke="#262a3d"/>',
        '<circle cx="18" cy="17" r="5.5" fill="#f7768e"/><circle cx="36" cy="17" r="5.5" fill="#e0af68"/><circle cx="54" cy="17" r="5.5" fill="#9ece6a"/>',
        f'<text x="72" y="21" fill="#8189ad" style="font-size:12px">{html.escape(title)}</text>',
    ]
    for n, line in enumerate(lines):
        y = round(BAR + PAD + n * LH + FS * 0.8, 1)
        for col, text, cls in runs(line):
            bold = ' font-weight="700"' if cls in SVG_BOLD else ""
            fit = f' textLength="{round(len(text) * CW, 1)}" lengthAdjust="spacingAndGlyphs"' if len(text) > 1 else ""
            out.append(f'<text x="{round(PAD + col * CW, 1)}" y="{y}" fill="{SVG_COLOURS[cls]}"{bold}{fit}>{html.escape(text, quote=False)}</text>')
    out.append("</svg>")
    return "\n".join(out) + "\n"


def main():
    page = INDEX.read_text()
    for key, name in SCREENS.items():
        start, end = f"<!-- screen:{key} -->", f"<!-- /screen:{key} -->"
        if start not in page:
            continue
        a = page.index(start) + len(start)
        b = page.index(end)
        page = page[:a] + render(name) + page[b:]
    INDEX.write_text(page)
    IMAGE_DIR.mkdir(parents=True, exist_ok=True)
    for key, (name, title) in IMAGES.items():
        (IMAGE_DIR / f"{key}.svg").write_text(svg(name, title))


if __name__ == "__main__":
    main()

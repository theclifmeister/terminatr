# Example mods

One small, working mod for each place a mod can show up in terminatr,
plus an agent tool and a code host: to see each surface working, and to get ideas to
build and share. They are samples, not defaults. Nothing here is loaded
or turned on until you install it, and installing goes through the
usual consent screen. Three of them, `pr-ci-column`, `memory-tab` and
`tasks-tab`, are also the sources of mods built into tm.

Each installs with one line, from its folder at a release tag:

```sh
tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/nord
```

The tag pins the example as that release made it, for that release's
mod API (main's may already need the next one); `tm mod update` moves
it on to the newest release tag that changed its folder, and `tm mod
examples` prints these lines for your tm's own release. Then turn it
on in the Mods tab (`,` then `3`), or with `tm mod enable <id>`. `e` in the Mods tab lists them too and installs the selected one with `i`, after the same consent; `tm mod examples` prints this list.

| # | Mod | Surface | Asks for | Install |
|---|-----|---------|----------|---------|
| 1 | [nord](nord) | theme and icon set (data) | nothing (safe) | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/nord` |
| 2 | [vim-keys](vim-keys) | keymap (data) | nothing (safe) | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/vim-keys` |
| 3 | [context-gauge](context-gauge) | a cell in the sidebar and on the dashboard, and a card whose rows bind `session.clear` and `session.attach` | reads your projects, draws its own panels, offers you keys that clear a session's conversation | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/context-gauge` |
| 4 | [cost-card](cost-card) | a dashboard card | reads your projects, draws its own panels | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/cost-card` |
| 5 | [notify-me](notify-me) | OS notifications with core-action buttons, a question's options among them | reads your projects, shows notifications, offers you buttons that answer an agent's question | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/notify-me` |
| 6 | [thread-timeline](thread-timeline) | a full view (≡ menu) | reads your projects, draws its own panels | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/thread-timeline` |
| 7 | [pr-ci-column](pr-ci-column) | a sidebar column (built in) | reads your projects | copy it under another id first, see below |
| 8 | [memory-tab](memory-tab) | a project-popup tab (built in) | reads your projects, draws its own panels | copy it under another id first, see below |
| + | [tasks-tab](tasks-tab) | the popup's Tasks tab, its rows bound to tm's task actions (built in) | reads your projects, draws its own panels | copy it under another id first, see below |
| + | [memory-search](memory-search) | an agent tool, `memory-search_find` | reads your projects, offers tools to agents | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/memory-search` |
| + | [standup](standup) | a command: the key `S`, a ≡ menu entry and `tm mod run standup show` | reads your projects, draws its own panels | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/standup` |
| + | [report-glance](report-glance) | panel sections: one at the bottom of the details panel, one in the coordinator's info panel | reads your projects, draws its own panels | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/report-glance` |
| + | [gitlab](gitlab) | a code host: GitLab merge requests as tm's PRs | runs glab | `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.21.0/examples/mods/gitlab` |

**Claude needs the Agent mod for three of them.** `memory-search`'s
tool, and the context and use that `context-gauge` and `cost-card` read
from `session.state`, reach tm from a Claude session through tm's Agent
mod, which is early access and off by default. Turn it on in Settings
(`,`, *Agent mod*) or with `[agent_mod] enabled = true` in
`config.toml`; it applies to sessions started after. With it off these
three load but stay empty for Claude, and their consent says so.

`pr-ci-column`, `memory-tab` and `tasks-tab` already ship inside tm,
and `tm mod install` refuses a built-in mod's id. To change one, copy it, give the
copy its own id and install that:

```sh
git clone --depth 1 --branch v0.21.0 https://github.com/theclifmeister/terminatr
cp -R terminatr/examples/mods/pr-ci-column my-pr-ci-column
# set id = "my-pr-ci-column" in my-pr-ci-column/mod.toml
tm mod install my-pr-ci-column
```

Turn the built-in one off in the Mods tab so the two don't both draw.
Your copy is a mod of its own: a tab after tm's tabs, or a column of its
own. The built-in ones are the only drawing of the Tasks tab, the Memory
tab and the PR/CI column, so with one off its tab says the mod is off
and how to turn it on (`,` then `3`), and the column takes no room.

## How they are kept working

Each code mod has fixtures in its `tests/` folder: events to send, what
the fake broker answers, and the calls the mod must make. `tm mod test
<folder>` replays them, and CI runs every example's fixtures
(`internal/modtest`'s `TestExamples`) and checks every example loads as
the console reads it (`internal/tui`'s `TestExampleMods`), and type-checks
each `main.ts` with `tsc --strict` against this tm's `tm.d.ts`. A change
to the mod API that breaks an example fails the build.

To start your own, `tm mod new <id> --kind column` (or `hook`, `theme`,
`icons`, `keymap`, `rules`) writes a scaffold with the types and a
fixture; `tm mod dev <folder>` runs it live and restarts it on each save.

## Notes on each

- **nord** sets every theme role for a dark and a light terminal; each
  colour has an ANSI fallback and a truecolor choice. Its icon set gives
  only the glyphs it changes over the `unicode` base. A glyph two cells
  wide, or an emoji, makes the set refuse to load, and the console keeps
  its built-in icons and says why.
- **vim-keys** moves actions by their stable ids. It can't take the
  escape hatches (`esc`, `ctrl+c`, the prefix and its commands), and a
  key two actions want is refused.
- **context-gauge** reads `session.state`. The card's rows only *name*
  `session.clear` (`C`) and `session.attach` (`enter`). The core shows the
  keys and checks the session still runs; `C` asks first, in tm's words,
  then has the agent start a fresh conversation once it is idle (the
  project's files keep its state), and `enter` attaches. The mod hears
  `action.done`. `session.clear` needs `actions = ["session.clear"]`
  under `[permissions]` (mod API 1.3), which the consent shows.
- **cost-card** uses the `tokens` and `cost_usd` that `session.state`
  carries for a thread's session (its thread's totals so far). Days
  count only what was spent while the mod ran: a thread started then
  counts from nothing (`thread.started`), its first turn included; one
  already running when the mod first hears of it counts from there.
- **notify-me** buttons are core action ids. A question menu open on a
  thread or a coordinator (`session.question`, mod API 1.3) gets its
  first three options as `question.answer` buttons; a review gets
  `task.accept`, `task.send-back` and `task.show`; a question in a
  thread's status gets `task.show`. Pressing one opens tm's own
  confirmation for the question as the session asks it now, or the task
  as the board has it now. The notification itself answers, accepts or
  sends back nothing. `question.answer` needs `actions =
  ["question.answer"]` under `[permissions]`, which the consent shows.
- **thread-timeline** records state changes in its own store, so its
  history starts when it is installed. The view keeps the header,
  sidebar and NEEDS YOU in sight, and `esc` returns.
- **memory-search** answers an agent's call with the matching lines of
  the project's memory. The agent's input is data; the consent screen
  shows the tool's description and input in full.
- **gitlab** serves `code_host = "gitlab"`: put that in a project's
  `PROJECT.md` front matter, and log glab in (`glab auth login`). tm then
  asks it about that project's merge requests as it asks gh about pull
  requests (state, pipeline, approvals, the failing job's log), and
  `tm thread merge` has it merge the head tm checked, with a merge
  commit. tm still decides what is safe: it finds merge commits in git
  itself, never deletes a branch or worktree on the mod's word alone
  (git must find the commits on the remote), and makes the merge
  checks itself. `tm doctor` shows which mod serves the kind and glab's
  login.
- **tasks-tab** is the project popup's Tasks tab, which tm draws with
  it. Its rows only *name* `task.show`, `task.delegate`, `task.accept`,
  `task.send-back` and `task.coordinator`, and its keys widget names
  `task.new` for the whole tab (`+`), since no row stands for a new
  task (mod API 1.4): tm shows the keys, checks the live board, asks
  and runs them. Section heads and the selected task's steps are
  widget rows (`head`, `more`), so the cursor never waits on the mod.
  It lists the tasks again on `tasks.changed`, so a title edited or an
  ask handled shows at once. Opened to show a task picked elsewhere
  (`view.opened` names it), it lists that task's row, done tasks
  included, and tm puts the cursor on it.
- **standup** declares a command in `[[provides.commands]]`. `S` on the
  dashboard or its ≡ menu entry runs it with the project shown, and
  being a human input it may open the mod's full view. `tm mod run
  standup show --project <slug>` can't open a view, so the mod says the
  same in one line as a toast. A key the core (or a mod enabled
  earlier) has stays theirs; the Mods tab then says key conflict.
- **report-glance** declares a `details.section` and a
  `coordinator.section`. When the details panel shows a thread or a
  task, the mod gets `view.opened` naming it, publishes that row's
  latest report for it, and clears it on `view.closed`. The
  coordinator's info panel gets the threads that reported. A row the mod
  draws nothing for (a project's own) has no section. Sections show
  only: their keys stay the panel's.

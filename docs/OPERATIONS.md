# Operating terminatr

How to install, run, check, upgrade and remove `tm`. The design behind all of this is in [SPEC.md](SPEC.md) §3 (server) and §5 (files).

## Install

On macOS 13+ or Linux with glibc 2.28+, on arm64 or x86_64.

**Homebrew** (macOS and Linux):

```sh
brew tap theclifmeister/terminatr https://github.com/theclifmeister/terminatr
brew trust theclifmeister/terminatr
brew install terminatr
tm doctor
```

The two-argument `brew tap` is needed because the formula lives in this repository (`Formula/terminatr.rb`), not in a `homebrew-terminatr` one. Newer Homebrew refuses to install from a tap it doesn't trust, hence `brew trust theclifmeister/terminatr` once after tapping (it trusts the tap, so the formula follows; `brew trust --formula` wants the full `theclifmeister/terminatr/terminatr`). `brew upgrade terminatr` (or `tm update`, which suggests it) picks up new releases.

**Direct download.** Release archives are on the [releases page](https://github.com/theclifmeister/terminatr/releases). Each holds one static `tm` binary (`tm.exe` in the Windows zips; plus this file, the README and the licence); `checksums.txt` lists their sha256. On Linux and macOS, to install the latest into `~/.local/bin`:

```sh
curl -fsSL https://github.com/theclifmeister/terminatr/releases/latest/download/install.sh | sh
tm doctor
```

`scripts/install.sh` (POSIX sh) picks the tarball for your OS and CPU, checks it against `checksums.txt`, replaces `tm` in `~/.local/bin` in one rename (also while `tm` runs) and says if that folder isn't on your PATH. To pass options, use `sh -s --`: `--version v0.5.0`, `--dir <folder>`.

`tm update` keeps a direct install up to date.

- **macOS:** the binary links only system libraries (libSystem, libresolv and, depending on the Go release, CoreFoundation). It is signed with a Developer ID (hardened runtime) and notarised. A bare binary can't carry a stapled ticket, so Gatekeeper checks the notarisation online the first time it meets a quarantined copy (one a browser downloaded); with no network that first run is refused. curl and Homebrew set no quarantine flag, so they never ask.
- **Linux:** glibc 2.28 or later (Debian 10, Ubuntu 18.10, RHEL 8 and newer); musl is not supported.
- **Windows (amd64, arm64):** `tm_windows_<arch>.zip` holds `tm.exe` with Microsoft's `conpty.dll` and `OpenConsole.exe` beside it (keep the three together; they give every Windows version the same console behaviour). Windows support works and is early. `tm.exe` is not code-signed, so a browser download shows SmartScreen's "unknown publisher" (More info, then Run anyway); `scripts/install.ps1` unblocks the files, so it doesn't. Codex threads need no sandbox setup on Windows. The first start after an install or upgrade can be slow while Defender checks the new exe. To install the latest in PowerShell, with no admin rights:

  ```powershell
  irm https://github.com/theclifmeister/terminatr/releases/latest/download/install.ps1 | iex
  ```

  `scripts/install.ps1` downloads the zip for your PC, checks it against `checksums.txt`, puts the three files in `%LOCALAPPDATA%\Programs\terminatr` and adds that folder to your user PATH. Save it to pass options: `-Version v0.5.0`, `-Dir <folder>`, `-Service` (also start at login), `-NoPath`. Running it again upgrades, also while `tm` runs: a file in use can be renamed but not overwritten, so the old one moves aside (`tm.exe.old-<time>`) and is deleted by the next install or `tm update`. If a file can't be replaced it says so and changes nothing.
- **Runtime:** git, and the agents you use (Claude Code, Codex; see [CODEX.md](CODEX.md)). For threads' sandbox Claude needs `bwrap` and `socat` on Linux. `tm doctor` checks all of these.

To build from source instead, see the [Contributing](../CONTRIBUTING.md#build). How releases are made: [Releasing](#releasing).

## Using tm

Run `tm`. It starts the background server if needed and opens the dashboard. (From a source checkout, `make run` builds `bin/tm` and does the same.) Press `+` (or `n` while there is none) to create a project, then `enter` on it to start its coordinator (Claude Code), which attaches right away, with a status bar at the bottom: where you are and how far it got on the left, the help, the dashboard and the `≡` menu on the right. You talk to the coordinator; it runs the threads, whose panes you can open too (`enter` on a thread) to read along or answer a prompt; typing into one tells its coordinator that you stepped in. Ctrl+B is the prefix key, as in tmux (the UI writes it `prefix+<key>`, e.g. `prefix+d`): Ctrl+B then `d` brings you back to the dashboard (then `]` or `[` switches project, `I` opens the inbox), where a session that waits for you (a permission dialog, say) shows under NEEDS YOU, the current project's (a click on PROJECTS, the sidebar's header, lists every project's); `enter` attaches again. `?` lists the keys, Ctrl+B then `q` quits (a prefix command means the same everywhere; popups close with `esc`). Sessions keep running after you quit, and after you close the terminal:

```sh
tm                                 # the dashboard again, or the session it showed
tm --own                           # a console of its own, which no other follows
tm attach s-1                      # attach again, from any terminal, at any size
tm attach                          # the newest session
tm session list                    # sessions in the server
tm session keys s-1 --enter 'ls'   # type into one
tm session read s-1                # print its screen
tm session stop s-1
tm server status | stop
```

`make run RUN_ARGS=top` also starts a session running `top`. Consoles of different sizes show the same screen: the one you type in, or resize, sizes the panes, and a larger one shows the frame padded, a smaller one cropped. A new pane fills the first console that shows it; after that, attaching and watching never resize anything. Shift+PgUp/PgDn scroll back through a shell's output; full-screen programs get the mouse wheel. The prefix key can be changed in the settings (`,` on the dashboard: `enter` on Prefix key, then press the new one, e.g. Ctrl+A); do that when you run `tm` inside tmux, which takes Ctrl+B itself. Ctrl+B twice sends Ctrl+B to the program (Claude Code uses it to background a running task). The projects sidebar lists each project with its coordinator and threads under it: the active projects first, then a divider, `── ▸ inactive (3) ──`, with the inactive ones folded away under it (`→`, `enter` or a click opens it). `←` on a project folds it to its row alone, which then shows its coordinator's state, and `→` unfolds it; `Z` folds everything, or unfolds everything when all is folded. What you fold stays folded, in every terminal showing the same view and the next time you start `tm`. `P` pins the project under the cursor to the top (`P` again unpins it); pinning only changes the order. Every row ends in the same columns, so they line up: open threads, context use or progress, a thread's PR checks (`☑` pass, `☒` fail, `☐` running), its state, `⌁` remote control, `∥` paused, `⊘` inactive and `⚑` when something there needs you (`?` lists the symbols). It is 36 columns wide by default and resizable: drag its border, or `{` `}` (Ctrl+B then `{` `}` in a session); `B` makes it a slim strip of projects (state, name, `⚑`). It works from the keyboard too: `tab` on the dashboard (Ctrl+B then Tab in a session) moves the keyboard to it and the arrows move through the tree. Where the cursor rests, the screen shows what is under it after a moment: a project its dashboard, a coordinator or a thread its live pane; this starts nothing, not even an inactive project, and the keyboard stays in the sidebar. `enter` (or `→`) goes into it: the keyboard moves to the pane or the dashboard's list, and a coordinator that isn't running starts. `→` on a folded project or the divider unfolds it first, `←` folds it again or goes up from a row to its project, `?` is the help and `esc` goes back. Below the tree, two lines describe the highlighted row (its full title, its state, its PR's number and checks), and a key bar lists the keys that work on that row. The server's version (and this `tm`'s when it differs) is in the help (`?`) and at the top of the `≡` menu; the sidebar shows a hint when `tm server restart` or an upgrade is due; the settings (`,`) start with an About line that also says whether the daily update check is on. The Icons setting (`,`) picks the sidebar's and the lists' glyphs: Nerd Font icons, plain Unicode or ASCII; auto uses Nerd Font icons in Ghostty and Unicode elsewhere. Everything works with the mouse too: a click selects, a double-click opens, a right-click on a row or the sidebar opens a menu of its actions, the footer's hints and the status bar's buttons (`prefix+? help`, `prefix+d dashboard`, `≡` menu) are buttons, popups take clicks (a click outside closes them, as `esc` does), the wheel scrolls, and the dividers drag; inside a pane, programs that use the mouse (Claude Code does) still get it; a drag over a shell selects text and copies it on release, and Claude Code's own copy is passed on, both to the clipboard of the terminal you sit at (OSC 52, so it works over SSH; tmux needs `set -g set-clipboard on`); Shift-drag (Option-drag in some macOS terminals) still selects natively. When the dashboard has 120 columns or more beside the sidebar the dashboard shows the selected row's details beside the list (`<` `>` resize it, `|` hides it). `A` (Ctrl+B then `A`, or `a`, in a session) opens the project popup: its overview with the repositories, its inbox (read-only), its tasks (the selected task's steps only, the newest ten done ones, `m` for all of them; `D`, `A` and `X` ask the coordinator to delegate, accept or send back the selected one; the coordinator changes tasks), its settings, changed in place with `enter` (and `+` / `-` for numbers, such as how many threads may work at once and after how many days a finished thread closes), every key, its memory, and its library (`7`: the files threads attached to reports; `enter` reads one, `X` deletes it, `D` deletes the thread's files, each after `Y`; `tm library list` lists them); `,` has the settings of every project, and its All projects tab the project settings every project follows (a new one too) unless it sets its own: a project's Settings tab marks those `· all projects`, and `X` on one it set itself makes it follow All projects again. In `~/.terminatr/config.toml` they are the `[defaults]` table, with the same keys as `[projects.<slug>]`. The server keeps its state in `~/.terminatr`; set `TERMINATR_HOME` to use somewhere else.

To pick up a coordinator from the Claude desktop or mobile app (Claude Code's Remote Control), turn on Remote control in the project popup's Settings tab (`A`, then `4`), or set `coordinator_remote_control = true` under `[projects.<slug>]` in `~/.terminatr/config.toml`: the coordinator then starts with remote control, listed under the project's slug. Ctrl+B then `r` on the coordinator's pane (or with its project selected on the dashboard), or `tm project remote on|off <slug>`, turns it on or off in the running session, and the conversation continues; that lasts until the coordinator is started anew. The sidebar shows `⌁` on the coordinator's row and the status bar says `remote control on` while it is.

Long-running coordinators fill their context window. Turn on Auto-clear coordinator in the Settings tab (or `auto_clear = true` under `[projects.<slug>]`) and tm sends `/clear` to the coordinator once its context reaches the hint (`[ui] context_hint`, 40% by default). It only does so while the coordinator is idle and nothing waits on it: no inbox item, no queued prompt, no open question and no blocked thread. Before the clear it asks the coordinator to save what it learned and hasn't written down, and clears at the next idle moment after that. The project's state lives in files, and the coordinator keeps its open questions to you in CONTEXT.md under Needs you. Each ask and clear is journaled. It is off by default.

To clear a coordinator yourself, don't type `/clear`: that skips the save. Use the save and clear instead, which takes the same steps whatever the setting says: `tm clear` (in the project's folder, or `--project <slug>`), `prefix+C` (no question; from a session, the dashboard or a popup, for that project's coordinator; the sidebar row and the info panel offer it, and the panel's line is clickable, once its context is past the hint), or `/saveclear` and the Save and clear button in a Claude coordinator's pane. A coordinator whose agent has no clear prompt is refused with a message saying so; a Codex coordinator has no `/saveclear`, so use `tm clear` or `prefix+C` there. Plain `c` no longer clears it, so a stray keypress can't.

If your company has a branch name policy, set Branch names in the Settings tab (for one project in its popup, or for all projects in the Settings tab of `,`), or `branch_name = "…"` under `[projects.<slug>]` or `[defaults]`; a project's own value wins over `[defaults]`. The template may use `{project}`, `{id}` (the thread's id, `t-0468`), `{task}` (`T497`, or empty for a thread without a task) and `{title}` (a short slug of its title), and must contain `{id}`, so every thread gets its own branch. The default, `tm/{project}/{id}-{title}`, gives the names tm always used. Examples: `feature/{task}-{title}-{id}` gives `feature/T497-fix-login-t-0468`, and `users/jane/{id}` gives `users/jane/t-0468`. An empty `{task}` or `{title}` takes the `-` or `/` beside it along, so a thread without a task gets `feature/fix-login-t-0468`. A template git would refuse as a branch name, one with characters other than letters, digits, `_`, `.`, `/` and `-`, one that could make a name over 100 characters even without a title, or one that would leave a doubled or trailing `-` or `/`, is refused with the reason when you save it and again when a thread starts; tm never changes it for you, but shortens a long title so the name fits. Only new threads use it: threads that exist keep their branches. The setting is yours alone: a mod can't change it. The panel shows the next branch name as you would get it.

Auto mode can refuse the coordinator's PR merge even though your project says the coordinator merges, because its classifier doesn't see that rule. Turn on Coordinator may merge in the Settings tab (or `coordinator_merges = true` under `[projects.<slug>]` or `[defaults]`) and tm adds a permission allow rule for `tm thread merge` to the coordinator's launch settings, from its next launch. The coordinator's rules tell it to merge only that way, never with `gh` or `az` itself. `tm thread merge <thread or T12>` refuses unless the project has `merge = "coordinator"`, the PR is the thread's own (from its branch, in its repo), the PR is open, its checks are green on the current head and the host calls it mergeable; then it merges by your merge method and journals it, and its answer says which method it used. It looks once and answers once; a refusal names the reason (`not-green`, `conflicting`, `not-mergeable`, `merge-not-allowed`, `merge-method-not-allowed`, `no-pr`, `no-code-host`, `not-thread-pr`, `pr-closed`, `merge-failed`). Threads can't run it. It only has an effect with `merge = "coordinator"`; threads never get it and the guard still refuses their merges. It is off by default.

The same goes for other tm commands your settings already let the coordinator run: tm adds allow rules for them to the coordinator only, from its next launch. Always for the read-only ones (`tm context`, `tm inbox list`, `tm task list`/`show`, `tm thread list`/`show`/`read`, `tm library list`, `tm memory find`/`check`, `tm access show`/`explain`); the changes to the project's own access rules it may make (`tm access add`, `rm`, `edit`, `drop`, `undrop`, `set local-ports`) while you let it set access; `tm thread start` and `tm task delegate` with Start threads on auto; `tm thread approve` while the coordinator approves prompts (the default). Commands that need your word in chat (marking a task done, discarding a thread's work, deleting library files, answering a thread's question) and your own (`tm mod`, resetting access, setting protection, escapes, yolo or the coordinator switch) never get one, so auto mode still looks at them. `tm access show` lists them under prompts.

**Merge method.** By default `tm thread merge` makes a regular merge commit. If your repository only allows squash or rebase merges (for example a branch rule that requires linear history, where GitHub answers "Merge commits are not allowed on this repository"), pick another method: Merge method in the Settings tab cycles merge commit, squash and rebase, for this project or for all projects, or set `merge_method = "squash"` (or `"rebase"`, or `"merge"`) under `[projects.<slug>]` or `[defaults]` in `config.toml`. A squash merge's commit title is the pull request's title and number (`fix(merge): … (#12)`; on Azure DevOps `(!12)`), whatever the repository's default squash message setting says, so the title should be a Conventional Commit; GitHub writes the body as it would. A merge commit keeps the host's own message. When the host refuses the method, `tm thread merge` refuses with `merge-method-not-allowed`, names the methods the repository allows where the host says so (GitHub does; Azure DevOps doesn't), and points you at the setting; nothing is merged. Completing tasks on merge, auto-close and the conflict follow-up work with every method: tm reads the merge from the pull request's state, not from a merge commit. Mods can't change this setting.

Each project lets at most `parallel_threads` threads work at once (10 by default). With several projects busy, that can still be more than your machine handles: set Threads across projects in the `,` popup's All projects tab, or `global_parallel_threads = 8` under `[defaults]` in `~/.terminatr/config.toml` (only there; a project's table can't set it), to cap the threads working in all projects together. Idle, done and stopped threads don't count. At the cap a coordinator proposes the thread and starts it once one finishes; tell it to start one anyway and it adds `--over-cap`. Unset or `0` is no such cap. A coordinator refused at either cap waits for a slot: once a thread of any project stops working and frees one, the coordinator refused first gets a `slot-free` inbox item (one coordinator per free slot, so they don't race), and the slot is held for it for ten minutes.

To let two projects' coordinators talk, link them, once: open the Network popup (`N` on the dashboard; a project's Settings tab, **Linked projects**, opens it on that project), select the project and press `L`. Pick the other project, and the link dialog shows the direction (both ways, send only, receive only), ask first and the files they send (ask me, take them, refuse), and says what crosses; nothing is written until `Y`, which sets both projects at once (send only here is receive only there). The link then shows under both projects; `enter` on it opens the dialog again, and `X` unlinks both after a `Y`. Ask first (`K` on the link) makes each message this project's coordinator sends wait for your OK; that switch is each side's own. From a shell, `tm project link terminatr terminatr-mods` does the same (add `send`, `receive` or `off`, and `--ask`). In `~/.terminatr/config.toml` each side keeps its own entry, `[projects.terminatr.links]` with `terminatr-mods = "both"` (or `{ allow = "send", ask = true }`); if you edit one side by hand so the two no longer match, `tm link list` and the Network popup say half-linked, `tm doctor` warns, and setting the link again makes it whole. A coordinator then writes with `tm link send terminatr-mods "…"`; the other gets a `link-message` inbox item (never the text) and reads it with `tm link read M-12`, framed as data. Messages are plain text of at most 8 KB with up to three library files of 1 MB attached (`--attach t-0042/notes.md`); each direction takes 10 an hour (3 at once). A message over that rate isn't refused: it waits queued in the sender's folder and goes in order as the rate allows, and `tm link send` says when (`queued M-21 … it goes in order about 15:04`), so a long reply split into parts arrives whole. The limits are settings: **Link limits** in the Settings tab (a project's, or all projects') lists messages at once, messages an hour and the message size (`link_burst`, `link_per_hour`, `link_max_kb` in `[defaults]` or `[projects.<slug>]`); between two projects the lower of the two holds, and raising one above its default asks first. A link that has gone back and forth six times in an hour without you pauses until you resume it. A file too big to attach goes with one message, by its own consent: `tm link send terminatr-mods "the report" --file t-0042/report.pdf` (a library file, or `M-12/<file>` to pass on a file a message brought; you may also name any file of yours by its path). The other project's user is asked: a `link-file` item says what is offered, and `A` accepts or `X` declines it in its message's dialog in the Messages tab (`tm link accept|decline X-17`); the coordinator can't. Accepted, it lands beside its message under `messages/<other>/<M-id>/`, checked by its SHA-256, and shows in the Library tab under `from <project>`; `tm link files` lists every transfer and its state, `tm link cancel X-17` (or `C`) stops one. Files are at most 25 MB (`link_file_mb`, up to 200) and go to a paired server at 1 MiB/s (`link_file_kbps`, 64–2048), slowing on their own while you type in a remote pane; a dropped link or a restart resumes from what arrived. `F` on the link in the Network popup sets its files to ask me (the default; `ask` in the config), take them (`auto`, taken without asking; it asks you first) or refuse (`off`) (`tm project link a b --files auto`). The change also answers the files already waiting on that link, as does a hand edit of `files` in the config: auto takes them as if they had just arrived (one over `link_file_mb` keeps waiting for you), off declines them; the confirm says how many (`This takes 1 waiting file.`). The project popup's **Messages** tab (`9`) lists every message of the project's links in one list, newest first, the same for a link here and one to a paired server's project: who it is from (`→ beta` for one you sent), its id, its first line and its date; the arrows, PgUp/PgDn and the wheel move through the newest 200, and a line says how many older ones there are (`tm link log` lists every one). `enter` or a click opens the message: its full text, from and to, the date, its id, what it answers, its state (sent, read, held, queued …), its attachments and its file with the transfer's state. There `A` sends a held message, `X` drops it, `A` / `X` accept or decline its file, `C` cancels a file on its way and `esc` closes it; on the list, `M` writes as you, and `N` opens the Network popup on the project. What needs you is marked ⚑ and counted on a NEEDS YOU line above the list: held messages and waiting files on their rows, a paused link and a link request as rows of their own at the top, where `R` resumes the link and `A` / `X` answer the request. The Network popup shows the same under each link, and answers them with the same keys. Each also has a row under NEEDS YOU on the dashboard: `enter` on a held message or a file opens its dialog, on a request or a paused link the Network popup at that row; the header says `· 1 network` when another project's link needs you. `tm link list` and `tm link log <project>` show the same from a shell; both projects keep a copy under `messages/<other>/`.

Pull requests can live on GitHub or Azure DevOps, or on GitLab through a code host mod; the host is picked per repo from its `origin` remote. GitHub needs `gh auth login`; Azure DevOps needs the Azure CLI and `az login` (or `AZURE_DEVOPS_EXT_PAT`). To force a host (an Azure DevOps Server, say), put `code_host: "azure"` and `azure_url: "https://tfs.example.com/tfs/Coll"` in `PROJECT.md`'s front matter. tm sends Azure DevOps credentials only to `dev.azure.com`, `*.visualstudio.com`, the host of the `origin` remote of a repo you added to the project (for that repo only, and not while an adopted or checkout thread works in it), the host of your `az` default organization (`az devops configure`), and hosts you list in `config.toml` (`[azure]`, `hosts = ["tfs.example.com"]`, an override for a host none of those cover); an `azure_url` on any other host is ignored. An Azure DevOps Server whose repo is cloned from it needs no setting. `tm doctor` checks what your projects use; details in [Code hosts](#code-hosts-github-and-azure-devops).

To put a project to sleep, `tm project deactivate <slug>` (or `S` on its dashboard or on it in the sidebar, or Active in the popup's Settings tab; the dashboard's project header ends in `● active — S to deactivate` or `⊘ inactive — S to activate`, which a click toggles too, and its footer says what `S` does): its coordinator and threads stop now (it asks first while they run; `--yes` without a terminal) and stay stopped across server restarts, and the sidebar moves it under the `inactive` divider, marked `⊘`; `tm project activate <slug>` (or `S` again) resumes them where they were. To look inside an inactive project without waking it, open the divider and rest the sidebar's cursor on it (its dashboard shows), or `→` on its row expands it: its coordinator and threads show dormant (`⊘`), and `enter` on the coordinator's row activates the project and starts it. Only active projects' agents come back when the server starts. To park a project with its agents running, `tm project pause <slug>` (or Paused in the popup's Settings tab): its coordinator gets no nudges, its threads no pull request follow-up, and no new thread starts until `tm project resume <slug>`; the sidebar shows `∥` after it. `tm project archive <slug>` hides a finished project from the sidebar and stops all background work for it (`tm project unarchive` brings it back), and `tm project delete <slug>` moves its folder to `~/.terminatr/.trash/`; both refuse while its coordinator or threads run. `tm project rename <slug> <new-slug>` renames a project's slug, its only name, and moves its folder and worktrees with it (no thread may run; its coordinator is restarted under the new slug; see [Renaming a project](#renaming-a-project)). The coordinator can give a thread a smaller or larger model with `--model` on `tm task delegate` / `tm thread start`, from the list `tm context` shows: what the installed agent says you can use (tm asks it; Settings > General > Models shows the list and `R` asks again), narrowed by the project's `models` setting if you limit them (Thread models in the popup). A model your account refuses is learnt from the thread that tried it: the coordinator gets a `model-refused` item, and tm stops offering it for that login. **Coordinator model** in a project's Settings picks the coordinator's model. When `gh` keeps failing (logged out, keychain refused), the coordinator gets a `gh-failing` inbox item, and `tm doctor` checks `gh auth status`.

### Trying projects and tasks

```sh
export TERMINATR_HOME=$(mktemp -d)        # leave ~/.terminatr alone while trying it
tm project new demo --goal "Try tm"   # the slug is its only name; "Demo App" would become demo-app
export TERMINATR_PROJECT=demo             # or cd into $TERMINATR_HOME/projects/demo
tm task add "Fix login redirect" --step "Reproduce" --step "Fix"
tm task status T1 started
tm task steps T1 check 1
tm task list                         # add --json for machine-readable output
tm context                           # what the coordinator reads every turn
tm skill coordinator                 # the coordinator's standing rules, versioned with tm
```

`tm task help` lists every task command. Exit codes: 0 done or already true, 1 refused (with a stable code such as `human-only`), 2 usage error, 3 I/O error.

**Which repository a thread works in.** Each thread works in one of the project's repositories. With one, it is that one. With several, the task says which: `tm task add "Fix the form" --repo web` (the repository's folder name, or its path), or later `tm task edit T1 --repo web`; `tm task show` and `tm context` show it, and the coordinator's `--repo` on `tm task delegate` overrides it. A task that names none in a project with several repositories isn't guessed at: delegating it is refused (`pick-repo`, listing the repositories), and the coordinator asks you which; `D` on the dashboard says so before it asks. For work that makes no pull request (research, notes, a review), `--repo none` gives the thread a plain folder without git, also in a project with repositories; a project without repositories always does. Such a thread hands in what it makes as report attachments (the project popup's Library). Its folder is removed when the thread closes only if it is empty: auto-close waits while files are left in it and tells the coordinator, and `tm thread resolve <id>` keeps it, saying how many files are in it; only `tm thread resolve <id> --discard --approved-by-user`, on your word, throws them away (without the flag it is refused). Work over several repositories is one task per repository.

## Where state lives

Everything is under `~/.terminatr`, or `$TERMINATR_HOME` when that is set. Nothing terminatr-owned is written into your repositories or thread worktrees.

| Path | What |
|---|---|
| `config.toml` | your settings: the default agent, the prefix key (`[keys]`), the icons (`[ui]`), and the project settings for all projects (`[defaults]`) and per project (`[projects.<slug>]`, which wins key by key). The settings popups (`,` and a project's Settings tab) write it; so do `tm project pause`, `archive`, `delete` and `rename` |
| `ui.json` | this console's layout: the details panel, the list width, and the sidebar and info panel widths new views start with |
| `agents/<name>.toml` | your own agent manifests (they override the built-in ones) |
| `projects/<slug>/` | one folder per project: `PROJECT.md`, `CONTEXT.md`, `MEMORY.md` and `memory/`, `TASKS.md`, `JOURNAL.md`, `inbox/`, `threads/<id>/` (brief, reports, status, attached files), `messages/<other>/` (messages with a linked project), `uploads/` |
| `.trash/<slug>-<time>/` | projects removed with `tm project delete`; tm never empties it |
| `worktrees/<slug>/<id>-…/` | thread worktrees: plain git checkouts of the project's repos |
| `state/sessions.json` | the sessions the server runs, rewritten on every change; the next server resumes agents from it |
| `state/views.json` | what the shared consoles show (screen, session, sidebar), restored after a restart |
| `state/ticker.json` | what the ticker already reported (thread states, PRs, nudges, checkout syncs), so a restart repeats nothing |
| `state/links.json` | the message counter, each link direction's rate and each linked pair's conversation breaker (`tm link`) |
| `state/slots.json` | the coordinators a thread cap refused, waiting for a `slot-free` item |
| `logs/server.log` | the server log, rotated at 10 MB (`server.log.1` … `.3` kept) |
| `logs/service.log` | output of a server started by launchd (macOS service only; not rotated) |
| `logs/console.log` | the consoles' own errors (what the footer showed, a failed view call, a panic), rotated at 1 MB (`console.log.1` kept) |
| `logs/mods/<id>.log` | each code mod's log (`tm mod logs <id>`), rotated |
| `run/` | `tm.sock`, `server.lock`, `server.pid` and per-session runtime dirs (`s/<id>/`: the agent's generated settings and plugin) and `bin/tm`: the running server's own copy of its binary, which it runs from, so `tm update` or `brew upgrade` can't pull it from under the server and macOS privacy settings see one `tm` across releases ([macOS privacy prompts](#macos-privacy-prompts)); per-build `bin/tm-<build>` copies and a `server-bin/` from older versions are removed on start |

The run directory is `$XDG_RUNTIME_DIR/terminatr` on Linux when that variable is set (and `TERMINATR_HOME` isn't), and falls back to `/tmp/terminatr-<uid>-<hash>` when the path to the socket would be too long. `tm server status` and `tm doctor` print the one in use. `$TERMINATR_SOCKET` overrides the socket path.

Agents keep their own files too: Claude Code stores conversations under `~/.claude/`, which is what resume uses.

### Renaming a project

`tm project rename <slug> <new-slug>` renames a project's slug, its only name and the name of its folder (lower case: `a-z`, `0-9` and `-`; a typed name like `My App` becomes `my-app`):

```sh
tm thread stop t-0012 --project termilator        # every running thread first; tm says which
tm project rename termilator terminatr
tm thread restart t-0012 --project terminatr      # back in its moved worktree, its conversation resumed
```

It moves `projects/<slug>/` and `worktrees/<slug>/`, reconnects git with each moved worktree (`git worktree repair`), renames `[projects.<slug>]` in `config.toml` (comments and order stay) and the project's key in other projects' links, moves other projects' `messages/<slug>/` folders, rewrites the worktree paths in the thread records, carries the ticker's memos over and Claude's conversations and folder settings for the moved folders, and journals `project.rename` in the project. The journal, inbox, tasks and memory move with the folder unchanged. It is refused while a thread of the project runs (stop them; the error names them), and when the new slug is taken by a project, a `worktrees/<new-slug>/` folder or a `[projects.<new-slug>]` table. A running coordinator is stopped and started again under the new slug. Branches keep their names: `tm/<old-slug>/…` stays on existing threads, and only new threads get `tm/<new-slug>/…`. With no server running, the CLI does the same itself.

### Moving a repository

When a project's repository moves (renamed folder, new disk), tell the project from your own shell (a coordinator can't add a repo): `tm project repo add NEW --project <slug>` and `tm project repo remove OLD --project <slug>`. A thread whose recorded repo is gone then follows its branch to the repo that has it on `tm thread restart` and `tm thread resolve` (its worktree reconnected with `git worktree repair`, its record updated). When no repo of the project has the branch, restart refuses with `no-repo`, and resolve says the repo is gone and keeps the worktree, without running git.

The build directory moves with a checkout: libghostty-vt's pkg-config files name their prefix relative to themselves (`${pcfiledir}`), which `make` rewrites once after building it, so a `.build/` built before that still links the library at the path it was built at until the next `make` there. A thread's worktree has no `.build/` of its own; `make` and `make env` in it use the main checkout's.

## The server

Any `tm` command starts the server when it isn't running, so normally you don't start it yourself. It runs detached from your terminal: closing the window, the client or an SSH session never stops it or its agents.

```sh
tm server status          # pid, uptime, version, sessions; what the last restart resumed or lost
tm server stop            # asks first while agents run; --yes to skip
tm server restart         # stop, start, resume the agents
tm server stop --force    # a hung server: SIGKILL the pid that holds the lock
```

### Over SSH (macOS)

On macOS a server started from an SSH login would run in that login's security session, and so would every session under it: the keychain refuses them, so the PR CLI (`gh`, or `az` if its token cache is in the keychain) says its token is invalid and git's credential helper fails ("Interaction with the Security Server is not allowed"). So every start (`tm server start` or `restart`, `tm update`'s restart, a command that auto-starts it, the TUI) goes through launchd: tm hands the server to your desktop session (`launchctl kickstart gui/<uid>/dev.terminatr.server`), wherever you typed the command. Starting over SSH is then the same as starting at the Mac, with the same environment: the server gets your shell's (`PATH`, `LANG`, tokens), less the SSH login's own variables.

That needs someone logged in at the Mac's console; the screen can stay locked. With nobody logged in, a start refuses:

```
tm server start: nobody is logged in at the Mac's console, so the server can't run in the desktop's session and its sessions couldn't use the keychain (the PR CLI's login, git credential helpers); log in on the Mac (the screen can stay locked) and try again, or start a server without the keychain: tm server start --no-launchd
```

`tm server start --no-launchd` (or `TERMINATR_LAUNCHD=off`) starts the server from your session as before, and over SSH warns that its sessions can't use the keychain. If launchd fails for another reason, the error names `launchctl` and the same way out.

Azure DevOps over SSH is not verified yet: whether `az`'s token cache works from a server started over SSH (it lives in `~/.azure/msal_token_cache.*`, in the keychain only when the Azure CLI's encrypted-cache setting is on) hasn't been run on a Mac. To check: log in at the Mac (`az login`), then from an SSH login run `tm server restart`, and from a session in it `az account get-access-token --resource 499b84ac-1321-427f-aa17-267ca6975798 -o none` and `tm doctor` (its `az login` and `az repo` lines). Note the result here.

`tm doctor` runs the code-host checks (`gh auth`, the `az` lines, `git origin`) in the server's context, not this shell's, so over SSH it doesn't warn "not logged in" for a `gh` that works in the sessions: lines are marked `[server]`, and a `local shell` line notes what this shell can't do (for instance reach the keychain) that the server's sessions can. With no server the checks run in this shell and are marked `[local]`. In `--json` each check has an added `source` field. `tm doctor` also shows whether the running server's sessions can reach the keychain (`server keychain`), wherever you run it from. The fix is `tm server restart`; `tm doctor --fix` offers it over SSH too, unless you run doctor from inside one of tm's own sessions. On Linux none of this applies.

What launchd holds: the job `dev.terminatr.server` (another `TERMINATR_HOME` gets `dev.terminatr.server.<hash>`), from `~/Library/LaunchAgents/` when the login service is installed, else from `run/dev.terminatr.server.plist`, which isn't loaded at login; `run/launch.json` (private) holds the environment; the server's own output goes to `logs/service.log`. `launchctl print gui/$(id -u)/dev.terminatr.server` shows it. `tm server stop` boots out a non-default home's on-demand job (nothing stays loaded for a dev or test home), and `tm doctor` lists `dev.terminatr.server.<hash>` jobs that are idle and whose plist is gone, in a temporary directory, or names a `tm` that no longer exists; `tm doctor --fix` boots them out.

`tm server stop` and `tm server restart` work whatever version the running server is. When a newer `tm` meets an older server, other commands say `the running tm server is older than this tm …; run 'tm server restart'`, and that is the fix: restart stops the old server (asking it in its own protocol, or with `SIGTERM` when it can't be asked) and starts this `tm`'s, which resumes the agents. tm only ever signals the process that holds this home's server lock and runs `tm server run`.

A `tm` from before this fix can't do that: it prints `tm server speaks protocol 1 …, this tm speaks 4 …; run 'tm server restart'` and the restart fails the same way. Get out once with (on Linux the pid file is in `$XDG_RUNTIME_DIR/terminatr/` when that is set):

```sh
kill $(cat ~/.terminatr/run/server.pid) && tm server start
```

The old server shuts down cleanly on `SIGTERM`, so the new one resumes the agents as after any restart.

### Crashes and restarts

When the server stops, every session it hosts stops with it. The next server reads `state/sessions.json` and:
- resumes every coordinator and thread whose agent has worked on a prompt, with the agent's latest session id (`claude --resume <id>`), in the same directory with the same brief and hooks;
- starts an agent that never got a prompt fresh (there is no conversation to resume);
- does not restore shell sessions; they are listed as lost.

Turns that were running are lost; resumed agents are idle. Each affected project gets a `server-restart` inbox item ("the server restarted after a crash: 2 session(s) resumed, 1 not restored"), and the coordinator decides what to re-prompt. `tm server status` lists the resumed and lost session ids, and `server.log` names each one (`server restarted after crash; resumed coordinator, t-0003; lost shell s-12`).

### Start at login (optional)

Any `tm` command starts the server when needed, so you don't need a service. If you want the server up from login:
- `tm server service install` writes `~/Library/LaunchAgents/dev.terminatr.server.plist` and loads it with launchd on macOS (`RunAtLoad`, no `KeepAlive`). It is the job every start uses anyway ([Over SSH](#over-ssh-macos)); installing it while the server runs restarts the server, and agents are resumed. On Linux it writes `~/.config/systemd/user/terminatr.service` and enables it with `systemctl --user enable --now`.
- The service runs `tm server run` (`--launchd` on macOS) with the `PATH` of the shell you installed it from, so `claude` and `git` are found. On macOS each start from a shell also hands the server that shell's environment, and a start after `brew upgrade` rewrites the plist for the new `tm`; on Linux re-run install after moving `tm` or changing `PATH`.
- On macOS the service's own output goes to `~/.terminatr/logs/service.log`; the server still logs to `server.log`.
- `tm server service uninstall` unloads and removes the file. That cleanly stops a server the service started, so the next server resumes its agents.
- `--print` shows the file without installing anything.
- On Linux a user service stops at logout unless lingering is on (`loginctl enable-linger`).
- On Windows it adds a value to your `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` key (named `dev.terminatr.server`; Task Manager's Startup tab lists it and can switch it off), which runs `tm.exe server start` at each login. It needs no admin rights and starts nothing now; any `tm` command still starts the server when needed. A console window shows for an instant at login. `uninstall` deletes the value.

## Code hosts: GitHub and Azure DevOps

Each repo's pull requests are asked of its code host, picked from `git remote get-url origin` (SPEC §3, **Code host**). `github.com` and anything unrecognised is GitHub; these remotes are Azure DevOps (cloud, `dev.azure.com`):

```
https://[user@]dev.azure.com/{org}/{project}/_git/{repo}
https://{org}.visualstudio.com/[DefaultCollection/]{project}/_git/{repo}
git@ssh.dev.azure.com:v3/{org}/{project}/{repo}
{org}@vs-ssh.visualstudio.com:v3/{org}/{project}/{repo}
```

For an Azure DevOps Server (or any host terminatr doesn't recognise) set it in the project's `PROJECT.md` front matter:

```yaml
code_host: "azure"                          # or "github"
azure_url: "https://tfs.example.com/tfs/Coll"   # organization or collection URL
```

**GitHub:** `gh auth login`.

**Azure DevOps:** the server runs on your machine and uses your login, as `gh` does.

1. Install the Azure CLI (`brew install azure-cli`). The `azure-devops` extension (`az extension add --name azure-devops`) is optional: the coordinator opens PRs with `az repos pr create`, else it uses `az rest`.
2. `az login` (Microsoft Entra ID), in the tenant the organization is connected to (`az login --tenant <id>` when you have several; `tm doctor` names it). That is all terminatr needs; it never stores or asks for a token. terminatr asks Azure DevOps' REST API with `az rest --resource 499b84ac-1321-427f-aa17-267ca6975798`, the Entra token of your login, as Microsoft documents for ad hoc REST calls ([Issue Entra tokens with Azure CLI](https://learn.microsoft.com/azure/devops/cli/entra-tokens)); Conditional Access applies to it as to any sign-in of yours. A personal access token also works by exporting `AZURE_DEVOPS_EXT_PAT` before `tm server restart`: tm uses it when `az` is missing, isn't logged in or is refused, and sends it only to the organization's URL, never on a command line; the guard stops threads reading or printing it. A PAT saved with `az devops login` is not used.
3. `git` needs its own credential for the remote (Git Credential Manager, or an SSH key), since threads push with it.
4. Run `tm doctor`: it checks `az` and the login (or the PAT), that tm can read each repo (and if not, why: the login, the organization's tenant, the origin URL), and that `git ls-remote origin HEAD` doesn't prompt.

If your email is both a work account and a personal Microsoft account, the `azure-devops` extension's commands (`az repos`, `az devops`, `az pipelines`) sign in as the personal one and fail with `TF400813: The user '…' is not authorized`, while `az rest` works. tm doesn't use them (nor checks them), and the coordinator falls back to `az rest`. For `az repos` yourself, `az devops login --organization <org URL>` with a PAT works around it.

Threads push their branch and report the PR's title and body; the coordinator opens the pull request (`gh pr create`, `az repos pr create` or an `az rest` POST), and tm finds it by the branch. A thread that opened one reports its URL on the `PR:` line, either form. Completing a PR is the coordinator's with `merge = "coordinator"`, and the guard refuses a thread's `gh pr merge` or `az repos pr update --status completed`. Build-validation policies are optional: a PR without them has no checks to follow, and merge commits, squash merges and rebase merges are all recognised from the PR's state. `tm thread merge` completes a PR by your Merge method (no fast-forward merge for a merge commit, squash, or rebase); a branch policy that limits merge types refuses the others with `merge-method-not-allowed`.

**GitLab** is served by a code host mod, not by tm itself (SPEC §3, **Code host**). terminatr's `examples/mods/gitlab` is one, through `glab`:

1. Install glab (`brew install glab`) and `glab auth login` (for a self-managed server, `glab auth login --hostname gitlab.example.com`). The mod runs glab on that login, so with your GitLab access; tm refuses the glab commands that would show or change the login (`auth`, but `auth status` without `-t`, `config`, `alias`).
2. `tm mod install https://github.com/theclifmeister/terminatr/tree/v0.46.0/examples/mods/gitlab` (or a later release's tag), read the consent screen (it asks only to run `glab`), and enable it in the Mods tab with the `commands` grant, for the projects that use GitLab or all.
3. Put `code_host: "gitlab"` in each such project's `PROJECT.md` front matter. Without it a `gitlab.com` remote has no PR host, as before.
4. Run `tm doctor`: under `code host` it names the mod that serves `gitlab`, then glab and its login.

The coordinator opens merge requests with `glab mr create`, threads report their URL (`https://gitlab.com/<group>/<project>/-/merge_requests/<n>`) on the `PR:` line, and `tm thread merge` merges one at the head it checked, with a merge commit or a squash by your Merge method (the example mod refuses rebase, which GitLab sets per project, not per merge: `merge-method-not-allowed`). tm still decides what is safe: it finds merge commits in git, and never deletes a branch or a worktree on the mod's word alone (git must find the commits on the remote).

Without the CLI and a login (or a PAT for Azure DevOps), that host's PRs are not followed: no follow-up prompts, no auto-close on merge, no completing tasks on merge, and after 3 failed polls the project's inbox gets a "PR host failing" item. Threads, tasks and reports are unaffected. The full table is in SPEC §3. terminatr's own release flow (below) is GitHub-only and unrelated to the hosts of your projects.

## Access: what threads may do

Everything that decides what a project's threads may do is on one tab: **Access**, in the project's popup (`A`, then `5`) and, for all projects, in the settings (`,`, then `3`). A project's row says `· all projects` while it follows all projects' value; changing it there makes it the project's own, and `X` on it follows all projects again (the footer offers `X` only on such a row). All projects' rows name the projects that set their own. The Settings and All projects tabs keep the workflow settings (starting threads, caps, pull requests, models, branch names, history). The tab's sections:

The tab shows only what your protection level uses: at open, the Protection and prompts section, Secrets and Built in; from guarded, the Guard section too; from sandboxed, the sandbox's sections. Raising the level shows them at once, with whatever they held before.

- **Protection and prompts**: Protection (below), Yolo mode (turning it on asks first), Coordinator approves and Coordinator sets access (below).
- **Inside the sandbox** (network hosts, local ports, guidance for threads, write folders, sockets), **Outside the sandbox** (excluded commands, approved commands) and **Permission rules** (allow rules): the sandbox's rules, below.
- **Guard**: the guard's rows, and **Secrets**: the secret environment ([The guard](#the-guard)).
- **Built in**: tm's own rules, which no setting changes (its worktree to write, the project folder to read only, `config.toml` never written, the repository's git objects and refs, tm's socket, the guard's floor); on a project `enter` lists them.

`E` on a project's Access tab opens its effective access: every rule a new thread gets, with where it comes from (below). Protection, yolo and the sandbox rows reach a thread at its next start or `tm thread restart`; the guard's rows at once.

**Coordinator sets access** lets a project's coordinator drive its rules for you: when a thread needs a host, a write folder, local ports or a command outside the sandbox, the coordinator adds it with `tm access` instead of asking you to. It is off by default; turning it on asks first (or `tm access set coordinator on <project> --yes`). It covers only that project and only the rules within the level you set: the protection level, escapes, yolo mode, resetting access, all projects' settings and this switch stay yours, and when a task needs another level the coordinator asks you. Each change is in the project's journal under the coordinator's name, and its rules tell it to open no more than a task needs and tell you what it changed.

**Protection** is how much of tm's security your agents run under. You start with none of it in the way and add layers when you want them; each level adds to the one before:

| Level | What threads get |
|---|---|
| **open** (the default) | Each agent as it runs on its own, with its own permission prompts. No thread sandbox; the guard keeps only its floor. |
| **guarded** | The guard's rules too: threads write only their worktree, no force-push or push to the default branch, no deleting branches, no reading credentials, threads don't merge (each rule can be turned off under Guard rules). |
| **sandboxed** | Claude threads run their shell commands in a sandbox, with the sandbox rules below; a thread may ask you to run one command outside it. |
| **strict** | No such escapes: what can't run in the sandbox doesn't run. |

At every level, threads never write the project folder (only the coordinator does), no agent writes your settings or reads tm's own files, and no agent can reach the socket tm keeps for you. None of that prompts in normal work. Codex threads always run in Codex's own sandbox, as Codex does on its own, so for them the levels change the guard and escapes. Set it with `enter` on the Protection row (going back to open asks first), or `tm access set protection sandboxed [<project> | --all]`. To start over, `R` on the Access tab (or `tm access reset [<project> | --all]`) puts the switches back: a project's protection, yolo mode, coordinator approves and guard settings follow all projects again, and all projects' go back to tm's defaults (protection open). It asks first, and keeps the sandbox lists, guard allowances and secret environment you built. If you used tm before protection existed, it kept what you ran with (sandboxed, or strict if you had turned escapes off), and each project's journal says so.

From **sandboxed** up, threads run their shell commands in a sandbox: they write only their worktree and a temp folder, and reach the network and sockets only where tm allows. A thread may still ask to run one command outside it, and you approve it: tm always puts that prompt to you, even in auto mode, and never lets the coordinator answer it; tests that listen on local ports or sockets, `docker`, and `gh` usually need that. Such a command runs with all your rights: it can reach the socket tm keeps for you alone, and so act as you in tm (start sessions, type into the coordinator's pane, change settings), as well as read your secrets and write anywhere. If you'd rather threads never leave the sandbox, set Protection to *strict* on the Access tab, or `protection = "strict"` under `[defaults]` (all projects) or `[projects.<slug>]` in `~/.terminatr/config.toml`. Claude threads then start with Claude's `allowUnsandboxedCommands` off, from their next start or `tm thread restart`, and what the sandbox refuses simply fails: listening on local ports (test servers, dev servers), unix sockets other than tm's (Docker), terminals (ptys), writing outside the worktree and the temp folder, and `gh` (its certificate check fails in the sandbox on macOS), so a thread can't open its own pull request. The thread says in its report what it couldn't run. Codex threads start with `-a never`; the coordinator is unchanged.

**Browser tests, dev servers and other tools in the sandbox.** Under **Inside the sandbox**, **Outside the sandbox** and **Permission rules**, the Access tab shows only what applies on the platform the server runs threads on. Every list adds down the layers: all projects' entries, then the project's own; a project that doesn't want one of all projects' drops it (`X` on it in the list, and `X` again takes it back). A project's list view shows all projects' entries too, each with where it comes from, and the dropped ones struck through (`─`). To change an entry, `enter` (or `e`) on it opens its value for editing, then an excluded command's choice to ask first or run without asking (a guide: `e` changes its title and then its text). The change is saved as one, through the same checks as adding an entry, and the list says what it opens first, as for a new entry; a value the checks refuse is left unchanged, with the same reason in the footer. A project's list changes the project's own entries; all projects' are changed in the `,` popup's Access tab, and a project that dropped one there drops it under its new value too.

Start inside the sandbox: each rule there opens just what a tool needs, and the thread's commands stay in the sandbox.

- **Network hosts** (macOS): hosts the thread's sandboxed commands may reach, a host name (`registry.npmjs.org`) or every host under one (`*.example.com`); never an address in any form, a URL, a port, `localhost`, a domain whose names resolve to any address you spell (`nip.io`, `sslip.io`, `xip.io`, `lvh.me`, `localtest.me`, `traefik.me`, `localhost.direct`, `vcap.me`, and names under them), or a public suffix or every host under one (`co.uk`, `*.co.uk`, `*.github.io`): local ports are the next row's. Another name that resolves to this computer or your network would reach it all the same, and tm can't know where a name resolves: don't add one unless you mean to open them. A host is open to every command a thread runs, not only the tool you add it for, which the tab says before adding one, and more plainly for a host that serves many owners (`storage.googleapis.com` serves every Google Cloud Storage bucket: a thread's commands could download from, or send your files to, any bucket there). Claude's `allowedDomains`; Codex threads reach them through Codex's network proxy, which limits their network to their hosts and the hosts of their repo's git remotes (so they can push); on Windows and Linux it isn't limited, as before this release (on Windows Codex's proxy needs its elevated sandbox; Codex's Linux sandbox can't allow tm's socket with its proxy on).
- **Local ports** (macOS): threads' commands may listen on and connect to local ports (a dev server, a test server, a local database). What that also opens, which the tab says before opening them: macOS's sandbox doesn't limit listening to localhost, so a server bound to all interfaces is reachable from your network, and a thread can connect to every service listening on a local port (databases, Docker over TCP, Chrome's debug port, IDE extensions, a local API) and can take a port another program expects: a service that runs commands lets the thread out of the sandbox. A project's own off is how it drops all projects' on; `X` follows all projects again. Claude's `allowLocalBinding`, Codex's `allow_local_binding`. Claude's Linux and Windows sandboxes have no such setting, so it isn't shown there.
- **Guidance for threads**: texts tm adds after a thread's brief, each a title and a text you write: say there what threads should know about your project and its sandbox (a fixture to regenerate, a command to ask for with `tm run`). `enter` on one shows and edits its text, `e` changes its title and then its text, `A` adds one (its title, one line, then its text in an editor: `enter` breaks a line, `ctrl+s` saves, `esc` leaves it as it was), `X` removes one (on a project, `X` on one of all projects' drops it, and on a dropped one takes it back). They open nothing, and are plain text, so they apply on every platform and for Claude and Codex threads. Two guides came from tm's browser presets (tm 0.38 to 0.43; tm has no presets now), and a project that added them keeps them as plain guides, worded from the rules its threads had then; tm doesn't reword them when those rules change, so edit them then. `Sandbox: browser testing`: Chromium can't start its usual child processes in macOS's sandbox, in Claude's or in Codex's (it may not register Mach services, and no setting allows that), but Playwright's headless shell in one process works; threads are told to launch it that way (`chromium.launch({ headless: true, args: ['--single-process'] })`) and get a ready `playwright.sandbox.config.ts` for `npx playwright test --config playwright.sandbox.config.ts`, which extends your project's config without changing it, reusing one context per test worker (a browser started this way holds one at a time); it says whether local ports are open. `Sandbox: browser downloads`: threads install npm packages with a cache of their own (`~/.npm` isn't writable in the sandbox), use a browser already in `~/Library/Caches/ms-playwright` as it is, install a missing one into a folder of their own, and fetch it with `curl` when Playwright's installer breaks off in the sandbox ("server closed connection"); it names which of the download hosts are open. Screenshots and headless tests work this way; full Chrome (`channel: 'chrome'`) and headed runs don't, and need an excluded command (below). On Linux Playwright's Chromium should need none of this (its sandbox has no Mach services to deny; not yet tried on a Linux server), so threads there need neither guide.

**No presets.** tm 0.38 to 0.43 had presets, named groups of these rules (browser testing, browser downloads, local dev servers). They are gone: the rules and guides a preset added stay as plain rules, to change or remove one by one, and tm 0.37's switches (`browser_testing`, `browser_downloads`, `local_dev_servers`) are still read as their rules. Add what a tool needs as rules of your own.

**Changing rules from a shell.** `tm access add <kind> <value> [<project> | --all]` adds a rule (kinds `write`, `host`, `guide`, `socket`, `excluded`, `approved`, `allow`; `tm access add excluded "npx playwright *" --without-asking` runs it without asking; `tm access add guide "Test data" --file notes.md` adds a guide, its text from `--text` or `--file`, `-` for stdin, and `--replace` changes one's text; `tm access show --guides` prints them as threads get them), `tm access rm` removes one, `tm access edit <kind> <old value> <new value> [<project> | --all]` changes one in place, through the same checks as `add` (`--without-asking` or `--asks-first` for an excluded command, else it keeps its own; for a guide the new title, and `--text` or `--file` for its text; the same value twice changes only those), all projects' carrying along each project's drop of it, `tm access drop <value> [<project>]` drops one of all projects' for a project and `tm access undrop` takes it back, `tm access set local-ports|escapes|yolo on|off [<project> | --all]` turns those on or off (`yolo on` asks first, or takes `--yes`, since it turns the sandbox off; `escapes off` is protection strict, `escapes on` sandboxed), and `tm access set protection open|guarded|sandboxed|strict [<project> | --all]` picks the level (`open` asks first, or takes `--yes`). They check each entry as the Access tab does, are yours alone (the server tells who runs them from the process tree, so an agent gets `human-only` even with its session's variables unset, and they refuse to run while no server can tell), and are journaled; threads get them from their next start or restart.

**The move from tm 0.37's settings.** The first server start of this release rewrites `config.toml` once, keeping the old file beside it as `config.toml.bak-0.38`: the old switches become their rules, a project's `excluded_commands` or `allow` list (which replaced all projects') becomes its entries plus a `drop` of each of all projects' it left out (an empty `[]` drops them all), a `Bash(…)` allow rule that covers an excluded command becomes that command's `ask = false`, and `Edit(…/**)`, `Write(…/**)` allow rules and `worktree-only` path allowances (`<folder>/*`) become write folders where the folder is one; a write folder also opens the folder to the thread's sandboxed commands and to Codex threads, so the note names each such folder and asks you to check it (`tm access rm write <folder> <project>` undoes one). It checks that every project without yolo gets the same rules before and after, and refuses otherwise, keeping the file and reading it as before. Each project's journal and inbox say what moved and what was left for you: a `Bash(…)` rule that covers no excluded command, a rule whose path is no write folder. They also say that yolo now turns the sandbox off for Claude threads too, and that escape prompts go only to you; every project with yolo on is told so once, even when nothing moves. The file then starts with `sandbox_lists = "add"`, which tm writes; `tm doctor` lists what is still in an old form. The same start writes out tm 0.38's guide names (`guides = ["browser-testing"]`) as guides, title and text, worded as each project's threads got them (a project worded otherwise gets its own copy in place of all projects'), with the same backup, the same check and a note in each project's inbox.

- **Browser testing (Playwright, Chromium)** (`browser-testing`): the guide `Sandbox: browser testing`.
- **Browser downloads (npm, Playwright)** (`browser-downloads`): the hosts `registry.npmjs.org`, `cdn.playwright.dev`, `playwright.download.prss.microsoft.com` and `storage.googleapis.com` (where Playwright's download site sends the browser from), and the guide `Sandbox: browser downloads`.
- **Local dev servers** (`local-dev-servers`): local ports on.

```toml
[defaults.sandbox]
excluded_commands = ["docker compose *"]
guides = [
  { title = "Test data", body = """
The fixtures in testdata/ are generated: run make fixtures, never edit them.
""" },
]

[projects.web.sandbox]
hosts = ["registry.npmjs.org", "cdn.playwright.dev"]
local_ports = true
drop = ["docker compose *"]                 # web doesn't take this one of all projects'
```

tm 0.37's switches (`browser_testing`, `browser_downloads`, `local_dev_servers`) are still read, as the rules they add (a project's `false` as drops of them); the tab writes the plain rules in their place the first time it changes that table.

**Write folders, excluded commands and allow rules.** A last resort, for a tool nothing inside the sandbox gets working: under Inside the sandbox, folders besides the worktree that threads write in; under Outside the sandbox, commands that run outside the sandbox, each asking first or running without asking; under Permission rules, permission rules for other tools. Rather than writing `.claude/settings.local.json` in each worktree:

```toml
[projects.web.sandbox]
excluded_commands = [
  "docker compose *",                                  # outside the sandbox, asks first
  { command = "npx playwright *", ask = false },       # outside the sandbox, without asking
]
write = ["~/src/shared-fixtures"]                      # file tools and sandboxed commands write there
allow = ["WebFetch(domain:playwright.dev)"]            # a tool call without a prompt
```

One rule, one choice: an excluded command **asks first** unless it says `ask = false`. Asking first holds whatever else would let it through: the guard puts the call to you (rule `excluded`), even in auto mode, with an allow rule in your own `~/.claude` settings or with the guard off, and like an escape that prompt is yours, never the coordinator's (press `W` on it in the list to change it, or `enter` to change the command and that choice together). With `ask = false` tm writes the `Bash(...)` allow rule itself. A **write folder** is one rule for a folder besides the worktree: the guard lets file tools write there, Claude gets the `Edit(...)` allow rule (which opens its Bash sandbox for writing there too) and Codex the folder in its sandbox. It is a whole folder, written plainly (`~/src/fixtures`, no `*`), and what it opens is everything below it: the thread can create, change and delete any file there, without asking, from its file tools and its sandboxed commands alike, so anything that later reads or runs those files (a build, a test runner, your editor, you) gets what the thread put there. So it is never your home or a folder one level below it, the disk's top levels, a folder of files that start programs (`.git`, `.ssh`, `.config`, `.claude`, `.terminatr`, shell profiles), or one whose files are loaded or run as code: a folder on your `PATH` or one holding one, `~/Library` (but `~/Library/Caches/<folder>`), `/usr`, `/opt/homebrew`, `/etc`, `/Library`, toolchains', package managers' and editors' folders (`~/.cargo`, `~/go`, `~/.local`, `~/.docker`, `~/.oh-my-zsh`, `~/.npm-global`, `~/.bun`, `~/.deno`, `~/bin`, `~/.vscode`, `~/.cursor`, `~/.gradle`, `~/.m2`, `~/.ipython`, `~/.jupyter`, `~/.config`, `~/.cache` and the like), and on Windows `~/AppData` and the PowerShell profiles under `~/Documents`. Names compare with case ignored on macOS (`~/.SSH` is `~/.ssh`, `/USERS/<you>` is your home). A cache folder such as `~/Library/Caches/ms-playwright` is allowed, since that is where Playwright keeps its browsers, but tools run what is in a cache (Playwright's browsers, go-build's objects), so a thread can plant code there that runs as you outside the sandbox: the tab says so before adding one. The launch also leaves out one that holds or is in tm's folder, the project folder or a repository's git dir of the project, or whose links lead to such a place, and checks `PATH` again: the server's own and your login shell's, which the server asks your shell for once at its start (`$SHELL -ilc`), since a server started at login may have a minimal one. The allow list takes no rule that runs commands or writes files (`Bash`, `PowerShell`, `Edit`, `Write`, `MultiEdit`, `NotebookEdit`): an excluded command or a write folder does that. A `config.toml` written before still loads with such rules and they still work (a `Bash(...)` rule that covers an excluded command makes it run without asking, as it did); the tab marks them, and the move to this release's rules (above) turns them into excluded commands and write folders where it can; only a new one is refused.

`[defaults.sandbox]` gives every project the same; a project's lists add to all projects', and its `drop` takes one of all projects' entries away. Entries use Claude's Bash rule syntax: `npx playwright *` matches `npx playwright` with any arguments, while an entry without `*` matches only that exact command (`docker` takes `docker` run bare, never `docker ps`: write `docker *`; `tm access add`, the Access tab, `tm access show` and `tm access explain` say so beside such an entry); a call made of several commands (`a && b`), or with a redirect, a `cd` or a `$(…)`, stays in the sandbox unless every part matches. tm writes all of this into each thread's launch settings at every start and resume, so a change reaches a running thread at `tm thread restart`; coordinators never get it. A setting that doesn't apply on the server's platform stays in `config.toml` and is skipped at launch, with a line in the server log. An entry that fails its check (one written by hand) is refused on its own: the thread starts without it, and with the rest; the server log and `tm thread start` say which and why, and the tab marks it `✗`. Only you set these, from the Access tab or the file: `config.toml` is closed to agents' file tools and to the sandbox, and the tab refuses a change from inside an agent; each change is journaled.

What the lists open: **an excluded command runs outside the sandbox, with all your rights**, like an approved escape above: it reads and writes anything you can, reaches the network freely, and can reach the socket tm keeps for you alone. It does so even with `sandbox_escapes = false`. The guard still judges each call, and asks you first unless the command says `ask = false`. **An excluded command that runs without asking runs outside the sandbox with no prompt: if that command can run code (a test runner runs test files the thread can write, `python3 -c` runs whatever follows), the thread may run any code as you without asking.** The tab says so before letting one run without asking, and under each. tm's own deny rules still win over any allow rule. So keep entries narrow: how wide is yours to say. tm refuses only what can't mean anything or would turn the sandbox off whole:

- an excluded command that matches every command (`*`, `**`: that is the sandbox off, which yolo is for, asking first), or with shell syntax (a pipe, redirect, `;`, `$(…)`, a variable, a parenthesis), which never matches;
- a `Bash(…)` allow rule that matches every command or leaves the program open: `Bash`, `Bash(*)`, `Bash(*e*)`;
- an allow rule without a target for a tool that takes one, or one that matches every target: `Read`, `WebFetch`, `Edit(//**)`, `Read(~/**)`, `WebFetch(domain:*)`;
- tool names count whatever their case (`bash(*)` is `Bash(*)`).

And it warns, at the moment you add or change one (the tab asks first with it; `tm access add` and `edit` print it on stderr), never as a line under the entry in the list, about an entry that opens more than it seems, and then takes it: a program that runs other programs or code with that left open (`npx *`, `bash *`, `/bin/sh *`, `env sh *`, `sh -c *`, `python3 -c *`), a program left open (`*-*`, `python* -c x`: any program whose name fits), a whole tool or MCP server (`WebSearch`, `mcp__browser`), and every `Edit(…)` rule, which lets the thread change files there without asking and its sandboxed commands write there too, more plainly when it covers a whole tree high up (`Edit(~/Work/**)`) or files that start programs (`~/.zshrc`, `.git/hooks`, `~/.claude`), where writing lets the thread run code as you outside the sandbox. Codex has no per-session setting for the lists: its threads ignore them, and the server log says so at their start.

**Sockets.** `sockets = ["~/.colima/default/docker.sock"]` lets a thread's sandboxed commands connect to a unix socket besides tm's: a local database's, a dev server's, Docker's (macOS; Claude's `allowUnixSockets`, Codex's `unix_sockets`; Claude's Linux sandbox can't name one socket, and on Linux a Codex thread's network is open anyway). What it opens is whatever the program listening there does: it does it for the thread's commands **as you, outside the sandbox**. Docker's socket runs any container with any of your folders mounted, which is everything outside the sandbox; an ssh or gpg agent's signs and logs in with your keys. A socket is one file, its path written whole: no pattern, never a folder (that would open every socket below it), never in tm's folder or its run folder, so never the human socket or another session's; the launch refuses one whose link leads there, and one in a folder threads write (their temp folders, the project's write folders, the worktrees in tm's folder), where a thread could swap in a link to another socket after its start.

**Approved commands.** With escapes off a thread can't leave its sandbox, so what the project needs outside it (its test suite, an e2e run, `docker compose`) goes in `approved_commands`: commands a thread may ask tm to run for it, outside the sandbox, in its worktree:

```toml
[projects.web.sandbox]
approved_commands = ["make test", "make e2e", "npm run e2e *", "docker compose up *"]
```

The thread runs `tm run -- make e2e` (`tm run --list` lists them; its brief does too); tm runs it with its output streamed back and exits with its exit code. A command matches word for word, and a last `*` takes any further arguments (none too). tm runs the words as they are, **without a shell**: no pipe, redirect, substitution, variable or quote means anything, so the list takes none, nor a pattern inside a word (`make test*`), nor `tm` itself (also behind `env` or `sudo`), which would run as you outside the sandbox. A pattern that leaves open which program or code runs (`make *`, `go *`, `npx *`, `sh -c *`, `npm run *`, `prlctl *`) is yours to set: tm takes it, saying what it lets through when you add or change it (the tab asks first with it; `tm access add` and `edit` print it on stderr), not in the list afterwards; naming the target or script keeps it narrow. A last `*` passes **any argument the program takes**, its options too: `git log *` lets `--output=` write any file of yours, `go test *` lets `-exec` run any program, `docker compose *` lets `run -v /:/host` mount any folder; leave the `*` off where the exact words will do. tm runs it with its own environment, never variables the thread sets: the server's whole environment, as a session starts with it, plus the thread's `TERMINATR_*` variables and its own temp folder (not the project's secret variables, which tm gets for threads at their start). **So an approved command gets your tokens and keys as an excluded command does**: whatever the server's environment holds (`GH_TOKEN`, `AWS_*`, `NPM_TOKEN`, …), your ssh agent (`SSH_AUTH_SOCK`), your keychain, and every file you can read. The guard judges it as the thread's own Bash call (a force-push or a merge is refused there too), each run is journaled (`run.start`, `run.end` with the exit code, `run.refused`), and when the thread's call ends, its session ends or the server stops, tm kills what it started (its process group; a thread runs at most four at once). What runs is the thread's: a `tm` call from inside it is the thread's, never yours. Both hold only for what stays in the command's process group: a process that detaches from it (a daemon, `setsid`, a double fork, a container `docker compose up -d` starts) keeps running, and once it leaves the process tree tm takes it for you. So on **macOS** tm runs an approved command in a sandbox of its own, which allows everything but tm's own files: its run folder and its sockets' folder (the human socket among them), its keys, the guard's log and `config.toml`, which the command, and anything it starts, detached or not, can't read, write or connect to (so a `tm` call from inside it fails too). macOS lets nothing in a sandbox start a setuid program, so tm lets the command start `/bin/ps` and `/usr/bin/top` outside it (they only read the process list; `prlctl` needs `ps`); every other setuid program (`sudo`, `crontab`, `at`, …) fails with "Operation not permitted". A server that itself runs in a sandbox can't apply it, and the run fails rather than run without it. **On Linux and Windows an approved command isn't confined**: a process it detaches reaches the socket tm keeps for you, as you, as an excluded command's can. There is no time limit on a run: it lasts until it ends, the thread's call ends or its session does. An approved command that asks first (a Codex thread's excluded command) shows the whole command line in its question, and one longer than 1000 characters is refused rather than shown in part. Approved commands are read when the thread asks, so removing one holds at once. **What an approved command opens:** it runs **as you, outside the sandbox**, and the thread writes the files most commands read (the Makefile, `package.json`, the tests themselves), so a thread can make `make test` run any code as you: approve only what you'd let the thread run anyway. A Codex thread has no excluded commands of its own, so each of the project's excluded commands is an approved command for it (one written in the approved form: `npx playwright *` is, `npm run test:*` isn't), and one that asks first asks you each run (a question on the thread's pane, which only you answer, never the coordinator).

**What a thread actually has.** `tm access show <project>` lists, in one table, what a new thread of the project gets: tm's built-in rules (its worktree, the read-only project folder, the git-dir grant, named by its repo, tm's socket, and for a Codex thread each host of its repo's git remotes, "git remote host"), the sandbox rules (all projects' that the project drops listed as dropped, and an entry that fails its check as refused), protection, the sandbox, yolo and escapes, the guard's settings and the secret variables (names and sources, never values), each with where it comes from: tm, all projects, the project or tm's default. A rule that doesn't apply on the server's platform is listed as not used rather than hidden; so is, under yolo, every rule that opens or leaves the sandbox (yolo means no sandbox and no prompts, for Claude and Codex threads alike), and the guard's settings but its floor ("floor only: yolo is on"). Below protection sandboxed a Claude thread's sandbox rules show as not used the same way ("the sandbox is off"), and below guarded the guard's settings but its floor. The floor holds under yolo too: no agent writes `config.toml`, no thread writes the project folder. Every thread start, restart and resume keeps what it launched with in `threads/<id>/access.json` (only tm writes it); `tm access show <project> --thread t-0042` lists that, marks RESTART TO APPLY on each rule `config.toml` added or removed since (the guard's rules are read live and need no restart), and adds the rules of the agent's own settings files (`~/.claude/settings.json`, the worktree's `.claude/settings.json` and `settings.local.json`, where "Yes, don't ask again" answers land), which tm only shows. An excluded command in those files (`sandbox.excludedCommands`) runs outside the sandbox without tm's say, so the guard treats a command it takes, and none of the project's excluded commands do, as an escape: it asks you, or with escapes off refuses it; the view says so beside each. `tm access explain <project> -- npx playwright test` says whether a thread runs that command inside or outside the sandbox, whether it asks first, and what the guard says. Both take `--json`; a thread may run `tm access show` for its own access only. In the console, `E` on the dashboard opens the same view for the selected thread (or, elsewhere, for a new thread of the project), and so does `E` on a project popup's Access tab. Its Built in row lists tm's own rules alone.

### The guard

The guard checks every tool call of a project's agents before it runs, in every permission mode, and refuses the ones that break the standing rules: force-pushing, pushing straight to the default branch, a thread writing outside its worktree, deleting branches or worktrees, a thread merging its pull request (while the coordinator merges), and reading credentials. The agent reads why and what to do instead; the project's journal records each refusal briefly (rule, tool and a short account, never the whole command).

**Settings.** A project's Access tab (`A`, then `5`) and all projects' (`,`, then `3`) have the guard's rows, under Guard, and the secret environment, under Secrets:

- **Guard**: on or off. Off, the agents have only their own permission rules and the sandbox. Turning it off asks first.
- **Guard mode**: *refuse* (the default) or *refuse, then ask you*, the learning mode. Either way the call is refused and nothing passes until you allow it. In learning mode the agent is also told you review refused calls, and each new refusal is put to you in the project's inbox.
- **Guard rules**: each rule on or off. A rule turned off still never lets through what is never allowed (below).
- **Guard allowances**: the calls the guard lets through though a rule would refuse them, an exact one (below) marked so. Enter changes one, `X` removes it. A project's own add to all projects'.
- **Guard refusals**: the calls refused lately, the latest first, with the whole command or file (enter shows a long one whole). `A` allows it for this project or for all projects (it asks which), `K` keeps refusing it and stops listing it. An allowance takes effect at once, is journaled, and the thread that was refused is told it may retry.
- **Secret environment**: the variables threads get when they start (below), by name and where the value comes from; a project's list shows all projects' too, marked. `A` adds one: its name, then its source (gh auth token, a login keychain item, or a variable of the server's environment, which asks the item or variable's name). `X` removes the selected one, from all projects' when it is theirs. `T` tests it: the server gets the value from its source as a thread's start would and says only whether that worked, or why not (gh not logged in, no such keychain item, the server's environment lacks the variable); the value is never shown. A change applies to threads started from then on.

The whole commands are kept in tm's state folder, which no agent or mod can read, not in the journal.

**Allowances by hand.** They are `guard_allow` in `~/.terminatr/config.toml`, under `[defaults]` or a project's table:

```toml
[projects.web]
guard_allow = [
  { rule = "force-push", command = "git push --force-with-lease origin tm/*" },
  { rule = "credentials", path = "~/.config/acme/registry.json" },
]
guard_mode = "learn"
```

`rule` is `force-push`, `push-default`, `worktree-only`, `delete-branch` or `credentials` (or a mod's rule, `mod:<mod>.<rule>`); `command` is the whole command line, `path` a file. A file write outside the worktree isn't an allowance any more: a write folder (above) opens the folder for the guard and the sandbox alike, so Guard refusals won't allow one, and `worktree-only` leaves the Guard rules list while it is on (a `config.toml` written before keeps its path allowances working; the move to this release's rules makes them write folders where it can). A `*` matches anything, so a command or folder ending in `*` is a prefix; without one it is that exact command or file. With `exact = true` a `*` is just a `*`: an allowance written from **Guard refusals** is exact, so allowing `cp build/* /opt/app/` allows that one command, not every command that starts and ends like it. A credentials allowance names one exact command or file, no `*`.

**What is never allowed**, in either mode and whatever the rules (Guard rules turned off included) and allowances say, while the guard is on: reading tm's own files (its run folder and sockets, its keys, the guard's log), printing a secret variable tm gives threads (below), force-pushing to a protected branch, and a thread merging (set `merge = "thread"` for that instead).

**Secret environment for threads.** A thread's tools sometimes need a token the agent must not see, such as a private npm registry's. Add it from the **Secret environment** row of the project's Access tab, or all projects' (above), or name it in the project's table, or under `[defaults.env]` for all projects:

```toml
[projects.web.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }         # the output of gh auth token
NPM_TOKEN = { from = "keychain:npm.acme.example" }   # a login keychain item (macOS)
ARTIFACTORY_TOKEN = { from = "env:ARTIFACTORY_TOKEN" } # the server's own environment
```

tm keeps only the name and the source, never a value: it gets each value when a thread starts and puts it only in that thread's environment, so `npm ci` with an `.npmrc` such as `//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}` works. The value never goes into a prompt, a report, a log or the journal, and the guard refuses printing it (`printenv`, `echo $NAME`, `env`). Coordinators don't get it. If a source fails (gh not logged in, no such keychain item), the thread starts without that variable and the server log names it and why. Test it from the settings row (`T`) to find out before a thread needs it. For the `env:` source, the server's environment is the one it started with: export the variable before `tm server restart`. `tm doctor` warns when a project repository's `.npmrc` reads a registry token from a variable its threads don't get.

## Running a relay

Two tm servers are joined through a relay that both dial out to (SPEC §16), so neither machine needs an open port. You run the relay yourself, on a host both machines can reach: a small VPS, or a NAS at home. It is `tm relay serve` from the same binary; it stores nothing and sees only key fingerprints, IP addresses, sizes and timing, never what the servers say to each other (that is TLS with keys pinned at pairing, inside the stream).

**What the relay can see, and what it can do.** A relay sees the IP address of each server that connects, which two servers are paired (each link's two key fingerprints) and when each is online, a hash of the code while two servers pair (never the code), and the timing and size of what passes. It never sees names, projects, messages or pane content. Someone who breaks into it, or runs it against you, can cut links, slow them or kick servers off; they can't read or change what the servers say to each other, pretend to be one of them, or pair a stranger in. So the cost of a bad relay is that your servers stop reaching each other, plus the facts above about who connects and when. Run it on a host you trust, and leave `--log-ips` off so it doesn't write addresses to its log. Its `/healthz` page answers `ok` and no more: it doesn't say which tm release the relay runs. A server linked through the relay sees the release (the Network popup and `tm doctor` show it).

What it needs:

- **A public address and a DNS name**, or an IP address both servers reach (below: [Without a domain](#without-a-domain-an-ip-address)). Both servers dial `wss://<name>`, often from outside your home network (a laptop on hotel Wi-Fi). A VPS has both. A **NAS at home doesn't by default**: give it a DNS name (your own domain, or a dynamic DNS name that follows your home IP) and forward a public port on the router to it (443, or any port: the link then dials `wss://<name>:<port>`). Behind carrier-grade NAT (no public IPv4 on the router) port forwarding can't work: use a VPS, or IPv6 if both ends have it. Without a public port the relay only serves machines on your LAN.
- **A certificate for that name**, from a CA: the link checks it as any HTTPS client does. The relay gets one from Let's Encrypt by itself (`--domain`); it protects the connection to the relay, while what the servers trust in each other is their pinned keys, not this certificate.
- **Port 443 open**, inbound TCP. The alternatives with Caddy or certbot also need port 80 while a certificate is issued or renewed: Caddy's ACME, `certbot --standalone` and `certbot --webroot` all answer the CA's HTTP challenge there.
- A `tm` release that has `tm relay serve`.

### One command (a Linux VPS)

On a Linux host with systemd (a VPS from any provider), with the DNS name pointing at it and port 443 open in its firewall:

```sh
curl -fsSL https://github.com/theclifmeister/terminatr/releases/latest/download/install.sh | sh
sudo ~/.local/bin/tm relay install --domain relay.example.net --email you@example.net
```

The first line is tm's usual install (into `~/.local/bin`, which `sudo` doesn't search, hence the full path); skip it if `tm` is already there. The second copies `tm` to `/usr/local/bin/tm`, writes and starts the `tm-relay` systemd service (`deploy/relay/tm-relay.service`: a throwaway user with only the right to bind 443, and a strict sandbox), and waits for `https://relay.example.net/healthz`. The relay gets its certificate from Let's Encrypt itself, by TLS-ALPN-01 on port 443 (no port 80, no certbot), keeps it in `/var/lib/private/tm-relay` and renews it before it expires; `--email` (optional) is where Let's Encrypt sends expiry warnings, and using it means accepting their [subscriber agreement](https://letsencrypt.org/repository/). It ends with the line to pair with:

```
The relay is up at wss://relay.example.net. Pair two servers through it, on one of them:

    tm peer pair --relay wss://relay.example.net
```

tm never changes the firewall. If `/healthz` doesn't answer, install says so, with what the name resolves to: open TCP port 443 to the host (your provider's firewall, and `sudo ufw allow 443/tcp` or firewalld if the host runs one), fix the DNS, and the relay gets its certificate on the next try by itself (`curl https://relay.example.net/healthz`, `journalctl -u tm-relay`).

To move to a new release: `sudo tm relay update` downloads the latest one for this machine's architecture, checks it against the release's `checksums.txt`, replaces `/usr/local/bin/tm`, restarts the relay and checks `/healthz` again; linked servers drop for a moment and reconnect by themselves. A tm that is already the latest changes nothing and isn't restarted. A tm that is *newer* than the latest release (a development build) or has no release version is refused, since the release could lack flags the relay's unit uses; `sudo tm relay update --force` installs the release anyway. `sudo tm relay uninstall` stops the service and removes it and its update units; `/usr/local/bin/tm` and the certificates stay (`sudo rm -r /var/lib/private/tm-relay`). Running install again (another `--domain`, say) rewrites the unit and restarts. Logs: `journalctl -u tm-relay`.

Without systemd or on another system, `tm relay serve --domain relay.example.net` does the same in the foreground (certificates in `~/.terminatr/relay`, or `--state DIR`); it needs to bind 443, so as root or with `CAP_NET_BIND_SERVICE`.

### Keeping the relay up to date

Install also sets up three systemd units next to `tm-relay` (`deploy/relay/tm-relay-update.*`):

- `tm-relay-update.service` runs `tm relay update` as root, as above.
- `tm-relay-update.timer` starts it **daily** (at a random time within the hour after midnight; a day the host was off is caught up at boot). This is **on by default**; `sudo tm relay install … --auto-update=off` leaves the timer disabled, and installing again with `--auto-update=on` turns it back on. A relay installed before this release has no timer until you run install again.
- `tm-relay-update.path` starts it when **a linked server asks**: in the Network popup (`N`), each paired server's details show its relay's version (`relay.example.net · v0.27.0 · update (U)` when the server knows of a newer release), and `U` on the server asks that relay to update to the latest official release; `tm peer relay-update ALIAS` does the same from a terminal.

How a request works, and who may make one: the server signs it with its own identity key, as it does its link's hello, and the relay takes it only from a key whose link through it is up at that moment. **So anyone who can use the relay (any server linked through it) can trigger an update**: a key whose link has been up for 10 minutes and carried traffic, at most once an hour per key and once every 10 minutes from all keys together (more often is refused with when the next is taken). A request can only mean "update to the latest official release": it carries no version and no URL, and the update checks the release's checksums and restarts only for a newer release, so it can't be used to install something else or to restart the relay in a loop. The relay itself stays an unprivileged throwaway user: all it does is write `update-request` into its state directory, and root's path unit does the rest; `tm relay update` removes the file first, so a failing update isn't retried until someone asks again. `journalctl -u tm-relay` logs each request (the first bytes of the asking key's fingerprint and what came of it), `journalctl -u tm-relay-update` each update. Don't want remote requests? `sudo systemctl disable --now tm-relay-update.path` (installing again turns it back on).

### Without a domain: an IP address

A relay can run on an IP address with no DNS name and no certificate authority: a VPS's public IP, a home server's `192.168.x.x`, or a Tailscale `100.x` address. It makes its own key and a self-signed certificate for that address (valid ten years; nobody checks its dates), and the servers check it by its key alone: the relay's address ends in the SHA-256 of its public key, `wss://203.0.113.5#sha256=<64 hex digits>`, and a relay that shows another key is refused. On a Linux host with systemd:

```sh
sudo ~/.local/bin/tm relay install --ip 203.0.113.5
```

Without an address, `--ip` takes this host's own (the one it reaches the internet from), and says so: behind NAT, or for a tailnet, name the address the servers reach it at. Install makes the key in `/var/lib/private/tm-relay`, writes and starts the service (`tm relay serve --self-signed --ip 203.0.113.5`), checks `/healthz` by the key and ends with the address to pair with:

```
The relay is up. Its address, with its key's pin:

    wss://203.0.113.5#sha256=9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08

Pair two servers through it, on one of them:

    tm peer pair --relay wss://203.0.113.5#sha256=9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
```

The pairing code then carries the pin (`203.0.113.5#sha256=…/7Q4M-K2XD-9HVA-PT3C`), so the joining server learns it without typing it; it needs a tm that knows pinned relays (this release or later; an older one refuses the code as not a relay). Each server keeps the address with its pin in `peers.json`.

What to know:

- **Both servers must reach the IP on TCP port 443.** A private address works only where that network reaches: both on the same LAN, or both on the same tailnet. tm warns when you pair through one.
- **Back up `/var/lib/private/tm-relay`.** It holds the key; `sudo tm relay update`, restarts and installing again keep it, and the certificate is issued again with the same key when it needs to be (a new address, or its end near), which changes nothing for the servers. A relay set up again with a new key, or a new address, is refused with *the relay at … has a different key than when you paired*: give both servers its new address with `tm peer relay ALIAS wss://…#sha256=…` (or the relay row in the server's details in the Network popup, `N`), on each side.
- The same `#sha256=` works with any name or address that reaches the relay: the servers check the key, not the name.
- To check the fingerprint by hand: `sudo openssl pkey -in /var/lib/private/tm-relay/self-signed.key -pubout -outform der | sha256sum`.

`tm relay serve --self-signed [--ip ADDR] [--state DIR]` does the same in the foreground (key in `~/.terminatr/relay`, or `--state DIR`) and logs the address with its pin when it starts. Without `--ip` it takes the address it listens on (`--listen 192.168.1.20:8443`), else this host's own.

On a NAS, or any host with Docker, `compose.selfsigned.yaml` runs it in a container, on port 8443 ([Docker](#docker-vps-or-nas) below).

### Alternatives

#### Docker (VPS or NAS)

The relay image is `ghcr.io/theclifmeister/tm-relay`, one per release (`:v0.46.0`, and `:latest`), for amd64 and arm64 (most NAS boxes are one of them); it runs as uid 65532, not root, and listens on 8443 inside. The compose files for it come with each release:

```sh
curl -fLO https://github.com/theclifmeister/terminatr/releases/latest/download/tm-relay-docker.tar.gz
tar xzf tm-relay-docker.tar.gz && cd tm-relay-docker
```

The folder has three compose files, each pinned to that release, and a short README. Settings go in a `.env` file next to them, which `docker compose` reads by itself.

**On an IP address, no domain** (`compose.selfsigned.yaml`), as in [Without a domain](#without-a-domain-an-ip-address): the relay makes its own key in `./state` and servers check it by its pin. Good for a NAS or home server on your LAN or tailnet:

```sh
mkdir state && sudo chown 65532:65532 state
echo RELAY_IP=192.168.1.20 > .env            # the address the servers reach the relay at
docker compose -f compose.selfsigned.yaml up -d
docker compose -f compose.selfsigned.yaml logs relay
```

The log shows the address to pair with, with its pin: `wss://192.168.1.20:8443#sha256=…`. It is published on port 8443, so it doesn't take the NAS's own 443. Back up `state/`: it holds the key. On a NAS where the folder belongs to your own user (Synology: create `state` in File Station), skip the `chown` and add `RELAY_USER=uid:gid` to `.env` instead: your `id -u` and `id -g`, for example `RELAY_USER=1026:100`.

**With a domain, Caddy getting the certificate by ACME** (`compose.acme.yaml`; needs ports 80 and 443 and the DNS name pointing at the host):

```sh
echo RELAY_HOST=relay.example.net > .env
docker compose -f compose.acme.yaml up -d
```

**With a domain and certificate files you already have** (`compose.yaml`; certbot, your NAS's own Let's Encrypt, a wildcard): put `fullchain.pem` and `privkey.pem` in `certs/`, readable by uid 65532, then `docker compose up -d`. The relay reads the files again when they change, so a renewal needs no restart; copy renewed files in from your ACME client's deploy hook (certbot's `live/` files are symlinks with a root-only key, so copy, don't mount them).

**Upgrading**: put the new release's tag in `.env` (`TM_VERSION=v0.46.0`) and run the same `up -d`; compose pulls the new image and restarts the relay, and connected servers drop and reconnect by themselves. The key in `state/` stays, so nothing changes for them.

**Building the image yourself** instead of pulling it: uncomment the `build:` lines in the compose file and add `--build` to `up -d`. The `Dockerfile` downloads the release's Linux binary and checks it against the release's `checksums.txt`, as the installer does.

On a NAS (Synology Container Manager, QNAP Container Station, Unraid, TrueNAS apps): create a project in the `tm-relay-docker` folder from the compose file you chose. If the NAS's own web interface holds ports 80 and 443, use `compose.selfsigned.yaml`, or publish the relay on another port (`"8443:8443"`, forwarded on the router as 8443) with the NAS's reverse proxy or your own certificate files instead of Caddy.

#### systemd with certbot's certificate

If certbot already manages a certificate for the name, install the service as above, then hand it certbot's files instead (`sudo systemctl edit tm-relay`):

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/tm relay serve --listen :443 --cert ${CREDENTIALS_DIRECTORY}/fullchain.pem --key ${CREDENTIALS_DIRECTORY}/privkey.pem
LoadCredential=fullchain.pem:/etc/letsencrypt/live/relay.example.net/fullchain.pem
LoadCredential=privkey.pem:/etc/letsencrypt/live/relay.example.net/privkey.pem
```

systemd hands the relay the certificate (read as root, so certbot's root-only key works). After a renewal restart it: `certbot renew --deploy-hook "systemctl restart tm-relay"`.

Install the service before you issue the certificate. If certbot runs first with `--deploy-hook "systemctl restart tm-relay"`, the hook fails on the first issue with `Unit tm-relay.service not found`; that is harmless (the certificate is issued and saved), and the restart after `systemctl edit` starts the relay with it.

### Updating a systemd relay by hand

`sudo tm relay update` does this (any `tm-relay` unit, certbot's too). By hand: download the release tarball for the host's architecture (`amd64` or `arm64`) and `checksums.txt` into an empty folder, then:

```sh
ARCH=amd64                                       # or arm64
V=vX.Y.Z                                         # the release tag
curl -fLO https://github.com/theclifmeister/terminatr/releases/download/$V/tm_linux_$ARCH.tar.gz
curl -fLO https://github.com/theclifmeister/terminatr/releases/download/$V/checksums.txt
grep " tm_linux_$ARCH.tar.gz\$" checksums.txt | sha256sum -c -    # must say OK
tar xzf tm_linux_$ARCH.tar.gz tm
sudo install -m 755 tm /usr/local/bin/tm
sudo systemctl restart tm-relay
```

Paired servers drop for a moment and reconnect by themselves; nothing to redo on them. (Docker: see Upgrading above.)

### Checking it

```sh
curl https://relay.example.net/healthz     # ok
```

The log (`docker compose logs relay`, or the journal) shows `serving on …`, rooms as they mate and connections as they leave, by room kind and a short room id, with bytes and duration; client addresses only if you add `--log-ips`. A relay on a public address is found by scanners within hours, and they fail the TLS handshake by the thousand (no server name when they dial the bare IP, plain HTTP on the TLS port, a hang-up or timeout, a TLS version or cipher it doesn't take). Peers never fail that way, so the relay doesn't log these one by one: it counts them by kind and logs one line an hour, only for an hour that had any, such as `tls: 1043 handshakes refused in the last hour from 212 addresses (no server name 980, plain http 63)`, and what it counted so far when it stops (`in the last 23m`). The line is noise you can ignore; it never names the addresses, even with `--log-ips`. A handshake error of another kind (a certificate the relay can't load, Let's Encrypt refusing it) is logged as it is, at most 10 a minute, the rest counted as `other` in the hourly line. Limits (SPEC §16.4) can be raised with `--max-conns`, `--max-per-ip`, `--bandwidth` (MiB/s per address) and `--total-bandwidth` (MiB/s for the whole relay, 16 by default); the defaults are generous for a few servers. Without `--max-conns` the relay holds no more connections than its memory limit fits (the unit's and the image's `GOMEMLIMIT=200MiB` fits the default 1024) and logs the lower number when it takes one. The relay doesn't know which keys are tm servers, so anyone who finds it can open rooms of their own: the limits keep that to a share of its connections and bandwidth, and `--total-bandwidth` is what bounds the traffic it can cost you.

Behind your own reverse proxy (nginx, Traefik, the NAS's), run the relay with `--plain --listen :8080` on a private address, have the proxy pass WebSockets to it, and name the proxy with `--trusted-proxy <its address or subnet>`, so the relay's limits count real clients and not the proxy.

### Pairing two servers

With the relay up, pair the two servers once, each of you in your own terminal or console (an agent can't). In the console it is the Network popup (`N` on the dashboard, `prefix+N` over a session): `P` on one server asks for the relay and shows the code, `J` on the other takes it, and both show the six digits to compare, with `Y` or `N`; `esc` cancels. In a terminal, on one (here `studio`):

```
$ tm peer pair --relay relay.example.net --as mini
Made this server's identity key: server id 4F2A-91C3
Pairing code (works once, for 10 minutes):

    relay.example.net/7Q4M-K2XD-9HVA-PT3C

On the other server: tm peer join relay.example.net/7Q4M-K2XD-9HVA-PT3C
```

Pass the code on yourself, in a chat or a call; it never goes through the relay, which can't read it. On the other server (`mini`), within 10 minutes:

```
$ tm peer join relay.example.net/7Q4M-K2XD-9HVA-PT3C --as studio
```

Both terminals then show the other server's name and id and the same six digits, `Check: 418 207`. Compare them (read them aloud, or paste them in the chat) and answer `y` on both sides only when they match; different digits mean someone else is in between, so answer `n` and make a new code. Each side pins the other's key once both said yes. `--as` names the other server here (by default its host name); the code is the relay's host and 16 letters and digits, and case, hyphens, `O`/`0` and `I`/`L`/`1` don't matter when you type it. A code works once; a join that says nobody is pairing with it was mistyped, has expired or was used. Later pairings use the same relay without `--relay`.

A relay without a domain pairs the same way, with its whole address: `tm peer pair --relay wss://203.0.113.5#sha256=…` (see [Without a domain](#without-a-domain-an-ip-address)). When a relay moves or gets a new key, `tm peer relay mini wss://…` (or the relay row in the Network popup) gives a paired server the new one, on each side; nothing else changes.

To call a paired server something else here, give it a display name: `tm peer name del-02-0468-lt work` (or `E` on it in the Network popup). Its alias stays as it was, and either names it from then on: `work/gatehouse` and `del-02-0468-lt/gatehouse` are the same project in `tm link`, `tm project link` and every `tm peer` command, and screens and agents see `work`, with `work (del-02-0468-lt)` where both help. The name can't be another paired server's alias or name, this server's own name or a project's here; `tm peer name work ""` clears it. It stays on this server: the other side never sees it.

`tm peer list` shows this server's id and each paired server with its link; `tm peer remove mini` drops one at once (remove this server on the other side too). The console has the same list in its Network popup (`N`): each server's name, id, connection and relay, with the projects it shares; `X` unpairs the selected one after asking, and `P` and `J` pair as above. A click on a server's header in the sidebar opens the popup on that server. `tm network` prints the same tree in a terminal, read-only: this server's projects and whether each is shared, their links with both sides and their health, each paired server with its connection, panes both ways and the projects it shares, and what needs you, each with the command that answers it (`--json` for scripts). `tm needs` lists everything that needs you in one place, the dashboard's NEEDS YOU across every project plus the Network's needs, each with the command that answers it (`--json` for scripts and the Obsidian plugin); from a terminal you can answer the coordinator's side of it too: `tm task accept T12` and `tm task send-back T12 --note "…"` are the Tasks tab's `A` and `X`, and `tm ask answer Q3 --choice 2` answers one of the coordinator's open questions as its question dialog would (`--other "…"` in your own words). On screen a server's connection is `connected 38 ms` (the sidebar, the remote dashboard and panel, the status bar) and a link is always a project's; `tm peer list` and `tm network` keep the link's own words (`up 4m · 38 ms`), and a link's files read `ask me`, `take them` or `refuse` on screen and `ask`, `auto`, `off` in the config and the CLI. `tm peer rotate` gives this server a new key, for a key you think was copied: every paired server then refuses it until you pair again with each (`tm peer pair --as mini` here, `tm peer join` there).

### Sharing a project

Pairing alone shows nothing of either server's projects. To let the paired servers see a project, turn on **Shared with paired servers** in its Settings tab (`A`, then `4`), or press `space` on it under this server in the Network popup (`N`; or a click on its box in the details), the same setting; it asks first. They then see its state, never its files: what waits for you, the inbox, the threads with their state and PR, the tasks on deck and its sessions' states and models. Never its files, paths, repos or worktrees, and nobody there can act on it. A server you also let view your panes (`tm peer panes <alias> view`, or `write`; below) may also open the project's popup, read-only: its board (titles, states, threads and how many steps are done, never the notes), its settings that say how it runs (never secrets, paths, the branch name template or the guard's allowed commands), and its links with that server's projects with the headers of the messages that reached it (never their text, and never a message still waiting for your OK or queued over the rate). Nothing of the project's memory crosses, not even its notes' titles. With panes off it can't. Only an active project is offered; turning the setting off hides it at once. Each project is shared on its own: all projects can't be shared in one go (SPEC §16.9). Your server keeps the latest of what each paired server shares in memory, keeps showing it while a link is down (marked stale, with when it was last read) and refreshes it as soon as the link is back (SPEC §16.10).

If a paired server shares nothing with you yet, ask its user to share one, instead of asking in a chat:

```
$ tm peer ask-share mini --for acme --note "the API docs"
asked mini to share a project (S-4F2A-33, for acme); its user answers there; tm peer requests shows it
```

(`S` on that server in the Network popup does the same: a menu of which of your projects it is for, then an optional note; the request then waits under `mini` as `asked to share a project (S-4F2A-33, for acme) · waiting on mini's user`.) A request asks; it never shares. On `mini` it waits for its user, in fixed words with your note quoted as data (`studio asks you to share a project with it, for its acme: "the API docs"; answer in Network (N)`), counted in the dashboard header's `⚑` and listed by `tm peer requests`. It is the server's, so no project's inbox gets it and no coordinator acts on it. `mini`'s user sees it flagged under `studio` in the Network popup, and in the all-projects NEEDS YOU, and accepts there with `A`, picking one of its active projects that aren't shared and then `Y` in the usual share confirm, which shares it as the Settings tab does, or declines (`X`, or `tm peer decline S-4F2A-33`); the CLI can't accept, since sharing is a safety setting the console writes on a keypress. A project shared that way shows under `mini` as any shared project does. One request waits per paired server (`request-pending`), at most 5 go to or come from one server an hour, the note is one line of at most 120 bytes, and an unanswered request expires after 7 days. A server older than tm 0.50.0 doesn't take share requests: `tm peer ask-share` refuses at once when its link says so (`old-tm`), else the request comes back undelivered (`mini runs a tm before 0.50.0, which doesn't take share requests; ask its user yourself`). `tm peer requests [--json]` lists the share and link requests that wait, both ways, with when they expire (SPEC §16.9).

A shared project shows in the other server's sidebar under a header for this server, and `enter` (or a click) on it there shows its dashboard: the header says where it is (`tm dashboard · mini › acme-api`), the link and how fresh the copy is (`◉ mini · connected 38 ms · synced 2s ago`, with `· view-only` after it when that server's panes are view-only for this one and `· panes writable` when they take keys), then what waits for its user, its inbox, threads and tasks on deck. Nothing there acts on the other server: the keys that would (open, activate, save and clear) aren't in its key bar and say `nothing here acts on it`, with the prefix too; the project popup (`A`, `T`, `I`, or after the prefix, there or in that server's pane) opens **that server's popup** of the project, titled `mini's acme-api`, when that server lets you view its panes: its overview, inbox, tasks, settings and messages with your projects (the same list and dialog as your own Messages tab, of the headers that reached you: no text, sent or read), read from it each time it opens and at each tab change, and read-only: a key that would change something says `change it on mini`. When that server runs an older tm, has its panes off for you, or is offline, the key says `no project popup here` instead: your messages with it are in the Messages tab of your project linked to it. To ask the other side's coordinator something, link a project of yours to it (below). While the link is down the dashboard keeps the last copy, dimmed, with when it was last seen.

### Linking projects across servers

A project's coordinator can write to a project on a paired server once both users link them, each on their own server. On `studio`:

```
$ tm project link acme mini/api
acme ⇄ mini/api: linked both ways on this side
```

and on `mini`, `tm project link api studio/acme`. The name is the other server's alias here, or the display name you gave it (`tm peer list`), and the project's slug there. Each side is written separately, since neither user can set the other's; a message goes only when both allow it, and a side that doesn't answers with a `link-undelivered` item in the sender's inbox. On a link to another server each of the coordinator's messages waits for your OK (approve it in the Messages tab, or tell the coordinator in chat) unless you link with `--no-ask`.

In the console it is the same dialog as a link here: in the Network popup, `L` on `acme` lists each paired server's shared projects after this server's (and last `a server not paired yet`, which pairs first), or `L` on `api` under `mini` lists your projects. The dialog writes only your side, with ask first on, and its **ask mini's user** field also asks them to link back. The link then shows under `acme` with what is known of their side (`waiting on mini's user`, `linked back`, `mini offline`, `not shared any more`), and `api`'s row under `mini` shows `⇄ acme`.

Instead of telling the other user what to type, you can ask them from tm: **ask mini's user** in the dialog, or `R` on the link in the Network popup, sets your side (both ways, asking first, when it has none) and sends them a link request, or from the command line:

```
$ tm project link acme mini/api --request
asked mini/api to link back (R-4F2A-18, both on this side): mini's user accepts or declines it there; tm link list shows the answer
```

On `mini` it shows in NEEDS YOU (`a link request … from studio/acme`, `enter` opens the Network popup at it), under `api` in the Network popup and in `api`'s Messages tab, as a ⚑ row: `A` offers the directions for `api`'s side (the mirror of yours first: both ways, or receive only when you asked to send only) with or without ask first, and writes only `mini`'s config; `X` declines it and changes nothing. `tm link accept R-4F2A-18 [both|send|receive] [--ask|--no-ask] --project api` and `tm link decline R-4F2A-18 --project api` do the same. Coordinators can't answer one; they tell you. The answer comes back to `studio`: the link's row in the Network popup says `linked back` or `declined`, and `acme`'s inbox gets a `link-answer` item. Only a shared project takes requests (one that isn't answers `not-shared`), one waits per pair, at most 5 go to or come from one server an hour, and an unanswered one expires after 7 days. A server older than tm 0.28.0 refuses requests (the item says so); set that side there by hand.

Messages wait in the sending server's outbox until the link is up, and `tm link log mini/api` shows each as `queued`, `sent` (the other server has it) or `read` (its coordinator handled it). One not taken within 72 hours is dropped with a `link-undelivered` item. A long text can go as parts: over the rate they wait (`queued over the rate, goes about 15:04` in the log) and go in order.

A file goes with one message, as between projects here: `tm link send mini/api "the report" --file t-0042/report.pdf` (a library file, or `M-4F2A-17/<file>` to pass on one a message brought; you may also name any file of yours by its path, a coordinator can't). Nothing leaves without the other side's consent: `mini`'s user gets a `link-file` item (`studio/acme offers a file with M-4F2A-17 (report.pdf, 4.2 MB)`) and accepts it with `A` or declines it with `X` in its message's dialog in the Messages tab, or `tm link accept|decline X-4F2A-18`; its coordinator can't. Each side sets its own link's files with `F` on the link in the Network popup (ask me, take them, refuse) or `tm project link acme mini/api --files ask|auto|off`: `ask` (the default) asks as above, `auto` takes files without asking (setting it asks you first), `off` refuses them; the change also answers the offers already waiting on that link (auto takes them, except one over `link_file_mb`, which keeps waiting; off declines them), and so does a hand edit of `files` that the server picks up. An offer unanswered for 7 days expires. Accepted, `mini` pulls the file over the link that is already up, nothing changes on the relay, at most `link_file_mb` (25 MB by default, up to 200; a file over the receiving side's size is refused) at `link_file_kbps` (1 MiB/s by default, 64–2048), slowing on its own while you type in a remote pane; a dropped link or a restart resumes from what arrived. The file is checked by its SHA-256, lands beside its message under `messages/@studio/acme/<M-id>/` on `mini`, never in a worktree, and shows in its Library tab under `from studio/acme`; tm never opens or runs it. `tm link files` lists each transfer and its state (`offered`, `receiving 40%`, `received`, `declined`, `failed: stalled` …) and `tm link cancel X-4F2A-18` (or `C`) stops one. A server older than tm 0.47.0 takes no files: the message comes back `undelivered` and its item says to send the text or update that server (SPEC §16.13). Each server keeps its own link limits and breaker; when the other server's rate is lower it answers retry and the outbox tries again when it says, nothing lost, and `tm link resume` lifts only this server's pause (SPEC §16.11).

A message waiting over the rate can be taken back: `X` in its dialog in the Messages tab (after a Y) or `tm link drop M-21` removes it from the queue and marks it `dropped`; the others keep their order. Once it has been sent, or handed to the outbox for a paired server, it can't be dropped.

### Remote panes

You can open a paired server's coordinator or thread in your console and type into it, as if it ran here. The other server decides: on `mini`,

```
$ tm peer panes studio write
Panes write for studio: it can watch and type into the coordinator and thread panes of the projects this server shares; the first keys into a thread tell its coordinator.
```

(`view` lets `studio` watch without typing; `view` and `write` also let it open your shared projects' popups, read-only (above); `off`, the default, shows nothing; the Network popup (`N`) sets it too: `V` on the server, or `←` `→` or a click on its panes switch in the details. `write` was `type` in tm 0.27.0, which `tm peer panes` still takes; a 0.27.0 server pairs and works with a newer one either way. A change reaches panes already open on `studio` at once.) Only projects `mini` shares cross. On `studio`, the sidebar's `mini` group then opens a remote coordinator or thread with `enter` or a click, and `tm attach mini/s-41` opens one by its id there. The status bar starts with `mini ›` and says `remote · connected 38 ms` (`view-only` at `view`, where the wheel still scrolls the pane here and a drag copies, and with `mini`'s size when it isn't your window's: `view-only · 120×40 on mini`); `prefix+R` asks `mini` for a fresh repaint if the pane ever looks off. Typing into a thread tells `mini`'s coordinator, as typing into one of your own threads does. While the link is down the pane keeps its last screen, your keys are dropped (never sent later), and it comes back by itself. Actions on the session (stop, model, remote control) stay on `mini`. Both servers can run different tm releases, as long as both have remote panes (SPEC §16.12).

### The servers' side

Each paired server keeps a link to the relay by itself; nothing to start. `tm server status` shows the server's id and each link:

```
server id 4F2A-91C3 (4f2a91c3…)
peer      mini  up 3m0s · 42 ms · tm 0.27.0 · relay relay.example.net
peer      nas  offline 4m0s · relay · dials again in 30s · relay relay.example.net
            dial tcp: connection refused
```

`offline · peer` means the relay is fine and the other server isn't connected (stopped, asleep, or not paired back); `offline · relay` that the relay can't be reached or closed the link (check `/healthz` and its log); `refused` that the keys don't match (pair again). A link dials again by itself, from 1 s up to once a minute. `--json` has the same as fields (SPEC §16.7).

## Obsidian: the projects folder as a vault

`~/.terminatr/projects` can be opened in [Obsidian](https://obsidian.md) as one vault, across every project: tm writes YAML front matter and wikilinks (`[[terminatr/tasks/T12|T12]]`) for everyone, and once you turn the vault on it also renders a note per task and per thread, Bases (Obsidian's table and card views over properties) and a home note. Nothing about it is needed to use tm.

Turn it on from your own terminal (not an agent's):

```
tm vault init
```

It lists what it changes and asks first (`--yes` skips the question, needed without a terminal):
- in `.obsidian/`, only the keys tm owns, merged into the settings Obsidian already has: links as tm writes them (absolute wikilinks), new notes in `_human/`, **Automatically update internal links off** (a rename in Obsidian would otherwise rewrite tm's files), the ignore filters (inbox/done, threads/archive, journal, `JOURNAL.md`, `AGENTS.md`, `CLAUDE.md`, review2 stay openable but out of search and the graph), the core plugins tm's notes use (Bases, Canvas, graph, backlinks, properties, tags, search), the graph's colours and default filter, property types, and the `tm` CSS snippet that marks tm's notes and other parties' words. Sync, Publish and daily notes stay as you have them;
- `_tm/bases/` (Tasks, Threads, Memory, Needs you, Inbox, Messages, each with a view per project) and `_tm/Home.md`: what needs you, the last two weeks' daily notes, and each project's board, context and memory;
- `_human/`, your folder: Obsidian puts new notes there and tm never writes in it;
- reports threads stored before tm guarded them (raw HTML, remote images, embeds, code blocks plugins run) are put through the guard once, so opening one fetches and runs nothing; the originals go to `~/.terminatr/state/migrate/report-guard-<time>.tar.gz`;
- the derived notes: `tasks/T12.md` (archived ones too, marked `archived: true`, so a link to a task resolves whether it is archived or not) and `threads/t-0001/t-0001.md` in each project, and a daily note per day across all projects (`_tm/daily/2026-10-24.md`: what shipped, started, was decided and needs you), rendered by the server whenever their sources change. To keep tm's day in your own daily notes, embed `![[_tm/daily/{{date}}]]` in their template. They wait for front matter to be YAML (the server's first start with 0.52.0 or later).

Then open the folder in Obsidian (Open folder as vault). tm remembers what it wrote in `.obsidian/tm.json`: run `tm vault init` again after an update and it changes only what is still as it left it; a setting you changed or a Base you edited is kept, and listed. To get tm's Base back, delete yours and run `tm vault render`.

tm's notes are tm's: task, thread, design and daily notes and the paired servers' stubs are read-only (Obsidian says it can't save), and `TASKS.md` and the threads' files stay the source. An edit that gets through anyway is not lost: before tm writes the note again it saves your version in `~/.terminatr/state/vault-saved/` and, for a project's note, tells the coordinator with an inbox item. A design's canvas stays yours to arrange.

**Telling tm something from Obsidian.** Write your own notes in `_human/` (where Obsidian puts new notes). A note there reaches a project's coordinator as an inbox item when it is for that project: put it under `_human/<slug>/`, give it the property `for: <slug>`, or tag it `for-tm` (`tags: [for-tm]`). Anywhere else outside tm's folders, tag a line `#for-tm`, or a card on a canvas (a design's canvas too); `#for-tm/<slug>` names the project when the vault has more than one. tm reads a note once it has been unchanged for 30 seconds and raises one item per version (a changed note replaces its open item); it never writes to your note, but the item links it, so it shows in the note's backlinks. The coordinator reads it as a note you probably left: it may add a task or answer you, and asks you in chat before anything you must approve, since any program of yours could have written the folder. Accepting work, answering questions and safety settings stay in the console and your own `tm` commands.

**The NEEDS YOU pane (optional plugin).** `tm vault init --plugin` also installs terminatr's Obsidian plugin in `.obsidian/plugins/terminatr/` (desktop only). Turn it on yourself in Obsidian (Settings → Community plugins → terminatr; tm turns on no plugin for you), then open it from the ribbon or the command palette (Open NEEDS YOU). It lists what `tm needs` lists, across every project, with the console's buttons: Accept and Send back… on a task in review, Delegate on a blocked one, Answer… on a question, Send and Drop on a held message, Resume on a paused link, Decline on a link request or a share request, and Open or Read for the task's note or the message. Each button runs your own `tm` command (`tm task accept T12 --project demo`, `tm ask answer Q3 --choice 2 --project demo`, …) as you, so tm's rules decide as they do in your terminal; the pane decides nothing, and the only text it passes to `tm` is what you typed in its own boxes (a send-back's note, a question's Other). A click on a row that changed in the last 300 ms does nothing (look again), as in the console. Titles, questions and other parties' words show as plain text in quotes, never rendered. Files offered, accepting a link or a share, a blocked session and a paired server's needs say to use the console: the pane changes no safety setting and never pairs or shares. It refreshes when a file in the vault changes and every 30 seconds. It finds `tm` where `tm vault init --plugin` ran it from (its settings tab changes that); after updating tm, run `tm vault init --plugin` again to update the plugin, which says so when the two don't match.

`PROJECT.md` stays yours to edit, in Obsidian too: when it changes the coordinator gets a `vault-edit` item and rereads it.

`tm vault render` renders the notes again, for when something looks stale: with `--project` one project's task and thread notes, and `--days 90` back-fills the daily notes of the last 90 days from the journals' archives (default: today and yesterday). Without `--project` it also refreshes `_tm/`'s Bases and Home.md, keeping one you edited. The coordinator can run it for its own project. `tm vault off` removes the derived notes (tasks', threads', designs' and paired servers' stubs) and `_tm/` with the daily notes and stops rendering them; design canvases stay, since your own cards and placements are on them, and so do `.obsidian/`, `_human/`, YAML and links.

`tm vault doctor` checks the vault and changes nothing; `tm doctor` shows the same under `vault` once the folder is a vault:
- **Sync and Publish.** The vault holds other projects' messages, paired servers' stubs and every report; Obsidian Sync or Publish would send them off this machine. tm warns and leaves the choice to you (Settings → Core plugins);
- **link auto-update** on, which lets Obsidian rewrite links in tm's files when you rename a note;
- community plugins that rewrite notes on their own (Linter, Update time on edit, Front matter modified date, Auto note mover, Filename heading sync): exclude the projects' folders in them, or turn them off;
- tm's settings changed since `tm vault init`, and derived notes edited outside tm;
- front matter that doesn't parse (tm can't read the file either: fix it by hand), files still in TOML, reports from before the guard;
- links that point at nothing: inside a project, into another project (renamed or deleted), to a paired server's stub (unpaired, or no longer shared), or to a task's note in `tasks/archive/`, where an earlier tm put archived tasks' notes (it is in `tasks/` now; `tm vault render --days N` rewrites older daily notes that link there).

It exits 1 for a broken link or a front matter that doesn't parse; warnings don't count. `--json` prints `checks` and `links` for scripts.

## Checking an installation: tm doctor

`tm doctor` checks your installation and changes nothing. It prints each group under an uppercase heading, with a blank line between groups and the status, name and detail in columns (`--json` is unchanged):
- the `tm` build and libghostty-vt, and git;
- under `code host`, printed last, for each code host your projects' repos use: `gh` and its login for GitHub; for a code host mod's kind (`gitlab`), the mod that serves it and its own checks (glab and its login); for Azure DevOps `az` and `az login` (or `AZURE_DEVOPS_EXT_PAT` set, never shown), that tm can read each repo through `az rest` (else why: the login, the organization's tenant with the `az login --tenant` to run, the origin URL), and that `git ls-remote origin HEAD` works without a password prompt. With no project yet, the GitHub checks run; and whenever `az` is on the server's `PATH` its `az` and `az login` lines show even when no repo uses Azure DevOps, marked "no project uses Azure DevOps" and never a warning (the per-repo lines stay with Azure repos);
- how `tm` was installed (Homebrew, a direct download, or built from source) and whether a newer release exists, with the command that updates it;
- the server: running and answering, the same build as this `tm` (a server of an older protocol is a warning; `tm doctor --fix` restarts it, agents are resumed), on macOS whether its sessions can reach the keychain (not when it was started over SSH without launchd; see [Over SSH](#over-ssh-macos)), a previous crash, stale `tm.sock`, `server.pid` and session runtime dirs (it never starts a server; runtime dirs only when run in a terminal, since from an agent's session it sees only its project's sessions);
- each agent's installed version, beside the version its manifest was last tested with (information: a newer agent is supported, and tm logs any change it runs into as `agent drift`, docs/SPEC.md §8.8), and a warning below the oldest version tm's generated files work with (`min_version`);
- the sandbox tools Claude needs for threads: `sandbox-exec` on macOS, `bwrap` and `socat` on Linux, and on Linux that an unprivileged process may map a user namespace, which bwrap needs (Ubuntu 24.04 restricts it by default: `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`, or an AppArmor profile for bwrap); without it a Codex thread's every command fails; on Windows, that a Codex thread's sandbox applies and reaches tm's socket (`codex thread sandbox`, a probe run inside a thread's profile; see [CODEX.md](CODEX.md));
- Claude plugins you have enabled that are known to be unsafe in terminatr's sessions (from `claude plugin list --json`), each with the reason and the `claude plugin disable` command; today `worktrees@supermods`, whose "Remove N finished" removes a fresh thread's clean worktree. A warning only: doctor never disables a plugin;
- a session whose queued prompts are held while its agent is idle (`prompt queue`: text left in its prompt box, or a dialog), which also holds a coordinator's nudges;
- leftovers: worktrees under `~/.terminatr/worktrees` whose thread is resolved or gone, and `tm/<project>/…` branches already merged into the default branch;
- paired servers, once this server has an identity key: a `peers` line for each paired server (link state, latency, when it was last seen, its tm version, the pane level each side allows, the projects shared each way, messages queued and undelivered) and one for each relay in use (`/healthz`, certificate days left or whether a pinned relay shows its pin, the relay's tm version and `update available`, auto-update when the relay says). A warning for an offline peer, a tm two minor releases or more from this one, undelivered messages, a relay that doesn't answer or is behind, a CA certificate with under 14 days left; a failure for a relay that shows another key than the pinned one (set it up again with `tm peer relay ALIAS ADDRESS`, or something else answers there) or an identity key that can't be read. `tm doctor --json` has the same fields on each line (`peer`, `relay`). Only you run it; agents see no more than `server.status`;
- upkeep: a project's `CONTEXT.md`, `MEMORY.md` or memory file over its size budget (the coordinator consolidates it);
- a project repository whose `.npmrc` reads a registry token from a variable the project's threads don't get from their secret environment (see The guard);
- settings in `config.toml`: keys tm doesn't know under `[keys]` or `[ui]` (ignored), the Claude agent mod's `[agent_mod]` keys (they do nothing now: delete them), and a project's (or All projects') Complete tasks still set to the removed "when released" (it now means by you; pick again in Settings).
- the Obsidian vault, once `~/.terminatr/projects` is one (Obsidian opened it, or `tm vault init` ran): Sync or Publish on, link auto-update, rewriting plugins, tm's notes and settings changed, front matter, reports before the guard, and the number of broken links (`tm vault doctor` lists them; see [Obsidian](#obsidian-the-projects-folder-as-a-vault)).

Each group prints as soon as it is checked. The code-host checks are the slow ones (every `az` call starts Python, several per repo), so they run in the background from the start, each repo beside the others (at most 4 at once), and print last, under `code host (checking…)` while they finish.

It exits 1 only when a check fails; warnings don't count. `--json` prints the results for scripts.

`tm doctor --fix` lists what it would remove or restart and asks before doing anything. `--yes` skips the question, and you need it when you're not on a terminal. Worktrees with uncommitted changes and unmerged branches are always kept. A branch whose PR was squash-merged looks unmerged to git; delete it yourself with `git branch -D`.

## Logs

The console reads them: `,` (Settings), then `5` for the **Logs** tab. `[` and `]` pick the server's log, the service's (launchd's output, on macOS), the consoles' and each mod's; the tab shows the end and follows it as it grows. `↑` `↓` and `pgup` `pgdn` scroll (scrolling pauses it; `f` or `G` follows again), and scrolling up reads the file further back, 64 KB at a time, so a large log stays quick. `/` searches, `n` and `N` go to the next older and newer match, `v` keeps only warnings and errors, or errors, when the lines carry a level (the consoles' log does). `o` opens the file in `$PAGER` (`less` by default) and `c` copies its path. The logs come through the server, so a console of a server on another machine shows that machine's logs (`o` then says the file isn't on this host: copy the path). Only you can read them there: an agent's `tm` is refused, and threads read what their sandbox lets them.

- `~/.terminatr/logs/server.log`: the server's log: starts and stops, sessions, agent state changes, resumes. Start here when something looks wrong.
- `~/.terminatr/logs/console.log`: what went wrong on the console's side (an error the footer showed, a view call that failed, a crash with its stack). It holds no keys or pane output; `TERMINATR_ATTACH_LOG=<file>` adds the attach client's diagnostics, keys included, to that file, for debugging only.
- `tm agent explain <session>`: why an agent session is in its current state (which signal decided it).
- `tm access show <project> --thread <id>`: what a thread launched with, and what a restart would change (see Browser tests, dev servers and other tools in the sandbox).
- A server started in the foreground (`tm server run`) logs to stderr instead; so does the Linux systemd service, whose log is `journalctl --user -u terminatr`.

## Upgrading

```sh
tm update            # asks, then installs the latest release
tm update --check    # only says whether there is one
```

- **Direct install:** `tm update` downloads the archive for your platform, checks it against `checksums.txt` and, on macOS, checks its Developer ID signature with `codesign`, then replaces `tm` in one rename. It needs to write to the directory `tm` is in. `--yes` skips the question (and is needed without a terminal).
- **Windows:** `tm update` downloads the zip and replaces `tm.exe`, `conpty.dll` and `OpenConsole.exe` by renaming each old file to `<name>.old-<time>` and the new one into its name (a running exe can be renamed, not overwritten), so it works while `tm` and its server run; the old files are deleted by the next `tm update` once nothing runs them. If a file can't be replaced, `tm update` names the processes that hold it (the Restart Manager's list) and restores the files it had replaced; close them and run it again.
- **Homebrew:** `tm update` never touches Homebrew's files: it shows `brew upgrade terminatr` and runs it if you say yes.
- **Built from source:** `tm update` refuses; `git pull && make`.

The running server keeps the old build until it restarts, and keeps working meanwhile: it runs from its own copy of its binary (`~/.terminatr/run/bin/tm`), so attaching and agent hooks are unaffected. Restarting switches it to the new build but stops every session: agents are resumed and lose only the turn they are in, shells are lost. So `tm update` asks before restarting (or restarts with `--restart`), and otherwise leaves it to you:

```sh
tm server restart
```

On a terminal the restart asks again before stopping agents that are mid-turn (`--yes` skips that). The restart works even when the server is older than the `tm` you ran `tm update` with.

On macOS the restart goes through launchd, so upgrading and restarting over SSH is fine as long as someone is logged in at the Mac ([Over SSH](#over-ssh-macos)).

**Front matter moves to YAML.** From 0.52.0 tm writes the front matter of its Markdown files (`PROJECT.md`, `TASKS.md`, memory notes, inbox items, messages, `STATUS.md`) as YAML between `---` lines, the form Obsidian and other editors read, instead of TOML between `+++` lines. The server's first start with it rewrites the files that still have TOML, once, each under its lock; it keeps the originals in `~/.terminatr/state/migrate/front-yaml-<time>.tar.gz`, journals `vault.migrate` per project and writes `~/.terminatr/state/front-yaml`; until then tm keeps writing TOML, so a server still running an older build reads what it writes. `tm vault migrate --check` lists what it would change, and any file it can't (it stays TOML, which still reads). TOML front matter keeps reading for good. Releases before 0.51.0 can't read YAML (they would lose `TASKS.md`'s task counter), so once the mark exists `tm update` refuses to install one unless `--force`.

### macOS privacy prompts

macOS counts every agent, `git` and tool a session runs as the server's doing (the server is their *responsible process*), so its privacy prompts and notices name `tm`: *"tm" would like to access files on a network volume*, *"tm" would like to access data from other apps*, or a *Data Access Blocked* notice. macOS keeps the answer for the path launchd started the server from. launchd starts it from `~/.terminatr/run/bin/tm` whatever version is installed: `tm` copies itself there before it starts the server, and an upgrade only replaces the file at that path. So there is one `tm` entry, and you answer once. macOS accepts each new build at that path because the Developer ID signature has the same identifier (`dev.terminatr.tm`) and team.

*Data Access Blocked* means something a session ran opened another app's data, and macOS doesn't ask on behalf of a command-line tool like `tm`: it refuses and says so. One cause seen in the privacy log is Claude Code itself, running in a session, reading Google Chrome's data. Nothing breaks: that access is just refused. To allow it, add `~/.terminatr/run/bin/tm` to Full Disk Access, but that gives every agent you run full disk access.

Releases up to 0.11.8 started the server from Homebrew's versioned path (`/opt/homebrew/Cellar/terminatr/<version>/bin/tm`) and only then switched to the pin. That didn't help, because macOS keeps the path launchd started, so every release added a `tm` entry and asked again. Even older releases (up to 0.11.3) also ran from `~/.terminatr/server-bin/tm-v0.x.y+…`, which left `tm-v0…` entries. macOS never matches these entries again, so they do no harm. To clear them:

- **System Settings:** in Privacy & Security › Files & Folders (and Full Disk Access, if you added `tm` there), select an old entry and click **−** (remove), or turn its switches off. Not every list on every macOS version has the **−** button. Keep the one for `~/.terminatr/run/bin/tm`.
- **tccutil:** `tccutil reset <service>` resets that permission for every app, not just `tm`, and they all ask again. Use `SystemPolicyNetworkVolumes` (network volumes), `SystemPolicyAppData` (data from other apps), or `SystemPolicyDocumentsFolder`, `SystemPolicyDownloadsFolder` and `SystemPolicyDesktopFolder`. `tccutil reset All dev.terminatr.tm` doesn't reach these entries, because macOS stores command-line tools by path, not by identifier.

Then restart the server (`tm server restart`) with the new `tm`, so launchd starts it from the pin, and answer the next prompt for the `tm` at `~/.terminatr/run/bin/tm`.

To see what asked, stream the privacy log while you reproduce the prompt. Each request names the service, the process that tried (`accessing`) and the one held responsible: `responsible_path` is the path macOS keeps the answer for, and should be `~/.terminatr/run/bin/tm`.

```sh
/usr/bin/log stream --style compact --info --predicate 'subsystem == "com.apple.TCC" AND eventMessage CONTAINS "AUTHREQ"'
```

### Upgrading from Termilator

Terminatr was called Termilator up to v0.6.2 (`brew install termilator`, state in `~/.termilator`). Nothing moves such an install over; by hand, with the old `tm`:

1. `tm server service uninstall` if you installed the login service, then `tm server stop --yes`.
2. Homebrew: `brew uninstall termilator`, `brew untap theclifmeister/termilator`, then tap, trust and install Terminatr as above (`brew tap theclifmeister/terminatr …`).
3. `mv ~/.termilator ~/.terminatr`, and edit the paths that still name `~/.termilator` by hand: in `~/.terminatr/config.toml` and in the files under `~/.terminatr/projects/`.
4. `git worktree repair` in each thread worktree (under `~/.terminatr/worktrees/`), and rename `TERMILATOR_*` variables to `TERMINATR_*`.
5. `tm server start` (and `tm server service install` again if you use it).

### Upgrading from Termalator

Termilator was called Termalator up to v0.1.0 (`brew install termalator`, state in `~/.termalator`). Termilator v0.2.0 to v0.5.2 moved such an install over by themselves; later releases don't. To upgrade from Termalator by hand: stop its server (`tm server stop` with the old `tm`), `brew uninstall termalator`, install Terminatr as above, `mv ~/.termalator ~/.terminatr` (editing the paths in it as for Termilator), run `git worktree repair` in each thread worktree, and rename `TERMALATOR_*` variables to `TERMINATR_*`. Or install the v0.5.2 release archive first, run `tm server restart` with it, then upgrade from Termilator as above.

## Keeping agent sessions lean

A fresh Claude Code coordinator or thread starts at roughly 45-47K tokens of context. terminatr's own share is ~5K for a coordinator (its rules and `tm context`, from its SessionStart hook) and ~2K for a thread (brief, rules, task state). The rest is Claude Code's, and comes with every Claude session: terminatr doesn't filter it, since agents keep the environment of any Claude session. You can trim it in your Claude Code settings. Rough token costs:

| What | ~tokens | Setting |
|---|---|---|
| Artifact tool schema | 14.5K | `"permissions": {"deny": ["Artifact"]}` (a bare tool name removes the tool from the context) |
| SendFeedback, Workflow, ScheduleWakeup tools | 2.4K + 2.4K + 2.1K | same, `"SendFeedback"`, `"Workflow"`, `"ScheduleWakeup"` |
| ReadNotifications, ReportFindings, ListAgents tools | 1.1K + 0.8K + 0.5K | same |
| Skills listing (built-in, claude.ai-synced, yours) | 6.3K (synced ~3.2K, built-in ~2.2K, `~/.claude/skills` ~1K) | `"skillOverrides": {"<skill>": "off"}` (or `"name-only"`), `"disableBundledSkills": true`, `"syncClaudeAiSkills": false` (user settings only) |
| claude.ai connectors (Gmail, Calendar, Drive, Claude Docs): deferred tool names, instructions, loaded Docs tools | ~3K | `"disableClaudeAiConnectors": true`, or `ENABLE_CLAUDEAI_MCP_SERVERS=false` for one shell |
| Auto memory section of the system prompt | 0.5K | `"autoMemoryEnabled": false` (user settings only); terminatr keeps its own memory |

Keep `AskUserQuestion` (a thread's question menus reach you through it), `Agent`, `Bash`, `Read`, `Edit`, `Write`, `Skill` and `ToolSearch`. A thread's sandbox instructions (~2K) come with the sandbox terminatr turns on for threads: keep them.

Where: `~/.claude/settings.json` applies to every Claude session, yours included. For the coordinator alone, a project folder's `.claude/settings.json` (`~/.terminatr/projects/<slug>/.claude/settings.json`) works for all but the user-only keys. Turning off the Artifact tool and the connectors alone takes a fresh coordinator from ~47K to ~29K tokens. Check with `/context` in a fresh session.

## Uninstalling

```sh
tm server service uninstall   # only if you installed the service
tm server stop --yes
rm "$(command -v tm)"
rm -rf ~/.terminatr          # all projects, tasks, reports and thread worktrees: keep a copy if you want them
```

On Windows, after `tm server service uninstall` and `tm server stop --yes`, delete the install folder (`%LOCALAPPDATA%\Programs\terminatr` for `install.ps1`), remove it from your user PATH and delete `%LOCALAPPDATA%\terminatr`.

Thread branches (`tm/<project>/…`) live in your repositories and are not removed by this; `tm doctor --fix` before uninstalling deletes the merged ones.

## Releasing

Releases go through release-please (`.github/workflows/release-please.yml`, `release-please-config.json`, `.release-please-manifest.json`). On every push to main it keeps one pull request open, titled like `chore: release 0.29.0`: the next version, worked out from the [Conventional Commit](https://www.conventionalcommits.org/) messages since the last release (`feat:` raises the minor version while terminatr is below 1.0, `fix:` and `perf:` the patch version), and that version's entry in `CHANGELOG.md`. To release:

1. Wait for `ci.yml` on main to be green (below), then merge the release pull request. Nothing else needs doing in it; it updates itself as more changes land, and `ci.yml` runs on each of its updates like on any pull request.
2. release-please creates a draft GitHub release whose notes are the new `CHANGELOG.md` entry and tags the merge commit (`v0.29.0`); the tag push starts the release workflow. The draft becomes the published release once its archives are checked.

That merge is the only click. release-please acts as a GitHub App of this repository (the release App, below), not with `GITHUB_TOKEN`: a pull request or tag that `GITHUB_TOKEN` makes starts no workflow, so the release pull request would wait for "Approve and run" on each update and the tag would not start the release.

The notes are the changelog entry: what users see on the releases page is what the release pull request showed in `CHANGELOG.md`. Only `feat:`, `fix:` and `perf:` commits appear there (and reverts); `chore:`, `ci:`, `docs:`, `test:`, `refactor:`, `build:` and `style:` commits don't, and don't make a release on their own. A squash-merged pull request's commit is its title, so pull request titles are written as Conventional Commits (CONTRIBUTING.md); a merge commit's own message ("Merge pull request #12 …") counts for nothing, only the commits it brings in do.

A tag pushed by hand (`git tag v0.2.0 && git push origin v0.2.0`) still releases, the same way from step 2, but release-please doesn't know about it: its draft release has no notes (write them on the release page before or after publishing), and you must set `.release-please-manifest.json` to that version on main yourself, or the next release pull request proposes it again. Release through the pull request when you can.

`.github/workflows/release.yml` runs on the tag, on one macOS runner:

1. imports the Developer ID certificate into a temporary keychain;
2. builds and checks a snapshot (`make release-snapshot`, `scripts/release/check.sh`), unsigned;
3. runs goreleaser (`make release`): every target cross-compiled with `zig cc`, except darwin, linked by Apple's clang and ld for chained fixups (the Windows targets as `*-windows-gnu`, their ConPTY files fetched by `scripts/release/conpty.sh`), each darwin binary signed (hardened runtime) and notarised by `scripts/release/sign.sh`; the archives, `tm-relay-docker.tar.gz` (`deploy/relay`'s Docker files with the compose files pinned to the tag, `scripts/release/relay-docker.sh`), the installers and `checksums.txt` uploaded to the draft release (release-please's, keeping its notes, or for a hand-made tag a new one without notes);
4. checks the archives again with `--signed` (Developer ID, hardened runtime, Gatekeeper says `Notarized Developer ID`), and only then publishes the release;
5. rewrites `Formula/terminatr.rb` from `checksums.txt` (`scripts/release/formula.sh`) and merges it through a pull request, opened and merged as the release App so that `ci.yml` runs on it. Prereleases (`v1.2.0-rc1`) skip this step.

To run a release again (a failed run, say), rerun its failed jobs, or Actions → release → Run workflow with Use workflow from set to the tag: on a branch it does nothing.

Then a second job, on a Linux runner (outside the `release` environment, which holds only the Apple secrets), builds the relay image from `deploy/relay/Dockerfile` for amd64 and arm64 (the published linux archives, checked against `checksums.txt`) and pushes it to `ghcr.io/theclifmeister/tm-relay:<tag>`, and `:latest` unless it is a prerelease. It runs once the release is published, even when the formula step failed; if it fails, the release stays published: rerun the `image` job.

Before you merge the release pull request (or push a tag), `ci.yml` on main has run `test` (unit tests with `-race`) and `macos` (build, unit tests, e2e smoke): wait for them to be green. Pull requests run neither macOS nor `-race`, and the release snapshot only when they touch release files; the tag run is where every release build is checked. If the repository requires status checks, require `test` (the one check every pull request runs). `macos` is main-only, so don't require it; `changes`, `mod-types` and `release-snapshot` are skipped when not needed, which counts as passed.

`make release-snapshot` runs the same build locally without a tag, signing nothing: `sign: TM_SIGN_IDENTITY not set; … keeps its linker signature` in the log. To try signing locally, set `TM_SIGN_IDENTITY` to a Developer ID identity in your keychain, by its SHA-1 hash from `security find-identity -v -p codesigning` (codesign refuses a name that two certificates share, as after a renewal); add `TM_NOTARY_KEY` (path to the .p8), `TM_NOTARY_KEY_ID` and `TM_NOTARY_ISSUER` to notarise.

### Secrets

The same Apple credentials as toe. Put them in the `release` environment (Settings → Environments → release), not as repository secrets, and limit that environment to `v*` tags (Deployment branches and tags → Selected → tag pattern `v*`): then only the release job can read them. The run the release App's tag starts is a run on the tag, like a hand-pushed tag's, so the same rule admits it; main needs no entry there. There is no approval step: the environment has no required reviewer, and only lets runs on `v*` tags use the signing secrets.

| Secret | What |
|---|---|
| `SIGNING_CERT_P12_BASE64` | the "Developer ID Application" certificate and its private key as .p12, base64; include the Developer ID Certification Authority intermediate |
| `SIGNING_CERT_PASSWORD` | the .p12's password |
| `KEYCHAIN_PASSWORD` | any random string, for the job's temporary keychain |
| `APPLE_TEAM_ID` | the team id in the certificate's name, e.g. `ABCDE12345` |
| `NOTARY_KEY_P8_BASE64` | an App Store Connect API key (.p8, role Developer), base64 |
| `NOTARY_KEY_ID` | that key's id |
| `NOTARY_ISSUER_ID` | the App Store Connect issuer id |

And two repository secrets (Settings → Secrets and variables → Actions → Repository secrets), for the release App: a GitHub App of your own (Settings → Developer settings → GitHub Apps → New GitHub App; no webhook), installed on this repository only, with the repository permissions Contents, Pull requests and Issues set to Read and write. `release-please.yml` and the release job's formula step get a short-lived token from it (`actions/create-github-app-token`).

| Secret | What |
|---|---|
| `RELEASE_APP_ID` | the App's id (its settings page, About) |
| `RELEASE_APP_KEY` | a private key of the App (Private keys → Generate a private key), the whole .pem |

`GITHUB_TOKEN` is provided by Actions: the release job uploads and publishes the release with it; the `image` job's token may write packages, for ghcr.io.

### Repository settings

- **Settings → Actions → General → Workflow permissions:** GitHub Actions needs no right to create or approve pull requests: the release App opens both the release pull request and the formula pull request.
- **Checks on the release pull request:** opened and updated by the release App, it gets `ci.yml` runs like any pull request, so rules on main that require status checks apply to it as usual.
- **The relay image's visibility:** the first release that pushes `ghcr.io/theclifmeister/tm-relay` creates the package as private. Make it public once (the package's Package settings → Danger Zone → Change visibility → Public), so `docker compose` pulls it without a login; check that it is linked to this repository (the image's `org.opencontainers.image.source` label does that) and that the repository has Admin or Write in the package's Manage Actions access, so later releases can push to it.
- **Rules on `main`:** the formula pull request merges at once when the rules allow, as toe's cask bump does. A ruleset that requires pull requests is fine. One that requires status checks refuses the immediate merge: the job then turns on auto-merge (if Settings → General allows auto-merge), and it merges once its `ci.yml` checks pass. One that requires approvals blocks it: merge it by hand, or add the release App as a bypass actor of that rule.
- **Forks:** no workflow uses `pull_request_target` or `workflow_run`, and the only one that sees secrets runs on tags, which only people with write access can push. For a public repository keep Settings → Actions → "Fork pull request workflows" at requiring approval for outside contributors.
- **Self-hosted runners:** none. On a public repository any fork pull request can target a self-hosted runner's labels from a workflow of its own and run code on that machine, so don't register one for this repository (the real-Claude suite runs by hand, `make test-claude`).


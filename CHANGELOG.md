# Changelog

## [0.30.0](https://github.com/theclifmeister/terminatr/compare/v0.29.1...v0.30.0) (2026-10-16)


### Features

* **dashboard:** list, add, remove and test the secret environment of threads in the settings popups ([0337007](https://github.com/theclifmeister/terminatr/commit/033700752b83b7006b4423c67ffc7152e1575652))


### Bug Fixes

* **ipc:** Listen's umask keeps the owner's bits, so folders made meanwhile stay usable (T525) ([#579](https://github.com/theclifmeister/terminatr/issues/579)) ([eb9e861](https://github.com/theclifmeister/terminatr/commit/eb9e8616d177b1727d28c6d0119e477a86b21425))
* **merge:** a squash merge uses the PR title as the commit title ([#566](https://github.com/theclifmeister/terminatr/issues/566)) ([9f1f115](https://github.com/theclifmeister/terminatr/commit/9f1f115046e74a8b60898ffb096c3d114029df02))
* pin goreleaser in CI, give the full e2e suite its own timeout, guard bundled mod folders both ways ([#576](https://github.com/theclifmeister/terminatr/issues/576)) ([3f766f4](https://github.com/theclifmeister/terminatr/commit/3f766f45977b3a2a5dfaf85afc429cfd19161d32))
* **test:** TestAzureMerge checks the rebase args on the rebase call, not the last map iteration ([#575](https://github.com/theclifmeister/terminatr/issues/575)) ([0b6d146](https://github.com/theclifmeister/terminatr/commit/0b6d146a90886f787180aa9e45039da52f4ba9fc))
* **theme:** Claude's ANSI theme follows the terminal's background, not its 2031 scheme ([#577](https://github.com/theclifmeister/terminatr/issues/577)) ([51a7b5d](https://github.com/theclifmeister/terminatr/commit/51a7b5da791719a0db397d172d920e22a9d2ba20))
* **windows:** the wheel scrolls back through a pane's earlier output ([#570](https://github.com/theclifmeister/terminatr/issues/570)) ([568b411](https://github.com/theclifmeister/terminatr/commit/568b411214ab4ec9242eb747fdda3e913699bd0a)), closes [#567](https://github.com/theclifmeister/terminatr/issues/567)

## [0.29.1](https://github.com/theclifmeister/terminatr/compare/v0.29.0...v0.29.1) (2026-10-16)


### Bug Fixes

* match env names case-insensitively on Windows (Path=) ([ad0f405](https://github.com/theclifmeister/terminatr/commit/ad0f405e510148fad2d72c1265c2097083bd900e))
* match env names case-insensitively on Windows (Path=) ([030d4ad](https://github.com/theclifmeister/terminatr/commit/030d4ad56aa15e98fa34be5508fab88022c149fc))

## [0.29.0](https://github.com/theclifmeister/terminatr/compare/v0.28.3...v0.29.0) (2026-10-16)


### Features

* **guard:** a learning mode tells the agent you review refused calls and puts each new refusal to you in the project's inbox; Guard, Guard mode, Guard rules and Guard allowances are in the settings popups ([#553](https://github.com/theclifmeister/terminatr/issues/553)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **guard:** allowances let through a call a rule would refuse, for one project or all projects, from the dashboard's Guard refusals list or guard_allow in config.toml ([#553](https://github.com/theclifmeister/terminatr/issues/553)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **guard:** what is never allowed (reading tm's own files, printing a thread's secret variables, force-pushing to a protected branch, a thread merging) stays refused with its rule turned off ([#553](https://github.com/theclifmeister/terminatr/issues/553)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **merge:** choose how tm thread merge merges a pull request: merge commit, squash or rebase (merge_method, or Merge method in the Settings tab) ([#546](https://github.com/theclifmeister/terminatr/issues/546)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **threads:** a secret environment for threads, under [defaults.env] or a project's env table, from gh auth token, a macOS keychain item or the server's environment, kept out of prompts, logs and the journal; tm doctor warns about an .npmrc token threads don't get ([#553](https://github.com/theclifmeister/terminatr/issues/553)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **threads:** name thread branches after your branch policy with a branch_name template, for all projects or one ([#545](https://github.com/theclifmeister/terminatr/issues/545)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))


### Bug Fixes

* **claude:** a project folder with non-ASCII characters keeps its conversations when renamed, and ~/.claude.json is backed up before tm first rewrites it ([#549](https://github.com/theclifmeister/terminatr/issues/549)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **mods:** manifest lists are bounded, a failed update keeps the old copy, an annotated tag resolves to its commit, fs.write takes its append option and huge timer delays are clamped ([#548](https://github.com/theclifmeister/terminatr/issues/548)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **mods:** tm mod test applies the real fetch, path and store checks, so a mod's tests refuse what tm would ([#547](https://github.com/theclifmeister/terminatr/issues/547)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* **sandbox:** on Linux the sandboxed host may signal only itself, and the guard backs off a server that hangs ([#551](https://github.com/theclifmeister/terminatr/issues/551)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))
* tm mcp answers an unsupported protocol version or an oversized message instead of exiting; a stale cached Azure subscription is dropped and retried; small fixes in the config editor, pty, Windows sandbox and theme colours ([#549](https://github.com/theclifmeister/terminatr/issues/549)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))


### Performance

* **tui:** fewer layout passes and frame copies per render, and the transcript tail and library viewer do less work per frame ([#551](https://github.com/theclifmeister/terminatr/issues/551)) ([e3e9687](https://github.com/theclifmeister/terminatr/commit/e3e96870b432542545b8fa46ccbc995a26379656))

## [0.28.3](https://github.com/theclifmeister/terminatr/releases/tag/v0.28.3)

The notes of v0.28.3 and earlier releases are on the [releases page](https://github.com/theclifmeister/terminatr/releases) and in [docs/releases](docs/releases/).

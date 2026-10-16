# Changelog

## [0.30.0](https://github.com/theclifmeister/terminatr/compare/v0.29.0...v0.30.0) (2026-10-16)


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

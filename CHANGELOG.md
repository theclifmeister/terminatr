# Changelog

## [0.41.0](https://github.com/theclifmeister/terminatr/compare/v0.40.0...v0.41.0) (2026-10-19)


### Features

* **tui:** save and clear a coordinator with prefix+C instead of c ([#681](https://github.com/theclifmeister/terminatr/issues/681)) ([b368e35](https://github.com/theclifmeister/terminatr/commit/b368e35ecfee1d1e05fd4417a8bbb3091dd8c028))

## [0.40.0](https://github.com/theclifmeister/terminatr/compare/v0.39.0...v0.40.0) (2026-10-19)


### Features

* **access:** edit rules in place and accept wide command patterns ([#677](https://github.com/theclifmeister/terminatr/issues/677)) ([a3cda82](https://github.com/theclifmeister/terminatr/commit/a3cda82b50e47f1b040783c8b5d10a5a5725681b))


### Bug Fixes

* **run:** let approved commands on macOS start ps and top outside the run's sandbox ([#676](https://github.com/theclifmeister/terminatr/issues/676)) ([c6e3a3a](https://github.com/theclifmeister/terminatr/commit/c6e3a3a8f96ac6fbca3b890e572db08be12f48e3))
* **sandbox:** keep later worktrees' git dirs and the main checkout's git state out of a thread's reach ([#672](https://github.com/theclifmeister/terminatr/issues/672)) ([d155fbf](https://github.com/theclifmeister/terminatr/commit/d155fbf9e75351ffa660e113522aca9f0470b8df))

## [0.39.0](https://github.com/theclifmeister/terminatr/compare/v0.38.0...v0.39.0) (2026-10-19)


### Features

* **access:** drop per-entry warning lines; x removes, drops or takes back ([#668](https://github.com/theclifmeister/terminatr/issues/668)) ([a43993c](https://github.com/theclifmeister/terminatr/commit/a43993c5cb0e667bbf7a656e84a665990ca187bb))
* **access:** guidance for threads is freeform, title and text ([#671](https://github.com/theclifmeister/terminatr/issues/671)) ([e093c6a](https://github.com/theclifmeister/terminatr/commit/e093c6aac0704920e43df146edddce9f4db229e6))
* **access:** the project's coordinator may read tm access show and explain ([#665](https://github.com/theclifmeister/terminatr/issues/665)) ([613a479](https://github.com/theclifmeister/terminatr/commit/613a479616b5c140b02e0d9208bf0213f560b390))
* **sidebar:** drop the repeated title from the footer for the session on screen ([#670](https://github.com/theclifmeister/terminatr/issues/670)) ([c2bda05](https://github.com/theclifmeister/terminatr/commit/c2bda057915db267014eedcc248d0959690b9396))
* **tui:** lowercase the settings popup's tab titles ([#669](https://github.com/theclifmeister/terminatr/issues/669)) ([91d6c39](https://github.com/theclifmeister/terminatr/commit/91d6c39c985bac9ac9174e9aba82b773767dab9f))


### Bug Fixes

* **git:** give pinned worktree runs a git dir of tm's own ([#667](https://github.com/theclifmeister/terminatr/issues/667)) ([ea08217](https://github.com/theclifmeister/terminatr/commit/ea082170feecb679144bced0d34047dbb0408be4))

## [0.38.0](https://github.com/theclifmeister/terminatr/compare/v0.37.0...v0.38.0) (2026-10-19)


### Features

* **access:** Codex threads get a limited network and no escalation with escapes off; your own Claude excluded commands count as escapes ([#654](https://github.com/theclifmeister/terminatr/issues/654)) ([bf985a7](https://github.com/theclifmeister/terminatr/commit/bf985a7f325e281fc397fe7813a9fb270e0f21c3))
* **access:** excluded commands ask first or run without asking; write folders ([#655](https://github.com/theclifmeister/terminatr/issues/655)) ([9f803e0](https://github.com/theclifmeister/terminatr/commit/9f803e092d43c3223c014c7b3745dc5318b64c6d))
* **access:** host, local-port and guidance rules; presets add their rules; drop and per-entry refusal ([#653](https://github.com/theclifmeister/terminatr/issues/653)) ([6a3114b](https://github.com/theclifmeister/terminatr/commit/6a3114b3a53af55be662d67f89e329019d8b1a9f))
* **access:** move config.toml to the access rules at the first server start; tm access add|rm|drop|undrop|set ([#658](https://github.com/theclifmeister/terminatr/issues/658)) ([74f8c70](https://github.com/theclifmeister/terminatr/commit/74f8c70341ab96825f5cb830a070b197a4f3f619))
* **access:** one Access tab per project and for all projects ([#651](https://github.com/theclifmeister/terminatr/issues/651)) ([260c628](https://github.com/theclifmeister/terminatr/commit/260c6285de728a5588469b59e6026558857dfd07))
* **access:** see a thread's effective access (tm access show/explain, Effective access view) ([#650](https://github.com/theclifmeister/terminatr/issues/650)) ([7ee7639](https://github.com/theclifmeister/terminatr/commit/7ee763985814b7adb4dc0dee1e735d0e47bf6811))
* **access:** socket rules and approved commands; a Codex thread's excluded commands become approved commands ([#659](https://github.com/theclifmeister/terminatr/issues/659)) ([5a3573d](https://github.com/theclifmeister/terminatr/commit/5a3573df407442205f8ee376c0ba44b00bde087d))
* **access:** yolo turns the sandbox off; the guard floor protects settings and the project folder; escape prompts go to the user ([#652](https://github.com/theclifmeister/terminatr/issues/652)) ([6ceac62](https://github.com/theclifmeister/terminatr/commit/6ceac62e0413bc82226d740690783670d280fe48))
* **doctor:** readable output (blank line between groups, uppercase headings, aligned columns) ([#645](https://github.com/theclifmeister/terminatr/issues/645)) ([619364d](https://github.com/theclifmeister/terminatr/commit/619364dc0f7d083706c6d03be360daf885a2dc4c))
* **mods:** install from public URLs only ([#647](https://github.com/theclifmeister/terminatr/issues/647)) ([726f35e](https://github.com/theclifmeister/terminatr/commit/726f35e1a055c325f84689136e7ab6673501d098))
* **mods:** show where an installed mod came from (Mods tab, tm mod show) ([#644](https://github.com/theclifmeister/terminatr/issues/644)) ([985c3a6](https://github.com/theclifmeister/terminatr/commit/985c3a68573e9e71a9985c3fc859dbc76f4635ba))
* **sandbox:** give each thread its own temp folder, kept from other sessions' ([#657](https://github.com/theclifmeister/terminatr/issues/657)) ([bdacb09](https://github.com/theclifmeister/terminatr/commit/bdacb09e1826a4a5cfbf605ecb055a381e3349b1))
* **sandbox:** take a thread's git dirs from its record ([#649](https://github.com/theclifmeister/terminatr/issues/649)) ([60e7dfa](https://github.com/theclifmeister/terminatr/commit/60e7dfa9dfbbb2e6846558cdb5736a3ffa41b01e))


### Bug Fixes

* **report:** accept attachments from a Claude thread's temp folders and its own scratchpad ([#648](https://github.com/theclifmeister/terminatr/issues/648)) ([2c060b0](https://github.com/theclifmeister/terminatr/commit/2c060b0c909ff5c7764c882bda49a9722a17ef14))

## [0.37.0](https://github.com/theclifmeister/terminatr/compare/v0.36.0...v0.37.0) (2026-10-18)


### Features

* **sandbox:** browser testing opens npm and Playwright downloads, ships a sandbox config ([#638](https://github.com/theclifmeister/terminatr/issues/638)) ([003f5a6](https://github.com/theclifmeister/terminatr/commit/003f5a603ef8178011c02f3b1c5b29dca7dd01eb))

## [0.36.0](https://github.com/theclifmeister/terminatr/compare/v0.35.1...v0.36.0) (2026-10-18)


### Features

* **threads:** priority prompts (--now, --interrupt) and a stuck-prompt warning ([#639](https://github.com/theclifmeister/terminatr/issues/639)) ([d33037e](https://github.com/theclifmeister/terminatr/commit/d33037ed1f7fda869745e7871bff6395181e576f))

## [0.35.1](https://github.com/theclifmeister/terminatr/compare/v0.35.0...v0.35.1) (2026-10-18)


### Bug Fixes

* **server:** rotate service.log at launchd start when over 1 MB ([#632](https://github.com/theclifmeister/terminatr/issues/632)) ([0296b88](https://github.com/theclifmeister/terminatr/commit/0296b8883b9a1d4ce36b1b41a4bc97ac077669be))

## [0.35.0](https://github.com/theclifmeister/terminatr/compare/v0.34.0...v0.35.0) (2026-10-17)


### Features

* **dashboard:** a Logs tab in the , popup for the server, service, console and mod logs ([#630](https://github.com/theclifmeister/terminatr/issues/630)) ([8642380](https://github.com/theclifmeister/terminatr/commit/8642380e9f6127394ed1e231b2c50ea8787536b2))
* **sandbox:** a Sandbox tab with presets (browser testing, local dev servers), per-OS, and hardened lists ([#626](https://github.com/theclifmeister/terminatr/issues/626)) ([ec091d8](https://github.com/theclifmeister/terminatr/commit/ec091d84d7dc28b4c040243343d401d8efe2ec3c))
* **sandbox:** say why a project's sandbox lists and presets were dropped ([#629](https://github.com/theclifmeister/terminatr/issues/629)) ([3825163](https://github.com/theclifmeister/terminatr/commit/38251639f43b4208cad09787beabab92d492811d))


### Bug Fixes

* **sandbox:** browser testing without local binding; plain local-port warning ([#628](https://github.com/theclifmeister/terminatr/issues/628)) ([a99225a](https://github.com/theclifmeister/terminatr/commit/a99225a5f1df979c220374a926003c67a7dc8289))

## [0.34.0](https://github.com/theclifmeister/terminatr/compare/v0.33.0...v0.34.0) (2026-10-17)


### Features

* **sandbox:** per-project sandbox lists for threads (excluded commands, allow rules), editable in settings ([#622](https://github.com/theclifmeister/terminatr/issues/622)) ([2c91af8](https://github.com/theclifmeister/terminatr/commit/2c91af8d169604f8637d85359bced6a5eda1b6c3))

## [0.33.0](https://github.com/theclifmeister/terminatr/compare/v0.32.0...v0.33.0) (2026-10-17)


### Features

* **memory:** BM25 note matching with stemming and stop words; coordinator-only notes ([#618](https://github.com/theclifmeister/terminatr/issues/618)) ([ad8145b](https://github.com/theclifmeister/terminatr/commit/ad8145b1a18ab4836ebcf2170e86f728a4fcf7a1))

## [0.32.0](https://github.com/theclifmeister/terminatr/compare/v0.31.0...v0.32.0) (2026-10-17)


### Features

* **briefs:** carry matched memory notes in thread briefs, preview with tm context --thread ([0a5886f](https://github.com/theclifmeister/terminatr/commit/0a5886f1681b88f7e013b49fe5eee5bdee226118))
* **briefs:** carry matched memory notes in thread briefs, preview with tm context --thread ([4f5dd1e](https://github.com/theclifmeister/terminatr/commit/4f5dd1e358eebfc3dcae968940bcb08f5920442f))
* **coordinator:** save and clear from the context hint, tm clear and /saveclear ([#614](https://github.com/theclifmeister/terminatr/issues/614)) ([4cdc262](https://github.com/theclifmeister/terminatr/commit/4cdc262345385feb066f3258c6b72443294d6615))
* **memory:** edit, create and delete memory notes from the Memory tab; tm keeps MEMORY.md's list ([#613](https://github.com/theclifmeister/terminatr/issues/613)) ([8ed9bb6](https://github.com/theclifmeister/terminatr/commit/8ed9bb678a644f313a9977352da18dc231bcaffb))
* **memory:** optional TOML front matter for memory notes, Match and tm memory find ([061bd44](https://github.com/theclifmeister/terminatr/commit/061bd44ba5041857cda180de6f6abf5bfb5cfa1c))
* **memory:** optional TOML front matter for memory notes, Match and tm memory find ([a0df23c](https://github.com/theclifmeister/terminatr/commit/a0df23cf991dab8bc8e7d7d6dd9cfecf2bd070ac))
* **memory:** tm memory check flags thread diffs that contradict notes ([9637ced](https://github.com/theclifmeister/terminatr/commit/9637ceddac5076a1e12a9d92a14b3ae1b6550b60))
* **memory:** tm memory check flags thread diffs that contradict recorded decisions ([a3bc418](https://github.com/theclifmeister/terminatr/commit/a3bc418db7720ed426d347585ed428363bf2e054))
* **ticker:** remember items for unfiled lessons, and a save-lessons ask before auto-clear ([8907e9c](https://github.com/theclifmeister/terminatr/commit/8907e9c1622d5b434ec9fbf43e500f9058e6a8a5))
* **ticker:** remember items for unfiled lessons, and a save-lessons ask before auto-clear ([b9c14f4](https://github.com/theclifmeister/terminatr/commit/b9c14f4277e5edc4453098e99358f73dade8e0e5))


### Bug Fixes

* **memory-tab:** no String.prototype.trimEnd in the mod's ES lib ([d3ecdec](https://github.com/theclifmeister/terminatr/commit/d3ecdec5cc031dbd84c5d97f8fca000d7da50ae7))

## [0.31.0](https://github.com/theclifmeister/terminatr/compare/v0.30.1...v0.31.0) (2026-10-17)


### Features

* **codehost:** trust the origin remote host and the az default organization for azure_url ([99f7587](https://github.com/theclifmeister/terminatr/commit/99f7587c98413cdf8aa69fe08b116c1ac78df32c))
* **codehost:** trust the origin remote host and the az default organization for azure_url ([63321f9](https://github.com/theclifmeister/terminatr/commit/63321f9c957f2fe4e46fdbdf57c6f617d30959b6))


### Bug Fixes

* bound session input answers, transcript tail reads and mod host writes ([ad81574](https://github.com/theclifmeister/terminatr/commit/ad81574fcdd97db25dfa661db6eccc8eb68f97ae))
* branch names, multi-line settings, and pane state in the attach client ([75bcf37](https://github.com/theclifmeister/terminatr/commit/75bcf37d400c861c9f9a94d962347af528647296))
* clean peer, agent and mod text with textsafe before any terminal ([5ce1f90](https://github.com/theclifmeister/terminatr/commit/5ce1f90f33a9cd07d5c03cb25062ecf8a9e00144))
* clean peer, agent and mod text with textsafe before any terminal ([30dc871](https://github.com/theclifmeister/terminatr/commit/30dc871f1465ea8a2d022071132346979a8233f2))
* **codehost:** send Azure DevOps credentials only to trusted hosts ([1b3b9e5](https://github.com/theclifmeister/terminatr/commit/1b3b9e55b4bc31d34b7bae1b55e5cd1486b665af))
* **codehost:** send Azure DevOps credentials only to trusted hosts ([cb365c8](https://github.com/theclifmeister/terminatr/commit/cb365c80d25eb022dd8314a5748806308e31c210))
* **codehost:** trust an origin host per target, only for repos the user registered ([570d68f](https://github.com/theclifmeister/terminatr/commit/570d68fadb2e024c4d1dd8dde4752c778b8f5db4))
* **config:** edit settings written over several lines ([9c1275b](https://github.com/theclifmeister/terminatr/commit/9c1275ba19b21db0b7022f2e60a0cb8bbfa58a3c))
* **doctor:** suggest the GitHub token only for GitHub's npm registry ([2cabf55](https://github.com/theclifmeister/terminatr/commit/2cabf55a5c25ed0007bc5c8f6afdbbe5056e7e57))
* **doctor:** suggest the GitHub token only for GitHub's npm registry ([4d746d5](https://github.com/theclifmeister/terminatr/commit/4d746d547630087112724829453f81874539ec9b))
* **git:** run every git call through one hardened runner and pin worktree git dirs ([9de70cd](https://github.com/theclifmeister/terminatr/commit/9de70cd947c41f90c7e1e1617b4f0c02e2216b0e))
* **git:** run every git call through one hardened runner and pin worktree git dirs ([42577d4](https://github.com/theclifmeister/terminatr/commit/42577d48f86ecd9f81fdd5b33148e2541de42f3d))
* **guard:** allow a refused call exactly, its * included ([f5f0c3e](https://github.com/theclifmeister/terminatr/commit/f5f0c3e0536d0e2e483d5b6e6bc8e10c5561fa95))
* **guard:** allow a refused call exactly, its * included ([6391cf1](https://github.com/theclifmeister/terminatr/commit/6391cf134dd9fe1f0c876858e40e35fb84d08088))
* **guard:** read abbreviated long options and options after bash -c ([b6a3979](https://github.com/theclifmeister/terminatr/commit/b6a3979fe079b739d969f5778ec3cd03008385c4))
* **guard:** read abbreviated long options and options after bash -c ([bbecd6c](https://github.com/theclifmeister/terminatr/commit/bbecd6c89e96d2f242071d4d2247e8fc985ce7f1))
* keep tm's project files out of the coordinator's reach and refuse links ([28b3a6e](https://github.com/theclifmeister/terminatr/commit/28b3a6ed62a07dec5340ce5d863ddc9ad4c8a379))
* keep tm's project files out of the coordinator's reach and refuse links ([9022cdb](https://github.com/theclifmeister/terminatr/commit/9022cdb0e24df82a54466df928469a40bc3d3b26))
* **modbroker:** keep git's repository config from naming a program ([2d8eb89](https://github.com/theclifmeister/terminatr/commit/2d8eb89d3abe6c9fc222c9c331190c0414432fbf))
* **modbroker:** keep git's repository config from naming a program ([3b7280a](https://github.com/theclifmeister/terminatr/commit/3b7280ab96b11015bd06912e4b34c95262647a51))
* **mod:** describe a commands grant as running anything unless the program is vetted as narrow ([6512db2](https://github.com/theclifmeister/terminatr/commit/6512db2dbf2bc163a020c724fc5b61b426e9196a))
* **mod:** describe a commands grant as running anything unless the program is vetted as narrow ([eee5210](https://github.com/theclifmeister/terminatr/commit/eee521048d45f6fe2773e9fbc224c14e4950378f))
* **mod:** keep only options-free programs on the narrow commands list ([9eb549d](https://github.com/theclifmeister/terminatr/commit/9eb549d625913d614568066d6f251210b4bbc6b3))
* **modproto:** let a request's context bound its write to the host ([db52a7a](https://github.com/theclifmeister/terminatr/commit/db52a7a93125bc399883e482623d3fabb0cf972e))
* peer link delivery, relay dial bounds, pane revocation and sizes, peer.watch export ([c741a7f](https://github.com/theclifmeister/terminatr/commit/c741a7f7017643f6cb1a8891552ed5c1f2929c0b))
* **peer:** bound the pane sizes and repaint rate a paired server sets ([258cc77](https://github.com/theclifmeister/terminatr/commit/258cc7707d992717fda41754956aa4538d3211c2))
* **peer:** deliver each link response at most once ([b516f98](https://github.com/theclifmeister/terminatr/commit/b516f983cdb9008583636b0c40f754bee0f25602))
* **peer:** don't serve a pane whose switch went off while it opened ([b91b811](https://github.com/theclifmeister/terminatr/commit/b91b8119f9aaeab1e1f6b0b41f827e0e53f05a98))
* **relay:** bound the dial up to the relay's challenge ([74d86a2](https://github.com/theclifmeister/terminatr/commit/74d86a26ed37e3b8aca91a05cf358ee28e06dfb8))
* **sandbox:** widen the Linux seccomp filter and check it by argument ([5fd6476](https://github.com/theclifmeister/terminatr/commit/5fd6476e45f366c1374940661e3d9d91e789f7af))
* **sandbox:** widen the Linux seccomp filter and check it by argument ([1cc3062](https://github.com/theclifmeister/terminatr/commit/1cc30624070cccf20a62dbaa21e4fc0f0c8742d2))
* **server:** copy what peer.watch sends field by field ([0afe623](https://github.com/theclifmeister/terminatr/commit/0afe623b0e95011195f7cb3906dc120af28ec40f))
* **session:** bound queued answers to terminal queries ([ee2dd64](https://github.com/theclifmeister/terminatr/commit/ee2dd645f0d8c7ac671a01a526327449c0b3faec))
* **session:** skip transcript lines longer than one tail read ([c5020b7](https://github.com/theclifmeister/terminatr/commit/c5020b7eee8bfa4608d701e3f32d1ff018624f0e))
* **thread:** make branch_name give names tm can look PRs up by ([315ff3a](https://github.com/theclifmeister/terminatr/commit/315ff3af84b1ac162e26b217f005617649c33a06))
* **tui:** close the sidebar's projects watch by value ([8768642](https://github.com/theclifmeister/terminatr/commit/87686427635c009970de0d6bfab528dfbb46c6aa))
* **tui:** keep pane state right across a session restart and a freed pane ([55c2a76](https://github.com/theclifmeister/terminatr/commit/55c2a76cc639f5d0c41fe12c2edf74519e220451))
* **tui:** open only http(s) addresses from a peer's PR line ([9963303](https://github.com/theclifmeister/terminatr/commit/9963303e0678f156c8aafdf7a1ef81d7e7e1d5d6))
* **tui:** open only http(s) addresses from a peer's PR line ([2edd8d5](https://github.com/theclifmeister/terminatr/commit/2edd8d5c12be27523f109f39b033c4fb4302a697))

## [0.30.1](https://github.com/theclifmeister/terminatr/compare/v0.30.0...v0.30.1) (2026-10-16)


### Miscellaneous Chores

* release 0.30.1 ([f4a90cd](https://github.com/theclifmeister/terminatr/commit/f4a90cd2dd2c90b09588eae1b3ccafdfe4cb91e1))

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

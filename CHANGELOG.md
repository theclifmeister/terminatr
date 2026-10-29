# Changelog

## [0.62.1](https://github.com/theclifmeister/terminatr/compare/v0.62.0...v0.62.1) (2026-10-29)


### Bug Fixes

* **relay,ios:** device links no longer drop every 90 s; phone ping, network follow, background grace, quiet resume ([#920](https://github.com/theclifmeister/terminatr/issues/920)) ([2bcd847](https://github.com/theclifmeister/terminatr/commit/2bcd8477a0e5ac062756d5ffe7fb998298f6f58d))

## [0.62.0](https://github.com/theclifmeister/terminatr/compare/v0.61.0...v0.62.0) (2026-10-29)


### Features

* **ios:** a session takes the whole width in portrait, and the settings gear sits on the title's line ([#917](https://github.com/theclifmeister/terminatr/issues/917)) ([2f293ee](https://github.com/theclifmeister/terminatr/commit/2f293eec6d9b50f2c6c2d8ea7e713889baf33517))


### Bug Fixes

* **ios:** composer dock at one height with a hide-keyboard button, keyboard kept across reconnects, Queued note cleared, board folds paired ([#918](https://github.com/theclifmeister/terminatr/issues/918)) ([8b1113a](https://github.com/theclifmeister/terminatr/commit/8b1113afd65254e8588a5728d2a2f0762d03e4d1))
* **ios:** the Terminal view shows at once and in the theme's colours ([#915](https://github.com/theclifmeister/terminatr/issues/915)) ([96c86e3](https://github.com/theclifmeister/terminatr/commit/96c86e3000db4f353e10dde44d223cde6542b9dc))

## [0.61.0](https://github.com/theclifmeister/terminatr/compare/v0.60.0...v0.61.0) (2026-10-29)


### Features

* **app:** redesign phase 0: deck glass, StateGlyph, badges, button roles, headers and keycap answers ([#906](https://github.com/theclifmeister/terminatr/issues/906)) ([2b3832b](https://github.com/theclifmeister/terminatr/commit/2b3832b05c4007439a8e719895a96e08b3f9a847))
* **ios:** Conversation / Terminal switch on a session, and the composer as the Mac's ([#904](https://github.com/theclifmeister/terminatr/issues/904)) ([a21b2ca](https://github.com/theclifmeister/terminatr/commit/a21b2caf3ff7afef529801b9c6a67a3e6a4a2c34))
* **ios:** redesign phase 1: Projects as cards, a project's screen with tiles and StateGlyph rows ([#908](https://github.com/theclifmeister/terminatr/issues/908)) ([bafcd3c](https://github.com/theclifmeister/terminatr/commit/bafcd3c9ad2fc78bcbf81cc808a368876b8e4995))
* **ios:** redesign phase 2: Needs you as an answer deck, with haptics and VoiceOver actions ([#910](https://github.com/theclifmeister/terminatr/issues/910)) ([c3f57cc](https://github.com/theclifmeister/terminatr/commit/c3f57ccd3c4088e88995feb40245c2e577f71834))
* **ios:** redesign phase 3: glass session header, run chips, the question as a hero, and the composer dock with a key strip ([#912](https://github.com/theclifmeister/terminatr/issues/912)) ([78160fd](https://github.com/theclifmeister/terminatr/commit/78160fd857ce81c1620eb49e56aaef53b8470c24))
* **ios:** redesign phase 4: LiveStrip, iPad sidebar in the TUI's order, the open Duo split at the hinge ([#911](https://github.com/theclifmeister/terminatr/issues/911)) ([a47d57a](https://github.com/theclifmeister/terminatr/commit/a47d57aa6c05a34b9bd7d57185253504c14a5cc8))
* **ios:** redesign phase 5: Welcome, Settings and pairing ([#909](https://github.com/theclifmeister/terminatr/issues/909)) ([7c531d2](https://github.com/theclifmeister/terminatr/commit/7c531d26f3421c780c81195a47f3a49ffd45324c))
* **ios:** redesign phase 6: accessibility pass and a Haptics switch ([#913](https://github.com/theclifmeister/terminatr/issues/913)) ([db61a69](https://github.com/theclifmeister/terminatr/commit/db61a692049dd198a52c3fe4c52be4d7acc2c3a3))
* **ios:** the server panel leaves Needs you; Settings shows the server and ends with Forget ([#902](https://github.com/theclifmeister/terminatr/issues/902)) ([ecc9697](https://github.com/theclifmeister/terminatr/commit/ecc9697423787ebe9c1d66b524923dc3b8133a86))
* **peer:** paired needs, phase 1: a per-peer needs switch and full needs at view ([#903](https://github.com/theclifmeister/terminatr/issues/903)) ([d252081](https://github.com/theclifmeister/terminatr/commit/d252081018157b7268d4801d67821ea20f93b333))
* **peer:** paired needs, phase 2: answer, approve, accept, send back and questions across a pairing ([#907](https://github.com/theclifmeister/terminatr/issues/907)) ([261d85b](https://github.com/theclifmeister/terminatr/commit/261d85b0ac38a1d0a73d3c73bec047406b710a13))


### Bug Fixes

* **ios:** a column's list fills the column side by side on a landscape iPhone Duo ([#899](https://github.com/theclifmeister/terminatr/issues/899)) ([db6a549](https://github.com/theclifmeister/terminatr/commit/db6a5495cea790bb414918a8dfc0938569b41220))
* **ios:** a tap opens its screen beside the list on an open iPhone Duo in portrait ([#901](https://github.com/theclifmeister/terminatr/issues/901)) ([9bd15a3](https://github.com/theclifmeister/terminatr/commit/9bd15a361943c5072f4db74a65792cbd8970be76))
* **tui:** call the Network popup's paired phones devices ([#905](https://github.com/theclifmeister/terminatr/issues/905)) ([e6beb21](https://github.com/theclifmeister/terminatr/commit/e6beb210fdc4000f05febaa12a312e46dc00cc82))

## [0.60.0](https://github.com/theclifmeister/terminatr/compare/v0.59.0...v0.60.0) (2026-10-28)


### Features

* **ios, device:** the write level, with the iPhone's write screens ([#881](https://github.com/theclifmeister/terminatr/issues/881)) ([f97279a](https://github.com/theclifmeister/terminatr/commit/f97279a6e3f0331251961f3873ecf89f5e2cff70))
* **ios:** build with the iOS 27.1 SDK for iPhone Duo, landscape on its outer display ([#883](https://github.com/theclifmeister/terminatr/issues/883)) ([7b2a7eb](https://github.com/theclifmeister/terminatr/commit/7b2a7eb663d5be0bcdffa336dd7a7cc819579dd1))
* **ios:** list and detail side by side when an iPhone Duo is open; opening or closing it keeps your place ([#886](https://github.com/theclifmeister/terminatr/issues/886)) ([a473265](https://github.com/theclifmeister/terminatr/commit/a4732650fc950ce27117406394b833fdc81203b8))
* **ios:** Needs you answers in one tap on a closed iPhone Duo; open, a need sits beside the conversation that asked it ([#888](https://github.com/theclifmeister/terminatr/issues/888)) ([46035b3](https://github.com/theclifmeister/terminatr/commit/46035b30ba81a20758a9f5bfb67eee52a2c99675))
* **ios:** run on iPad too, one universal build, with the two columns ([#889](https://github.com/theclifmeister/terminatr/issues/889)) ([219fadc](https://github.com/theclifmeister/terminatr/commit/219fadc313b213f95d47cd80edfc592ae74936bc))
* **ios:** the phone's write key signs without Face ID ([#879](https://github.com/theclifmeister/terminatr/issues/879)) ([b979712](https://github.com/theclifmeister/terminatr/commit/b979712b3e93f2c34a9f8f7a69cd09b303765f7f))
* **peer:** this server's own name in tm, told to paired servers ([#896](https://github.com/theclifmeister/terminatr/issues/896)) ([50489e4](https://github.com/theclifmeister/terminatr/commit/50489e40bc65d9ea9d656ca0e8d6320164e426a6))
* **server:** re-prompt agents a restart stopped mid-turn ([#894](https://github.com/theclifmeister/terminatr/issues/894)) ([08af14d](https://github.com/theclifmeister/terminatr/commit/08af14d328741fd4068104cab0715c5e6676d178))
* **tui:** phones in the Network popup — pair with a QR code, level, rename, remove; last_up ([#885](https://github.com/theclifmeister/terminatr/issues/885)) ([e9b770c](https://github.com/theclifmeister/terminatr/commit/e9b770c2ca865170847534d685f72aa743f8a363))


### Bug Fixes

* **app:** centre the working spinner in tool rows on the label's line ([#880](https://github.com/theclifmeister/terminatr/issues/880)) ([b7c3236](https://github.com/theclifmeister/terminatr/commit/b7c323647167e867771e6bb5bd1ed8c5078ff3d0))
* **app:** draw Markdown pipe tables as tables in the Mac and iPhone conversation views ([#877](https://github.com/theclifmeister/terminatr/issues/877)) ([90aa3c0](https://github.com/theclifmeister/terminatr/commit/90aa3c0a886c01ebd551eb752a0aef8100735308))
* **app:** Jump to latest wins over a flick still decelerating, on the iPhone and the Mac ([#892](https://github.com/theclifmeister/terminatr/issues/892)) ([c38fbb5](https://github.com/theclifmeister/terminatr/commit/c38fbb5baf0c61dfbb61c2257f12f6f231dddeb5))
* **device:** pairing a known phone again with another write key drops it to view and says the key changed ([#890](https://github.com/theclifmeister/terminatr/issues/890)) ([6e1cf78](https://github.com/theclifmeister/terminatr/commit/6e1cf7884738603579b8518977340de783d0e932))
* **guard:** let a scratch tm of a thread's own through; follow line variables; say where human-only commands run ([#895](https://github.com/theclifmeister/terminatr/issues/895)) ([14714f5](https://github.com/theclifmeister/terminatr/commit/14714f538b3759ef6164d32b6eadb7259e2f770a))
* **ios:** tm's own prompts show as a muted folded line on the iPhone, as on the Mac ([#882](https://github.com/theclifmeister/terminatr/issues/882)) ([32a6009](https://github.com/theclifmeister/terminatr/commit/32a600961b7c42fec8fa0718b0d67de202c908f2))
* **macos:** paired servers' threads show their title in the sidebar ([#875](https://github.com/theclifmeister/terminatr/issues/875)) ([281f01f](https://github.com/theclifmeister/terminatr/commit/281f01f5fbbbc730e49032cc2ca28b7acd4d0334))
* **macos:** the bridge keeps the human's TERMINATR_SOCKET, still dropping a session's ([#891](https://github.com/theclifmeister/terminatr/issues/891)) ([78d04e1](https://github.com/theclifmeister/terminatr/commit/78d04e15db9e91f9db5d74019bec9a596f32fd89))
* **relay:** harden the iPhone relay client, the self-signed state reads and the Docker deploy ([#884](https://github.com/theclifmeister/terminatr/issues/884)) ([3d4ee81](https://github.com/theclifmeister/terminatr/commit/3d4ee8142741745e09ce82489f1570ad1fb4e3b4))
* **server:** keys from a paired server's pane or a device never answer an escape's prompt ([#897](https://github.com/theclifmeister/terminatr/issues/897)) ([82aaf08](https://github.com/theclifmeister/terminatr/commit/82aaf08fe7d97700069422f36b403123682823b2))

## [0.59.0](https://github.com/theclifmeister/terminatr/compare/v0.58.0...v0.59.0) (2026-10-28)


### Features

* **app:** Settings › Devices on the Mac: pair an iPhone with a QR code, list, rename, remove, level (app API 26) ([#870](https://github.com/theclifmeister/terminatr/issues/870)) ([02df4d9](https://github.com/theclifmeister/terminatr/commit/02df4d905e06f94f9d80ea1b62174e8c1ad068f8))
* **app:** waiting cards breathe, and the newest one's edge runs, on Mac and iPhone ([#867](https://github.com/theclifmeister/terminatr/issues/867)) ([4afc4d3](https://github.com/theclifmeister/terminatr/commit/4afc4d37b337039037e6d97fe92810580d43e789))
* **ios:** Settings with the theme picker and appearance; text follows Dynamic Type ([#871](https://github.com/theclifmeister/terminatr/issues/871)) ([59582fe](https://github.com/theclifmeister/terminatr/commit/59582fe79a9ae032191a4b1d2e0eda555cfaf3a4))


### Bug Fixes

* **app:** Jump to latest hides at the conversation's end, and lands on it, on Mac and iPhone ([#869](https://github.com/theclifmeister/terminatr/issues/869)) ([36a0d5e](https://github.com/theclifmeister/terminatr/commit/36a0d5e58cd3396ccff74987560e392ad7a1b7aa))
* **device:** read the link's device under the Manager's lock ([#874](https://github.com/theclifmeister/terminatr/issues/874)) ([99b2249](https://github.com/theclifmeister/terminatr/commit/99b22493a902e5d6ebdf0b15391e590f0a107cdd))
* **ios:** keep the pairing's last line when the relay's close comes with it; room-id vectors; device wait log ([#872](https://github.com/theclifmeister/terminatr/issues/872)) ([00eef2e](https://github.com/theclifmeister/terminatr/commit/00eef2eef1c4473028dff78475ecfeb1e76afa2b))

## [0.58.0](https://github.com/theclifmeister/terminatr/compare/v0.57.0...v0.58.0) (2026-10-28)


### Features

* **app:** board Done fold and filter, New Task templates, Ask the Coordinator about a task (app API 20, protocol 24) ([#855](https://github.com/theclifmeister/terminatr/issues/855)) ([a7f6ff2](https://github.com/theclifmeister/terminatr/commit/a7f6ff29c3df243e6d7050cee93baf7e36daed27))
* **app:** Needs you shows only guard refusals the user can allow (app API 24) ([#863](https://github.com/theclifmeister/terminatr/issues/863)) ([8a12217](https://github.com/theclifmeister/terminatr/commit/8a122172e2316a46f98f5839677b02c96ffc6474))
* **app:** pin, new project, adopt a session, remote control and link messages (app API 22) ([#857](https://github.com/theclifmeister/terminatr/issues/857)) ([736c1f2](https://github.com/theclifmeister/terminatr/commit/736c1f21b1bd1f25564a59f908b5b7ac4a858796))
* **app:** read-only project pages: Overview, Memory, Library (app API 21) ([#854](https://github.com/theclifmeister/terminatr/issues/854)) ([40fa0d5](https://github.com/theclifmeister/terminatr/commit/40fa0d557a24c0d5d16df190bf80175a0b82b97b))
* **app:** show when a thread or coordinator is busy, and follow the conversation's end, on Mac and iPhone (app API 25, protocol 25) ([#865](https://github.com/theclifmeister/terminatr/issues/865)) ([b81875e](https://github.com/theclifmeister/terminatr/commit/b81875e1c09e3cfcd5c4e31737088be26c0afd41))
* **device:** answer level with Face ID-signed writes; device-escape-off e2e; relay --max-per-ip 32 (app API 23) ([#861](https://github.com/theclifmeister/terminatr/issues/861)) ([f5dfd82](https://github.com/theclifmeister/terminatr/commit/f5dfd82ff18e8ba9e7a85e6c82a750e281d148f2))
* **ios:** connect the iPhone app to a real server — pairing by camera, device link, asleep/offline state ([#856](https://github.com/theclifmeister/terminatr/issues/856)) ([ce8f794](https://github.com/theclifmeister/terminatr/commit/ce8f79467831be06fe94319691e9a08c908be5f6))
* **ios:** project Overview, Memory, Library, board filter and Done fold, task detail on the iPhone ([#860](https://github.com/theclifmeister/terminatr/issues/860)) ([6d9f106](https://github.com/theclifmeister/terminatr/commit/6d9f106cd4a83a4bd629ee2c96a44677480a949e))
* **ios:** TestFlight prerequisites and release upload job ([#858](https://github.com/theclifmeister/terminatr/issues/858)) ([7aaaed6](https://github.com/theclifmeister/terminatr/commit/7aaaed65664e02b43641dcd5e34749cc3bcfa79d))


### Bug Fixes

* **app:** terminal box gets the pages' top margin under the title bar ([#862](https://github.com/theclifmeister/terminatr/issues/862)) ([cfec97d](https://github.com/theclifmeister/terminatr/commit/cfec97de6662e489fa166dff5531675299be6395))
* **app:** Terminal tab scrolls back through the pane's history, wheel to mouse-tracking apps ([#864](https://github.com/theclifmeister/terminatr/issues/864)) ([aa445cc](https://github.com/theclifmeister/terminatr/commit/aa445cc4960a4230242b7c5a2c65d6128d29f86b))
* five flaky tests — server tests off the network, peer per-mux stall, bridge stderr, models WaitGroup, moddev save ([#851](https://github.com/theclifmeister/terminatr/issues/851)) ([aa23798](https://github.com/theclifmeister/terminatr/commit/aa237986a6fdec7e4c91f90648742e5d4253cd52))

## [0.57.0](https://github.com/theclifmeister/terminatr/compare/v0.56.1...v0.57.0) (2026-10-28)


### Features

* **app:** a task view for review: notes, steps, Blocked on, how to check and the PR ([#842](https://github.com/theclifmeister/terminatr/issues/842)) ([ad9fcb8](https://github.com/theclifmeister/terminatr/commit/ad9fcb81f5f43cea62928e0ea6f2cb7111ad7744))
* **app:** answer a guard refusal from Needs you ([#845](https://github.com/theclifmeister/terminatr/issues/845)) ([e332335](https://github.com/theclifmeister/terminatr/commit/e332335a1cb2b155f6aa83e07b636a439134d3dd))
* **app:** answer link needs and share requests in place ([#844](https://github.com/theclifmeister/terminatr/issues/844)) ([edc1c09](https://github.com/theclifmeister/terminatr/commit/edc1c0916333fa2656dceaa8f852d3c73ef5d844))
* **app:** coordinator controls — Save & Clear, Activate/Deactivate, context warning (app API 13) ([#841](https://github.com/theclifmeister/terminatr/issues/841)) ([3dbe5d2](https://github.com/theclifmeister/terminatr/commit/3dbe5d21f2662ed4ada5034986c341f63f16632a))
* **app:** paste, drop or pick files into the composer ([#843](https://github.com/theclifmeister/terminatr/issues/843)) ([69fa4db](https://github.com/theclifmeister/terminatr/commit/69fa4dbb07222f14e07b09b4b19544552ec797a6))
* **device:** pair phones as devices; device links run the app API on the server (view level); transcript tail (app API 19) ([#850](https://github.com/theclifmeister/terminatr/issues/850)) ([9aaa705](https://github.com/theclifmeister/terminatr/commit/9aaa705ac6a98eded604d89fe40f86ca4688cc1c))
* **ios:** iPhone app skeleton — multiplatform TerminatrKit, TerminatrUI, demo mode, read-only screens ([#849](https://github.com/theclifmeister/terminatr/issues/849)) ([760a3d3](https://github.com/theclifmeister/terminatr/commit/760a3d3fee533645624675952a2086e45c960ea7))
* **ios:** TerminatrLink — phone link over the relay with pinned Ed25519 TLS ([#848](https://github.com/theclifmeister/terminatr/issues/848)) ([7567386](https://github.com/theclifmeister/terminatr/commit/756738602b4019855e5f144df5c13e748c3ad72d))
* the Mac app may pick an agent's 'allow for this session' option ([#847](https://github.com/theclifmeister/terminatr/issues/847)) ([9e35c17](https://github.com/theclifmeister/terminatr/commit/9e35c17e820e06f3dd0991ce280e64f576458d40))

## [0.56.1](https://github.com/theclifmeister/terminatr/compare/v0.56.0...v0.56.1) (2026-10-27)


### Bug Fixes

* **app:** a paired server's projects in the sidebar look and fold as this server's ([#837](https://github.com/theclifmeister/terminatr/issues/837)) ([2625be5](https://github.com/theclifmeister/terminatr/commit/2625be5ac281c8e05328ce63a3a585dc4ebb64d7))
* **app:** split view columns' least widths are fixed, never their content's ([#839](https://github.com/theclifmeister/terminatr/issues/839)) ([84ae134](https://github.com/theclifmeister/terminatr/commit/84ae1340d04365226b5ee9456ea547479571c58c))

## [0.56.0](https://github.com/theclifmeister/terminatr/compare/v0.55.0...v0.56.0) (2026-10-27)


### Features

* **app:** a clear outcome row after Accept, Send back or Delegate ([#824](https://github.com/theclifmeister/terminatr/issues/824)) ([1a48ad5](https://github.com/theclifmeister/terminatr/commit/1a48ad5dc30afef925288cbf6180227b78189c2f))
* **app:** app icon and a menu bar glyph ([#825](https://github.com/theclifmeister/terminatr/issues/825)) ([23e3a50](https://github.com/theclifmeister/terminatr/commit/23e3a5059768793c9359c17bc9ea873560d66781))
* **app:** AskUserQuestion shows a real answer card, not a spinner ([#828](https://github.com/theclifmeister/terminatr/issues/828)) ([4993fc4](https://github.com/theclifmeister/terminatr/commit/4993fc4dc0412468f5ab92703b97e2be4e24ff86))
* **app:** auto-fit the terminal, align its boxes, and widen the inspector to its toolbar ([#820](https://github.com/theclifmeister/terminatr/issues/820)) ([0016168](https://github.com/theclifmeister/terminatr/commit/0016168d4a62447c43f0b2e2f1e586d26f6868bb))
* **app:** group consecutive tool calls in the conversation ([#822](https://github.com/theclifmeister/terminatr/issues/822)) ([47afa33](https://github.com/theclifmeister/terminatr/commit/47afa332a6dfdd83fa6d1423dd1d696ac7474ff7))
* **app:** no Terminal tab footer; Repaint in the context menu and View (⌥⌘R) ([#827](https://github.com/theclifmeister/terminatr/issues/827)) ([9b481ed](https://github.com/theclifmeister/terminatr/commit/9b481edc29e5b9ae2dc78b4b59752bad8d59b430))
* **app:** pages use the full window width ([#832](https://github.com/theclifmeister/terminatr/issues/832)) ([635113f](https://github.com/theclifmeister/terminatr/commit/635113fda01ef0e0a7781fbf4d94b43d6e7743d3))
* **app:** paired projects' watched threads and needs_first in the sidebar ([#830](https://github.com/theclifmeister/terminatr/issues/830)) ([c1ca69c](https://github.com/theclifmeister/terminatr/commit/c1ca69cc65bdaba1b792aa79cfdc4b9f54285711))
* **app:** SendUserFile files and pasted images show inline in the conversation ([#831](https://github.com/theclifmeister/terminatr/issues/831)) ([f3f7b1c](https://github.com/theclifmeister/terminatr/commit/f3f7b1cf8aa8b3f30808494d51aef195eae46dea))
* **app:** six more themes beside Bloom, with a picker and a contrast test ([#829](https://github.com/theclifmeister/terminatr/issues/829)) ([c226bfe](https://github.com/theclifmeister/terminatr/commit/c226bfe3c9cf7bcf5a8a31ed10be924e7079013b))
* **app:** the sidebar lists the TUI's projects, coordinators and threads ([#821](https://github.com/theclifmeister/terminatr/issues/821)) ([5de2e19](https://github.com/theclifmeister/terminatr/commit/5de2e19e5ca3ccd6b0c1d2ba737104ac1bec412a))
* paired servers' conversations in the Mac app, gated by a per-pairing switch ([#833](https://github.com/theclifmeister/terminatr/issues/833)) ([abb2e1b](https://github.com/theclifmeister/terminatr/commit/abb2e1bf9d78a2a9066a09662044e5fd3b1bbaff))


### Bug Fixes

* **app:** clean scroll edges under the toolbar and above the pinned cards ([#826](https://github.com/theclifmeister/terminatr/issues/826)) ([8c24117](https://github.com/theclifmeister/terminatr/commit/8c24117ed9f7ca6c0f6bf81e772bfb0dd88d55d4))
* **app:** the inspector width watcher's warning and range; our Swift warnings fail the build ([#835](https://github.com/theclifmeister/terminatr/issues/835)) ([950fb8b](https://github.com/theclifmeister/terminatr/commit/950fb8be21041b8c2f66918f748293ec16333785))

## [0.55.0](https://github.com/theclifmeister/terminatr/compare/v0.54.0...v0.55.0) (2026-10-27)


### Features

* **app:** a lean sidebar that leaves inactive projects out ([#810](https://github.com/theclifmeister/terminatr/issues/810)) ([b3f9990](https://github.com/theclifmeister/terminatr/commit/b3f9990d78b46c062f764295fa0c47b219913ab6))
* **app:** accessibility ([#817](https://github.com/theclifmeister/terminatr/issues/817)) ([9adeea0](https://github.com/theclifmeister/terminatr/commit/9adeea0d86f0a6523291b2181915278e8119f9a5))
* **app:** app API 4 carries each project's active, paused and pinned ([#807](https://github.com/theclifmeister/terminatr/issues/807)) ([352db5f](https://github.com/theclifmeister/terminatr/commit/352db5f6c7a58125f1fb4d23bf645d32a6d5b6bd))
* **app:** app API task actions and project.open ([#809](https://github.com/theclifmeister/terminatr/issues/809)) ([9774289](https://github.com/theclifmeister/terminatr/commit/977428983fa94612eeefceb8ffa5b9e402a2a2dc))
* **app:** Bloom in both themes ([#812](https://github.com/theclifmeister/terminatr/issues/812)) ([2a2abe5](https://github.com/theclifmeister/terminatr/commit/2a2abe52f3acf230b5c703b22a3497b3304d4104))
* **app:** first run, empty and error states ([#816](https://github.com/theclifmeister/terminatr/issues/816)) ([6bd3c60](https://github.com/theclifmeister/terminatr/commit/6bd3c6043b97fbe54f4be6af5edac5de8e363c33))
* **app:** keyboard navigation and a command palette ([#813](https://github.com/theclifmeister/terminatr/issues/813)) ([bf2855e](https://github.com/theclifmeister/terminatr/commit/bf2855e96866fbcc04e5e0de28e2370d2d24087a))
* **app:** Mac menus, toolbar and Settings ([#815](https://github.com/theclifmeister/terminatr/issues/815)) ([7f81814](https://github.com/theclifmeister/terminatr/commit/7f81814cad28e669b9b461bfc6d7d1237885a716))
* **app:** notifications, menu bar and Dock badge ([#811](https://github.com/theclifmeister/terminatr/issues/811)) ([aea6fae](https://github.com/theclifmeister/terminatr/commit/aea6faeec0722b503ee853168bc33cffb6b858ea))
* **app:** one attention model for needs ([#808](https://github.com/theclifmeister/terminatr/issues/808)) ([cae2aef](https://github.com/theclifmeister/terminatr/commit/cae2aeff94435d3ea9d05d646b4bf5b67865a462))
* **app:** project, thread and coordinator detail you can act from ([#814](https://github.com/theclifmeister/terminatr/issues/814)) ([ed24c32](https://github.com/theclifmeister/terminatr/commit/ed24c32884aed9322c15b48a7a816039eeee317d))
* **macos:** one window for everything: sidebar, dashboard, coordinator, thread ([#804](https://github.com/theclifmeister/terminatr/issues/804)) ([faa598e](https://github.com/theclifmeister/terminatr/commit/faa598ef02d12071436faf44789f38f03ac6aa81))


### Performance

* **app:** a lean projects watch, and conversations drawn from their end ([#818](https://github.com/theclifmeister/terminatr/issues/818)) ([9c36554](https://github.com/theclifmeister/terminatr/commit/9c36554abc089ed998140bb149e87285976bd09f))

## [0.54.0](https://github.com/theclifmeister/terminatr/compare/v0.53.0...v0.54.0) (2026-10-26)


### Features

* **app:** paired servers' panes in the Mac app, through the peer link ([#803](https://github.com/theclifmeister/terminatr/issues/803)) ([b77d3c1](https://github.com/theclifmeister/terminatr/commit/b77d3c141cb2f6d66f9ba299e9c4c5604bcf72c0))
* **macos:** coordinator and thread windows: conversation, SwiftTerm terminal, cards, composer ([#799](https://github.com/theclifmeister/terminatr/issues/799)) ([8645930](https://github.com/theclifmeister/terminatr/commit/8645930157f452758eedcf019d3e31342d90cd36))
* **macos:** menu bar extra, notifications, answer buttons and open at login ([#798](https://github.com/theclifmeister/terminatr/issues/798)) ([9023b8a](https://github.com/theclifmeister/terminatr/commit/9023b8a09d6ebe695a175307c9ca9553717106d8))
* **macos:** ship the Mac app: signed, notarised release, terminatr-app cask, tm update installs it ([#802](https://github.com/theclifmeister/terminatr/issues/802)) ([0313cd5](https://github.com/theclifmeister/terminatr/commit/0313cd51e6eb07309324ca31550c977478857c12))

## [0.53.0](https://github.com/theclifmeister/terminatr/compare/v0.52.0...v0.53.0) (2026-10-26)


### Features

* **app:** tm app bridge, the app API, and session.approve ([#796](https://github.com/theclifmeister/terminatr/issues/796)) ([182841a](https://github.com/theclifmeister/terminatr/commit/182841aefa1e637edec520b1c09d13f37e862c35))
* **macos:** the Mac app's Swift package, bridge client and projects window ([#797](https://github.com/theclifmeister/terminatr/issues/797)) ([5b1c0f6](https://github.com/theclifmeister/terminatr/commit/5b1c0f63dbe1d92a723d650286838651348ba0e7))
* pane.vt and transcript.watch, the macOS app's pane and conversation streams ([#795](https://github.com/theclifmeister/terminatr/issues/795)) ([8bb7168](https://github.com/theclifmeister/terminatr/commit/8bb716821319435e1ee8d201037636022a5bb502))
* **tui:** a short prefix bar, ? for every key ([#792](https://github.com/theclifmeister/terminatr/issues/792)) ([ce02ee3](https://github.com/theclifmeister/terminatr/commit/ce02ee37c629170d0513dab0310e9b2ea733d3b2))


### Bug Fixes

* **mods:** the Memory tab shows a wikilink in a file as its text ([#794](https://github.com/theclifmeister/terminatr/issues/794)) ([03ab847](https://github.com/theclifmeister/terminatr/commit/03ab8477c523b7f45ca3622e1eab607b704234ce))
* **tui:** the Network popup highlights the selected row across its width ([#791](https://github.com/theclifmeister/terminatr/issues/791)) ([9c5fbf4](https://github.com/theclifmeister/terminatr/commit/9c5fbf422985936abcf9b5a9f159f080029b2e5f))

## [0.52.0](https://github.com/theclifmeister/terminatr/compare/v0.51.0...v0.52.0) (2026-10-25)


### Features

* **mdfile:** front matter writes YAML, with a one-time migration ([#777](https://github.com/theclifmeister/terminatr/issues/777)) ([a28622c](https://github.com/theclifmeister/terminatr/commit/a28622c7331979b48ac1c720fdd228847c106e00))
* **vault:** daily notes from the journals ([#784](https://github.com/theclifmeister/terminatr/issues/784)) ([cc098c3](https://github.com/theclifmeister/terminatr/commit/cc098c3f19590b565f77bdfb135eed88be0cc559))
* **vault:** design notes and canvases ([#783](https://github.com/theclifmeister/terminatr/issues/783)) ([bf4493a](https://github.com/theclifmeister/terminatr/commit/bf4493a130387849347235776773bb6a2835e634))
* **vault:** from-obsidian items, read-only owned notes ([#787](https://github.com/theclifmeister/terminatr/issues/787)) ([d9891f5](https://github.com/theclifmeister/terminatr/commit/d9891f5399ae9a05e67aa89e58accd019fa9a047))
* **vault:** Obsidian plugin, the NEEDS YOU pane ([#788](https://github.com/theclifmeister/terminatr/issues/788)) ([4a2e65b](https://github.com/theclifmeister/terminatr/commit/4a2e65bd8d23122562753748bb280a775c928114))
* **vault:** paired servers' stubs and cross-project link checks ([#782](https://github.com/theclifmeister/terminatr/issues/782)) ([51ae692](https://github.com/theclifmeister/terminatr/commit/51ae692dffbf7ddd5e798fa6dffe95a5c7715d98))
* **vault:** task and thread notes, tags, the vault guard ([#781](https://github.com/theclifmeister/terminatr/issues/781)) ([3679a52](https://github.com/theclifmeister/terminatr/commit/3679a52d19db4a8f8a3f0597dd24040e88ec8c5b))
* **vault:** tm vault init, render, doctor, off ([#785](https://github.com/theclifmeister/terminatr/issues/785)) ([c44bf96](https://github.com/theclifmeister/terminatr/commit/c44bf96a712ce8cc13cdd4ddb5382373ad00832a))
* **vault:** wikilinks in what tm writes ([#780](https://github.com/theclifmeister/terminatr/issues/780)) ([ea5e0af](https://github.com/theclifmeister/terminatr/commit/ea5e0afb7eb6e9244aeada36bdd0dcb13efd82c3))


### Bug Fixes

* **threads:** a report's PR line counts only for a PR of the thread's branch ([#779](https://github.com/theclifmeister/terminatr/issues/779)) ([446b4b7](https://github.com/theclifmeister/terminatr/commit/446b4b715971980767ce840df594460b6c7ef6bd))
* **vault:** one path for every task note, archived or not ([#786](https://github.com/theclifmeister/terminatr/issues/786)) ([8d96134](https://github.com/theclifmeister/terminatr/commit/8d96134839413abc7f8e56013781ead8c10143d0))

## [0.51.0](https://github.com/theclifmeister/terminatr/compare/v0.50.0...v0.51.0) (2026-10-24)


### Features

* **cli:** tm needs, tm task accept|send-back and tm ask answer ([#774](https://github.com/theclifmeister/terminatr/issues/774)) ([8ffcbec](https://github.com/theclifmeister/terminatr/commit/8ffcbece2a67a2b54137000d1a3ed5ecc413692c))
* **mdfile:** front matter reads YAML between --- lines too ([#773](https://github.com/theclifmeister/terminatr/issues/773)) ([c0f8a08](https://github.com/theclifmeister/terminatr/commit/c0f8a0805f9a6be1660c604bdbab547801bd85ca))

## [0.50.0](https://github.com/theclifmeister/terminatr/compare/v0.49.0...v0.50.0) (2026-10-24)


### Features

* **peer:** ask a paired server to share a project ([#765](https://github.com/theclifmeister/terminatr/issues/765)) ([01eb25f](https://github.com/theclifmeister/terminatr/commit/01eb25ffa304dc86764444e35638e879dbbd391b))
* tm network, and 'connected' and plain file words on screen ([#770](https://github.com/theclifmeister/terminatr/issues/770)) ([18605bb](https://github.com/theclifmeister/terminatr/commit/18605bbb3e1c06a828cc85675f43a61c68a53eb2))
* **tui:** links and what they need in the Network popup; Linked projects becomes a summary line ([#768](https://github.com/theclifmeister/terminatr/issues/768)) ([8e8acdb](https://github.com/theclifmeister/terminatr/commit/8e8acdb9d66d77de0ae96709f54b54aed2b850d0))
* **tui:** NEEDS YOU list, link map and link health in the Network popup ([#771](https://github.com/theclifmeister/terminatr/issues/771)) ([f215c2a](https://github.com/theclifmeister/terminatr/commit/f215c2a9454f526efa2c848c32fd07db58140cd6))
* **tui:** popups open at 90% of the window instead of 80% ([#764](https://github.com/theclifmeister/terminatr/issues/764)) ([f91b163](https://github.com/theclifmeister/terminatr/commit/f91b16337a1017bfd1ff474285ec720811354860))
* **tui:** share requests in the Network popup ([#769](https://github.com/theclifmeister/terminatr/issues/769)) ([65b3160](https://github.com/theclifmeister/terminatr/commit/65b3160744258bd9703501aced07cca17084ff77))
* **tui:** the Network popup for servers and sharing; the Peers tab goes ([#766](https://github.com/theclifmeister/terminatr/issues/766)) ([7cf2e80](https://github.com/theclifmeister/terminatr/commit/7cf2e800b54fdec0eb21d6dca22fe6da5462c614))


### Bug Fixes

* **relay:** fold scanners' TLS handshake errors into one line an hour ([#761](https://github.com/theclifmeister/terminatr/issues/761)) ([1731495](https://github.com/theclifmeister/terminatr/commit/17314956c3fab6024386c4ebba135c67ef30b802))
* **tui:** a long host name is cut, never '· this server', on this server's sidebar header ([#767](https://github.com/theclifmeister/terminatr/issues/767)) ([1f3c2ae](https://github.com/theclifmeister/terminatr/commit/1f3c2ae31c215471693f3a387c5425a487f14389))
* **tui:** no link glyph on this server's own sidebar header ([#763](https://github.com/theclifmeister/terminatr/issues/763)) ([b8ba81d](https://github.com/theclifmeister/terminatr/commit/b8ba81d383480957eac82b94f0201688c98d2bb2))

## [0.49.0](https://github.com/theclifmeister/terminatr/compare/v0.48.0...v0.49.0) (2026-10-23)


### Features

* **peer:** a display name for a paired server, usable as &lt;name&gt;/project ([#759](https://github.com/theclifmeister/terminatr/issues/759)) ([9dd4dda](https://github.com/theclifmeister/terminatr/commit/9dd4dda56189fa63ee53e5280a0e027e7f5d665f))


### Bug Fixes

* **e2e:** remove a test's temp dirs only after its server stops ([#756](https://github.com/theclifmeister/terminatr/issues/756)) ([cff90b7](https://github.com/theclifmeister/terminatr/commit/cff90b772e9ba1a3a1d1f5d05dc8c2065cf64408))
* **tui:** Linked projects drops a paired server's projects it no longer shares ([#757](https://github.com/theclifmeister/terminatr/issues/757)) ([a405bac](https://github.com/theclifmeister/terminatr/commit/a405bac31c8e2371459a9f3aeed1d2b5adcf9026))

## [0.48.0](https://github.com/theclifmeister/terminatr/compare/v0.47.0...v0.48.0) (2026-10-23)


### Features

* **link:** turning a link's files to auto or off answers the offers waiting on it ([#753](https://github.com/theclifmeister/terminatr/issues/753)) ([4c7c6fa](https://github.com/theclifmeister/terminatr/commit/4c7c6fa0b5932b021104fa6c07dbf9ba4f667fab))
* **tui:** Messages tab is one list of all messages, newest first, with a detail dialog ([#754](https://github.com/theclifmeister/terminatr/issues/754)) ([473bc83](https://github.com/theclifmeister/terminatr/commit/473bc836bb9aa15c5fcd2c4e60a42b0d2506b63f))


### Bug Fixes

* **guard:** a loop variable over plain relative words is no unplaced folder ([#751](https://github.com/theclifmeister/terminatr/issues/751)) ([1c7f2f4](https://github.com/theclifmeister/terminatr/commit/1c7f2f4decd5d5e43381fb414726f42fc8718439))

## [0.47.0](https://github.com/theclifmeister/terminatr/compare/v0.46.0...v0.47.0) (2026-10-22)


### Features

* **link:** files over links: offer, consent, stream, resume ([#747](https://github.com/theclifmeister/terminatr/issues/747)) ([819d335](https://github.com/theclifmeister/terminatr/commit/819d3358f3d4aa5084314c7a996db8f5a0b3b209))
* **link:** queue messages over the rate instead of refusing; link limits as settings ([#741](https://github.com/theclifmeister/terminatr/issues/741)) ([14b96c8](https://github.com/theclifmeister/terminatr/commit/14b96c8574ff8678a8bc13f53a39e068cb506fd8))
* **link:** tm link drop also drops a message queued over the rate ([#745](https://github.com/theclifmeister/terminatr/issues/745)) ([bf5b043](https://github.com/theclifmeister/terminatr/commit/bf5b04367dd42b189d17f29c1841c82f0b78e91b))
* **peer,tui:** read-only project popup for a paired server's project ([#749](https://github.com/theclifmeister/terminatr/issues/749)) ([e190436](https://github.com/theclifmeister/terminatr/commit/e1904362e2d78266e5b0cc241bfc68fe9f90edbd))
* **tui:** a real cursor, line-editing keys and placeholders in edit dialogs ([#735](https://github.com/theclifmeister/terminatr/issues/735)) ([ca57597](https://github.com/theclifmeister/terminatr/commit/ca575977d578f3530ec8dc2a492cdb241b069f4c))


### Bug Fixes

* **guard:** judge commands after a cd instead of refusing them ([#740](https://github.com/theclifmeister/terminatr/issues/740)) ([c68468e](https://github.com/theclifmeister/terminatr/commit/c68468e7789f1a4314e6bf6c94ef7b946aca26c7))
* **guard:** quoted text is no function definition; summaries show paths as written ([#743](https://github.com/theclifmeister/terminatr/issues/743)) ([83a6e66](https://github.com/theclifmeister/terminatr/commit/83a6e66f44de1a81619919efc45e692d87de9d53))
* **guard:** tm --version is read-only; tm command refusals say settings, not files ([#738](https://github.com/theclifmeister/terminatr/issues/738)) ([3feddef](https://github.com/theclifmeister/terminatr/commit/3feddef7199edef4d2724a339529e33f533a64a4))
* **server,session,emu:** takeover only on typing; console replies never reach the agent ([#742](https://github.com/theclifmeister/terminatr/issues/742)) ([b74daf0](https://github.com/theclifmeister/terminatr/commit/b74daf08748ddee9216469c624106c754cd2d519))
* **server:** make goneEvery race-free (T672) ([#730](https://github.com/theclifmeister/terminatr/issues/730)) ([e67a4b7](https://github.com/theclifmeister/terminatr/commit/e67a4b7c954db5434900ab64c6446491eb077a9b))
* **server:** make TestAnswerBound repeatable under -count&gt;1 (T673) ([#733](https://github.com/theclifmeister/terminatr/issues/733)) ([afa17c0](https://github.com/theclifmeister/terminatr/commit/afa17c086c5cf993ca7ba12a3b1df382cce60572))
* **session,server,emu:** takeover only when a person types text into the prompt box ([#746](https://github.com/theclifmeister/terminatr/issues/746)) ([4c60051](https://github.com/theclifmeister/terminatr/commit/4c60051d882c5985a0d6b07a5987b28497f49527))
* **session:** data race on boxWatchWait in takeover tests (-race) ([#748](https://github.com/theclifmeister/terminatr/issues/748)) ([e11d7fd](https://github.com/theclifmeister/terminatr/commit/e11d7fdfd45137c3e32fc50a313bb0f716dc7901))
* **tui,link:** paired-server links in the Messages tab: whole names, real last message, queued state ([#739](https://github.com/theclifmeister/terminatr/issues/739)) ([d0accf2](https://github.com/theclifmeister/terminatr/commit/d0accf2f0487970452d030504f611772dec2e28c))
* **tui,server:** type through to a paired server's coordinator; size remote panes when shown ([#736](https://github.com/theclifmeister/terminatr/issues/736)) ([d0a9510](https://github.com/theclifmeister/terminatr/commit/d0a9510d48171948134e16a260ccaa5e113bad3e))
* **tui:** prefix+a on a paired server's project no longer opens a local project's popup ([#744](https://github.com/theclifmeister/terminatr/issues/744)) ([a6bac08](https://github.com/theclifmeister/terminatr/commit/a6bac083c4b0f51fd877a731a2376ade89011f50))

## [0.46.0](https://github.com/theclifmeister/terminatr/compare/v0.45.0...v0.46.0) (2026-10-22)


### Features

* **server:** coordinator allow rules for tm commands its settings authorize (T671) ([#729](https://github.com/theclifmeister/terminatr/issues/729)) ([d7106f4](https://github.com/theclifmeister/terminatr/commit/d7106f4dfc62f6cf51ea722e6f43e2de35d42fb7))


### Bug Fixes

* **coordinator:** run tm commands bare so allow rules match (T670) ([#728](https://github.com/theclifmeister/terminatr/issues/728)) ([5ced378](https://github.com/theclifmeister/terminatr/commit/5ced37868b26a270f8fc3dda26574521df927d29))
* **e2e:** never leave test servers running after a killed run (T667) ([#726](https://github.com/theclifmeister/terminatr/issues/726)) ([6e4d1c8](https://github.com/theclifmeister/terminatr/commit/6e4d1c8775d380b0a1d0fde295a2324b92dfd07d))
* **guard:** mapfile -C, enable -f, cdable_vars, autocd and +=/subscripted shell env ([#723](https://github.com/theclifmeister/terminatr/issues/723)) ([93b662f](https://github.com/theclifmeister/terminatr/commit/93b662f8f34e351fc536ff51069bb51bbeb6a3d7))
* **session:** separate consecutive --now steers with a blank line ([#725](https://github.com/theclifmeister/terminatr/issues/725)) ([da14646](https://github.com/theclifmeister/terminatr/commit/da14646f0d9e26d2d2fcaa1294898935399af92b))
* **ticker:** remind of inbox items left open, re-nudge after a clear (T669) ([#727](https://github.com/theclifmeister/terminatr/issues/727)) ([90ff0fd](https://github.com/theclifmeister/terminatr/commit/90ff0fdabd828d750967956853c04a8357529df9))

## [0.45.0](https://github.com/theclifmeister/terminatr/compare/v0.44.0...v0.45.0) (2026-10-21)


### ⚠ BREAKING CHANGES

* **agent:** Claude panes lose the status band, the status-line entry and the CI toast. Coordinators lose the /tm pane. Claude sessions show tokens with `no cost`. `tm thread answer --option` and `tm session ask` are gone. `[agent_mod]` settings do nothing.

### Features

* **agent:** Claude without the agent mod: usage, typed tools, /saveclear and /answer ([#709](https://github.com/theclifmeister/terminatr/issues/709)) ([ba7f057](https://github.com/theclifmeister/terminatr/commit/ba7f057d53f5ba8b7ad9beef0eb89278253bd2a1))
* **agent:** remove the Claude agent mod ([#713](https://github.com/theclifmeister/terminatr/issues/713)) ([fc674b5](https://github.com/theclifmeister/terminatr/commit/fc674b5e74c1c0d9714d9fbc0d6de890523f740d))
* **usage:** show 'no cost' instead of $0.00 for tokens with no cost source ([#712](https://github.com/theclifmeister/terminatr/issues/712)) ([c523d33](https://github.com/theclifmeister/terminatr/commit/c523d33c038b7fa45370fdeb8a5d0cc7fdd35a26))


### Bug Fixes

* **agent:** harden Claude without the agent mod ([#711](https://github.com/theclifmeister/terminatr/issues/711)) ([b03f1c2](https://github.com/theclifmeister/terminatr/commit/b03f1c2b22aeee4d74530a67d5aed4667978a118))
* **agent:** read Claude's prompt box as empty mid-turn, so --now steers ([#714](https://github.com/theclifmeister/terminatr/issues/714)) ([b98b828](https://github.com/theclifmeister/terminatr/commit/b98b8289596003a8b97e4ff899a2bf1be054840a))
* **guard:** follow a cd inside ( ), { } and substitutions; cd +N/-N; .. after a link ([#717](https://github.com/theclifmeister/terminatr/issues/717)) ([0c02a1f](https://github.com/theclifmeister/terminatr/commit/0c02a1f9b30c1c13ec365f5afef7b6b8d62823c6))
* **guard:** follow folders through loose brackets, funsubs, last pipes, loops and braces ([#719](https://github.com/theclifmeister/terminatr/issues/719)) ([4304385](https://github.com/theclifmeister/terminatr/commit/4304385e3fe9722d092c5a758747cd88520ca825))
* **guard:** hold floor paths where a write lands under folders not made yet ([#718](https://github.com/theclifmeister/terminatr/issues/718)) ([1f7a01b](https://github.com/theclifmeister/terminatr/commit/1f7a01b49ddff9129c5e46fdef58ac31456a4787))
* **guard:** judge function bodies where they're called, and copies into a protected file's folder ([#720](https://github.com/theclifmeister/terminatr/issues/720)) ([e1baf05](https://github.com/theclifmeister/terminatr/commit/e1baf0550b787048fd62693f9c7df333920b1418))
* **guard:** read trap actions, follow physical cd, keep shell state on the floor ([#716](https://github.com/theclifmeister/terminatr/issues/716)) ([a046b32](https://github.com/theclifmeister/terminatr/commit/a046b32f8e0bc974ddbf8a485784ae2b8c00aa58))
* **server:** journal a run's end before its exit event ([#707](https://github.com/theclifmeister/terminatr/issues/707)) ([cb621d6](https://github.com/theclifmeister/terminatr/commit/cb621d6a7068f8cc82ae9997fce72d55fce59ca8))

## [0.44.0](https://github.com/theclifmeister/terminatr/compare/v0.43.0...v0.44.0) (2026-10-21)


### ⚠ BREAKING CHANGES

* **keys:** shift, merge and drop single unshifted shortcuts; type through to coordinators ([#705](https://github.com/theclifmeister/terminatr/issues/705))
* **access:** access presets are gone (no `p` on the Access tab, no `tm access preset`); rules and guides a preset already added stay as plain rules, and old config files still read.

### Features

* **access:** remove access presets ([#702](https://github.com/theclifmeister/terminatr/issues/702)) ([a2b2c75](https://github.com/theclifmeister/terminatr/commit/a2b2c753d202ba867379c90a939abb2097efaa03))
* **keys:** shift, merge and drop single unshifted shortcuts; type through to coordinators ([#705](https://github.com/theclifmeister/terminatr/issues/705)) ([f890a19](https://github.com/theclifmeister/terminatr/commit/f890a1961a6688796cc5fbc09ec72a00b0447c35))
* **upkeep:** combined memory budget, upkeep prompt after a clear, note-writing guide ([#696](https://github.com/theclifmeister/terminatr/issues/696)) ([172be43](https://github.com/theclifmeister/terminatr/commit/172be43c8a997560724120f7d2377b4c07e08fdd))


### Bug Fixes

* **guard:** fewer tm-files false positives, and the floor reads the shell as the shell does ([#703](https://github.com/theclifmeister/terminatr/issues/703)) ([8c8dec7](https://github.com/theclifmeister/terminatr/commit/8c8dec79184aa0912ba12eef6364469cc2046143))
* **threads:** --now and --interrupt know tm's own pending steer ([#701](https://github.com/theclifmeister/terminatr/issues/701)) ([b6082a3](https://github.com/theclifmeister/terminatr/commit/b6082a3bdad73252c6516077eeb7e68abe9f80cb))

## [0.43.0](https://github.com/theclifmeister/terminatr/compare/v0.42.0...v0.43.0) (2026-10-21)


### ⚠ BREAKING CHANGES

* **access:** protection levels, open by default ([#693](https://github.com/theclifmeister/terminatr/issues/693))

### Features

* **access:** protection levels, open by default ([#693](https://github.com/theclifmeister/terminatr/issues/693)) ([01be8bc](https://github.com/theclifmeister/terminatr/commit/01be8bc69a8ad6f8742e99a776623eba566f9992))

## [0.42.0](https://github.com/theclifmeister/terminatr/compare/v0.41.0...v0.42.0) (2026-10-20)


### Features

* **threads:** pick a repo per task, refuse to guess with several, allow none ([#683](https://github.com/theclifmeister/terminatr/issues/683)) ([8d23a76](https://github.com/theclifmeister/terminatr/commit/8d23a76654d18d60e6b56769533b1dbf7237cd8e))
* **threads:** plain-folder discard needs --approved-by-user; List skips a mismatched record ([#690](https://github.com/theclifmeister/terminatr/issues/690)) ([32f6916](https://github.com/theclifmeister/terminatr/commit/32f691615514753ca42ef7fd227f31f17df1c22c))


### Bug Fixes

* **access:** say an excluded command without * takes only the bare command ([#688](https://github.com/theclifmeister/terminatr/issues/688)) ([95a273d](https://github.com/theclifmeister/terminatr/commit/95a273d9fdd984970871a8c8233304d384653b20))
* **claude:** give the context through command hooks when another mod skips the mod's ([#689](https://github.com/theclifmeister/terminatr/issues/689)) ([8f3464f](https://github.com/theclifmeister/terminatr/commit/8f3464f8e55d273e713c223bfd6c4224b9f5cd5e))
* **claude:** hooks-skipped follow-ups and a best-effort prompt close in the guard ([#691](https://github.com/theclifmeister/terminatr/issues/691)) ([e0ddbbb](https://github.com/theclifmeister/terminatr/commit/e0ddbbbe1f83ca40238ef89085ecf598386b71cf))
* **threads:** tm thread merge tells no code host from no PR, and names what it asked ([#686](https://github.com/theclifmeister/terminatr/issues/686)) ([c49737c](https://github.com/theclifmeister/terminatr/commit/c49737ceb5f66d132cbfcf3f90af03e721ca5719))

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

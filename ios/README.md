# terminatr for iPhone

The iPhone app: `Terminatr.xcodeproj`, its sources in `Terminatr/`, the phone's end of a device link in `TerminatrLink/`, and TerminatrKit, TerminatrUI and TerminatrPane from the Swift package in `../macos`.
`Makefile` says how to build it, run it in the Simulator (demo or paired with a dev server) and archive it.

## iPhone Duo

The app builds with Xcode 27.1 and its iOS 27.1 SDK (T824), so it runs natively on iPhone Duo: on the outer display, and on the inner one in landscape, the tab and navigation bars move to a vertical bar on the side.
It still runs on iOS 17 and later; the 27.1 APIs are behind `#available`.
On an iPhone the app is portrait only, except on a phone with a hinge (`Duo.swift`), where the outer display turns to landscape too; the inner display ignores the lock anyway.
CI's `ios-app` job and the release's `testflight` job run on GitHub's `xcode-27` image (a preview), the only one with Xcode 27.1; the Mac app's jobs stay on `macos-latest`.
Check it in the Simulator on an iPhone Duo (iOS 27.1 runtime): `make run DEVICE="…"`; opening, closing and rotating it are buttons in Xcode's Device Hub (`simctl` can't), and `simctl io … screenshot --display=1` shoots the outer display.

## iPad

The app is universal (T830): one target and one build, for iPhone and iPad (`TARGETED_DEVICE_FAMILY = 1,2`), so the TestFlight and App Store uploads carry both and CI needs no other job.
An iPad turns all four ways (`AppDelegate` in `Duo.swift`; Info.plist's iPad orientations), which Split View, Slide Over and Stage Manager expect; an iPhone stays as above.
A regular-width window shows the two columns (`Columns.swift`), a compact one (a narrow Split View or Stage Manager window) the stack; resizing across them keeps your place (`Places.swift`).
With a hardware keyboard, Command-Return sends a prompt and Shift-Command-Return steers; list rows and cards light up under the pointer.
Check it in the Simulator on an iPad (iOS 27.0 runtime: 27.1 takes only iPhone Duo): `make run DEVICE="…"`. Resizing the window into compact width needs Stage Manager or Split View, by hand: `simctl` can't, and iPadOS refuses programmatic orientation and size changes to an app that joins multitasking.
App Store submission (not TestFlight) needs iPad screenshots: 13-inch, 2064×2752 or 2048×2732 portrait (landscape the same, turned), which the iPad Pro 13-inch (M5) Simulator shoots as is; App Store Connect scales them down for the smaller iPads. `make store-shots` makes them (App Store screenshots, below).

## Flat look

Every screen draws its own flat bar (`ScreenBar.swift`, `SessionScreen.swift`) on the theme's solid colours and hides the navigation bar, whose glass buttons and, on an open iPhone Duo, whose rail placement can't be flattened; a swipe from the edge still goes back (`swipesBack`). A filter is a field in the screen (`FilterField`), as the system's search bar needs the navigation bar. Lists are flat panels (`PanelList`), not `List`, whose grouped sections iOS rounds itself.

What stays iOS's own on iOS 27: the tab bar's glass capsule on an iPhone, the Duo's rail, an iPad's floating tab bar and sidebar, and sheets' system chrome. `TabBand` puts a solid band with a line under the capsule, so nothing shows through it. `UITabBarAppearance`, `.toolbarBackground` and `UIDesignRequiresCompatibility` don't change it (tried on the iOS 27.1 and 27.0 Simulators). Going further means replacing `TabView` with our own bar and rail.

## Terminal

A session's screen switches between its conversation and its terminal (T840), as the Mac's session window does (`SessionScreen.swift`, the switch in the navigation bar, kept per session while the server is).
The terminal (`TerminalScreen.swift`) is the pane through `pane.vt`, streamed by `PaneController` from TerminatrPane, which the Mac's Terminal tab streams with too, and drawn by SwiftTerm's iOS view.
The phone never resizes the pane for the other viewers: the grid keeps the pane's size, its font fitted to the pane's width (`PaneZoom` in TerminatrKit, 7 to 16 points), and a pinch zooms it (up to 28); past the screen it pans.
It shows the pane's screen, not its scrollback: the conversation is the history.
At the write level a tap brings the keyboard with SwiftTerm's key bar (Esc, Ctrl, Tab, the arrows, function keys), each key a signed `pane.input`; below it, and while an escape's prompt may be up, a line says why keys are off.
SwiftTerm runs a build-tool plugin, so `xcodebuild` takes `-skipPackagePluginValidation` (the Makefile passes it); Xcode asks once to trust it.
`-terminal YES` opens sessions on the terminal (the Simulator's screenshots), and `make dev-server LEVEL=write` pairs the phone at write, to type into a pane.

## Conversation scroll tests

A conversation must always show its end, whatever happens to the screen (T976). Its rows are laid out exactly: a `VStack` of the last drawn entries, with Earlier messages for more. Only SwiftUI scrolls it: its bottom anchor while it follows, and its `scrollTo` of the end row for Jump to latest. SwiftUI clamps every offset it sets to the content, so the view can't rest past its end. No code writes the scroll offset (`ConversationScroll` in TerminatrUI, `Follow` in TerminatrKit). A guard holds this anyway: a rest past the end is scrolled back, and goes to the server as an app report of kind `scroll` (`tm device reports`).

`TerminatrUITests` checks it on the demo with a scroll fixture (`-scroll-fixture YES`, `ScrollFixture`): acme-web's coordinator has a long conversation, and t-0104 streams an answer every 0.35 s. Each case asserts that the last message is on the screen, at its end:
- opening;
- switching to the terminal and to another session;
- background and foreground;
- rotating;
- the keyboard opening and closing;
- streaming;
- a fast flick, then Jump to latest, both mid-flick and at rest.

```sh
make ui-test DEVICE="iPhone 17" ITERATIONS=20      # ONLY=testStreaming runs one case
```

Run it on an iPhone, an iPad and an iPhone Duo. Neither `simctl` nor XCTest can open or close a Duo, so a fold while the app runs is checked by hand: open the Duo in Xcode's Device Hub (it then runs on the inner display), and fold and unfold it with a conversation on screen.

## TestFlight

Each release uploads the app to TestFlight: the `testflight` job of `.github/workflows/release.yml` runs `scripts/release/ios.sh` on the tag, once the release is published.

- **Version:** the tag's three numbers (`v0.57.0` and `v0.57.0-rc1` are both `0.57.0`), like the Mac app.
- **Build number:** the tag's commit count (`git rev-list --count`), which only grows along main, so every upload, prereleases included, is higher than the one before.
- **Signing:** manual, with an Apple Distribution certificate and an App Store provisioning profile for `dev.terminatr.ios` from the secrets below. A CI runner has no Xcode account, and automatic (cloud) signing there would make a new certificate on every run. The team is the project's Release `DEVELOPMENT_TEAM` (4689D3AT8J). Builds in Xcode on your Mac keep signing automatically.
- **Upload:** `xcodebuild -exportArchive` with an App Store Connect API key.
- **Without the secrets** the job is skipped (a notice on the release run says so) and the release goes on as before. With some but not all of them it fails, naming the missing ones.

To try it locally: `scripts/release/ios.sh v0.57.0` makes an unsigned archive (a dry run, no Apple account needed) in `.build/release/ios/`. With `TM_IOS_PROFILE` (the profile's path) it signs with the Apple Distribution identity in your keychain and exports `Terminatr.ipa`; add `TM_ASC_KEY` (the .p8's path), `TM_ASC_KEY_ID` and `TM_ASC_ISSUER` to upload too.

### What you do once, in your Apple account

1. **Register the App ID.** In [Certificates, Identifiers & Profiles](https://developer.apple.com/account/resources/identifiers/list), add an App ID (App) with the bundle ID `dev.terminatr.ios`, explicit. No capabilities are needed.
2. **Create the app record.** In [App Store Connect](https://appstoreconnect.apple.com/apps), add a new iOS app: name terminatr (or what's free), the bundle ID `dev.terminatr.ios`, SKU anything (`terminatr-ios`), primary language English.
3. **Make the distribution certificate.** In Certificates, add an Apple Distribution certificate (from a CSR made by Keychain Access → Certificate Assistant → Request a Certificate From a Certificate Authority, saved to disk). Open the downloaded .cer so it lands in your login keychain with its key. In Keychain Access select the certificate (My Certificates, with its key) and the Apple Worldwide Developer Relations Certification Authority intermediate (G3; [Apple PKI](https://www.apple.com/certificateauthority/) if it's missing) and export them together as a .p12 with a password, so the runner can trust the certificate.
4. **Make the provisioning profile.** In Profiles, add a Distribution → App Store Connect profile for `dev.terminatr.ios` with that certificate, and download it (`.mobileprovision`). It and the certificate expire after a year: renew both and update their secrets then.
5. **Create the API key.** In App Store Connect → Users and Access → Integrations → App Store Connect API, generate a Team key with the App Manager role. Download the .p8 (you can only download it once) and note its Key ID and the Issuer ID shown above the list. The notary key the release already has is a Developer key, which can't upload.
6. **Add the secrets** to the `release` environment (Settings → Environments → release), where the release's Apple secrets already are:

   | Secret | What |
   |---|---|
   | `ASC_KEY_ID` | the API key's Key ID |
   | `ASC_ISSUER_ID` | the Issuer ID |
   | `ASC_KEY_P8` | the whole .p8 file, as text |
   | `IOS_DIST_CERT_P12_BASE64` | the .p12 from step 3, base64 (`base64 -i cert.p12 \| pbcopy`) |
   | `IOS_DIST_CERT_PASSWORD` | its password |
   | `IOS_PROFILE_BASE64` | the profile from step 4, base64 |

   `KEYCHAIN_PASSWORD` is there already (docs/OPERATIONS.md, Releasing). The team needs no secret: it is the Release config's `DEVELOPMENT_TEAM` in `Terminatr.xcodeproj`, 4689D3AT8J, the Mac app's team.
7. **Add internal testers.** In App Store Connect → the app → TestFlight → Internal Testing, add a group and yourself (and anyone else on your team) to it. Builds then reach the group without review once processed; install TestFlight on the iPhone and accept the invitation. The first build asks for export compliance only if the Info.plist answer below is missing; it isn't.

The next release then uploads the app. To upload a release already published, rerun its `testflight` job once the secrets are set (Actions → the release run → Re-run jobs).

### Crash reports

A crash or watchdog report (`.ips`) from a TestFlight build names the app by its `slice_uuid`, and its app frames are bare offsets until the dSYM of exactly that build symbolicates them. A rebuild of the same commit doesn't match: another Xcode lays the binary out differently. So the `testflight` job keeps each build's dSYMs as the artifact `ios-dsyms-<tag>` (`Terminatr-<version>-<build>-dSYMs.zip`, 90 days), and its log lists their UUIDs ("Archive and upload"). App Store Connect has them too (`uploadSymbols`), for the reports Xcode's Organizer shows.

To read one: `gh run download <run> -n ios-dsyms-v0.73.1`, unzip, check `dwarfdump --uuid` against the report's `slice_uuid`, then for each app frame (`imageOffset`):

```sh
atos -o dSYMs/Terminatr.app.dSYM/Contents/Resources/DWARF/Terminatr -arch arm64 -l 0x100000000 0x$(printf %x $((0x100000000 + OFFSET)))
```

## App Store

The App Store listing's text and the App Review notes live in `AppStore/`, in fastlane deliver's layout (a later automated submission, T913, can read it as is), drafted in the App Store design (T908) and copied from there into App Store Connect by hand until then:

| App Store Connect field | File | Limit |
|---|---|---|
| Name | [`AppStore/en-US/name.txt`](AppStore/en-US/name.txt) | 30 characters |
| Subtitle | [`AppStore/en-US/subtitle.txt`](AppStore/en-US/subtitle.txt) | 30 characters |
| Promotional Text | [`AppStore/en-US/promotional_text.txt`](AppStore/en-US/promotional_text.txt) | 170 characters |
| Keywords | [`AppStore/en-US/keywords.txt`](AppStore/en-US/keywords.txt) | 100 bytes, commas without spaces |
| Description | [`AppStore/en-US/description.txt`](AppStore/en-US/description.txt) | 4000 characters |
| What's New | [`AppStore/en-US/release_notes.txt`](AppStore/en-US/release_notes.txt) | 4000 characters |
| Support, Marketing and Privacy Policy URLs | [`support_url.txt`](AppStore/en-US/support_url.txt), [`marketing_url.txt`](AppStore/en-US/marketing_url.txt), [`privacy_url.txt`](AppStore/en-US/privacy_url.txt) | |
| Copyright | [`AppStore/copyright.txt`](AppStore/copyright.txt) | |
| Category | [`primary_category.txt`](AppStore/primary_category.txt), [`secondary_category.txt`](AppStore/secondary_category.txt) | |
| App Review Information → Notes | [`AppStore/review_information/notes.txt`](AppStore/review_information/notes.txt) | 4000 bytes |

`make store-text` checks each file against its limit (Apple's [App information](https://developer.apple.com/help/app-store-connect/reference/app-information/app-information) and [Platform version information](https://developer.apple.com/help/app-store-connect/reference/app-information/platform-version-information) pages).
What's New holds the first version's line; later versions take the release notes' iPhone lines.

- **Names:** "Claude Code" and "Codex" appear only in the description, saying what tm works with, with a line that terminatr isn't affiliated with Anthropic or OpenAI. Under App Review 2.3.7 the name, subtitle and keywords carry no other company's or app's name.
- **App Review:** "Sign-in required" stays off: the notes send the reviewer to Explore the demo, and the submission attaches a screen recording of a real pairing (`make review-video` records it against `make dev-server`, below). A live session is offered only if Apple asks; the notes can't carry a pairing code, which works once and expires in 10 minutes.
- **Privacy policy in the app** (App Review 5.1.1(i)): Settings → About links the privacy policy, support and terminatr.dev, and shows the version; the welcome screen's footer links the first two. The pages themselves are T909's, on terminatr.dev.
- **Third-party AI** (App Review 5.1.2(i)): the confirm step of pairing says, above Pair, that prompts and photos go to the user's tm server and that its agents may pass them to the AI provider set up there. The app itself never contacts one.

## App Store screenshots

`make store-shots` (T911, `store-shots.sh`) shoots the App Store's screenshots from the demo, so they carry fictional names only, into `build/store-shots/<set>/NN-screen.png`, with `index.html` to look them over:

| Set | Pixels | Simulator | |
|---|---|---|---|
| `iphone-medium` | 1206×2622 | iPhone 17 | required (Dynamic Island, medium) |
| `ipad-13` | 2064×2752 | iPad Pro 13-inch (M5) | required |
| `iphone-large` | 1320×2868 | iPhone 17 Pro Max | |
| `duo-outer` | 1398×2034 | iPhone Duo, closed | required from April 2027 |
| `duo-inner` | 2007×2853 | iPhone Duo, opened | required from April 2027; by hand, below |

The screens, in the store's order: Needs you, answering T14's question, T12's conversation at its approval, T12's terminal, the acme-web project and its board. The outer Duo display takes the first, second and terminal; the iPad all but Needs you, which in portrait shows its list beside an empty detail, and its board with the sidebar closed (`-sidebar NO`), which open lies over the list.
Each set gets a new Simulator, deleted after, in light appearance with a 9:41 status bar; every file is checked against its set's size and written without alpha, which App Store Connect refuses.
`SETS="iphone-medium ipad-13"` shoots only those.

The demo's times are recorded ones that move to now as it opens (its `now`, `DemoTransport`), so ages read the same on any day ("19m"), and the script launches the app in the time zone where now is nearest 10:00, so its clock times read within half an hour of 9:02 to 9:41, as recorded, on any run.
The demo's terminal is T12's pane, recorded in its `panes`.

App Review's screen recording of a real pairing: start `make dev-server LEVEL=write`, then `make review-video`, which records a fresh DEVICE to `build/store-shots/review.mp4` while it pairs with the dev server's code; you answer a question, send a prompt and type into a terminal (simctl can't tap), and Return stops it.

simctl can't open an iPhone Duo or turn a device, so every set is portrait and `duo-inner` isn't in the default sets: create an iPhone Duo Simulator, open it in Xcode's Device Hub, and run `make store-shots SETS=duo-inner DUO=<its UDID>`.

## Encryption

`Info.plist` says `ITSAppUsesNonExemptEncryption` is `NO`, so App Store Connect doesn't ask about export compliance on every upload.

The app's only encryption is TLS 1.3: to the relay (Apple's, through Network.framework) and, inside that, end to end to the user's own tm server (BoringSSL, through swift-nio-ssl, because Apple's TLS takes no Ed25519 identities), with standard algorithms only (X25519, Ed25519, AES-GCM and ChaCha20-Poly1305, SHA-2, HKDF). There is nothing proprietary. Apple's [table of what each kind of encryption needs](https://developer.apple.com/help/app-store-connect/reference/export-compliance-documentation-for-encryption) puts "an industry standard algorithm, not provided within the Apple operating system" down for one document only: the French encryption declaration, and only when the app is on the App Store in France. TestFlight isn't the App Store, so the app needs no documentation, and [Apple's guidance for the key](https://developer.apple.com/documentation/security/complying-with-encryption-export-regulations) is `NO` for an app that is exempt from export documentation.

Before the app goes on the App Store in France, file the French declaration, upload it in App Store Connect (App Information → App Encryption Documentation), and set the key to `YES` with the `ITSEncryptionExportComplianceCode` Apple gives back. Apple also notes that an app using exempt encryption may owe the US government a year-end self-classification report; check whether that still applies when the app goes on sale.

## Privacy

`Terminatr/PrivacyInfo.xcprivacy` declares no tracking and no collected data: the app talks only to the user's own tm server, end to end through a relay that can't read the link. Its one required-reason API is UserDefaults, for its own launch arguments (reason CA92.1). The swift-nio packages ship their own manifests. In App Store Connect, the app's privacy answer is then Data Not Collected, as long as the relay only forwards the link and keeps nothing.

The app's crash and hang reports (T957) keep that answer true. They are MetricKit's diagnostics, which iOS hands the app on a later launch; the app keeps them in its own container until it is linked, then sends each over the device link to the user's own tm server (app API `appreport.add`), which keeps them for `tm device reports`. They go nowhere else: no crash service, no developer server, and the relay can't read them. Apple counts data as collected only when the developer or a partner can access it, which never happens here. Whether a device shares analytics with Apple and developers (Settings › Privacy & Security › Analytics & Improvements) is the user's own switch and doesn't change what this app sends.

## Icon

The app's icon is the Mac app's, `../macos/Icon/AppIcon.icon` (Icon Composer), which the project compiles for iOS: every iPhone size, the 1024-pixel App Store icon, and the dark, tinted and clear looks. Edit it there; the iPhone app follows on its next build, the Mac app once `macos/Icon/icon.sh` regenerates its compiled copies.

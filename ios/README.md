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
App Store submission (not TestFlight) needs iPad screenshots: 13-inch, 2064×2752 or 2048×2732 portrait (landscape the same, turned), which the iPad Pro 13-inch (M5) Simulator shoots as is; App Store Connect scales them down for the smaller iPads.

## Terminal

A session's screen switches between its conversation and its terminal (T840), as the Mac's session window does (`SessionScreen.swift`, the switch in the navigation bar, kept per session while the server is).
The terminal (`TerminalScreen.swift`) is the pane through `pane.vt`, streamed by `PaneController` from TerminatrPane, which the Mac's Terminal tab streams with too, and drawn by SwiftTerm's iOS view.
The phone never resizes the pane for the other viewers: the grid keeps the pane's size, its font fitted to the pane's width (`PaneZoom` in TerminatrKit, 7 to 16 points), and a pinch zooms it (up to 28); past the screen it pans.
It shows the pane's screen, not its scrollback: the conversation is the history.
At the write level a tap brings the keyboard with SwiftTerm's key bar (Esc, Ctrl, Tab, the arrows, function keys), each key a signed `pane.input`; below it, and while an escape's prompt may be up, a line says why keys are off.
SwiftTerm runs a build-tool plugin, so `xcodebuild` takes `-skipPackagePluginValidation` (the Makefile passes it); Xcode asks once to trust it.
`-terminal YES` opens sessions on the terminal (the Simulator's screenshots), and `make dev-server LEVEL=write` pairs the phone at write, to type into a pane.

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
- **App Review:** "Sign-in required" stays off: the notes send the reviewer to Explore the demo, and the submission attaches a screen recording of a real pairing (T911 records it against `make dev-server`). A live session is offered only if Apple asks; the notes can't carry a pairing code, which works once and expires in 10 minutes.
- **Privacy policy in the app** (App Review 5.1.1(i)): Settings → About links the privacy policy, support and terminatr.dev, and shows the version; the welcome screen's footer links the first two. The pages themselves are T909's, on terminatr.dev.
- **Third-party AI** (App Review 5.1.2(i)): the confirm step of pairing says, above Pair, that prompts and photos go to the user's tm server and that its agents may pass them to the AI provider set up there. The app itself never contacts one.

## Encryption

`Info.plist` says `ITSAppUsesNonExemptEncryption` is `NO`, so App Store Connect doesn't ask about export compliance on every upload.

The app's only encryption is TLS 1.3: to the relay (Apple's, through Network.framework) and, inside that, end to end to the user's own tm server (BoringSSL, through swift-nio-ssl, because Apple's TLS takes no Ed25519 identities), with standard algorithms only (X25519, Ed25519, AES-GCM and ChaCha20-Poly1305, SHA-2, HKDF). There is nothing proprietary. Apple's [table of what each kind of encryption needs](https://developer.apple.com/help/app-store-connect/reference/export-compliance-documentation-for-encryption) puts "an industry standard algorithm, not provided within the Apple operating system" down for one document only: the French encryption declaration, and only when the app is on the App Store in France. TestFlight isn't the App Store, so the app needs no documentation, and [Apple's guidance for the key](https://developer.apple.com/documentation/security/complying-with-encryption-export-regulations) is `NO` for an app that is exempt from export documentation.

Before the app goes on the App Store in France, file the French declaration, upload it in App Store Connect (App Information → App Encryption Documentation), and set the key to `YES` with the `ITSEncryptionExportComplianceCode` Apple gives back. Apple also notes that an app using exempt encryption may owe the US government a year-end self-classification report; check whether that still applies when the app goes on sale.

## Privacy

`Terminatr/PrivacyInfo.xcprivacy` declares no tracking and no collected data: the app talks only to the user's own tm server, end to end through a relay that can't read the link. Its one required-reason API is UserDefaults, for its own launch arguments (reason CA92.1). The swift-nio packages ship their own manifests. In App Store Connect, the app's privacy answer is then Data Not Collected, as long as the relay only forwards the link and keeps nothing.

## Icon

The app's icon is the Mac app's, `../macos/Icon/AppIcon.icon` (Icon Composer), which the project compiles for iOS: every iPhone size, the 1024-pixel App Store icon, and the dark, tinted and clear looks. Edit it there; the iPhone app follows on its next build, the Mac app once `macos/Icon/icon.sh` regenerates its compiled copies.

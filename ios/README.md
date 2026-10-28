# terminatr for iPhone

The iPhone app: `Terminatr.xcodeproj`, its sources in `Terminatr/`, the phone's end of a device link in `TerminatrLink/`, and TerminatrKit and TerminatrUI from the Swift package in `../macos`.
`Makefile` says how to build it, run it in the Simulator (demo or paired with a dev server) and archive it.

## TestFlight

Each release uploads the app to TestFlight: the `testflight` job of `.github/workflows/release.yml` runs `scripts/release/ios.sh` on the tag, once the release is published.

- **Version:** the tag's three numbers (`v0.57.0` and `v0.57.0-rc1` are both `0.57.0`), like the Mac app.
- **Build number:** the tag's commit count (`git rev-list --count`), which only grows along main, so every upload, prereleases included, is higher than the one before.
- **Signing:** manual, with an Apple Distribution certificate and an App Store provisioning profile for `dev.terminatr.ios` from the secrets below. A CI runner has no Xcode account, and automatic (cloud) signing there would make a new certificate on every run. Builds in Xcode on your Mac keep signing automatically with your own team.
- **Upload:** `xcodebuild -exportArchive` with an App Store Connect API key.
- **Without the secrets** the job is skipped (a notice on the release run says so) and the release goes on as before. With some but not all of them it fails, naming the missing ones.

To try it locally: `scripts/release/ios.sh v0.57.0` makes an unsigned archive (a dry run, no Apple account needed) in `.build/release/ios/`. With `TM_IOS_PROFILE` (the profile's path) and `TM_TEAM_ID` it signs with the Apple Distribution identity in your keychain and exports `Terminatr.ipa`; add `TM_ASC_KEY` (the .p8's path), `TM_ASC_KEY_ID` and `TM_ASC_ISSUER` to upload too.

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

   `APPLE_TEAM_ID` and `KEYCHAIN_PASSWORD` are there already (docs/OPERATIONS.md, Releasing).
7. **Add internal testers.** In App Store Connect → the app → TestFlight → Internal Testing, add a group and yourself (and anyone else on your team) to it. Builds then reach the group without review once processed; install TestFlight on the iPhone and accept the invitation. The first build asks for export compliance only if the Info.plist answer below is missing; it isn't.

The next release then uploads the app. To upload a release already published, rerun its `testflight` job once the secrets are set (Actions → the release run → Re-run jobs).

## Encryption

`Info.plist` says `ITSAppUsesNonExemptEncryption` is `NO`, so App Store Connect doesn't ask about export compliance on every upload.

The app's only encryption is TLS 1.3: to the relay (Apple's, through Network.framework) and, inside that, end to end to the user's own tm server (BoringSSL, through swift-nio-ssl, because Apple's TLS takes no Ed25519 identities), with standard algorithms only (X25519, Ed25519, AES-GCM and ChaCha20-Poly1305, SHA-2, HKDF). There is nothing proprietary. Apple's [table of what each kind of encryption needs](https://developer.apple.com/help/app-store-connect/reference/export-compliance-documentation-for-encryption) puts "an industry standard algorithm, not provided within the Apple operating system" down for one document only: the French encryption declaration, and only when the app is on the App Store in France. TestFlight isn't the App Store, so the app needs no documentation, and [Apple's guidance for the key](https://developer.apple.com/documentation/security/complying-with-encryption-export-regulations) is `NO` for an app that is exempt from export documentation.

Before the app goes on the App Store in France, file the French declaration, upload it in App Store Connect (App Information → App Encryption Documentation), and set the key to `YES` with the `ITSEncryptionExportComplianceCode` Apple gives back. Apple also notes that an app using exempt encryption may owe the US government a year-end self-classification report; check whether that still applies when the app goes on sale.

## Privacy

`Terminatr/PrivacyInfo.xcprivacy` declares no tracking and no collected data: the app talks only to the user's own tm server, end to end through a relay that can't read the link. Its one required-reason API is UserDefaults, for its own launch arguments (reason CA92.1). The swift-nio packages ship their own manifests. In App Store Connect, the app's privacy answer is then Data Not Collected, as long as the relay only forwards the link and keeps nothing.

## Icon

The app's icon is the Mac app's, `../macos/Icon/AppIcon.icon` (Icon Composer), which the project compiles for iOS: every iPhone size, the 1024-pixel App Store icon, and the dark, tinted and clear looks. Edit it there; the iPhone app follows on its next build, the Mac app once `macos/Icon/icon.sh` regenerates its compiled copies.

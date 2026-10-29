// swift-tools-version: 6.0
//
// terminatr for Mac (design T734, docs/SPEC.md §3.8). TerminatrKit is the
// bridge client, the app API's types and the logic the windows show
// (tested here); TerminatrUI the theme and the views the Mac and iOS
// apps share (ios/ builds the phone's app from both); TerminatrPane the
// pane.vt mirror both apps' Terminal views stream (T840); Terminatr is
// the SwiftUI app. SwiftTerm (MIT) draws the
// terminal tab, pinned to its 1.x line: 1.99 previews 2.0's API.
// `make -C macos app` wraps the app in Terminatr.app (Info.plist).
import PackageDescription

// TERMINATR_WERROR=1 (macos/Makefile's default) makes our targets'
// warnings errors; SwiftTerm's stay warnings, as target settings apply
// to their own target only. Release builds (scripts/release/app.sh) go
// without, so a newer Xcode's new warning doesn't stop a release.
let werror: [SwiftSetting] = Context.environment["TERMINATR_WERROR"] == "1" ? [.unsafeFlags(["-warnings-as-errors"])] : []

let package = Package(
    name: "Terminatr",
    platforms: [.macOS(.v14), .iOS(.v17)],
    products: [
        .executable(name: "Terminatr", targets: ["Terminatr"]),
        .library(name: "TerminatrKit", targets: ["TerminatrKit"]),
        .library(name: "TerminatrUI", targets: ["TerminatrUI"]),
        .library(name: "TerminatrPane", targets: ["TerminatrPane"]),
    ],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm", exact: "1.20.0"),
    ],
    targets: [
        .target(name: "TerminatrKit", swiftSettings: werror),
        .target(name: "TerminatrUI", dependencies: ["TerminatrKit"], swiftSettings: werror),
        .target(
            name: "TerminatrPane",
            dependencies: ["TerminatrKit", "TerminatrUI", .product(name: "SwiftTerm", package: "SwiftTerm")],
            swiftSettings: werror
        ),
        .executableTarget(
            name: "Terminatr",
            dependencies: ["TerminatrKit", "TerminatrUI", "TerminatrPane", .product(name: "SwiftTerm", package: "SwiftTerm")],
            resources: [.copy("Resources/Fonts")],
            swiftSettings: werror
        ),
        .testTarget(name: "TerminatrKitTests", dependencies: ["TerminatrKit"], swiftSettings: werror),
    ]
)

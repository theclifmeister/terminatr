// swift-tools-version: 6.0
//
// terminatr for Mac (design T734, docs/SPEC.md §3.8). TerminatrKit is the
// bridge client, the app API's types and the logic the windows show
// (tested here); Terminatr is the SwiftUI app. SwiftTerm (MIT) draws the
// terminal tab, pinned to its 1.x line: 1.99 previews 2.0's API.
// `make -C macos app` wraps the app in Terminatr.app (Info.plist).
import PackageDescription

let package = Package(
    name: "Terminatr",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "Terminatr", targets: ["Terminatr"]),
        .library(name: "TerminatrKit", targets: ["TerminatrKit"]),
    ],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm", exact: "1.20.0"),
    ],
    targets: [
        .target(name: "TerminatrKit"),
        .executableTarget(
            name: "Terminatr",
            dependencies: ["TerminatrKit", .product(name: "SwiftTerm", package: "SwiftTerm")],
            resources: [.copy("Resources/Fonts")]
        ),
        .testTarget(name: "TerminatrKitTests", dependencies: ["TerminatrKit"]),
    ]
)

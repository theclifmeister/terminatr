// swift-tools-version: 6.0
//
// terminatr for Mac (design T734, docs/SPEC.md §3.8). TerminatrKit is the
// bridge client and the app API's types; Terminatr is the SwiftUI app.
// `make -C macos app` wraps the app in Terminatr.app (Info.plist).
import PackageDescription

let package = Package(
    name: "Terminatr",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "Terminatr", targets: ["Terminatr"]),
        .library(name: "TerminatrKit", targets: ["TerminatrKit"]),
    ],
    targets: [
        .target(name: "TerminatrKit"),
        .executableTarget(
            name: "Terminatr",
            dependencies: ["TerminatrKit"],
            resources: [.copy("Resources/Fonts")]
        ),
        .testTarget(name: "TerminatrKitTests", dependencies: ["TerminatrKit"]),
    ]
)

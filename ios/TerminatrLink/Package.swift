// swift-tools-version: 6.0
//
// TerminatrLink: the phone's end of a peer link (T786 spike). It dials a
// relay, joins a link room, runs TLS 1.3 with self-signed Ed25519
// certificates on both ends with the other end's key pinned, exactly as
// peers do (internal/relay, internal/peer), and carries NDJSON lines.
// BoringSSL comes with swift-nio-ssl: Apple's TLS takes no Ed25519
// identities.
import PackageDescription

let package = Package(
    name: "TerminatrLink",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [
        .library(name: "TerminatrLink", targets: ["TerminatrLink"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-nio", exact: "2.104.0"),
        .package(url: "https://github.com/apple/swift-nio-ssl", exact: "2.37.5"),
    ],
    targets: [
        .target(name: "TerminatrLink", dependencies: [
            .product(name: "NIOCore", package: "swift-nio"),
            .product(name: "NIOPosix", package: "swift-nio"),
            .product(name: "NIOHTTP1", package: "swift-nio"),
            .product(name: "NIOWebSocket", package: "swift-nio"),
            .product(name: "NIOSSL", package: "swift-nio-ssl"),
            .product(name: "NIOTLS", package: "swift-nio"),
        ]),
        .testTarget(name: "TerminatrLinkTests", dependencies: [
            "TerminatrLink",
            .product(name: "NIOPosix", package: "swift-nio"),
            .product(name: "NIOSSL", package: "swift-nio-ssl"),
        ]),
    ]
)

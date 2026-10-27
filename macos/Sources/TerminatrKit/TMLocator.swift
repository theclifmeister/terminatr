import Foundation

// The Mac app's only: iOS runs no child processes (the phone's
// transport is a device link, docs/SPEC.md §3.8).
#if os(macOS)

/// Finds the installed tm the bridge runs. The app never embeds tm: one
/// tm per machine keeps one server, one pin and one tm update. It looks,
/// in order, at the path the user configured, the Homebrew prefixes and
/// ~/.local/bin (where tm's install script puts it).
public struct TMLocator: Sendable {
    /// The Homebrew prefixes' bin directories: Apple silicon, then Intel.
    public static let homebrewBins = ["/opt/homebrew/bin", "/usr/local/bin"]

    public var configured: String?
    public var home: String
    public var isExecutable: @Sendable (String) -> Bool

    public init(
        configured: String? = nil,
        home: String = NSHomeDirectory(),
        isExecutable: @escaping @Sendable (String) -> Bool = { FileManager.default.isExecutableFile(atPath: $0) }
    ) {
        self.configured = configured
        self.home = home
        self.isExecutable = isExecutable
    }

    /// The places looked at, in order.
    public var candidates: [String] {
        var c: [String] = []
        if let p = configured?.trimmingCharacters(in: .whitespaces), !p.isEmpty {
            c.append((p as NSString).expandingTildeInPath)
        }
        c += Self.homebrewBins.map { $0 + "/tm" }
        c.append((home as NSString).appendingPathComponent(".local/bin/tm"))
        return c
    }

    /// The first executable candidate. A configured path that isn't one
    /// is an error, not a reason to run some other tm.
    public func locate() throws -> URL {
        if let p = configured?.trimmingCharacters(in: .whitespaces), !p.isEmpty {
            let path = (p as NSString).expandingTildeInPath
            guard isExecutable(path) else { throw TMNotFound(looked: [path], configured: true) }
            return URL(fileURLWithPath: path)
        }
        let c = candidates
        guard let path = c.first(where: isExecutable) else { throw TMNotFound(looked: c, configured: false) }
        return URL(fileURLWithPath: path)
    }
}

/// No tm where the app looked: it says where, and what to install.
public struct TMNotFound: Error, LocalizedError, Equatable {
    public let looked: [String]
    public let configured: Bool

    public var errorDescription: String? {
        if configured {
            return "No tm at \(looked.joined()), the path set in Settings."
        }
        return "No tm found in \(looked.joined(separator: ", ")). Install it with Homebrew (brew install terminatr, from the theclifmeister/terminatr tap) or the install script, or set its path in Settings."
    }
}
#endif

import Foundation

// The Mac app's only, like TMLocator: iOS runs no child processes.
#if os(macOS)
import Security

/// What the signature of a program says, measured against a Team ID.
public enum TMSignature: Equatable, Sendable {
    /// Signed by that team through Apple's certificates, and intact.
    case sameTeam
    /// No developer's signature: none at all (a script, a wrapper), an
    /// ad hoc one, or Apple's own.
    case noTeam
    /// Intact or not, signed by another team.
    case otherTeam(String)
    /// Says it is that team's, but the file no longer matches the signature.
    case broken

    /// The Team ID of the running app; nil for a development build, which
    /// swift build signs ad hoc at most.
    public static let ownTeam: String? = {
        var me: SecCode?
        var code: SecStaticCode?
        guard SecCodeCopySelf([], &me) == errSecSuccess, let me,
              SecCodeCopyStaticCode(me, [], &code) == errSecSuccess, let code else { return nil }
        return teamID(code)
    }()

    /// Checks the program at url, following a symlink as running it would
    /// (Homebrew's bin/tm is one). It reads the whole file: not for the
    /// main thread. It asks no server, so it works offline.
    public static func check(_ url: URL, team: String) -> TMSignature {
        var code: SecStaticCode?
        guard SecStaticCodeCreateWithPath(url.resolvingSymlinksInPath() as CFURL, [], &code) == errSecSuccess,
              let code else { return .noTeam }
        var requirement: SecRequirement?
        let words = "anchor apple generic and certificate leaf[subject.OU] = \"\(team)\""
        if SecRequirementCreateWithString(words as CFString, [], &requirement) == errSecSuccess,
           SecStaticCodeCheckValidity(code, SecCSFlags(rawValue: kSecCSCheckAllArchitectures | kSecCSStrictValidate), requirement) == errSecSuccess {
            return .sameTeam
        }
        guard let found = teamID(code) else { return .noTeam }
        return found == team ? .broken : .otherTeam(found)
    }

    /// The Team ID a program's signature names; not proof on its own.
    public static func team(of url: URL) -> String? {
        var code: SecStaticCode?
        guard SecStaticCodeCreateWithPath(url.resolvingSymlinksInPath() as CFURL, [], &code) == errSecSuccess,
              let code else { return nil }
        return teamID(code)
    }

    private static func teamID(_ code: SecStaticCode) -> String? {
        var info: CFDictionary?
        guard SecCodeCopySigningInformation(code, SecCSFlags(rawValue: kSecCSSigningInformation), &info) == errSecSuccess,
              let dict = info as? [String: Any] else { return nil }
        return dict[kSecCodeInfoTeamIdentifier as String] as? String
    }
}

/// The question a signed app asks before it runs a tm: the tm fails the
/// signature check, or passes it at another path than last time. A first
/// run that passes has nothing to compare with, and asks nothing.
public struct TMAsk: Equatable, Sendable {
    public static let title = "Run this tm?"
    public static let warning = "tm runs with your rights: it can read your projects and change your settings. Run it only if you put it there."

    public let path: String
    /// Why it asks, in plain words.
    public let why: String

    public init?(path: String, signature: TMSignature, last: String?) {
        self.path = path
        switch signature {
        case .sameTeam:
            guard let last, last != path else { return nil }
            why = "It is signed by this app's developer, but it isn't the path the app ran last time: \(last)."
        case .noTeam:
            why = "It isn't signed by this app's developer."
        case .otherTeam(let team):
            why = "It is signed by another developer, team \(team), not this app's."
        case .broken:
            why = "It has been changed since this app's developer signed it."
        }
    }
}

/// The user chose not to run the tm the app asked about.
public struct TMNotRun: Error, LocalizedError, Equatable {
    public let path: String

    public init(path: String) {
        self.path = path
    }

    public var errorDescription: String? {
        "You chose not to run the tm at \(path). Set another path in Settings, or Retry to be asked again."
    }
}

/// What Settings › Advanced says about the check.
public enum TMCheckState: Equatable, Sendable {
    /// A development build: it never checks.
    case skipped
    /// Not reached yet, or no tm to check.
    case none
    case passed
    /// It failed, or its path changed, and the user answered Run It.
    case ranAnyway
    /// It failed, or its path changed, and tm isn't running.
    case asked

    public var text: String {
        switch self {
        case .skipped: "Not checked: this build of the app isn't signed."
        case .none: "Not checked yet."
        case .passed: "Signed by this app's developer."
        case .ranAnyway: "You chose to run it after the app asked."
        case .asked: "The app asked before running it, and it isn't running."
        }
    }
}
#endif

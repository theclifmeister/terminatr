import Foundation

/// A coordinator the server put to sleep after a long idle (app API 47,
/// docs/SPEC.md §7.5 Sleep): stopped with its conversation kept, it
/// wakes when it is opened (project.open), as enter on its row in the
/// TUI. The apps show it as the TUI's sidebar does (☾, "asleep · its
/// conversation kept"), and while project.open wakes it, a spinner.
public enum Sleep: Equatable, Sendable {
    /// It runs, or none runs and none sleeps.
    case awake
    /// It sleeps: opening it wakes it.
    case asleep
    /// The app asked it to wake (project.open), and it doesn't run yet.
    case waking

    /// Project slug's coordinator's: with running whether its session
    /// is in session.list, asleep the projects of SessionList.asleep and
    /// waking those the app is waking.
    public static func of(_ slug: String, running: Bool, asleep: Set<String>, waking: Set<String>) -> Sleep {
        if running { return .awake }
        if waking.contains(slug) { return .waking }
        return asleep.contains(slug) ? .asleep : .awake
    }

    /// The projects whose coordinator sleeps, from a session list.
    public static func projects(_ l: AppAPI.SessionList) -> Set<String> {
        Set(l.asleep.map(\.project))
    }

    /// The projects still waking once sessions is the list: those
    /// whose coordinator runs now are awake.
    public static func settle(_ waking: Set<String>, sessions: [AppAPI.Session]) -> Set<String> {
        waking.filter { slug in !sessions.contains { $0.role == "coordinator" && $0.project == slug && $0.server.isEmpty } }
    }

    /// The row's and tile's line, as the TUI's sidebar footer: "asleep ·
    /// its conversation kept: opening wakes it".
    public static let words = "asleep · its conversation kept: opening wakes it"
    /// While it wakes.
    public static let wakingWords = "waking · its conversation kept"
}

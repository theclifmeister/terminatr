import Foundation

/// The one agent the user talks to for a project or a program (T1003),
/// as both apps mark it wherever it appears: one SF Symbol and one word
/// for each role (RoleMark, RoleLabel). A thread has no role.
public enum SessionRole: String, CaseIterable, Sendable {
    case coordinator, director

    /// What its rows and titles call it.
    public var word: String {
        switch self {
        case .coordinator: "Coordinator"
        case .director: "Director"
        }
    }

    /// Its mark's SF Symbol: the one agent of a project, the one over a
    /// program's several.
    public var symbol: String {
        switch self {
        case .coordinator: "person.fill"
        case .director: "person.2.fill"
        }
    }
}

extension SessionRef {
    /// The role of the session it names; nil for a thread's.
    public var role: SessionRole? {
        director ? .director : isCoordinator ? .coordinator : nil
    }
}

/// The Mac sidebar's one grid (T1003), in points from the row's selection
/// fill. A project's tile and a role's mark share a column, and the
/// project's name, its role's word and its threads' text share the edge
/// after it. A program's contents, its director's row and its members'
/// whole trees, sit one step in; so do a project's coordinator and
/// threads, under the project's own row, as the director does. At the row's end, right to left: the
/// state's dot, the percent column (a role's context use, a project's
/// count), then status words and chips, ending at one edge.
public enum SidebarGrid {
    /// From the selection fill's edge to the tile and mark column.
    public static let inset: Double = 8
    /// The tile and mark column's width.
    public static let slot: Double = 20
    /// From that column to the row's text.
    public static let gap: Double = 9
    /// A thread's mark in the tile column, beside its role's: one speech
    /// bubble, filled like theirs, for the one task an agent works on.
    public static let threadSymbol = "bubble.left.fill"
    /// How far a program's contents sit in.
    public static let step: Double = 12
    /// The percent column at 100% text size: holds "99%", and "100%" runs left.
    public static let percent: Double = 28

    /// Where the rows under a row indented `indent` sit: one step in,
    /// for a program's director and a project's coordinator alike.
    public static func contents(of indent: Double) -> Double { indent + step }

    /// Where a row's text starts, depth programs in.
    public static func textEdge(depth: Int) -> Double {
        inset + Double(depth) * step + slot + gap
    }
}

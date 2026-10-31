import Foundation

/// What the menu bar's dropdown lists (T748 milestone F), kept short:
/// the first needs, each named with its project, and a line for the
/// rest; then the active projects that have something going, each with
/// its ⌘1–⌘9 as the Go menu numbers it, and a line counting the others
/// ("2 more active · 3 inactive"), which the window shows.
public enum MenuBarList {
    /// The needs shown before "+N more", as the sidebar's.
    public static let needsShown = SidebarTree.needsShown
    /// The projects shown at most.
    public static let projectsShown = 6

    /// The needs listed, and the rest that one line sums up.
    public static func needs(_ needs: [Need]) -> (shown: [Need], rest: [Need]) {
        (Array(needs.prefix(needsShown)), Array(needs.dropFirst(needsShown)))
    }

    /// The projects of rest, in the order they come: "acme-web,
    /// terminatr, toe".
    public static func projects(of rest: [Need]) -> String {
        var seen: Set<String> = [], out: [String] = []
        for n in rest where !n.project.isEmpty && seen.insert(n.project).inserted {
            out.append(n.project)
        }
        return out.joined(separator: ", ")
    }

    /// A listed project with its place among the active ones (from 0):
    /// its ⌘ key when under 9.
    public struct Row: Equatable, Sendable {
        public let index: Int
        public let slug: String
    }

    /// The active projects (SidebarTree.active's order) listed: those
    /// with an open thread or a need, at most projectsShown; and how
    /// many active ones are left out.
    public static func projects(_ active: [AppAPI.Project], needs: [Need]) -> (shown: [Row], more: Int) {
        let busy = active.enumerated().filter { _, p in
            p.threads.contains { !$0.record.done } || needs.contains { $0.project == p.slug }
        }
        let shown = busy.prefix(projectsShown).map { Row(index: $0.offset, slug: $0.element.slug) }
        return (shown, active.count - shown.count)
    }

    /// "2 more active · 3 inactive", "1 more active", "3 inactive"; nil
    /// when nothing is left out.
    public static func others(more: Int, inactive: Int) -> String? {
        var parts: [String] = []
        if more > 0 { parts.append("\(more) more active") }
        if inactive > 0 { parts.append("\(inactive) inactive") }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

import Foundation

/// What the app's one window shows (design T746): a project's
/// dashboard, or a coordinator's or thread's session. A paired server's
/// project has server set to its alias.
public enum Selection: Codable, Hashable, Sendable {
    case project(String, server: String = "")
    case session(SessionRef)

    /// The project it is in, and its server.
    public var project: String {
        switch self {
        case .project(let slug, _): slug
        case .session(let ref): ref.project
        }
    }

    public var server: String {
        switch self {
        case .project(_, let server): server
        case .session(let ref): ref.server
        }
    }

    /// The project's dashboard it is in.
    public var dashboard: Selection { .project(project, server: server) }

    /// A thread's card on its project's dashboard.
    public static func card(_ t: AppAPI.Thread, in project: String) -> Selection {
        .session(SessionRef(project: project, thread: t.record.id))
    }

    /// A task of the dashboard's Needs you: its thread when the project
    /// has it, else the coordinator, who asks.
    public static func of(task t: AppAPI.Task, in p: AppAPI.Project) -> Selection {
        if !t.thread.isEmpty, p.threads.contains(where: { $0.record.id == t.thread }) {
            return .session(SessionRef(project: p.slug, thread: t.thread))
        }
        return .session(SessionRef(project: p.slug))
    }

    /// What a need, a notification or a menu bar row names: session's
    /// coordinator or thread when it is one, else project ("alias/slug"
    /// for a paired server's) when it is listed; nil leaves the window
    /// as it was.
    public static func route(project: String?, session: String = "", projects: [AppAPI.Project],
                             sessions: [AppAPI.Session], peers: [AppAPI.Peer]) -> Selection? {
        if !session.isEmpty, let s = sessions.first(where: { $0.id == session }), let ref = SessionRef(s) {
            return .session(ref)
        }
        guard let project, !project.isEmpty else { return nil }
        if projects.contains(where: { $0.slug == project }) {
            return .project(project)
        }
        if let slash = project.firstIndex(of: "/") {
            let alias = String(project[..<slash]), slug = String(project[project.index(after: slash)...])
            if peers.first(where: { $0.alias == alias })?.projects.contains(where: { $0.project == slug }) == true {
                return .project(slug, server: alias)
            }
        }
        return nil
    }

    /// Whether it still names something listed: a project of this
    /// server or a paired one's, a coordinator (shown even while it
    /// isn't running) or a thread the project has.
    /// The inactive project of this server it shows: what a lean
    /// projects.watch sends whole (shown); "" for any other.
    public func shownInactive(_ projects: [AppAPI.Project]) -> String {
        guard server.isEmpty, let p = projects.first(where: { $0.slug == project }), !p.active else { return "" }
        return p.slug
    }

    public func exists(projects: [AppAPI.Project], peers: [AppAPI.Peer]) -> Bool {
        if !server.isEmpty {
            guard let p = peers.first(where: { $0.alias == server })?.projects.first(where: { $0.project == project }) else { return false }
            if case .session(let ref) = self, !ref.isCoordinator {
                return p.sessions.contains { $0.role == "thread" && $0.thread == ref.thread }
            }
            return true
        }
        guard let p = projects.first(where: { $0.slug == project }) else { return false }
        if case .session(let ref) = self, !ref.isCoordinator {
            return p.threads.contains { $0.record.id == ref.thread }
        }
        return true
    }
}

/// The window's selection with its history (⌘[ and ⌘]) and the
/// sidebar's collapsed sections, saved as one value so a relaunch
/// restores them.
public struct Navigation: Codable, Equatable, Sendable {
    public private(set) var selection: Selection?
    public private(set) var back: [Selection] = []
    public private(set) var forward: [Selection] = []
    /// The sidebar's collapsed sections, by SidebarSection key.
    public var collapsed: Set<String> = []
    /// Whether the sidebar unfolds its inactive projects (⇧⌘H).
    public var showInactive = false
    /// What → unfolded in the sidebar: the needs' "+N more" (moreKey),
    /// and the inactive projects expanded (sectionKey), as the TUI's
    /// view.Tree has them.
    public var unfolded: Set<String> = []

    /// How many steps back are kept.
    public static let historyLimit = 50

    public init(selection: Selection? = nil) {
        self.selection = selection
    }

    enum CodingKeys: String, CodingKey {
        case selection, back, forward, collapsed, showInactive, unfolded
    }

    /// A save from before a field was added reads as its default.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        selection = try c.decodeIfPresent(Selection.self, forKey: .selection)
        back = try c.decodeIfPresent([Selection].self, forKey: .back) ?? []
        forward = try c.decodeIfPresent([Selection].self, forKey: .forward) ?? []
        collapsed = try c.decodeIfPresent(Set<String>.self, forKey: .collapsed) ?? []
        showInactive = try c.decodeIfPresent(Bool.self, forKey: .showInactive) ?? false
        unfolded = try c.decodeIfPresent(Set<String>.self, forKey: .unfolded) ?? []
    }

    /// Shows s; the one shown before goes on the back list.
    public mutating func select(_ s: Selection?) {
        guard s != selection else { return }
        if let old = selection {
            back.append(old)
            if back.count > Self.historyLimit { back.removeFirst(back.count - Self.historyLimit) }
        }
        forward = []
        selection = s
    }

    public var canGoBack: Bool { !back.isEmpty }
    public var canGoForward: Bool { !forward.isEmpty }

    public mutating func goBack() {
        guard let s = back.popLast() else { return }
        if let old = selection { forward.append(old) }
        selection = s
    }

    public mutating func goForward() {
        guard let s = forward.popLast() else { return }
        if let old = selection { back.append(old) }
        selection = s
    }

    /// Drops from the history what keep refuses (a thread removed since,
    /// a project gone); the selection itself becomes its project's
    /// dashboard when keep takes that, else fallback, and none becomes
    /// fallback. Nothing goes on the back list.
    public mutating func prune(fallback: Selection?, keep: (Selection) -> Bool) {
        back = back.filter(keep).dedupedRuns()
        forward = forward.filter(keep).dedupedRuns()
        if let s = selection, keep(s) { return }
        if let d = selection?.dashboard, keep(d) {
            selection = d
        } else {
            selection = fallback
        }
    }

    /// The saved value: JSON.
    public func encoded() -> Data? {
        try? JSONEncoder().encode(self)
    }

    /// A saved value back; nothing, or one this release can't read,
    /// starts afresh.
    public static func restore(_ data: Data?) -> Navigation {
        guard let data, let n = try? JSONDecoder().decode(Navigation.self, from: data) else { return Navigation() }
        return n
    }

    /// The sidebar's sections: a server, a project, the needs.
    public static func sectionKey(server: String, project: String = "") -> String {
        project.isEmpty ? "server:\(server)" : "project:\(server)/\(project)"
    }

    public static let needsSection = "needs"

    /// The needs' "+N more" row of the sidebar.
    public static func moreKey() -> String { "more:needs" }
}

private extension Array where Element: Equatable {
    /// Without an element equal to the one before it.
    func dedupedRuns() -> [Element] {
        var out: [Element] = []
        for e in self where out.last != e {
            out.append(e)
        }
        return out
    }
}

/// The sessions whose streams stay open (design T746): the one shown
/// and the few shown just before it, so switching back is instant.
/// touch tells which fell out, to be stopped.
public struct WarmSet<Key: Hashable>: Sendable where Key: Sendable {
    public let limit: Int
    /// Most recent last.
    public private(set) var keys: [Key] = []

    public init(limit: Int) {
        self.limit = max(1, limit)
    }

    /// k is shown now: it goes to the front; returns the keys dropped.
    public mutating func touch(_ k: Key) -> [Key] {
        keys.removeAll { $0 == k }
        keys.append(k)
        guard keys.count > limit else { return [] }
        let out = Array(keys.prefix(keys.count - limit))
        keys.removeFirst(keys.count - limit)
        return out
    }

    /// Drops k (its session went); true when it was warm.
    @discardableResult
    public mutating func remove(_ k: Key) -> Bool {
        guard let i = keys.firstIndex(of: k) else { return false }
        keys.remove(at: i)
        return true
    }
}

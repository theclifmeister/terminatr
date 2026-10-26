import Foundation

/// The window's keys (T748 milestone E): ⌥⌘↓ and ⌥⌘↑ through what needs
/// the user, the sidebar's type-to-select and its filter field.
extension Need {
    /// The need after (by 1) or before (by -1) the current one, in the
    /// needs' order, wrapping round. The current one is last (the need
    /// the keys showed last) while it is listed and the window still
    /// shows what it names, else the first that names what the window
    /// shows (shown); with none, the first (down) or the last (up).
    /// Several needs may name one thing (a project's blocked task and
    /// its task in review both show its dashboard): last tells them
    /// apart.
    public static func step(_ needs: [Need], by: Int, last: String?, shown: Selection?,
                            route: (Need) -> Selection?) -> Need? {
        guard !needs.isEmpty else { return nil }
        var at: Int?
        if let last, let i = needs.firstIndex(where: { $0.id == last }), route(needs[i]) == shown {
            at = i
        }
        if at == nil, let shown {
            at = needs.firstIndex { route($0) == shown }
        }
        guard let at else { return by < 0 ? needs.last : needs.first }
        let n = needs.count
        return needs[((at + by) % n + n) % n]
    }
}

/// Type-to-select in the sidebar, as Finder's: letters typed within a
/// moment of each other select the first row that begins with them;
/// the same letter again moves on to the next such row.
public struct TypeSelect: Sendable {
    /// A pause this long starts a new word.
    public static let pause: TimeInterval = 1

    public private(set) var typed = ""
    private var at: Date?

    public init() {}

    /// Adds s to the word typed (after a pause, starts it afresh) and
    /// returns it, lowercased.
    public mutating func type(_ s: String, now: Date = Date()) -> String {
        if let at, now.timeIntervalSince(at) > Self.pause { typed = "" }
        typed += s.lowercased()
        at = now
        return typed
    }

    /// The row of labels that typed selects, the current one being
    /// current: the first from current on that begins with typed; for
    /// one letter, or the same letter typed again, the next after
    /// current that begins with it. nil: none does.
    public static func match(_ typed: String, in labels: [String], from current: Int?) -> Int? {
        guard !typed.isEmpty, !labels.isEmpty else { return nil }
        let lower = labels.map { $0.lowercased() }
        func find(_ prefix: String, after: Bool) -> Int? {
            let start = current.map { after ? $0 + 1 : $0 } ?? 0
            for k in 0..<lower.count {
                let i = (start + k) % lower.count
                if lower[i].hasPrefix(prefix) { return i }
            }
            return nil
        }
        if typed.count == 1 { return find(typed, after: true) }
        if let i = find(typed, after: false) { return i }
        if let c = typed.first, typed.allSatisfy({ $0 == c }) { return find(String(c), after: true) }
        return nil
    }
}

extension SidebarTree {
    /// Whether the filter field's query finds fields: each of its words
    /// is in one of them, regardless of case and accents. An empty
    /// query finds everything.
    public static func matches(_ query: String, _ fields: String...) -> Bool {
        matches(query, fields)
    }

    public static func matches(_ query: String, _ fields: [String]) -> Bool {
        let words = query.split(whereSeparator: \.isWhitespace)
        guard !words.isEmpty else { return true }
        let text = fields.joined(separator: "\n")
        return words.allSatisfy { text.range(of: $0, options: [.caseInsensitive, .diacriticInsensitive]) != nil }
    }

    /// The open threads of p the filter finds: by title, id, or its
    /// task's ref and title.
    public static func threads(of p: AppAPI.Project, matching query: String) -> [AppAPI.Thread] {
        p.threads.filter { t in
            !t.record.done && matches(query, t.record.title, t.record.id, t.task?.ref ?? "", t.task?.title ?? "")
        }
    }
}

extension SidebarTree {
    /// Whether this server's project p lists its coordinator and threads:
    /// always while one of its sessions is shown; else not once ← folded
    /// it, and when → unfolded it or it expands (needs, selection).
    public static func opens(_ p: AppAPI.Project, nav: Navigation, needs: Int) -> Bool {
        if case .session(let ref) = nav.selection, ref.server.isEmpty, ref.project == p.slug { return true }
        let key = Navigation.sectionKey(server: "", project: p.slug)
        if nav.collapsed.contains(key) { return false }
        return nav.unfolded.contains(key) || expands(p, selection: nav.selection, needs: needs)
    }
}

extension Navigation {
    /// ← on a project row: it no longer lists its sessions.
    public mutating func fold(_ key: String) {
        collapsed.insert(key)
        unfolded.remove(key)
    }

    /// → on a project row: it lists them, needs or not.
    public mutating func unfold(_ key: String) {
        collapsed.remove(key)
        unfolded.insert(key)
    }
}

extension Transcript {
    /// ⌘F: the entries of the conversation that show query, by id in
    /// their order: a prompt's or the agent's words, a tool call with
    /// its argument and result. Case and accents don't count.
    public static func find(_ query: String, in entries: [Entry]) -> [Int] {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return [] }
        return entries.filter { e in
            [e.text, e.tool, e.result?.text ?? ""].contains { $0.range(of: q, options: [.caseInsensitive, .diacriticInsensitive]) != nil }
        }.map(\.id)
    }
}

import Foundation

/// A row of the command palette (⌘K, T748 milestone E): somewhere to go
/// (↩) and, for some, something to do there (⌘↩): allow a dialog,
/// answer a question, message a session, delegate a task, accept or
/// send back one in review. The window
/// asks the same as its cards before it acts (E1).
public struct PaletteItem: Identifiable, Hashable, Sendable {
    public enum Group: String, CaseIterable, Sendable {
        case needs = "Needs you"
        case projects = "Projects"
        case threads = "Threads"
        case tasks = "Tasks"
        case actions = "Actions"
    }

    /// What ⌘↩ does.
    public enum Act: Hashable, Sendable {
        /// The session's permission dialog (session.approve).
        case approve(SessionRef)
        /// The session's open question menu (session.answer).
        case answer(SessionRef)
        /// A prompt to the session (session.prompt).
        case prompt(SessionRef)
        /// Asks project's coordinator to delegate task (task.delegate).
        case delegate(project: String, task: String)
        /// A task in review: accept it or send it back (task.accept,
        /// task.send-back), as the dashboard's cards do.
        case review(project: String, task: String)

        /// The row's hint: "⌘↩ allow".
        public var verb: String {
            switch self {
            case .approve: "allow"
            case .answer: "answer"
            case .prompt: "message"
            case .delegate: "delegate"
            case .review: "review"
            }
        }
    }

    /// The window's own commands, the Actions group.
    public enum Command: Hashable, Sendable {
        case nextNeed, previousNeed, inactive, tab, inspector, find, filter, back, forward, shortcuts
        /// Picks a theme (T770).
        case theme(ThemeName)
    }

    public let id: String
    public let group: Group
    public let title: String
    public let detail: String
    /// The dot's tone, for a need, a project or a thread.
    public let tone: Attention.Tone?
    /// Where ↩ goes.
    public let go: Selection?
    public let act: Act?
    public let command: Command?
    /// The key the row's command has, shown on its right: "⌥⌘↓".
    public let keys: String
    /// What the search finds it by besides its title and detail.
    public let keywords: String

    public init(id: String, group: Group, title: String, detail: String = "", tone: Attention.Tone? = nil,
                go: Selection? = nil, act: Act? = nil, command: Command? = nil, keys: String = "",
                keywords: String = "") {
        self.id = id
        self.group = group
        self.title = title
        self.detail = detail
        self.tone = tone
        self.go = go
        self.act = act
        self.command = command
        self.keys = keys
        self.keywords = keywords
    }

    var picksTheme: Bool {
        if case .theme = command { return true }
        return false
    }
}

public enum Palette {
    /// The rows a group shows at most.
    public static let perGroup = 8

    /// Every place the palette goes, with what it may do there: the
    /// needs, this server's projects (inactive ones too) and a paired
    /// server's, each project's coordinator and open threads, and the
    /// tasks the app knows: those that need the user, the threads', and
    /// with the board (app API 6) those on deck and in the backlog.
    public static func items(projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer],
                             needs: [Need], now: Date = Date()) -> [PaletteItem] {
        var out: [PaletteItem] = []
        func session(_ id: String) -> AppAPI.Session? {
            id.isEmpty ? nil : sessions.first { $0.id == id }
        }
        for n in needs {
            let go = Selection.route(project: n.project, session: n.session, projects: projects, sessions: sessions, peers: peers)
            out.append(PaletteItem(id: "need:\(n.id)", group: .needs, title: n.title, detail: n.detail,
                                   tone: tone(n.kind), go: go, act: act(n, session(n.session))))
        }
        let ordered = SidebarTree.active(projects) + SidebarTree.inactive(projects)
        for p in ordered {
            let coordinator = sessions.first { $0.role == "coordinator" && $0.project == p.slug && $0.server.isEmpty }
            let att = Attention.of(coordinator: coordinator, in: p, now: now)
            let n = Need.count(needs, project: p.slug)
            let detail = [p.active ? "" : "inactive", n == 0 ? "" : n == 1 ? "1 needs you" : "\(n) need you",
                          p.threads.isEmpty ? "" : p.threads.count == 1 ? "1 thread" : "\(p.threads.count) threads"]
            out.append(PaletteItem(id: "project:\(p.slug)", group: .projects, title: p.slug,
                                   detail: detail.filter { !$0.isEmpty }.joined(separator: " · "),
                                   tone: att.tone, go: .project(p.slug)))
            if let coordinator {
                let ref = SessionRef(project: p.slug)
                out.append(PaletteItem(id: "session:\(p.slug)/", group: .threads, title: "\(p.slug) · coordinator",
                                       detail: att.word, tone: att.tone, go: .session(ref), act: act(ref, coordinator)))
            }
            for t in p.threads where !t.record.done {
                let s = session(t.record.session)
                let a = Attention.of(thread: t, session: s, now: now)
                let ref = SessionRef(project: p.slug, thread: t.record.id)
                out.append(PaletteItem(id: "session:\(p.slug)/\(t.record.id)", group: .threads,
                                       title: [t.task?.ref, t.record.title].compactMap { $0 }.joined(separator: " "),
                                       detail: "\(p.slug) · \(t.record.id) · \(a.word)", tone: a.tone, go: .session(ref),
                                       act: s.flatMap { act(ref, $0) }, keywords: t.task?.title ?? ""))
            }
            var seen: Set<String> = []
            // A thread's task goes to that thread.
            // A thread's task goes to that thread; a board's to the
            // project.
            let tasks = p.needsYou.map { ($0, Selection.of(task: $0, in: p)) }
                + p.threads.compactMap { th in th.task.map { ($0, Selection.card(th, in: p.slug)) } }
                + p.board.map { ($0, Selection.project(p.slug)) }
            for (t, go) in tasks where seen.insert(t.ref).inserted && t.status != "done" {
                out.append(PaletteItem(id: "task:\(p.slug)/\(t.ref)", group: .tasks, title: "\(t.ref) \(t.title)",
                                       detail: "\(p.slug) · \(words(t.status))", go: go, act: act(task: t.ref, t.status, in: p.slug)))
            }
        }
        for peer in peers {
            let server = peer.name.isEmpty ? peer.alias : peer.name
            for p in peer.projects {
                out.append(PaletteItem(id: "project:\(peer.alias)/\(p.project)", group: .projects, title: p.project,
                                       detail: "on \(server)", go: .project(p.project, server: peer.alias)))
                for s in p.sessions {
                    guard let ref = SessionRef(s, of: peer.alias) else { continue }
                    out.append(PaletteItem(id: "session:\(peer.alias)/\(p.project)/\(ref.thread)", group: .threads,
                                           title: [s.task, ref.isCoordinator ? "\(p.project) · coordinator" : ref.thread]
                                               .filter { !$0.isEmpty }.joined(separator: " "),
                                           detail: "\(p.project) on \(server)", go: .session(ref)))
                }
            }
        }
        return out
    }

    /// The rows query finds, in the groups' order and each group's own,
    /// those whose title begins with it first, at most perGroup each.
    /// An empty query lists the needs, the projects and the actions.
    public static func search(_ items: [PaletteItem], _ query: String) -> [PaletteItem] {
        let q = query.trimmingCharacters(in: .whitespaces)
        var out: [PaletteItem] = []
        for g in PaletteItem.Group.allCases {
            let rows = items.filter { $0.group == g }
            if q.isEmpty {
                if g == .threads || g == .tasks { continue }
                // The themes wait for a word ("theme", "dusk").
                out += g == .actions ? rows.filter { !$0.picksTheme } : Array(rows.prefix(perGroup))
                continue
            }
            let found = rows.filter { SidebarTree.matches(q, $0.title, $0.detail, $0.keywords) }
            let first = found.filter { $0.title.range(of: q, options: [.caseInsensitive, .diacriticInsensitive, .anchored]) != nil }
            out += Array((first + found.filter { !first.contains($0) }).prefix(perGroup))
        }
        return out
    }

    /// The Actions group's themes, "Theme: Dusk", the one in use said
    /// so: found by "theme" or a theme's name.
    public static func themes(current: ThemeName) -> [PaletteItem] {
        ThemeName.allCases.map { t in
            PaletteItem(id: "cmd:theme:\(t.rawValue)", group: .actions, title: "Theme: \(t.label)",
                        detail: t == current ? "\(t.mood) · in use" : t.mood, command: .theme(t),
                        keywords: "theme appearance colours colors look")
        }
    }

    /// What ⌘↩ does on a need: allow its dialog, answer its question
    /// when tm read the menu, delegate its blocked task, review the task
    /// in review.
    static func act(_ n: Need, _ s: AppAPI.Session?) -> PaletteItem.Act? {
        switch n.kind {
        case .approval, .question:
            return s.flatMap { s in SessionRef(s).flatMap { act($0, s) } }
        case .blocked where !n.task.isEmpty:
            return .delegate(project: n.project, task: n.task)
        case .review where !n.task.isEmpty:
            return .review(project: n.project, task: n.task)
        default:
            return nil
        }
    }

    /// What ⌘↩ does on a running session: what it waits on, else a
    /// prompt.
    static func act(_ ref: SessionRef, _ s: AppAPI.Session) -> PaletteItem.Act? {
        if s.question.map({ q in q.questions.contains { !$0.answered } }) == true { return .answer(ref) }
        if s.state == "blocked" && s.reason == "permission" { return .approve(ref) }
        if s.state == "blocked" && s.reason == "question" { return nil } // answered in Terminal
        return .prompt(ref)
    }

    /// What ⌘↩ does on a task of status: review it, or delegate it.
    static func act(task: String, _ status: String, in project: String) -> PaletteItem.Act? {
        if status == "review" { return .review(project: project, task: task) }
        return delegable(status) ? .delegate(project: project, task: task) : nil
    }

    /// Whether the coordinator may be asked to delegate a task of this
    /// status: tm's check (modaction.NotDelegable).
    public static func delegable(_ status: String) -> Bool {
        ["open", "ready", "blocked"].contains(status)
    }

    static func words(_ status: String) -> String {
        switch status {
        case "ready": "on deck"
        case "started": "in motion"
        case "open": "backlog"
        default: status
        }
    }

    static func tone(_ k: Need.Kind) -> Attention.Tone {
        switch k {
        case .approval: .approval
        case .question: .question
        case .review: .review
        default: .attention
        }
    }
}

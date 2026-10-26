import Foundation

/// What waits on the user, as the menu bar lists it and notifications
/// tell it (design T734 §6, R9): from projects.watch {detail},
/// session.list and peer.list, in the dashboard's order.
public struct Need: Identifiable, Hashable, Sendable {
    public enum Kind: String, CaseIterable, Sendable {
        /// A question menu open on a session, or a coordinator's open
        /// questions (tm ask).
        case question
        /// A permission dialog on a session's screen.
        case approval
        /// A task in review.
        case review
        /// A blocked task.
        case blocked
        /// A thread's PR whose checks fail.
        case checks
        /// A session's prompt queue held for a minute or more.
        case held
        /// A paired server: a shared project that needs its user, or a
        /// share request.
        case network
    }

    /// Stable while the need lasts; a new question on the same session
    /// is a new id. A notification's identifier.
    public let id: String
    public let kind: Kind
    /// The project's slug; a paired server's is "alias/slug". Empty for
    /// a session outside the projects or a server-level need.
    public let project: String
    /// What the need says, without the agent's words: "T812 asks a
    /// question".
    public let title: String
    /// Where it is: "terminatr · Cache layer".
    public let detail: String
    /// The agent's words (a question's text, why a task is blocked):
    /// shown only when the user opts in (R9).
    public let text: String
    /// The session to open, when it is one.
    public let session: String
    /// The answers a button may give, for a single-select, non-escape
    /// question.
    public let answer: Answer?

    public init(id: String, kind: Kind, project: String, title: String, detail: String, text: String = "",
                session: String = "", answer: Answer? = nil) {
        self.id = id
        self.kind = kind
        self.project = project
        self.title = title
        self.detail = detail
        self.text = text
        self.session = session
        self.answer = answer
    }

    /// A session.answer a notification's button sends: Question is the
    /// guard (the menu must still ask it, single-select, with the label
    /// among its options), so a replaced menu gets nothing.
    public struct Answer: Hashable, Sendable {
        public let session: String
        public let index: Int
        public let question: String
        public let options: [String]

        public func params(_ option: String) -> AppAPI.AnswerParams {
            AppAPI.AnswerParams(id: session, index: index, answer: option, question: question)
        }
    }
}

extension Need {
    /// At most this many option buttons on a notification.
    public static let maxButtons = 4

    /// Every need, sessions' first (questions, approvals, held queues),
    /// then each project's (its coordinator's questions, its tasks,
    /// failing checks), then the paired servers'. A paired server's
    /// panes (sessions with a server) are its user's there, not here.
    public static func of(projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer] = [],
                          now: Date = Date()) -> [Need] {
        var out: [Need] = []
        for s in sessions where s.server.isEmpty {
            out += of(session: s, now: now)
        }
        for p in projects {
            out += of(project: p)
        }
        for peer in peers {
            out += of(peer: peer)
        }
        return out
    }

    static func of(session s: AppAPI.Session, now: Date) -> [Need] {
        var out: [Need] = []
        let who = who(s), place = place(s)
        if let q = s.question {
            let open = q.questions.enumerated().filter { !$0.element.answered }
            if !open.isEmpty {
                let key = q.since.map { String(Int($0.timeIntervalSince1970)) } ?? open.map(\.element.question).joined(separator: "\n")
                out.append(Need(id: "question:\(s.id):\(key)", kind: .question, project: s.project,
                                title: open.count == 1 ? "\(who) asks a question" : "\(who) asks \(open.count) questions",
                                detail: place, text: open.map(\.element.question).joined(separator: "\n"),
                                session: s.id, answer: answerable(s.id, q)))
            }
        } else if s.state == "blocked" && s.reason == "permission" {
            out.append(Need(id: "approval:\(s.id)", kind: .approval, project: s.project,
                            title: s.escape ? "\(who) asks to run outside the sandbox" : "\(who) needs approval",
                            detail: place, session: s.id))
        }
        if s.queuedPrompts > 0, !s.queueHeld.isEmpty, let since = s.queueHeldSince, now.timeIntervalSince(since) >= 60 {
            out.append(Need(id: "held:\(s.id):\(Int(since.timeIntervalSince1970))", kind: .held, project: s.project,
                            title: s.queuedPrompts == 1 ? "\(who)'s prompt is held" : "\(who)'s \(s.queuedPrompts) prompts are held",
                            detail: place, text: s.queueHeld, session: s.id))
        }
        return out
    }

    /// The buttons' answer: the menu's one open question, when it is
    /// single-select and the menu isn't an escape's. None otherwise.
    static func answerable(_ session: String, _ q: AppAPI.Question) -> Answer? {
        guard !q.escape else { return nil }
        let open = q.questions.enumerated().filter { !$0.element.answered }
        guard open.count == 1, let (i, item) = open.first, !item.multiSelect, !item.options.isEmpty else { return nil }
        return Answer(session: session, index: i, question: item.question,
                      options: item.options.prefix(maxButtons).map(\.label))
    }

    static func of(project p: AppAPI.Project) -> [Need] {
        var out: [Need] = []
        if p.questions > 0 {
            out.append(Need(id: "questions:\(p.slug)", kind: .question, project: p.slug,
                            title: p.questions == 1 ? "\(p.slug)'s coordinator has a question" : "\(p.slug)'s coordinator has \(p.questions) questions",
                            detail: "\(p.slug) · coordinator"))
        }
        for t in p.needsYou {
            let thread = p.threads.first { $0.record.id == t.thread }
            let why = thread?.status?.needsYou ?? ""
            switch t.status {
            case "review":
                out.append(Need(id: "review:\(p.slug):\(t.ref)", kind: .review, project: p.slug,
                                title: "\(t.ref) is ready to review", detail: "\(p.slug) · \(t.title)",
                                session: thread?.record.session ?? ""))
            case "blocked":
                out.append(Need(id: "blocked:\(p.slug):\(t.ref)", kind: .blocked, project: p.slug,
                                title: "\(t.ref) is blocked", detail: "\(p.slug) · \(t.title)", text: why,
                                session: thread?.record.session ?? ""))
            default:
                break
            }
        }
        for t in p.threads where t.checks == "fail" && t.pr > 0 {
            let name = t.task?.ref ?? t.record.id
            out.append(Need(id: "checks:\(p.slug):\(t.record.id):\(t.pr)", kind: .checks, project: p.slug,
                            title: "\(name) PR #\(t.pr) checks fail", detail: "\(p.slug) · \(t.record.title)",
                            session: t.record.session))
        }
        return out
    }

    static func of(peer: AppAPI.Peer) -> [Need] {
        var out: [Need] = []
        let server = peer.name.isEmpty ? peer.alias : peer.name
        if peer.shareWaits > 0 {
            out.append(Need(id: "share:\(peer.alias)", kind: .network, project: "",
                            title: "\(server) asks you to share a project", detail: "\(server) · paired server"))
        }
        for p in peer.projects where p.needsYou > 0 && !p.stale {
            out.append(Need(id: "paired:\(peer.alias)/\(p.project)", kind: .network, project: "\(peer.alias)/\(p.project)",
                            title: p.needsYou == 1 ? "\(server) › \(p.project): 1 needs you" : "\(server) › \(p.project): \(p.needsYou) need you",
                            detail: "\(server) · paired server"))
        }
        return out
    }

    /// Who a session is in a need's title: its task, its thread, its
    /// project's coordinator, or the session.
    static func who(_ s: AppAPI.Session) -> String {
        if !s.task.isEmpty { return s.task }
        if !s.thread.isEmpty { return s.thread }
        if s.role == "coordinator" { return s.project.isEmpty ? "The coordinator" : "\(s.project)'s coordinator" }
        return s.id
    }

    /// Where a session is: its project and title.
    static func place(_ s: AppAPI.Session) -> String {
        let title = s.role == "coordinator" ? "coordinator" : s.title
        return [s.project, title].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

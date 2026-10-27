import Foundation

/// What waits on the user (design T734 §6, R9; T748 milestone B): one
/// list, from projects.watch {detail}, session.list and peer.list, that
/// the sidebar and its badges, the dashboard, the menu bar, the Dock
/// badge and notifications all show, so every count agrees.
public struct Need: Identifiable, Hashable, Sendable {
    public enum Kind: String, CaseIterable, Sendable {
        /// A question on a session (with its menu parsed or not), or a
        /// coordinator's open questions (tm ask).
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
        /// A project's link: a message held for the OK, a file offered,
        /// a paired server's link request (app API 15).
        case link
    }

    /// Stable while the need lasts; a new question on the same session
    /// is a new id. A notification's identifier.
    public let id: String
    public let kind: Kind
    /// The project's slug; a paired server's is "alias/slug". Empty for
    /// a session outside the projects or a server-level need.
    public let project: String
    /// What the need says, without its project or the agent's words:
    /// "T812 asks a question".
    public let what: String
    /// Its task's title, and where to answer when the app can't: "Cache
    /// layer · answer in Terminal". Never the pane's title.
    public let detail: String
    /// The agent's words (a question's text, why a task is blocked):
    /// shown only when the user opts in (R9).
    public let text: String
    /// The session to open, when it is one.
    public let session: String
    /// The answers a button may give, for a single-select, non-escape
    /// question.
    public let answer: Answer?
    /// The task it is about ("T12"): a task in review or blocked.
    public let task: String
    /// What its buttons answer in place: a link need, a share request.
    public let link: LinkAct?

    public init(id: String, kind: Kind, project: String, what: String, detail: String, text: String = "",
                session: String = "", answer: Answer? = nil, task: String = "", link: LinkAct? = nil) {
        self.id = id
        self.kind = kind
        self.project = project
        self.what = what
        self.detail = detail
        self.text = text
        self.session = session
        self.answer = answer
        self.task = task
        self.link = link
    }

    /// The need named with its project, as the sidebar, the menu bar
    /// and notifications show it: "terminatr · T812 asks a question". A
    /// paired server's names its server already.
    public var title: String {
        kind == .network || project.isEmpty ? what : "\(project) · \(what)"
    }

    /// Whether it goes in the Review group under Needs you (B2): still
    /// counted with the rest (B1).
    public var isReview: Bool { kind == .review }

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

    /// Every need: sessions' first (questions, approvals, held queues),
    /// then each project's (its coordinator's questions, its blocked
    /// tasks, failing checks, its links'), then the paired servers'
    /// (share requests, their projects'), then the tasks in review. A
    /// paired server's panes (sessions with a server) are its user's
    /// there, not here.
    public static func of(projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer] = [],
                          now: Date = Date()) -> [Need] {
        var out: [Need] = []
        for s in sessions where s.server.isEmpty {
            out += of(session: s, in: projects, now: now)
        }
        for p in projects {
            out += of(project: p)
        }
        for peer in peers {
            out += of(peer: peer)
        }
        return out.filter { !$0.isReview } + out.filter(\.isReview)
    }

    /// How many of needs are project's: its badge.
    public static func count(_ needs: [Need], project: String) -> Int {
        needs.reduce(0) { $0 + ($1.project == project ? 1 : 0) }
    }

    static func of(session s: AppAPI.Session, in projects: [AppAPI.Project], now: Date) -> [Need] {
        var out: [Need] = []
        let who = who(s), task = taskTitle(s, projects)
        let open = s.question.map { q in q.questions.enumerated().filter { !$0.element.answered } } ?? []
        if let q = s.question, !open.isEmpty {
            let key = q.since.map { String(Int($0.timeIntervalSince1970)) } ?? open.map(\.element.question).joined(separator: "\n")
            out.append(Need(id: "question:\(s.id):\(key)", kind: .question, project: s.project,
                            what: open.count == 1 ? "\(who) asks a question" : "\(who) asks \(open.count) questions",
                            detail: task, text: open.map(\.element.question).joined(separator: "\n"),
                            session: s.id, answer: answerable(s.id, q)))
        } else if s.state == "blocked" && s.reason == "question" {
            // Blocked on a question whose menu tm didn't parse: still the
            // user's, answered at the agent's own prompt.
            out.append(Need(id: "question:\(s.id)", kind: .question, project: s.project,
                            what: "\(who) asks a question", detail: line(task, "answer in Terminal"), session: s.id))
        } else if s.state == "blocked" && s.reason == "permission" {
            out.append(Need(id: "approval:\(s.id)", kind: .approval, project: s.project,
                            what: s.escape ? "\(who) asks to run outside the sandbox" : "\(who) needs approval",
                            detail: task, session: s.id))
        }
        if held(s, now: now), let since = s.queueHeldSince {
            out.append(Need(id: "held:\(s.id):\(Int(since.timeIntervalSince1970))", kind: .held, project: s.project,
                            what: s.queuedPrompts == 1 ? "\(who)'s prompt is held" : "\(who)'s \(s.queuedPrompts) prompts are held",
                            detail: task, text: s.queueHeld, session: s.id))
        }
        return out
    }

    /// Whether s's prompts have been held a minute or more: a need.
    static func held(_ s: AppAPI.Session, now: Date) -> Bool {
        guard s.queuedPrompts > 0, !s.queueHeld.isEmpty, let since = s.queueHeldSince else { return false }
        return now.timeIntervalSince(since) >= 60
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
                            what: p.questions == 1 ? "coordinator has a question" : "coordinator has \(p.questions) questions",
                            detail: "coordinator"))
        }
        for t in p.needsYou {
            let thread = p.threads.first { $0.record.id == t.thread }
            let why = thread?.status?.needsYou ?? ""
            switch t.status {
            case "review":
                out.append(Need(id: "review:\(p.slug):\(t.ref)", kind: .review, project: p.slug,
                                what: "\(t.ref) is ready to review", detail: t.title,
                                session: thread?.record.session ?? "", task: t.ref))
            case "blocked":
                out.append(Need(id: "blocked:\(p.slug):\(t.ref)", kind: .blocked, project: p.slug,
                                what: "\(t.ref) is blocked", detail: t.title, text: why,
                                session: thread?.record.session ?? "", task: t.ref))
            default:
                break
            }
        }
        for t in p.threads where t.checks == "fail" && t.pr > 0 {
            let name = t.task?.ref ?? t.record.id
            out.append(Need(id: "checks:\(p.slug):\(t.record.id):\(t.pr)", kind: .checks, project: p.slug,
                            what: "\(name) PR #\(t.pr) checks fail", detail: t.task?.title ?? t.record.title,
                            session: t.record.session))
        }
        return out + links(of: p)
    }

    static func of(peer: AppAPI.Peer) -> [Need] {
        var out: [Need] = []
        let server = peer.name.isEmpty ? peer.alias : peer.name
        out += shares(of: peer, server: server)
        for p in peer.projects where p.needsYou > 0 && !p.stale {
            out.append(Need(id: "paired:\(peer.alias)/\(p.project)", kind: .network, project: "\(peer.alias)/\(p.project)",
                            what: p.needsYou == 1 ? "\(server) › \(p.project): 1 needs you" : "\(server) › \(p.project): \(p.needsYou) need you",
                            detail: "\(server) · paired server"))
        }
        return out
    }

    /// Who a session is in a need: its task, its thread, its project's
    /// coordinator (the title names the project), or the session.
    static func who(_ s: AppAPI.Session) -> String {
        if !s.task.isEmpty { return s.task }
        if !s.thread.isEmpty { return s.thread }
        if s.role == "coordinator" { return s.project.isEmpty ? "The coordinator" : "coordinator" }
        return s.id
    }

    /// A session's second line: its thread's task title (or the
    /// thread's), "coordinator", or for a plain session its title.
    static func taskTitle(_ s: AppAPI.Session, _ projects: [AppAPI.Project]) -> String {
        if s.role == "coordinator" { return "coordinator" }
        if !s.thread.isEmpty {
            let t = projects.first { $0.slug == s.project }?.threads.first { $0.record.id == s.thread }
            return t?.task?.title ?? t?.record.title ?? ""
        }
        return s.title
    }

    /// The parts that aren't empty, as one second line.
    static func line(_ parts: String...) -> String {
        parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

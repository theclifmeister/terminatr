import Foundation

/// What waits on the user (design T734 §6, R9; T748 milestone B): one
/// list, from projects.watch {detail}, session.list, peer.list and
/// guard.refusals, that
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
        /// a paired server's link request (app API 16).
        case link
        /// A call the guard refused, waiting on the user's answer: allow
        /// it in its project or all projects, or keep refusing it
        /// (guard.refusals, app API 18).
        case refusal
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
    /// The refused call a refusal's card answers, its whole command.
    public let refusal: AppAPI.GuardRefusal?
    /// Since when it waits, when that is known: a card's age.
    public let since: Date?
    /// A paired server's need its buttons answer there (peer.need-act,
    /// app API 30), while that server takes answers from this one.
    public let peer: PeerAct?

    public init(id: String, kind: Kind, project: String, what: String, detail: String, text: String = "",
                session: String = "", answer: Answer? = nil, task: String = "", link: LinkAct? = nil,
                refusal: AppAPI.GuardRefusal? = nil, since: Date? = nil, peer: PeerAct? = nil) {
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
        self.refusal = refusal
        self.since = since
        self.peer = peer
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
        /// Each option's line of description ("" for none), for its row.
        public var descriptions: [String] = []
        /// The agent's own menu, its answer typed on its screen.
        public var screen = false

        public func params(_ option: String) -> AppAPI.AnswerParams {
            AppAPI.AnswerParams(id: session, index: index, answer: option, question: question)
        }
    }

    /// The acts on a paired server's need (docs/SPEC.md §16.16): each
    /// choice's params carry the words shown as the guard, so that
    /// server refuses an act once the need changed, and never takes an
    /// escape's (which gets no PeerAct).
    public struct PeerAct: Hashable, Sendable {
        /// One button: its title and the act; note says send back,
        /// whose note the user writes first.
        public struct Choice: Hashable, Sendable {
            public let title: String
            public let params: AppAPI.PeerNeedActParams
            public var note: Bool { params.act == "send-back" }

            /// The act with send back's note.
            public func params(note text: String) -> AppAPI.PeerNeedActParams {
                var p = params
                p.note = text
                return p
            }
        }

        public let server: String
        public let choices: [Choice]

        /// The acts on need n of the paired server alias's project: the
        /// first open question's options (single-select), a dialog's
        /// options that allow once (the rest stay that server's user's),
        /// accept and send back, or opening the coordinator's questions.
        /// Nil when there is none.
        public init?(need n: AppAPI.PeerNeed, alias: String, project: String, server: String) {
            guard !n.escape else { return nil }
            func p(_ act: String) -> AppAPI.PeerNeedActParams {
                AppAPI.PeerNeedActParams(peer: alias, project: project, need: n.id, act: act)
            }
            var out: [Choice] = []
            for act in n.acts {
                switch act {
                case "answer":
                    guard let qs = n.question?.questions, let i = qs.firstIndex(where: { !$0.answered }), !qs[i].multiSelect else { continue }
                    for o in qs[i].options {
                        var a = p(act)
                        a.index = i
                        a.question = qs[i].question
                        a.answer = o.label
                        out.append(Choice(title: o.label, params: a))
                    }
                case "approve":
                    guard let d = n.approval else { continue }
                    for (i, o) in d.options.enumerated() where i >= d.scopes.count || d.scopes[i] == "once" {
                        var a = p(act)
                        a.choice = i + 1
                        a.option = o
                        out.append(Choice(title: o, params: a))
                    }
                case "accept", "send-back":
                    var a = p(act)
                    a.status = n.status
                    out.append(Choice(title: act == "accept" ? "Accept" : "Send Back…", params: a))
                case "questions":
                    out.append(Choice(title: "Answer now", params: p(act)))
                default:
                    continue
                }
            }
            guard !out.isEmpty else { return nil }
            self.server = server
            self.choices = out
        }
    }
}

extension Need {
    /// At most this many option buttons on a notification.
    public static let maxButtons = 4

    /// Every need: sessions' first (questions, approvals, held queues),
    /// then each project's (its coordinator's questions, its blocked
    /// tasks, failing checks, its links'), then the guard's refusals
    /// the user can allow, then the paired servers' (share requests, their projects'), then
    /// the tasks in review. A paired server's panes (sessions with a
    /// server) are its user's there, not here.
    public static func of(projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer] = [],
                          refusals: [AppAPI.GuardRefusal] = [], now: Date = Date()) -> [Need] {
        var out: [Need] = []
        for s in sessions where s.server.isEmpty {
            out += of(session: s, in: projects, now: now)
        }
        for p in projects {
            out += of(project: p)
        }
        for r in refusals where r.actionable {
            out.append(of(refusal: r, sessions: sessions))
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
                            session: s.id, answer: answerable(s.id, q), since: q.since))
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
                            detail: task, text: s.queueHeld, session: s.id, since: since))
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
        let open = q.questions.indices.filter { !q.questions[$0].answered }
        guard open.count == 1, let a = answer(session, q, index: open[0]) else { return nil }
        let n = min(a.options.count, maxButtons)
        return Answer(session: session, index: a.index, question: a.question,
                      options: Array(a.options.prefix(n)), descriptions: Array(a.descriptions.prefix(n)), screen: a.screen)
    }

    /// The answer a conversation's card for question index of session's
    /// menu q gives, every option: the question open and single-select,
    /// the menu not an escape's, and on the agent's own menu (screen)
    /// the first open one, as it asks them in order. A menu of several
    /// questions answers each this way (T887); nil otherwise, and the
    /// card's options only read.
    public static func answer(_ session: String, _ q: AppAPI.Question, index: Int) -> Answer? {
        guard !q.escape, q.questions.indices.contains(index) else { return nil }
        let item = q.questions[index]
        guard !item.answered, !item.multiSelect, !item.options.isEmpty else { return nil }
        if q.screen, q.questions.firstIndex(where: { !$0.answered }) != index { return nil }
        return Answer(session: session, index: index, question: item.question,
                      options: item.options.map(\.label), descriptions: item.options.map(\.description), screen: q.screen)
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

    /// A refused call: who was refused (its thread's task, its thread,
    /// its session's role) and under which rule. It opens its project,
    /// whose Needs you card answers it, never the thread.
    static func of(refusal r: AppAPI.GuardRefusal, sessions: [AppAPI.Session]) -> Need {
        let s = r.thread.isEmpty ? nil : sessions.first {
            $0.server.isEmpty && $0.project == r.project && $0.thread == r.thread && $0.role == "thread"
        }
        let who = !(s?.task ?? "").isEmpty ? s!.task : !r.thread.isEmpty ? r.thread : !r.role.isEmpty ? r.role : "a session"
        let times = r.count > 1 ? " · \(r.count) times" : ""
        return Need(id: "refusal:\(r.id)", kind: .refusal, project: r.project,
                    what: "\(who)'s \(r.tool) call was refused",
                    detail: line(ruleWord(r.rule), r.summary) + times, refusal: r, since: r.last)
    }

    /// A guard rule in the user's words, as the TUI's Guard refusals
    /// names it; a mod's rule as it is named.
    public static func ruleWord(_ rule: String) -> String {
        switch rule {
        case "force-push": "force-pushing"
        case "push-default": "pushing to the default branch"
        case "worktree-only": "threads write only in their worktree"
        case "delete-branch": "deleting branches and worktrees"
        case "merge": "threads merging"
        case "credentials": "reading credentials"
        default: rule
        }
    }

    /// Why the guard never allows a refusal of the floor, as the TUI
    /// says it.
    public static func floorWhy(_ rule: String) -> String {
        switch rule {
        case "merge": "a thread doesn't merge while the coordinator merges"
        case "force-push": "it force-pushes to a protected branch"
        default: "it reads tm's own files or prints a secret variable"
        }
    }

    static func of(peer: AppAPI.Peer) -> [Need] {
        var out: [Need] = []
        let server = peer.name.isEmpty ? peer.alias : peer.name
        out += shares(of: peer, server: server)
        for p in peer.projects where !p.stale {
            if !p.needs.isEmpty || peer.needs == "view" || peer.needs == "answer" {
                // Its needs switch for this server shows them in full
                // (app API 29): a card each, read-only here.
                out += p.needs.map { of(peerNeed: $0, peer: peer, project: p.project, server: server) }
            } else if p.needsYou > 0 {
                out.append(Need(id: "paired:\(peer.alias)/\(p.project)", kind: .network, project: "\(peer.alias)/\(p.project)",
                                what: p.needsYou == 1 ? "\(server) › \(p.project): 1 needs you" : "\(server) › \(p.project): \(p.needsYou) need you",
                                detail: "\(server) · paired server"))
            }
        }
        return out
    }

    /// A paired server's need in full, as the TUI's NEEDS YOU row says
    /// it: who asks and what, its task, and where it is answered: on
    /// that server, an escape's by its user alone. Its question's text
    /// is the card's text (shown only when the user opts in, R9).
    static func of(peerNeed n: AppAPI.PeerNeed, peer: AppAPI.Peer, project: String, server: String) -> Need {
        let who = !n.task.isEmpty ? n.task : !n.thread.isEmpty ? n.thread : n.role == "coordinator" ? "coordinator" : n.session
        let open = n.question?.questions.filter { !$0.answered } ?? []
        var what: String
        var text = open.map(\.question).joined(separator: "\n")
        switch n.kind {
        case "question":
            what = open.count > 1 ? "\(who) asks \(open.count) questions" : "\(who) asks a question"
            if text.isEmpty { text = n.text }
        case "ask":
            what = "coordinator has a question"
        case "approval":
            what = "\(who) needs approval"
            text = n.approval?.question ?? ""
        case "review":
            what = "\(who) is ready to review"
        case "blocked":
            what = "\(who) is blocked"
        case "ci":
            what = "\(who)'s checks fail"
        case "queue":
            what = "\(who)'s prompts are held"
            text = n.text
        default:
            what = "\(who) needs you"
        }
        if n.escape {
            what = "\(who) asks to run outside the sandbox"
            text = ""
        }
        // At answer, its buttons answer it there (app API 30).
        let acts = peer.needs == "answer" ? PeerAct(need: n, alias: peer.alias, project: project, server: server) : nil
        let where_ = n.escape ? "only on \(server)" : acts != nil ? "for \(server)" : "answer on \(server)"
        return Need(id: "paired:\(peer.alias)/\(project):\(n.id)", kind: .network, project: "\(peer.alias)/\(project)",
                    what: "\(server) › \(project): \(what)", detail: line(n.title, where_), text: text,
                    session: n.session, task: n.task, since: n.since, peer: acts)
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

extension AppAPI.GuardRefusal {
    /// Whether the user has a real choice on the refusal: an allowance
    /// the guard would honour. The floor's, tm's files' and any other no
    /// allowance names (fixed, app API 24; an older tm marks the floor's
    /// only) can only be kept refusing, so they stay in the guard's log,
    /// never under Needs you.
    public var actionable: Bool { !floor && !fixed }
}

import Foundation

/// What a link need's buttons answer in place (T778), as the TUI's
/// Messages tab, message dialog and Network popup do: a project's
/// message held for the OK (Send, Drop…), a file offered to it
/// (Accept, Decline…), a paired server's link request (Accept… this
/// side's direction, Decline…), or a paired server's share request
/// (Share… one of this server's projects, Decline…). Each goes as the
/// human (link.act, peer.share-answer), and asks first where the TUI
/// does; the server checks it again.
public struct LinkAct: Hashable, Sendable {
    public enum Kind: String, Hashable, Sendable {
        case held, file, request, share
    }

    public let kind: Kind
    /// This server's project whose link it is; "" for a share request.
    public let project: String
    /// The message's (M-…), the file's (X-…) or the request's id.
    public let id: String
    /// The other end as the user reads it: a project ("beta",
    /// "mini/api" by its server's name), or a share request's server.
    public let other: String
    /// A held message's or an offered file's file, and its size.
    public let file: String
    public let size: Int
    /// A link request's asking side's entry (both, send, receive).
    public let allow: String
    /// A share request's paired server (its alias), and the project it
    /// asked for there ("" for none in particular).
    public let alias: String
    public let forProject: String

    public init(kind: Kind, project: String, id: String, other: String, file: String = "", size: Int = 0,
                allow: String = "", alias: String = "", forProject: String = "") {
        self.kind = kind
        self.project = project
        self.id = id
        self.other = other
        self.file = file
        self.size = size
        self.allow = allow
        self.alias = alias
        self.forProject = forProject
    }

    /// The button that answers yes, and the one that answers no; "…"
    /// when it asks first.
    public var yes: String {
        switch kind {
        case .held: "Send"
        case .file: "Accept"
        case .request: "Accept…"
        case .share: "Share…"
        }
    }

    public var no: String {
        switch kind {
        case .held: "Drop…"
        case .file, .request, .share: "Decline…"
        }
    }

    /// What the no button asks first, as the TUI's confirmation says it.
    public var declineTitle: String {
        switch kind {
        case .held: "Drop \(id)?"
        case .file: "Decline \(file)?"
        case .request: "Decline the link request?"
        case .share: "Decline the share request?"
        }
    }

    public var declineMessage: String {
        switch kind {
        case .held: "Drop \(id) to \(other)? It is never sent."
        case .file: "Decline \(file) (\(Self.size(size))) from \(other)? \(other) hears of it."
        case .request: "Decline \(other)'s request to link with \(project)? Nothing here changes; \(other) hears of it."
        case .share: "Decline \(other)'s request (\(id))? Nothing is shared; \(other) hears of it."
        }
    }

    /// The no button's confirming button.
    public var declineButton: String {
        kind == .held ? "Drop" : "Decline"
    }

    /// A link request's answers, its mirror first (send there is
    /// receive here), each asking first and not: what Accept… offers.
    public var directions: [Direction] {
        let mirror = Self.mirror(allow)
        let order = [mirror] + ["both", "send", "receive"].filter { $0 != mirror }
        return order.flatMap { a in [Direction(allow: a, ask: true), Direction(allow: a, ask: false)] }
    }

    /// One of the directions a link request is accepted with.
    public struct Direction: Hashable, Sendable {
        public let allow: String
        public let ask: Bool

        public init(allow: String, ask: Bool) {
            self.allow = allow
            self.ask = ask
        }

        /// "both ways, ask first", as the TUI's menu says it.
        public var words: String {
            let w = ["both": "both ways", "send": "send only", "receive": "receive only"][allow] ?? allow
            return ask ? w + ", ask first" : w
        }
    }

    /// What Share… asks before it shares slug, as the TUI does.
    public func shareMessage(_ slug: String) -> String {
        let asked = forProject.isEmpty ? "" : ", for its \(forProject)"
        return "Share \(slug) with every paired server? \(other) asked for a project (\(id)\(asked)). "
            + "They see its state read-only (what waits for you, the inbox, threads, tasks on deck), never its files."
    }

    /// The projects Share… offers: this server's active ones that aren't
    /// shared yet, in slug order.
    public static func shareable(_ projects: [AppAPI.Project]) -> [String] {
        projects.filter { $0.active && !$0.shared }.map(\.slug).sorted()
    }

    /// The params of the act: yes (with a link request's direction, a
    /// share's project) or no.
    public func params(yes: Bool, direction: Direction? = nil) -> AppAPI.LinkActParams {
        switch kind {
        case .held: AppAPI.LinkActParams(project: project, action: yes ? "approve" : "drop", id: id)
        case .file: AppAPI.LinkActParams(project: project, action: yes ? "accept-file" : "decline-file", id: id)
        case .request, .share:
            yes ? AppAPI.LinkActParams(project: project, action: "accept", id: id, allow: direction?.allow ?? "", ask: direction?.ask ?? false)
                : AppAPI.LinkActParams(project: project, action: "decline", id: id)
        }
    }

    /// The other side's entry's mirror: send there is receive here.
    static func mirror(_ allow: String) -> String {
        switch allow {
        case "send": "receive"
        case "receive": "send"
        default: allow.isEmpty ? "both" : allow
        }
    }

    /// A size as tm says it: "640 bytes", "1.1 KB", "2.0 MB".
    public static func size(_ n: Int) -> String {
        if n < 1024 { return "\(n) bytes" }
        if n < 1 << 20 { return String(format: "%.1f KB", Double(n) / 1024) }
        return String(format: "%.1f MB", Double(n) / Double(1 << 20))
    }
}

/// What came of a link need's act, the card's outcome row: running,
/// done in the server's words, or refused. A done one outlives its
/// need for a moment (AppModel keeps it), so the user sees it went.
public struct LinkOutcome: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        case acting
        case done(String)
        case failed(String)
    }

    public let act: LinkAct
    /// The need's title, for the row once the need is gone.
    public let title: String
    public var phase: Phase

    public init(act: LinkAct, title: String, phase: Phase = .acting) {
        self.act = act
        self.title = title
        self.phase = phase
    }

    /// Whether the card's buttons stay away: while it runs and once it
    /// went. A refused one leaves them.
    public var holdsButtons: Bool {
        if case .failed = phase { return false }
        return true
    }

    public var text: String {
        switch phase {
        case .acting: "Answering…"
        case let .done(words): Self.sentence(words)
        case let .failed(why): why
        }
    }

    /// The server's words as a sentence: "Sent M-3 to beta."
    static func sentence(_ words: String) -> String {
        guard let first = words.first else { return words }
        let s = first.uppercased() + words.dropFirst()
        return s.hasSuffix(".") ? s : s + "."
    }

    public var symbol: String {
        switch phase {
        case .acting: "ellipsis.circle"
        case .done: "checkmark.circle.fill"
        case .failed: "exclamationmark.triangle.fill"
        }
    }
}

extension Need {
    /// The link needs of project p (app API 13), each link's oldest
    /// first: a held message's words are the coordinator's (text).
    static func links(of p: AppAPI.Project) -> [Need] {
        p.linkNeeds.compactMap { n in
            let other = n.shown.isEmpty ? n.peer : n.shown
            switch n.kind {
            case "held":
                let size = n.file.isEmpty ? LinkAct.size(n.bytes) : "\(LinkAct.size(n.bytes)) · \(n.file), \(LinkAct.size(n.size))"
                return Need(id: "link:\(p.slug):\(n.id)", kind: .link, project: p.slug,
                            what: "\(n.id) to \(other) waits for your OK", detail: "Message · \(size)", text: n.title,
                            link: LinkAct(kind: .held, project: p.slug, id: n.id, other: other, file: n.file, size: n.size))
            case "file":
                return Need(id: "link:\(p.slug):\(n.id)", kind: .link, project: p.slug,
                            what: "\(other) offers a file", detail: "\(n.file) · \(LinkAct.size(n.size))",
                            link: LinkAct(kind: .file, project: p.slug, id: n.id, other: other, file: n.file, size: n.size))
            case "request":
                let act = LinkAct(kind: .request, project: p.slug, id: n.id, other: other, allow: n.allow)
                return Need(id: "link:\(p.slug):\(n.id)", kind: .link, project: p.slug,
                            what: "\(other) asks to link", detail: "It asks for \(LinkAct.Direction(allow: LinkAct.mirror(n.allow), ask: false).words) on this side",
                            link: act)
            default:
                return nil
            }
        }
    }

    /// A paired server's share requests that wait for the user, each
    /// its own need; a tm before app API 13 sends only their count.
    static func shares(of peer: AppAPI.Peer, server: String) -> [Need] {
        guard !peer.shareRequests.isEmpty else {
            guard peer.shareWaits > 0 else { return [] }
            return [Need(id: "share:\(peer.alias)", kind: .network, project: "",
                         what: "\(server) asks you to share a project", detail: "\(server) · paired server")]
        }
        return peer.shareRequests.map { q in
            Need(id: "share:\(peer.alias):\(q.id)", kind: .network, project: "",
                 what: "\(server) asks you to share a project",
                 detail: q.for.isEmpty ? "\(server) · paired server" : "For its \(q.for) · \(server)", text: q.note,
                 link: LinkAct(kind: .share, project: "", id: q.id, other: server, alias: peer.alias, forProject: q.for))
        }
    }
}

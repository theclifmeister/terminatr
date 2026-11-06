import Foundation

/// What a session window shows: a project's coordinator (thread empty)
/// or one of its threads, this server's or, with server set, a paired
/// server's (its alias), whose pane and conversation come through the
/// server's peer link. It names the session by its place, not its id, so the window
/// outlives a restart of the agent.
public struct SessionRef: Codable, Hashable, Sendable, CustomStringConvertible {
    public var project: String
    public var thread: String
    public var server: String
    /// A program's director (the programs design §11.1): project is the
    /// program's slug, thread empty. This server's only.
    public var director: Bool

    public init(project: String, thread: String = "", server: String = "") {
        self.project = project
        self.thread = thread
        self.server = server
        self.director = false
    }

    /// Program slug's director's window.
    public static func director(_ slug: String) -> SessionRef {
        var r = SessionRef(project: slug)
        r.director = true
        return r
    }

    enum CodingKeys: String, CodingKey {
        case project, thread, server, director
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(project, forKey: .project)
        try c.encode(thread, forKey: .thread)
        try c.encode(server, forKey: .server)
        if director { try c.encode(true, forKey: .director) }
    }

    /// A window restored from before paired servers had windows is
    /// this server's.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        project = try c.decode(String.self, forKey: .project)
        thread = try c.decodeIfPresent(String.self, forKey: .thread) ?? ""
        server = try c.decodeIfPresent(String.self, forKey: .server) ?? ""
        director = try c.decodeIfPresent(Bool.self, forKey: .director) ?? false
    }

    /// A project's coordinator: not a thread, nor a program's director.
    public var isCoordinator: Bool { thread.isEmpty && !director }

    /// What its rows and titles call it: "coordinator", "director", or
    /// its thread's id.
    public var who: String { director ? "director" : thread.isEmpty ? "coordinator" : thread }

    /// Its place as text, for keys ("prompt \(ref)"): "project",
    /// "project/thread", and a paired server's "server:project/thread".
    /// Written out: without it "\(ref)" walks the struct by reflection
    /// (a Mirror) on every view update that builds such a key (T956).
    public var description: String {
        (server.isEmpty ? "" : server + ":") + project + (director ? "#director" : thread.isEmpty ? "" : "/" + thread)
    }

    /// A paired server's session: its conversation crosses the link
    /// only under that server's switches (RemoteConversation).
    public var isRemote: Bool { !server.isEmpty }

    /// The window of a paired server's session s, of peer alias; nil
    /// for one that isn't a coordinator's or a thread's.
    public init?(_ s: AppAPI.PeerSession, of alias: String) {
        guard !s.project.isEmpty else { return nil }
        switch s.role {
        case "coordinator": self.init(project: s.project, server: alias)
        case "thread" where !s.thread.isEmpty: self.init(project: s.project, thread: s.thread, server: alias)
        default: return nil
        }
    }

    /// The paired server's session running it now, as the server last
    /// read it; nil for this server's ref.
    public func session(in peers: [AppAPI.Peer]) -> AppAPI.PeerSession? {
        guard isRemote, let peer = peers.first(where: { $0.alias == server }) else { return nil }
        let sessions = peer.projects.first { $0.project == project }?.sessions ?? []
        return sessions.first { isCoordinator ? $0.role == "coordinator" : $0.role == "thread" && $0.thread == thread }
    }

    /// The thread it names on this server; nil for a paired server's ref:
    /// a project of the same name here holds other threads, under the
    /// same ids (T1037).
    public func ownThread(in projects: [AppAPI.Project]) -> AppAPI.Thread? {
        guard !isRemote, !thread.isEmpty else { return nil }
        return projects.first { $0.slug == project }?.threads.first { $0.record.id == thread }
    }

    /// The thread it names on its paired server, as that server last
    /// said; nil for this server's ref.
    public func peerThread(in peers: [AppAPI.Peer]) -> AppAPI.PeerThread? {
        guard isRemote, !thread.isEmpty, let peer = peers.first(where: { $0.alias == server }) else { return nil }
        return peer.projects.first { $0.project == project }?.watched.first { $0.id == thread }
    }

    /// Its thread's task and title, from the server it lives on, as its
    /// sidebar row names it; empty when that server doesn't say.
    public func naming(in projects: [AppAPI.Project], peers: [AppAPI.Peer]) -> (task: String, title: String) {
        if isRemote {
            let t = peerThread(in: peers)
            let task = t?.task ?? ""
            return (task.isEmpty ? session(in: peers)?.task ?? "" : task, t?.title ?? "")
        }
        let t = ownThread(in: projects)
        return (t?.task?.ref ?? "", t.map { $0.task?.title ?? $0.record.title } ?? "")
    }

    /// The window of session s: its project's coordinator or one of its
    /// threads; nil for any other session (a plain shell, one outside
    /// the projects).
    public init?(_ s: AppAPI.Session) {
        guard !s.project.isEmpty else { return nil }
        switch s.role {
        case "coordinator": self.init(project: s.project)
        case "thread" where !s.thread.isEmpty: self.init(project: s.project, thread: s.thread)
        case "director" where s.server.isEmpty: self = .director(s.project)
        default: return nil
        }
    }

    /// The session running it now, or nil: a thread's from its record,
    /// a coordinator's or a director's from the session list.
    public func session(in projects: [AppAPI.Project], sessions: [AppAPI.Session]) -> String? {
        if isRemote { return nil }
        if director {
            return sessions.first { $0.role == "director" && $0.project == project && $0.server.isEmpty }?.id
        }
        if isCoordinator {
            return sessions.first { $0.role == "coordinator" && $0.project == project }?.id
        }
        let id = projects.first { $0.slug == project }?.threads.first { $0.record.id == thread }?.record.session
        return id.flatMap { $0.isEmpty ? nil : $0 }
    }
}

/// The window's two views of a session (design T734 §2 e): the
/// conversation from its transcript, or the terminal from its pane.
public enum SessionTab: String, CaseIterable, Sendable {
    case conversation = "Conversation"
    case terminal = "Terminal"
}

/// Whether the conversation can be shown, and why not when it can't:
/// the window opens on the conversation, and falls back to the terminal
/// and says so when the server, the agent or its transcript rule it out.
public enum ConversationState: Equatable, Sendable {
    /// The transcript stream hasn't answered yet.
    case loading
    /// The agent hasn't named its transcript yet.
    case waiting
    case mapped
    /// Terminal only, with the window's words for why.
    case unavailable(String)

    public var available: Bool {
        switch self {
        case .unavailable: false
        default: true
        }
    }

    /// caps are the server's hello capabilities, agent the session's,
    /// status the transcript stream's last, failure why the stream
    /// broke or was refused, remote what a paired server's session
    /// allows (nil for this server's): one that can't open is
    /// terminal-only until a stream answered.
    public static func of(caps: [String], agent: String, status: AppAPI.TranscriptStatus?, failure: String?, remote: RemoteConversation? = nil) -> ConversationState {
        if case .closed(let why) = remote, status == nil {
            return .unavailable(why)
        }
        if !caps.contains("transcript") {
            return .unavailable("This tm server doesn't serve conversations; tm update and tm server restart bring them.")
        }
        if agent == "codex" {
            return .unavailable("Codex sessions show in the terminal.")
        }
        if let failure {
            return .unavailable("The conversation stopped: \(failure)")
        }
        guard let status else { return .loading }
        switch status.state {
        case "mapped": return .mapped
        case "waiting": return .waiting
        default:
            let who = [status.agent.isEmpty ? agent : status.agent, status.version].filter { !$0.isEmpty }.joined(separator: " ")
            let why = status.reason.isEmpty ? "" : " (\(status.reason))"
            return .unavailable("tm can't read this \(who.isEmpty ? "agent" : who)'s transcript\(why), so the terminal shows it.")
        }
    }
}

/// Whether a paired server's conversation can be read, and what the
/// window may do with it (T773): the far server shares it while its pane
/// switch for this server is view or write and its conversations switch
/// is on; the far server checks both again on every open, and every
/// prompt goes into its pane as typing does, at write only.
public enum RemoteConversation: Equatable, Sendable {
    /// Read, and the composer types into the far pane.
    case write
    /// Read only: the composer is a muted View only line.
    case view
    /// Terminal only, with the window's words for why.
    case closed(String)

    /// caps are this server's, peer the paired server as peer.list last
    /// had it (nil: not paired any more).
    public static func of(caps: [String], peer: AppAPI.Peer?, alias: String) -> RemoteConversation {
        guard caps.contains("transcript-remote") else {
            return .closed("Conversations stay on \(alias): this tm server doesn't show paired servers' conversations; tm update and tm server restart bring them.")
        }
        guard let peer else { return .closed("\(alias) isn't paired with this server any more.") }
        guard peer.state == "up" else {
            return .closed("\(alias) is offline\(peer.reason.isEmpty ? "" : " (\(peer.reason))"); the conversation opens once its link is back.")
        }
        guard peer.transcript else {
            return .closed("Conversations stay on \(alias): its tm needs an update to share them; only its pane crosses the link.")
        }
        guard peer.panes == "view" || peer.panes == "type" else {
            return .closed("\(alias) doesn't share conversations with this server: on \(alias), tm peer panes <this server> view, or write.")
        }
        guard peer.conversations != "off" else {
            return .closed("\(alias) doesn't share conversations with this server: on \(alias), tm peer conversations <this server> on.")
        }
        return peer.panes == "type" ? .write : .view
    }

    /// The composer sends: at write only.
    public var sends: Bool { self == .write }

    /// The conversation can be read.
    public var reads: Bool {
        if case .closed = self { return false }
        return true
    }

    /// Why a paired server's stream ended, in the window's words, from
    /// the server's fixed word (transcript.closed's reason).
    public static func ended(_ reason: String, alias: String) -> String {
        switch reason {
        case "exited": "The session ended on \(alias)."
        case "stopping": "\(alias)'s tm server is stopping."
        case "not-shared": "\(alias) stopped sharing this project."
        case "panes-off": "\(alias) turned its panes off for this server, and its conversations with them."
        case "conversations-off": "\(alias) stopped sharing conversations with this server."
        case "link": "The link to \(alias) dropped."
        default: "The conversation on \(alias) closed (\(reason))."
        }
    }
}

/// Whether a paired server's pane can be opened, and what it lets this
/// app do with it: the far side's switch and its link decide (design
/// T734 R7); the server checks both again on every open and every key.
public enum RemotePane: Equatable, Sendable {
    /// Its keys go to the far pane.
    case write
    /// It shows, and keys are dropped there.
    case view
    /// It can't open, with the window's words for why.
    case closed(String)

    /// caps are this server's, peer the paired server as peer.list last
    /// had it (nil: not paired any more).
    public static func of(caps: [String], peer: AppAPI.Peer?, alias: String) -> RemotePane {
        guard caps.contains("pane-vt-remote") else {
            return .closed("This tm server doesn't open paired servers' panes in the app; tm update and tm server restart bring it.")
        }
        guard let peer else { return .closed("\(alias) isn't paired with this server any more.") }
        guard peer.state == "up" else {
            return .closed("\(alias) is offline\(peer.reason.isEmpty ? "" : " (\(peer.reason))"); the pane opens once its link is back.")
        }
        guard peer.pane else { return .closed("\(alias)'s tm has no remote panes; update it to open them.") }
        switch peer.panes {
        case "type": return .write
        case "view": return .view
        default: return .closed("\(alias) doesn't share panes with this server: on \(alias), tm peer panes <this server> view, or write.")
        }
    }
}

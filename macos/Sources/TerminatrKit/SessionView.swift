import Foundation

/// What a session window shows: a project's coordinator (thread empty)
/// or one of its threads. It names the session by its place, not its
/// id, so the window outlives a restart of the agent.
public struct SessionRef: Codable, Hashable, Sendable {
    public var project: String
    public var thread: String

    public init(project: String, thread: String = "") {
        self.project = project
        self.thread = thread
    }

    public var isCoordinator: Bool { thread.isEmpty }

    /// The window of session s: its project's coordinator or one of its
    /// threads; nil for any other session (a plain shell, one outside
    /// the projects).
    public init?(_ s: AppAPI.Session) {
        guard !s.project.isEmpty else { return nil }
        switch s.role {
        case "coordinator": self.init(project: s.project)
        case "thread" where !s.thread.isEmpty: self.init(project: s.project, thread: s.thread)
        default: return nil
        }
    }

    /// The session running it now, or nil: a thread's from its record,
    /// a coordinator's from the session list.
    public func session(in projects: [AppAPI.Project], sessions: [AppAPI.Session]) -> String? {
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
    /// broke or was refused.
    public static func of(caps: [String], agent: String, status: AppAPI.TranscriptStatus?, failure: String?) -> ConversationState {
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

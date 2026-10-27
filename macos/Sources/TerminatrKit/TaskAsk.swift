/// An ask the window put to a task's coordinator (task.accept,
/// task.send-back, task.delegate) and what came of it, which the card
/// shows as its outcome row in place of the buttons (T769). It is the
/// task's while the task keeps the status it was asked in: once the
/// board moves the task on, the ask is done with.
public struct TaskAsk: Equatable, Sendable {
    public enum Kind: Equatable, Sendable {
        case accept
        /// The user's note, kept for Retry.
        case sendBack(note: String)
        case delegate
    }

    public enum Phase: Equatable, Sendable {
        case asking
        /// The coordinator has the ask.
        case asked
        /// An ask about the task waited on the coordinator already.
        case waiting
        /// The server refused it, in the window's words.
        case failed(String)
    }

    /// The row's colour family: the card's own for an ask made, the
    /// muted one for one that waits, attention for a refusal.
    public enum Tone: Equatable, Sendable {
        case good, action, neutral, attention
    }

    public let task: String
    public let kind: Kind
    /// The task's status when it was asked.
    public let status: String
    public var phase: Phase

    public init(task: String, kind: Kind, status: String, phase: Phase = .asking) {
        self.task = task
        self.kind = kind
        self.status = status
        self.phase = phase
    }

    /// The dictionary key the app keeps asks under.
    public static func key(_ project: String, _ task: String) -> String {
        "\(project)/\(task)"
    }

    /// Whether the card's buttons stay away: while the ask runs or waits
    /// on the coordinator. A refused one leaves them, beside Retry.
    public var holdsButtons: Bool {
        if case .failed = phase { return false }
        return true
    }

    /// Whether the ask still stands for the task at status: a task the
    /// board moved on (accepted, sent back, delegated) has done with it.
    public func stands(_ status: String) -> Bool {
        self.status == status
    }

    public var text: String {
        switch phase {
        case .asking:
            switch kind {
            case .accept: return "Asking the coordinator to accept \(task)…"
            case .sendBack: return "Asking the coordinator to send \(task) back…"
            case .delegate: return "Asking the coordinator to delegate \(task)…"
            }
        case .asked:
            switch kind {
            case .accept: return "Accept requested: the coordinator will merge and close \(task)."
            case .sendBack: return "Send back requested: the coordinator will pass your note to \(task)'s thread."
            case .delegate: return "Delegate requested: the coordinator will start a thread for \(task)."
            }
        case .waiting:
            return "\(task) waits on the coordinator already."
        case let .failed(why):
            return why
        }
    }

    /// The row's SF Symbol.
    public var symbol: String {
        switch phase {
        case .asking: return "ellipsis.circle"
        case .waiting: return "clock"
        case .failed: return "exclamationmark.triangle.fill"
        case .asked:
            switch kind {
            case .accept: return "checkmark.circle.fill"
            case .sendBack: return "arrow.uturn.backward.circle"
            case .delegate: return "person.badge.plus"
            }
        }
    }

    public var tone: Tone {
        switch phase {
        case .asking, .waiting: return .neutral
        case .failed: return .attention
        case .asked: return kind == .accept ? .good : .action
        }
    }

    /// Retry's name for VoiceOver ("Retry accepting T1").
    public var retryLabel: String {
        switch kind {
        case .accept: return "Retry accepting \(task)"
        case .sendBack: return "Retry sending \(task) back"
        case .delegate: return "Retry delegating \(task)"
        }
    }
}

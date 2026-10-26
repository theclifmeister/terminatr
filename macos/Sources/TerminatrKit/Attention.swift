import Foundation

/// What a thread's card and sidebar row say of it (T748 milestone B):
/// its tag, the dot's tone and one line of why. What waits on the user
/// comes first and agrees with the needs list (Need.of): a thread that
/// waits on you never reads "running".
public struct Attention: Hashable, Sendable {
    public enum Tone: Sendable {
        case approval, question, attention, review, working, idle, done
    }

    /// The tag: "allow?", "asks", "held", "review", "blocked", "CI
    /// fails", "needs you", or the session's state.
    public let word: String
    public let tone: Tone
    /// One line: what it waits on, or what it does.
    public let why: String
    /// Whether it waits on the user.
    public let waits: Bool

    public init(word: String, tone: Tone, why: String, waits: Bool) {
        self.word = word
        self.tone = tone
        self.why = why
        self.waits = waits
    }

    /// t's attention, with s its session as session.list has it (nil
    /// when none runs).
    public static func of(thread t: AppAPI.Thread, session s: AppAPI.Session?, now: Date = Date()) -> Attention {
        let asked = t.status?.needsYou ?? ""
        if let s {
            let menu = s.question.map { q in q.questions.contains { !$0.answered } } ?? false
            if menu || (s.state == "blocked" && s.reason == "question") {
                let why = menu ? "asks a question" : "asks a question · answer in Terminal"
                return Attention(word: "asks", tone: .question, why: why, waits: true)
            }
            if s.state == "blocked" && s.reason == "permission" {
                return Attention(word: "allow?", tone: .approval,
                                 why: s.escape ? "asks to run outside the sandbox" : "needs approval", waits: true)
            }
            if Need.held(s, now: now) {
                return Attention(word: "held", tone: .attention, why: "prompt held: \(s.queueHeld)", waits: true)
            }
        }
        switch t.task?.status {
        case "review":
            return Attention(word: "review", tone: .review, why: "ready to review", waits: true)
        case "blocked":
            return Attention(word: "blocked", tone: .attention, why: asked.isEmpty ? "blocked" : asked, waits: true)
        default:
            break
        }
        if t.checks == "fail" && t.pr > 0 {
            return Attention(word: "CI fails", tone: .attention, why: "PR #\(t.pr) checks fail", waits: true)
        }
        if !asked.isEmpty {
            return Attention(word: "needs you", tone: .attention, why: asked, waits: true)
        }
        let doing = t.status.map { $0.current.isEmpty ? $0.activity : $0.current } ?? ""
        if t.record.done {
            return Attention(word: "done", tone: .done, why: doing, waits: false)
        }
        guard let s else {
            return Attention(word: "stopped", tone: .idle, why: "no session runs", waits: false)
        }
        switch s.state {
        case "working":
            return Attention(word: "working", tone: .working, why: doing.isEmpty ? s.current : doing, waits: false)
        case "blocked":
            return Attention(word: s.reason.isEmpty ? "blocked" : s.reason, tone: .attention, why: doing, waits: false)
        case "":
            return Attention(word: "running", tone: .working, why: doing, waits: false)
        default:
            return Attention(word: s.state, tone: .idle, why: doing, waits: false)
        }
    }
}

extension AppAPI.Item {
    /// The ticker's notes on a thread's agent state: written for the
    /// coordinator, and what they say is a need already.
    static let coordinatorKinds: Set<String> = ["blocked", "idle", "exited", "prompt-waiting", "needs-you"]

    /// Whether the inbox shows it to the user: an item that needs the
    /// user, other than the coordinator's nudges about its threads.
    public var forYou: Bool {
        needsUser && !Self.coordinatorKinds.contains(kind)
    }

    /// The summary without the tm commands it ends with for the
    /// coordinator: "… (tm link resume acme)".
    public var shownSummary: String {
        guard summary.hasSuffix(")"), let open = summary.range(of: " (tm ", options: .backwards) else { return summary }
        return String(summary[..<open.lowerBound])
    }
}

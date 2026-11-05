import Foundation

/// A refused call as Settings › Guard lists it (T998), in the words of
/// the TUI's Guard refusals (internal/tui/settings_guard.go): who was
/// refused and under which rule, and whether the user has a choice. A
/// refusal is no need: it waits there until it is answered.
extension AppAPI.GuardRefusal {
    /// Whether the user has a real choice on the refusal: an allowance
    /// the guard would honour. The floor's, tm's files' and any other no
    /// allowance names (fixed, app API 24; an older tm marks the floor's
    /// only) can only be kept refusing.
    public var actionable: Bool { !floor && !fixed }

    /// Who was refused and with what, the project named: its thread's
    /// task while one runs, its thread, its session's role.
    public func what(sessions: [AppAPI.Session]) -> String {
        let s = thread.isEmpty ? nil : sessions.first {
            $0.server.isEmpty && $0.project == project && $0.thread == thread && $0.role == "thread"
        }
        let who = !(s?.task ?? "").isEmpty ? s!.task : !thread.isEmpty ? thread : !role.isEmpty ? role : "a session"
        return "\(project) · \(who)'s \(tool) call was refused"
    }

    /// The rule in the user's words, what the journal says of the call,
    /// and how often it was refused. Never the whole command: that shows
    /// on its own, selectable.
    public var detail: String {
        Need.line(ruleWord, summary, count > 1 ? "\(count) times" : "")
    }

    /// Its rule as the TUI's Guard refusals names it; a mod's as it is
    /// named.
    public var ruleWord: String {
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

    /// Why it can't be allowed, nil when it can: the floor's reason as
    /// the TUI says it.
    public var neverWhy: String? {
        if floor {
            let why = switch rule {
            case "merge": "a thread doesn't merge while the coordinator merges"
            case "force-push": "it force-pushes to a protected branch"
            default: "it reads tm's own files or prints a secret variable"
            }
            return "Never allowed: \(why)."
        }
        guard fixed else { return nil }
        // tm's answer to allowing it (guardlog's allowance): a folder, not a call.
        if rule == "worktree-only" && path {
            return "A write outside the worktree is allowed by a write folder: add its folder in tm's Settings, Access, Write folders."
        }
        return "No allowance lifts this one: keeping it refused is all there is."
    }
}

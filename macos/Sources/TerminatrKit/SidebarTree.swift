import Foundation

/// What the window's sidebar lists (T748 milestone A), kept lean: the
/// projects in the TUI's order, a coordinator row only when it runs or
/// asks, under a project only the threads worth a row, and the needs
/// cut to a few.
public enum SidebarTree {
    /// The needs the sidebar's Needs you shows before "+N more" (A3).
    public static let needsShown = 3

    /// The active projects as the TUI lists them (SPEC §5.1): the
    /// pinned ones first, each group in the server's order. ⌘1–⌘9, the
    /// Go menu, the menu bar and the first selection count only these.
    public static func active(_ projects: [AppAPI.Project]) -> [AppAPI.Project] {
        let active = projects.filter(\.active)
        return active.filter(\.pinned) + active.filter { !$0.pinned }
    }

    /// The inactive projects, folded under "Inactive (n)" at the bottom,
    /// pinned or not.
    public static func inactive(_ projects: [AppAPI.Project]) -> [AppAPI.Project] {
        projects.filter { !$0.active }
    }

    /// The open threads a shown project lists (A4), in its order: those
    /// that wait on the user always, and the selected one; the rest fold
    /// into "N more" (more) unless unfolded.
    public static func threads(of p: AppAPI.Project, selected: String?, unfolded: Bool,
                               attention: (AppAPI.Thread) -> Attention) -> (shown: [AppAPI.Thread], more: Int) {
        let open = p.threads.filter { !$0.record.done }
        if unfolded { return (open, 0) }
        let shown = open.filter { $0.record.id == selected || attention($0).waits }
        return (shown, open.count - shown.count)
    }

    /// Whether a project lists its coordinator and threads: the one
    /// selected (or one of its sessions), and one that needs the user.
    public static func expands(_ p: AppAPI.Project, selection: Selection?, needs: Int) -> Bool {
        needs > 0 || (selection?.server == "" && selection?.project == p.slug)
    }

    /// "+4 more: 2 review, 2 blocked": the needs past the shown ones,
    /// by kind in the order they come.
    public static func moreNeeds(_ rest: [Need]) -> String {
        var kinds: [Need.Kind] = [], n: [Need.Kind: Int] = [:]
        for r in rest {
            if n[r.kind] == nil { kinds.append(r.kind) }
            n[r.kind, default: 0] += 1
        }
        let parts = kinds.map { "\(n[$0]!) \(word($0))" }
        return "+\(rest.count) more" + (parts.isEmpty ? "" : ": " + parts.joined(separator: ", "))
    }

    static func word(_ k: Need.Kind) -> String {
        switch k {
        case .question: "asking"
        case .approval: "to allow"
        case .review: "review"
        case .blocked: "blocked"
        case .checks: "CI failing"
        case .held: "held"
        case .network: "paired"
        }
    }
}

extension Attention {
    /// What a project's coordinator says of itself, its project's dot:
    /// with s its session as session.list has it (nil when none runs).
    /// It has a sidebar row only when it runs or waits on the user
    /// (A2): its open questions (tm ask) wait even while none runs.
    public static func of(coordinator s: AppAPI.Session?, in p: AppAPI.Project, now: Date = Date()) -> Attention {
        if let s {
            let menu = s.question.map { q in q.questions.contains { !$0.answered } } ?? false
            if menu || (s.state == "blocked" && s.reason == "question") {
                return Attention(word: "asks", tone: .question, why: menu ? "asks a question" : "asks a question · answer in Terminal", waits: true)
            }
            if s.state == "blocked" && s.reason == "permission" {
                return Attention(word: "allow?", tone: .approval,
                                 why: s.escape ? "asks to run outside the sandbox" : "needs approval", waits: true)
            }
            if Need.held(s, now: now) {
                return Attention(word: "held", tone: .attention, why: "prompt held: \(s.queueHeld)", waits: true)
            }
        }
        if p.questions > 0 {
            return Attention(word: "asks", tone: .question,
                             why: p.questions == 1 ? "has a question" : "has \(p.questions) questions", waits: true)
        }
        guard let s else {
            return Attention(word: "not running", tone: .idle, why: "no coordinator runs", waits: false)
        }
        switch s.state {
        case "working":
            return Attention(word: "working", tone: .working, why: s.current, waits: false)
        case "":
            return Attention(word: "running", tone: .working, why: s.current, waits: false)
        case "blocked":
            return Attention(word: s.reason.isEmpty ? "blocked" : s.reason, tone: .attention, why: s.current, waits: false)
        default:
            return Attention(word: s.state, tone: .idle, why: s.current, waits: false)
        }
    }
}

import Foundation

/// What a board row says of its after links (the board design, step 7;
/// app API 53): muted "after T912" while a link is unmet, "2 wait" on the
/// task they wait for, nothing once the link is met. The server reads the
/// after field and sends only the unmet links (waits_for) and the count
/// (waiters); a program task keeps its "acme-api/T12" form. The words are
/// tm's tasks.WaitWords.
public enum Waits {
    /// "after T9", "after T9, acme-api/T12", "after T9, T10 +2" for
    /// refs; "" for none.
    public static func after(_ refs: [String]) -> String {
        guard let first = refs.first else { return "" }
        if refs.count <= 2 { return "after " + refs.joined(separator: ", ") }
        return "after \(first), \(refs[1]) +\(refs.count - 2)"
    }

    /// "2 wait": how many open tasks come after a task; "" for none.
    public static func waiters(_ n: Int) -> String {
        n > 0 ? "\(n) wait" : ""
    }

    /// Both, joined with " · "; "" when the row says nothing.
    public static func words(_ t: AppAPI.Task) -> String {
        [after(t.waitsFor), waiters(t.waiters)].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// For VoiceOver: "waits for T9", "2 tasks wait for it".
    public static func spoken(_ t: AppAPI.Task) -> String {
        var out: [String] = []
        if !t.waitsFor.isEmpty { out.append("comes after " + t.waitsFor.joined(separator: ", ")) }
        if t.waiters > 0 { out.append(t.waiters == 1 ? "1 task waits for it" : "\(t.waiters) tasks wait for it") }
        return out.joined(separator: ", ")
    }
}

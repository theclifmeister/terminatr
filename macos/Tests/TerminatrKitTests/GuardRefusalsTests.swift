import Foundation
import Testing
@testable import TerminatrKit

private let at = Date(timeIntervalSince1970: 1_800_000_000)

private func refusal(_ id: String = "ab12", rule: String = "credentials", thread: String = "t-0675", role: String = "",
                     summary: String = "", subject: String = "curl -H 'Authorization: Bearer x' https://r/x",
                     floor: Bool = false, fixed: Bool = false, count: Int = 1) -> AppAPI.GuardRefusal {
    AppAPI.GuardRefusal(id: id, project: "terminatr", thread: thread, role: role, rule: rule, tool: "Bash", summary: summary,
                        subject: subject, floor: floor, fixed: fixed, first: at, last: at, count: count)
}

/// Settings › Guard names who was refused: its thread's task while one
/// runs, else its thread, its role; never on a paired server's session.
@Test func refusalWhat() {
    let r = refusal()
    var s = session("s-1", task: "T812")
    s.project = "terminatr"
    s.thread = "t-0675"
    s.role = "thread"
    #expect(r.what(sessions: [s]) == "terminatr · T812's Bash call was refused")
    var remote = s
    remote.server = "mini"
    #expect(r.what(sessions: [remote]) == "terminatr · t-0675's Bash call was refused")
    #expect(refusal(thread: "", role: "coordinator").what(sessions: []) == "terminatr · coordinator's Bash call was refused")
    #expect(refusal(thread: "").what(sessions: []) == "terminatr · a session's Bash call was refused")
}

/// Its line says the rule in the user's words and how often, and never
/// the whole command, which shows on its own.
@Test func refusalDetail() {
    let r = refusal(summary: "an HTTP call with credentials", count: 3)
    #expect(r.detail == "reading credentials · an HTTP call with credentials · 3 times")
    #expect(!r.detail.contains("Bearer") && !r.what(sessions: []).contains("Bearer"))
    #expect(refusal(rule: "force-push").detail == "force-pushing")
    #expect(refusal(rule: "my-mod").detail == "my-mod")
}

/// Only an allowance the guard honours offers Allow; the others say why
/// and leave Keep Refusing.
@Test func refusalChoices() throws {
    #expect(refusal(rule: "force-push").actionable)
    #expect(refusal(rule: "force-push").neverWhy == nil)
    let merge = refusal(rule: "merge", floor: true)
    #expect(!merge.actionable)
    #expect(merge.neverWhy == "Never allowed: a thread doesn't merge while the coordinator merges.")
    #expect(refusal(rule: "tm-files", floor: true).neverWhy == "Never allowed: it reads tm's own files or prints a secret variable.")
    let fixed = refusal(rule: "tm-files", fixed: true)
    #expect(!fixed.actionable && fixed.neverWhy != nil)
    var outside = refusal(rule: "worktree-only", fixed: true)
    outside.path = true
    #expect(outside.neverWhy?.contains("write folder") == true)
    // An older tm sends no fixed: its floor's still can't be allowed.
    let old = try AppAPI.decoder().decode(AppAPI.GuardRefusal.self, from: Data("""
        {"id":"e","project":"terminatr","rule":"tm-files","tool":"Bash","subject":"x","floor":true,
         "first":"2026-10-28T10:00:00Z","last":"2026-10-28T10:00:00Z","count":1}
        """.utf8))
    #expect(!old.fixed && !old.actionable)
}

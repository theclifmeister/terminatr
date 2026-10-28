import Foundation
import Testing
@testable import TerminatrKit

let t0 = Date(timeIntervalSince1970: 1_800_000_000)

func session(_ id: String, task: String = "T812", state: String = "working", reason: String = "",
             question: AppAPI.Question? = nil, escape: Bool = false, server: String = "") -> AppAPI.Session {
    AppAPI.Session(id: id, role: "thread", project: "terminatr", thread: "t-0675", task: task, title: "Cache layer",
                   cols: 80, rows: 24, created: t0, clients: 0, state: state, reason: reason, escape: escape,
                   question: question, server: server)
}

func menu(_ items: [AppAPI.QuestionItem], escape: Bool = false) -> AppAPI.Question {
    AppAPI.Question(tool: "AskUserQuestion", questions: items, since: t0, escape: escape)
}

let noUsage = AppAPI.Usage(turns: 0, input: 0, output: 0, cacheRead: 0, cacheCreation: 0, costUsd: 0)

let backend = AppAPI.QuestionItem(question: "Which cache backend?", options: [.init(label: "In memory"), .init(label: "On disk")])

@Test func questionNeed() throws {
    let needs = Need.of(projects: [], sessions: [session("s-1", state: "blocked", reason: "question", question: menu([backend]))])
    let n = try #require(needs.first)
    #expect(needs.count == 1)
    #expect(n.kind == .question)
    #expect(n.title == "terminatr · T812 asks a question")
    // Its thread isn't among the projects: no task title, and never the
    // pane's (taskTitles).
    #expect(n.detail == "")
    // The question's words are text, kept off the title and detail.
    #expect(n.text == "Which cache backend?")
    #expect(!n.title.contains("cache backend") && !n.detail.contains("cache backend"))
    let a = try #require(n.answer)
    #expect(a.options == ["In memory", "On disk"])
    #expect(a.params("On disk") == AppAPI.AnswerParams(id: "s-1", index: 0, answer: "On disk", question: "Which cache backend?"))
}

/// Buttons only for one open, single-select question of a menu that
/// isn't an escape's.
@Test func answerable() {
    let multi = AppAPI.QuestionItem(question: "Which ones?", multiSelect: true, options: [.init(label: "A"), .init(label: "B")])
    let done = AppAPI.QuestionItem(question: "First?", options: [.init(label: "Yes")], answered: true, answer: "Yes")
    let free = AppAPI.QuestionItem(question: "Why?")
    #expect(Need.answerable("s", menu([backend], escape: true)) == nil)
    #expect(Need.answerable("s", menu([multi])) == nil)
    #expect(Need.answerable("s", menu([backend, multi])) == nil)
    #expect(Need.answerable("s", menu([free])) == nil)
    // The one left open of two: its index is the menu's.
    #expect(Need.answerable("s", menu([done, backend]))?.index == 1)
    let many = AppAPI.QuestionItem(question: "Pick", options: (1...6).map { .init(label: "o\($0)") })
    #expect(Need.answerable("s", menu([many]))?.options.count == Need.maxButtons)
}

@Test func sessionNeeds() {
    let approval = Need.of(projects: [], sessions: [session("s-2", state: "blocked", reason: "permission")])
    #expect(approval.map(\.id) == ["approval:s-2"])
    #expect(approval.first?.title == "terminatr · T812 needs approval")
    let escape = Need.of(projects: [], sessions: [session("s-2", state: "blocked", reason: "permission", escape: true)])
    #expect(escape.first?.title == "terminatr · T812 asks to run outside the sandbox")
    // A paired server's pane is its user's there.
    #expect(Need.of(projects: [], sessions: [session("mini/s-3", state: "blocked", reason: "permission", server: "mini")]).isEmpty)

    var held = session("s-4", state: "idle")
    held.queuedPrompts = 2
    held.queueHeld = "prompt box not empty"
    held.queueHeldSince = t0
    #expect(Need.of(projects: [], sessions: [held], now: t0.addingTimeInterval(30)).isEmpty)
    let h = Need.of(projects: [], sessions: [held], now: t0.addingTimeInterval(61))
    #expect(h.map(\.kind) == [.held])
    #expect(h.first?.what == "T812's 2 prompts are held")
}

@Test func projectNeeds() {
    let thread = AppAPI.Thread(record: AppAPI.ThreadRecord(id: "t-0670", title: "Sidebar CI", agent: "claude", worktree: "/w",
                                                           state: "working", session: "s-9", created: t0, reports: 0,
                                                           reportAcked: 0, usage: noUsage),
                               checks: "fail", pr: 801, task: AppAPI.Task(id: 809, ref: "T809", title: "Sidebar CI", status: "doing"))
    let p = AppAPI.Project(slug: "terminatr", active: true,
                           needsYou: [AppAPI.Task(id: 806, ref: "T806", title: "Docs pins", status: "review"),
                                      AppAPI.Task(id: 807, ref: "T807", title: "Paste guard", status: "blocked")],
                           questions: 2, threads: [thread], usage: noUsage)
    let needs = Need.of(projects: [p], sessions: [])
    // Tasks in review last: their own group (B2), still counted (B1).
    #expect(needs.map(\.title) == ["terminatr · coordinator has 2 questions", "terminatr · T807 is blocked",
                                   "terminatr · T809 PR #801 checks fail", "terminatr · T806 is ready to review"])
    #expect(needs.map(\.kind) == [.question, .blocked, .checks, .review])
    #expect(needs.map(\.isReview) == [false, false, false, true])
    // The second line is the task's title.
    #expect(needs.map(\.detail) == ["coordinator", "Paste guard", "Sidebar CI", "Docs pins"])
    #expect(needs[2].session == "s-9")
    #expect(Need.count(needs, project: "terminatr") == 4)
    #expect(Need.count(needs, project: "toe") == 0)
}

@Test func peerNeeds() {
    let peer = AppAPI.Peer(alias: "mini", state: "up", projects: [
        AppAPI.PeerProject(project: "acme", needsYou: 2, threads: 1, ready: 0),
        AppAPI.PeerProject(project: "old", needsYou: 1, threads: 0, ready: 0, stale: true),
    ], shareWaits: 1)
    let needs = Need.of(projects: [], sessions: [], peers: [peer])
    #expect(needs.map(\.id) == ["share:mini", "paired:mini/acme"])
    #expect(needs.last?.title == "mini › acme: 2 need you")
}

@Test func tracker() {
    func need(_ id: String) -> Need { Need(id: id, kind: .review, project: "p", what: id, detail: "") }
    var t = NeedTracker()
    // The first look learns: nothing is fresh.
    #expect(t.update([need("a"), need("b")]).fresh.isEmpty)
    let next = t.update([need("b"), need("c")])
    #expect(next.fresh.map(\.id) == ["c"])
    #expect(next.gone == ["a"])
    #expect(t.update([need("b"), need("c")]).fresh.isEmpty)
    t.reset()
    #expect(t.update([need("d")]).fresh.isEmpty)
}

func thread(_ id: String, task: AppAPI.Task? = nil, session: String = "", checks: String = "", pr: Int = 0,
            needsYou: String = "") -> AppAPI.Thread {
    AppAPI.Thread(record: AppAPI.ThreadRecord(id: id, title: "thread \(id)", agent: "claude", worktree: "/w",
                                              state: "running", session: session, created: t0, reports: 0,
                                              reportAcked: 0, usage: noUsage),
                  status: needsYou.isEmpty ? nil : AppAPI.ThreadStatus(percent: 0, stepsDone: 0, stepsTotal: 0, todosDone: 0,
                                                                       todosTotal: 0, needsYou: needsYou, selfPercent: 0, updated: t0),
                  checks: checks, pr: pr, task: task)
}

/// A session's second line is its thread's task title, never the
/// pane's title (Claude's terminal title, "✳ Fake Claude").
@Test func taskTitles() {
    let p = AppAPI.Project(slug: "terminatr", active: true, needsYou: [],
                           threads: [thread("t-0675", task: AppAPI.Task(id: 812, ref: "T812", title: "Cache layer for project.watch", status: "started"))],
                           usage: noUsage)
    var s = session("s-1", state: "blocked", reason: "permission")
    s.title = "✳ Fake Claude"
    let n = Need.of(projects: [p], sessions: [s])
    #expect(n.map(\.detail) == ["Cache layer for project.watch"])
    var c = AppAPI.Session(id: "s-2", role: "coordinator", project: "toe", title: "✳ Claude", cols: 80, rows: 24,
                           created: t0, clients: 0, state: "blocked", reason: "permission")
    c.task = ""
    #expect(Need.of(projects: [], sessions: [c]).map(\.title) == ["toe · coordinator needs approval"])
}

/// Blocked on a question with no menu parsed: still a need, answered in
/// Terminal, with no buttons.
@Test func questionWithoutMenu() throws {
    let needs = Need.of(projects: [], sessions: [session("s-5", task: "T2", state: "blocked", reason: "question")])
    let n = try #require(needs.first)
    #expect(needs.count == 1)
    #expect(n.kind == .question)
    #expect(n.id == "question:s-5")
    #expect(n.title == "terminatr · T2 asks a question")
    #expect(n.detail == "answer in Terminal")
    #expect(n.answer == nil)
    #expect(n.session == "s-5")
    // A menu whose questions are all answered, the session still
    // blocked on it: the same.
    let done = AppAPI.QuestionItem(question: "First?", options: [.init(label: "Yes")], answered: true, answer: "Yes")
    #expect(Need.of(projects: [], sessions: [session("s-5", state: "blocked", reason: "question", question: menu([done]))]).map(\.id)
        == ["question:s-5"])
    // Other blocks (a trust dialog, an error) are tm's or the agent's.
    #expect(Need.of(projects: [], sessions: [session("s-5", state: "blocked", reason: "trust")]).isEmpty)
}

@Test func attention() {
    let started = AppAPI.Task(id: 2, ref: "T2", title: "Sidebar CI", status: "started")
    let t = thread("t-0002", task: started, session: "s-2")
    func att(_ s: AppAPI.Session?, _ th: AppAPI.Thread = t) -> Attention { Attention.of(thread: th, session: s, now: t0) }

    let asks = att(session("s-2", state: "blocked", reason: "question"))
    #expect(asks.word == "asks" && asks.tone == .question && asks.waits)
    #expect(asks.why == "asks a question · answer in Terminal")
    #expect(att(session("s-2", state: "blocked", reason: "question", question: menu([backend]))).why == "asks a question")
    let allow = att(session("s-2", state: "blocked", reason: "permission"))
    #expect(allow.word == "allow?" && allow.tone == .approval && allow.why == "needs approval")
    var held = session("s-2", state: "idle")
    held.queuedPrompts = 1
    held.queueHeld = "prompt box not empty"
    held.queueHeldSince = t0.addingTimeInterval(-90)
    #expect(att(held).word == "held")

    // Never "running" (the record's state) while it waits.
    let review = att(session("s-2", state: "idle"), thread("t-0002", task: AppAPI.Task(id: 2, ref: "T2", title: "x", status: "review"), session: "s-2"))
    #expect(review.word == "review" && review.waits)
    #expect(att(nil, thread("t-0002", task: started, checks: "fail", pr: 9)).word == "CI fails")
    #expect(att(nil, thread("t-0002", needsYou: "a question in its session")).why == "a question in its session")

    var working = session("s-2", state: "working")
    working.current = "Reading the cache"
    let w = att(working)
    #expect(w.word == "working" && !w.waits && w.why == "Reading the cache")
    #expect(att(session("s-2", state: "idle")).word == "idle")
    #expect(att(nil).word == "stopped")
}

/// The inbox shows what is for the user, not the ticker's nudges to the
/// coordinator, and drops the tm commands a summary ends with.
@Test func inboxForYou() {
    func item(_ kind: String, _ needsUser: Bool, _ summary: String = "") -> AppAPI.Item {
        AppAPI.Item(id: kind, kind: kind, subject: "s", created: t0, summary: summary, needsUser: needsUser)
    }
    #expect(!item("blocked", true).forYou)
    #expect(!item("needs-you", true).forYou)
    #expect(!item("report", false).forYou)
    #expect(item("link-paused", true).forYou)
    #expect(item("guard", true).forYou)
    #expect(item("x", true, "the link with acme is paused; ask the user (tm link resume acme)").shownSummary
        == "the link with acme is paused; ask the user")
    #expect(item("x", true, "plain (no command)").shownSummary == "plain (no command)")
}

/// A guard refusal (app API 18) is a need of its project, after the
/// projects' and before the paired servers': named by its thread's task
/// while one runs, with the rule in the user's words, the whole command
/// on the card's refusal only, and no session to open (its card is the
/// project's).
@Test func refusalNeed() throws {
    let r = AppAPI.GuardRefusal(id: "ab12", project: "terminatr", thread: "t-0675", rule: "credentials", tool: "Bash",
                                summary: "an HTTP call with credentials", subject: "curl -H 'Authorization: Bearer x' https://r/x",
                                first: t0, last: t0, count: 3)
    let needs = Need.of(projects: [], sessions: [session("s-1")], refusals: [r])
    let n = try #require(needs.first)
    #expect(needs.count == 1)
    #expect(n.kind == .refusal && n.id == "refusal:ab12")
    #expect(n.title == "terminatr · T812's Bash call was refused")
    #expect(n.detail == "reading credentials · an HTTP call with credentials · 3 times")
    #expect(n.refusal == r && n.session.isEmpty && n.text.isEmpty)
    #expect(!n.title.contains("Bearer") && !n.detail.contains("Bearer"))
    // Without its thread's session: the thread.
    let alone = Need.of(projects: [], sessions: [], refusals: [r])
    #expect(alone.first?.title == "terminatr · t-0675's Bash call was refused")
    #expect(Need.count(alone, project: "terminatr") == 1)
}

/// A refusal the user can't allow (the floor's, tm's files', app API 23's
/// fixed) is no need: only keeping it refusing is left, so it stays in
/// the guard's log; one refused again is still one need, counted.
@Test func refusalNeedOnlyWhenAllowable() throws {
    func refusal(_ id: String, _ rule: String, floor: Bool = false, fixed: Bool = false) -> AppAPI.GuardRefusal {
        AppAPI.GuardRefusal(id: id, project: "terminatr", thread: "t-0675", rule: rule, tool: "Bash",
                            subject: "cmd \(id)", floor: floor, fixed: fixed, first: t0, last: t0, count: 5)
    }
    let refusals = [refusal("a", "tm-files", floor: true), refusal("b", "tm-files", fixed: true),
                    refusal("c", "merge", floor: true), refusal("d", "force-push")]
    let needs = Need.of(projects: [], sessions: [], refusals: refusals)
    #expect(needs.map(\.id) == ["refusal:d"])
    #expect(needs.first?.detail == "force-pushing · 5 times")
    #expect(Need.count(needs, project: "terminatr") == 1)
    // An older tm sends no fixed: its floor's refusals are still left out.
    let old = try AppAPI.decoder().decode(AppAPI.GuardRefusal.self, from: Data("""
        {"id":"e","project":"terminatr","rule":"tm-files","tool":"Bash","subject":"x","floor":true,
         "first":"2026-10-28T10:00:00Z","last":"2026-10-28T10:00:00Z","count":1}
        """.utf8))
    #expect(!old.fixed && !old.actionable)
}

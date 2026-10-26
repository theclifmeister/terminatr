import Foundation
import Testing
@testable import TerminatrKit

// What the detail shows and acts from (T748 milestone C): the
// dashboard's thread groups and board, an empty project's next step,
// the task actions' words, and who and when each turn of a
// conversation is.

private func working(_ id: String, task: AppAPI.Task? = nil) -> AppAPI.Session {
    AppAPI.Session(id: id, role: "thread", project: "demo", thread: id, cols: 80, rows: 24, created: t0, clients: 0,
                   state: "working")
}

@Test func dashboardThreadGroups() {
    let threads = [thread("t-1", session: "s-1"), thread("t-2", session: "s-2"), thread("t-3"),
                   thread("t-4", task: AppAPI.Task(id: 4, ref: "T4", title: "Docs", status: "review"), session: "s-4"),
                   thread("t-5", session: "s-5")]
    let sessions = [working("s-1"),
                    AppAPI.Session(id: "s-2", role: "thread", project: "demo", thread: "t-2", cols: 80, rows: 24, created: t0,
                                   clients: 0, state: "idle"),
                    AppAPI.Session(id: "s-4", role: "thread", project: "demo", thread: "t-4", cols: 80, rows: 24, created: t0,
                                   clients: 0, state: "idle"),
                    working("s-5")]
    func session(_ t: AppAPI.Thread) -> AppAPI.Session? { sessions.first { $0.id == t.record.session } }
    let groups = Dashboard.groups(threads, attention: { Attention.of(thread: $0, session: session($0), now: t0) },
                                  running: { session($0) != nil })
    // What waits on you first, then the working ones in their order;
    // idle and stopped ones last, folded.
    #expect(groups.map(\.group) == [.waiting, .working, .idle, .stopped])
    #expect(groups.map { $0.threads.map(\.record.id) } == [["t-4"], ["t-1", "t-5"], ["t-2"], ["t-3"]])
    #expect(groups.map(\.group.folded) == [false, false, true, true])
}

@Test func dashboardBoard() {
    let p = AppAPI.Project(slug: "demo", active: true, counts: ["on_deck": 2, "backlog": 1, "done": 4],
                           board: [AppAPI.Task(id: 2, ref: "T2", title: "B", status: "ready"),
                                   AppAPI.Task(id: 3, ref: "T3", title: "C", status: "open"),
                                   AppAPI.Task(id: 1, ref: "T1", title: "A", status: "ready")],
                           usage: noUsage)
    #expect(Dashboard.onDeck(p).map(\.ref) == ["T2", "T1"])
    #expect(Dashboard.backlog(p).map(\.ref) == ["T3"])
    #expect(!Dashboard.isEmpty(p, needs: 0))
}

@Test func dashboardEmpty() {
    // Only done tasks, no threads, nothing for the user: empty.
    var p = AppAPI.Project(slug: "demo", active: true, counts: ["needs_you": 0, "in_motion": 0, "done": 3], usage: noUsage)
    #expect(Dashboard.isEmpty(p, needs: 0))
    #expect(!Dashboard.isEmpty(p, needs: 1))
    #expect(Dashboard.next(p, coordinatorRuns: false) == .start)
    #expect(Dashboard.next(p, coordinatorRuns: true) == .message)
    // An inbox item for the user isn't empty; the coordinator's nudges
    // don't count.
    p.items = [AppAPI.Item(id: "i1", kind: "idle", subject: "t-1", created: t0, summary: "idle", needsUser: true)]
    #expect(Dashboard.isEmpty(p, needs: 0))
    p.items.append(AppAPI.Item(id: "i2", kind: "report", subject: "t-1", created: t0, summary: "done", needsUser: true))
    #expect(!Dashboard.isEmpty(p, needs: 0))
    p.active = false
    #expect(Dashboard.next(p, coordinatorRuns: false) == .activate)
}

@Test func taskActionWords() {
    #expect(Dashboard.words(AppAPI.Error(code: "already-asked", message: "x"), task: "T3") == "Another ask about T3 waits on the coordinator.")
    #expect(Dashboard.words(AppAPI.Error(code: "not-accept", message: "x"), task: "T3") == "T3 isn't in review any more.")
    #expect(Dashboard.words(AppAPI.Error(code: "project-inactive", message: "x")).hasPrefix("The project is inactive"))
    // The rest: the server's own words.
    #expect(Dashboard.words(AppAPI.Error(code: "invalid-note", message: "the note is empty"), task: "T3") == "the note is empty")
}

private func entry(_ kind: Transcript.Entry.Kind, _ text: String, from: String = "") -> Transcript.Entry {
    Transcript.Entry(id: 0, kind: kind, text: text, truncated: false, tool: "", call: "", at: t0, from: from)
}

@Test func conversationTurns() {
    #expect(entry(.prompt, "run demo").who(agent: "claude") == "you")
    #expect(entry(.prompt, "merge main", from: "coordinator").who(agent: "claude") == "coordinator")
    #expect(entry(.text, "Let me plan this.").who(agent: "claude") == "claude")
    #expect(entry(.tool, "ls").who(agent: "claude") == "")
    // tm's prompts fold to one line; the user's and the coordinator's
    // don't.
    let kickoff = entry(.prompt, "Start on your task. Your brief (/p/brief.md) and your rules…", from: "tm")
    #expect(kickoff.folds)
    #expect(kickoff.headline == "started the thread · brief and rules")
    #expect(entry(.prompt, "You are this project's coordinator. Greet the user…", from: "tm").headline == "started the coordinator")
    let note = entry(.prompt, "[tm] 2 check(s) failed on your PR #7. Read them…\nmore", from: "tm")
    #expect(note.headline == "2 check(s) failed on your PR #7. Read them…")
    #expect(!entry(.prompt, "[tm] typed by the user").folds)
    #expect(!entry(.prompt, "x", from: "coordinator").folds)
}

@Test func transcriptKeepsWhoAndWhen() {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [
        AppAPI.TranscriptItem(kind: "prompt", text: "Start on your task.", at: t0, from: "tm"),
        AppAPI.TranscriptItem(kind: "text", text: "On it.", at: t0.addingTimeInterval(60)),
    ]))
    #expect(t.entries.map(\.from) == ["tm", ""])
    #expect(t.entries.map(\.at) == [t0, t0.addingTimeInterval(60)])
}

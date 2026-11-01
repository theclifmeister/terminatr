import Foundation
import Testing
@testable import TerminatrKit

/// The iOS app's demo recording (ios/Terminatr/Demo/demo.json), played
/// through a Bridge: it must decode as this app API's lines, so a
/// schema change that breaks it fails here, not in App Review.
func demoBridge() async throws -> Bridge {
    let url = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().appendingPathComponent("ios/Terminatr/Demo/demo.json")
    let b = Bridge(transport: try DemoTransport(recording: Data(contentsOf: url)))
    _ = try await b.start()
    return b
}

@Test func demoRecordingDecodes() async throws {
    let b = try await demoBridge()
    #expect(await b.hello?.appApi == AppAPI.version)
    let sessions = try await b.call(AppAPI.Methods.sessionList).sessions
    let projects = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true, lean: true)).result.projects
    let peers = try await b.call(AppAPI.Methods.peerList).peers
    #expect(SidebarTree.active(projects).map(\.slug) == ["acme-web", "billing-api"])
    #expect(SidebarTree.inactive(projects).map(\.slug) == ["docs-site"])
    // What the start screen shows: one need of every kind it draws.
    let kinds = Set(Need.of(projects: projects, sessions: sessions, peers: peers).map(\.kind))
    #expect(kinds == [.approval, .question, .review, .blocked])
    // The project screen's Designs: one being built, its build in after
    // order, and one still designing.
    #expect(Compatibility.listsDesigns(try #require(await b.hello)))
    let designs = try await b.call(AppAPI.Methods.designList, AppAPI.ProjectParams(project: "acme-web")).designs
    #expect(designs.map(\.status) == ["building", "designing"])
    #expect(designs.first?.build.map(\.id) == ["T9", "T12", "T18", "T21"])
    // Every running session has its conversation.
    for s in sessions {
        let sub = try await b.subscribe(AppAPI.Methods.transcriptWatch, AppAPI.TranscriptParams(session: s.id))
        var t = Transcript()
        t.start(sub.result.status)
        for try await e in sub.events {
            t.apply(e.name, e.data)
            break
        }
        #expect(!t.entries.isEmpty, "\(s.id)")
    }
}

@Test func demoRefusesWrites() async throws {
    let b = try await demoBridge()
    await #expect(throws: AppAPI.Error.self) {
        _ = try await b.call(AppAPI.Methods.sessionPrompt, AppAPI.PromptParams(id: "s-101", text: "hi"))
    }
}

/// What the phone's project pages read of the demo (T798): acme-web's
/// overview, board with its done tasks and each task whole, memory and
/// library, as the Mac's pages gate and read them; another project's
/// lists come empty, not refused.
@Test func demoProjectPages() async throws {
    let b = try await demoBridge()
    let hello = try #require(await b.hello)
    #expect(ProjectPage.pages(hello) == ProjectPage.allCases)
    #expect(Compatibility.listsDone(hello) && Compatibility.getsTasks(hello))
    let projects = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true, lean: true)).result.projects
    let web = try #require(projects.first { $0.slug == "acme-web" })
    let overview = ProjectOverview(web, coordinator: nil, needs: 1)
    #expect(overview.repos.map(\.note) == ["", "behind origin by 2"])
    #expect(overview.tasks == "1 needs you · 3 in motion · 2 on deck · 4 done")

    #expect(Dashboard.onDeck(web).count == 2 && Dashboard.backlog(web).count == 3)
    let done = try await b.call(AppAPI.Methods.taskDone, AppAPI.ProjectParams(project: "acme-web")).tasks
    #expect(done.count == Dashboard.doneCount(web))
    #expect(Dashboard.filtered(done, by: "tokens").map(\.ref) == ["T8", "T11"])
    for t in web.board + done {
        let full = try await b.call(AppAPI.Methods.taskGet, AppAPI.TaskGetParams(project: "acme-web", task: t.ref))
        let d = try #require(TaskDetail.of(t.ref, in: web, fetched: full))
        #expect(!d.partial && !d.notes.isEmpty, "\(t.ref)")
    }

    let memory = try await b.call(AppAPI.Methods.memoryList, AppAPI.ProjectParams(project: "acme-web")).files
    #expect(MemoryPage.first(memory) == "CONTEXT.md")
    #expect(MemoryPage.project(memory).count == 2 && MemoryPage.notes(memory).count == 3)
    for f in memory {
        let m = try await b.call(AppAPI.Methods.memoryRead, AppAPI.MemoryReadParams(project: "acme-web", path: f.path))
        #expect(m.path == f.path && m.text.utf8.count == f.bytes)
    }

    let library = try await b.call(AppAPI.Methods.libraryList, AppAPI.ProjectParams(project: "acme-web")).files
    #expect(LibraryPage.groups(library).map(\.id) == ["t-0103", "t-0105", "@billing-api", "t-0098"])
    for f in library {
        let read = await LibraryPage.read({ p in try await b.call(AppAPI.Methods.libraryRead, p) }, project: "acme-web", file: f)
        guard case .data(let d, _) = read else {
            Issue.record("\(f.name): \(read)")
            continue
        }
        #expect(d.count == f.size)
    }

    #expect(try await b.call(AppAPI.Methods.memoryList, AppAPI.ProjectParams(project: "billing-api")).files.isEmpty)
    #expect(try await b.call(AppAPI.Methods.libraryList, AppAPI.ProjectParams(project: "docs-site")).files.isEmpty)
}

/// A recording's answers go by the request's params: the first that
/// fits, else calls', else refused.
@Test func demoAnswersByParams() async throws {
    let recording = """
    {"hello": {"app_api": \(AppAPI.version), "tm": "dev", "build": "demo", "level": "", "methods": [], "capabilities": []},
     "calls": {"memory.read": {"path": "", "text": "fallback"}},
     "answers": {"memory.read": [{"params": {"project": "a", "path": "CONTEXT.md"}, "result": {"path": "CONTEXT.md", "text": "a's"}},
                                 {"params": {"project": "b"}, "result": {"path": "", "text": "b's"}}]}}
    """
    let b = Bridge(transport: try DemoTransport(recording: Data(recording.utf8)))
    _ = try await b.start()
    func read(_ project: String, _ path: String) async throws -> String {
        try await b.call(AppAPI.Methods.memoryRead, AppAPI.MemoryReadParams(project: project, path: path)).text
    }
    #expect(try await read("a", "CONTEXT.md") == "a's")
    #expect(try await read("b", "MEMORY.md") == "b's")
    #expect(try await read("a", "MEMORY.md") == "fallback")
    await #expect(throws: AppAPI.Error.self) {
        _ = try await b.call(AppAPI.Methods.libraryList, AppAPI.ProjectParams(project: "a"))
    }
}

/// The demo shows the phone's write-level controls (T821): its hello
/// lists the write methods, New Task's templates come recorded, and
/// each write is refused as the demo's, not sent.
@Test func demoOffersWrites() async throws {
    let b = try await demoBridge()
    let hello = try #require(await b.hello)
    #expect(hello.level == "write" && !hello.sign)
    for m in [AppAPI.Methods.sessionPrompt.name, AppAPI.Methods.sessionKeys.name, AppAPI.Methods.taskAdd.name,
              AppAPI.Methods.taskDelegate.name, AppAPI.Methods.projectOpen.name] {
        #expect(hello.methods.contains(m), "\(m)")
    }
    #expect(Compatibility.offersTemplates(hello))
    let ts = try await b.call(AppAPI.Methods.taskTemplates, AppAPI.ProjectParams(project: "acme-web")).templates
    #expect(ts.map(\.name) == ["bug", "spike", "chore"])
    #expect(NewTaskForm.title("Login loops", with: ts.first) == "Fix: Login loops")
    do {
        _ = try await b.call(AppAPI.Methods.taskDelegate, AppAPI.TaskParams(project: "acme-web", task: "T17"))
        Issue.record("the demo delegated")
    } catch let e as AppAPI.Error {
        #expect(e.code == DemoTransport.refused)
    }
}

/// The demo's composer takes files, and its conversation's image reads
/// whole through FileStore (T860).
@Test func demoFiles() async throws {
    let b = try await demoBridge()
    let h = try #require(await b.hello)
    #expect(Compatibility.takesFiles(h) && Compatibility.sendsFiles(h))
    let store = FileStore { p in try await b.call(AppAPI.Methods.sessionFile, p) }
    guard case .data(let d, _) = await store.data("s-104", "4.e3.0.0") else {
        Issue.record("no bytes")
        return
    }
    #expect(d.starts(with: [0x89, 0x50, 0x4E, 0x47]))
    let up = try await ComposerFiles.upload(Data("hi".utf8), name: "a.md", session: "s-104",
                                            call: { try await b.call(AppAPI.Methods.sessionUpload, $0) })
    #expect(!up.ref.isEmpty)
}

/// The demo's search (T924): acme-web's hits, each with its text and
/// its timeline, through the search model the apps share.
@MainActor @Test func demoSearch() async throws {
    let b = try await demoBridge()
    let hello = await b.hello
    #expect(Compatibility.searches(hello))
    let m = SearchModel(project: "acme-web", calls: Search.Calls(b))
    m.words = "checkout"
    await m.search()
    let hits = try #require(m.result?.hits)
    #expect(hits.count == 7 && m.failure == nil)
    for h in hits {
        await m.show(h)
        let shown = m.text?.hit.id
        #expect(shown == h.id && m.timeline?.at.id == h.id, "\(h.id)")
    }
}

/// The demo's terminal (T911): T12's session streams its pane, a repaint
/// at the pane's size, for the App Store's screenshot and App Review.
@Test func demoPane() async throws {
    let b = try await demoBridge()
    #expect(try #require(await b.hello).server?.caps.contains("pane-vt") == true)
    let sub = try await b.subscribe(AppAPI.Methods.paneVt, AppAPI.PaneParams(session: "s-101"))
    #expect(sub.result.session.cols == 72)
    for try await e in sub.events {
        #expect(e.name == "pane.repaint")
        #expect(e.data.cols == 72 && e.data.rows == 34)
        #expect(String(decoding: e.data.data, as: UTF8.self).contains("Do you want to proceed?"))
        break
    }
}

/// The recording's times move to now (T911), so its ages read the same on
/// any day: a time by the span to the second, a bare date by whole days;
/// a time inside text stays.
@Test func demoTimesMoveToNow() async throws {
    let rec = Data("""
    {"now": "2026-10-27T10:00:00Z", "hello": {"app_api": 1},
     "calls": {"x": {"at": "2026-10-27T09:41:00Z", "day": "2026-10-26", "list": ["2026-10-20T08:00:00Z"],
                     "text": "seen 2026-10-27T09:41:00Z", "n": 3}}}
    """.utf8)
    let now = try Date("2026-11-03T22:30:00Z", strategy: .iso8601)
    let t = try DemoTransport(recording: rec, now: now)
    var lines = t.lines.makeAsyncIterator()
    _ = try await lines.next()
    try t.send(Data(#"{"id": 1, "method": "x"}"#.utf8))
    let line = try #require(try await lines.next())
    let r = try #require(try JSONSerialization.jsonObject(with: line) as? [String: Any])["result"] as? [String: Any]
    #expect(r?["at"] as? String == "2026-11-03T22:11:00Z")
    #expect(r?["day"] as? String == "2026-11-03")
    #expect(r?["list"] as? [String] == ["2026-10-27T20:30:00Z"])
    #expect(r?["text"] as? String == "seen 2026-10-27T09:41:00Z")
    #expect(r?["n"] as? Int == 3)
}

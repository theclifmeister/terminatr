import Foundation
import Testing
@testable import TerminatrKit

private func pages() -> AppAPI.PeerPages {
    AppAPI.PeerPages(project: "al0ft", goal: " the loft \n", body: "# Rules\nKeep it small.\n", repos: 2, tasks: [
        AppAPI.Task(id: 1, ref: "T1", title: "Deck", status: "ready"),
        AppAPI.Task(id: 2, ref: "T2", title: "Later", status: "open"),
        AppAPI.Task(id: 3, ref: "T3", title: "Shipped", status: "done"),
        AppAPI.Task(id: 4, ref: "T4", title: "Going", status: "started",
                    steps: [AppAPI.Step(n: 1, text: "a", done: true), AppAPI.Step(n: 2, text: "b", done: false)]),
        AppAPI.Task(id: 5, ref: "T5", title: "Deck too", status: "ready"),
    ])
}

@Test func peerPagesNeedAPI35AndTheCap() {
    let caps = AppAPI.Server(version: "0.69.0", protocol: 30, caps: ["peer-pages"])
    var h = AppAPI.Hello(appApi: 35, tm: "0.69.0", build: "", methods: ["peer.pages-read", "peer.library-list", "peer.library-read"], server: caps)
    #expect(Compatibility.readsPeerPages(h))
    h.appApi = 34
    #expect(!Compatibility.readsPeerPages(h))
    h.appApi = 35
    h.server = AppAPI.Server(version: "0.68.0", protocol: 30)
    #expect(!Compatibility.readsPeerPages(h))
}

@Test func peerPagesNotSharedStates() {
    for code in ["pages-off", "no-pages", "not-shared", "unknown-method"] {
        let s: PeerPages.State<AppAPI.PeerPages> = PeerPages.state(AppAPI.Error(code: code, message: "x"))
        #expect(s == .notShared, "\(code)")
    }
    let s: PeerPages.State<AppAPI.PeerPages> = PeerPages.state(AppAPI.Error(code: "refused", message: "work can't be reached now"))
    #expect(s == .failed("work can't be reached now"))
    // Memory never, designs not yet; the rest wait for a tm that reads them.
    #expect(PeerPages.initial(.memory, reads: true) == PeerPages.State<Int>.notShared)
    #expect(PeerPages.initial(.designs, reads: true) == PeerPages.State<Int>.notShared)
    #expect(PeerPages.initial(.board, reads: true) == PeerPages.State<Int>.loading)
    #expect(PeerPages.initial(.library, reads: false) == PeerPages.State<Int>.notShared)
    #expect(PeerPages.notShared(by: "work") == "Not shared by work")
    #expect(PeerPages.notSharedDetail(.memory, server: "work").contains("never"))
}

@Test func peerPagesBoardAndTiles() {
    let p = pages()
    #expect(PeerPages.onDeck(p).map(\.ref) == ["T1", "T5"])
    #expect(PeerPages.backlog(p).map(\.ref) == ["T2"])
    #expect(PeerPages.done(p).map(\.ref) == ["T3"])
    #expect(PeerPages.moving(p).map(\.ref) == ["T4"])
    #expect(PeerPages.stats(p).map(\.0) == [2, 1, 1])
    #expect(PeerPages.stats(p).map(\.1) == ["on deck", "backlog", "done"])
    #expect(PeerPages.stats(nil).map(\.0) == [nil, nil, nil])
}

@Test func peerOverviewKeepsPathsThere() {
    let now = Date(timeIntervalSince1970: 0)
    let project = AppAPI.PeerProject(project: "al0ft", needsYou: 1, threads: 1, ready: 2, sessions: [
        AppAPI.PeerSession(id: "work/s-1", role: "coordinator", agent: "claude", project: "al0ft", created: now, state: "working"),
        AppAPI.PeerSession(id: "work/s-2", role: "thread", agent: "codex", project: "al0ft", thread: "t-0004", created: now),
    ], pages: true)
    let o = ProjectOverview(pages(), project: project)
    #expect(o.goal == "the loft")
    #expect(o.repos.isEmpty && o.repoCount == 2)
    #expect(o.instructions == "# Rules\nKeep it small.")
    #expect(o.coordinator == "claude · working")
    #expect(o.agents == [ProjectOverview.Agent(agent: "codex", threads: ["t-0004"])])
    #expect(o.tasks == "1 needs you · 1 in motion · 2 on deck · 1 done")
}

/// The demo's paired server shares al0ft's pages, not cowork's: an
/// answer can be an error (DemoTransport), which reads as not shared.
@Test func demoPeerPages() async throws {
    let b = try await demoBridge()
    #expect(try await b.call(AppAPI.Methods.peerList).peers.first?.projects.map(\.pages) == [true, false])
    let p = try await b.call(AppAPI.Methods.peerPagesRead, AppAPI.PeerPagesParams(alias: "work", project: "al0ft"))
    #expect(PeerPages.stats(p).map(\.0) == [2, 3, 3])
    do {
        _ = try await b.call(AppAPI.Methods.peerPagesRead, AppAPI.PeerPagesParams(alias: "work", project: "cowork"))
        Issue.record("cowork's pages answered")
    } catch {
        #expect(PeerPages.state(error) == PeerPages.State<AppAPI.PeerPages>.notShared)
    }
    let files = try await b.call(AppAPI.Methods.peerLibraryList, AppAPI.PeerPagesParams(alias: "work", project: "al0ft")).files
    let read = await PeerPages.read({ p in try await b.call(AppAPI.Methods.peerLibraryRead, p) }, alias: "work", project: "al0ft", file: files[0])
    guard case .data(let d, _) = read else {
        Issue.record("the library file didn't read: \(read)")
        return
    }
    #expect(String(decoding: d, as: UTF8.self).hasPrefix("# Photo sizes"))
}

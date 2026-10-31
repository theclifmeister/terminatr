import Foundation
import Testing
@testable import TerminatrKit

private func pages() -> AppAPI.PeerPages {
    AppAPI.PeerPages(project: "acme-api", goal: " the shop's API \n", body: "# Rules\nKeep it small.\n", repos: 2, tasks: [
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
    let s: PeerPages.State<AppAPI.PeerPages> = PeerPages.state(AppAPI.Error(code: "refused", message: "studio can't be reached now"))
    #expect(s == .failed("studio can't be reached now"))
    // Memory never; the rest wait for a tm that reads them.
    #expect(PeerPages.initial(.memory, reads: true) == PeerPages.State<Int>.notShared)
    #expect(PeerPages.initial(.designs, reads: true) == PeerPages.State<Int>.loading)
    #expect(PeerPages.initial(.designs, reads: false) == PeerPages.State<Int>.notShared)
    #expect(PeerPages.initial(.board, reads: true) == PeerPages.State<Int>.loading)
    #expect(PeerPages.initial(.library, reads: false) == PeerPages.State<Int>.notShared)
    #expect(PeerPages.notShared(by: "studio") == "Not shared by studio")
    #expect(PeerPages.notSharedDetail(.memory, server: "studio").contains("never"))
}

@Test func peerDesignsNeedAPI36AndTheCap() {
    let caps = AppAPI.Server(version: "0.70.0", protocol: 30, caps: ["peer-pages", "peer-designs"])
    var h = AppAPI.Hello(appApi: 36, tm: "0.70.0", build: "", methods: ["peer.pages-read", "peer.library-read", "peer.design-list"], server: caps)
    #expect(Compatibility.readsPeerDesigns(h))
    h.appApi = 35
    #expect(!Compatibility.readsPeerDesigns(h))
    h.appApi = 36
    h.server = AppAPI.Server(version: "0.69.0", protocol: 30, caps: ["peer-pages"])
    #expect(!Compatibility.readsPeerDesigns(h) && Compatibility.readsPeerPages(h))
}

@Test func peerDesignsKeepTheirPageThere() {
    let d = AppAPI.Design(name: "rate-limits", title: "Rate limits", status: "building", tasksDone: 1, pageStays: true)
    #expect(Designs.page(d) == nil)
    #expect(PeerPages.pageStays(d, server: "studio") == "Its page stays on studio")
    #expect(PeerPages.pageStays(AppAPI.Design(name: "x", title: "x", status: "planned", tasksDone: 0), server: "studio") == nil)
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
    let project = AppAPI.PeerProject(project: "acme-api", needsYou: 1, threads: 1, ready: 2, sessions: [
        AppAPI.PeerSession(id: "studio/s-1", role: "coordinator", agent: "claude", project: "acme-api", created: now, state: "working"),
        AppAPI.PeerSession(id: "studio/s-2", role: "thread", agent: "codex", project: "acme-api", thread: "t-0004", created: now),
    ], pages: true)
    let o = ProjectOverview(pages(), project: project)
    #expect(o.goal == "the shop's API")
    #expect(o.repos.isEmpty && o.repoCount == 2)
    #expect(o.instructions == "# Rules\nKeep it small.")
    #expect(o.coordinator == "claude · working")
    #expect(o.agents == [ProjectOverview.Agent(agent: "codex", threads: ["t-0004"])])
    #expect(o.tasks == "1 needs you · 1 in motion · 2 on deck · 1 done")
}

/// The demo's paired server shares acme-api's pages, not acme-docs's: an
/// answer can be an error (DemoTransport), which reads as not shared.
@Test func demoPeerPages() async throws {
    let b = try await demoBridge()
    #expect(try await b.call(AppAPI.Methods.peerList).peers.first?.projects.map(\.pages) == [true, false])
    let p = try await b.call(AppAPI.Methods.peerPagesRead, AppAPI.PeerPagesParams(alias: "studio", project: "acme-api"))
    #expect(PeerPages.stats(p).map(\.0) == [2, 3, 3])
    do {
        _ = try await b.call(AppAPI.Methods.peerPagesRead, AppAPI.PeerPagesParams(alias: "studio", project: "acme-docs"))
        Issue.record("acme-docs's pages answered")
    } catch {
        #expect(PeerPages.state(error) == PeerPages.State<AppAPI.PeerPages>.notShared)
    }
    let files = try await b.call(AppAPI.Methods.peerLibraryList, AppAPI.PeerPagesParams(alias: "studio", project: "acme-api")).files
    let read = await PeerPages.read({ p in try await b.call(AppAPI.Methods.peerLibraryRead, p) }, alias: "studio", project: "acme-api", file: files[0])
    guard case .data(let d, _) = read else {
        Issue.record("the library file didn't read: \(read)")
        return
    }
    #expect(String(decoding: d, as: UTF8.self).hasPrefix("# Rate limit load test"))
}

/// The demo's paired server shares acme-api's designs, their pages kept
/// there and no open questions; acme-docs's read as not shared.
@Test func demoPeerDesigns() async throws {
    let b = try await demoBridge()
    #expect(Compatibility.readsPeerDesigns(try #require(await b.hello)))
    let ds = try await b.call(AppAPI.Methods.peerDesignList, AppAPI.PeerPagesParams(alias: "studio", project: "acme-api")).designs
    #expect(Designs.ordered(ds).map(\.name) == ["rate-limits", "webhooks-v2"])
    #expect(ds.allSatisfy { $0.artifact.isEmpty && $0.questions.isEmpty })
    #expect(PeerPages.pageStays(ds[0], server: "studio") == "Its page stays on studio")
    #expect(Designs.built(ds[0]) == "1/3 built" && ds[0].working)
    do {
        _ = try await b.call(AppAPI.Methods.peerDesignList, AppAPI.PeerPagesParams(alias: "studio", project: "acme-docs"))
        Issue.record("acme-docs's designs answered")
    } catch {
        #expect(PeerPages.state(error) == PeerPages.State<AppAPI.PeerPages>.notShared)
    }
}

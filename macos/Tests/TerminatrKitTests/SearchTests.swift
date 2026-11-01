import Foundation
import Testing
@testable import TerminatrKit

@Suite struct SearchTests {
    static let at = Date(timeIntervalSince1970: 1_793_534_400) // 2026-11-01 12:00 UTC
    static let note = AppAPI.SearchHit(id: "note:relay#decided", kind: "notes", title: "Relay · Decided", time: at,
                                       path: "memory/relay.md", terms: ["wombat"])
    static let report = AppAPI.SearchHit(id: "t-0833/report.3#next", kind: "threads", title: "Report · Next", time: at,
                                         task: "T914", thread: "t-0833", path: "threads/t-0833/report.md", more: 3)
    static let task = AppAPI.SearchHit(id: "T914", kind: "tasks", title: "tm search", time: at, path: "TASKS.md")

    @Test func targets() {
        #expect(Search.targets(Self.note) == [.note(path: "memory/relay.md")])
        #expect(Search.targets(Self.report) == [.task("T914"), .thread("t-0833")])
        #expect(Search.targets(Self.task) == [.task("T914")])
        let archived = AppAPI.SearchHit(id: "j:1", kind: "journal", title: "x", time: Self.at, path: "journal/2026.md.gz", member: "a.md")
        #expect(Search.targets(archived).isEmpty)
        #expect(!Search.readsAsMemory("memory/sub/x.md") && Search.readsAsMemory("CONTEXT.md") && !Search.readsAsMemory("PROJECT.md"))
        #expect(Search.isTaskRef("T12") && !Search.isTaskRef("T") && !Search.isTaskRef("t-0001"))
    }

    @Test func words() {
        #expect(Search.day(Self.at, in: TimeZone(identifier: "UTC")!) == "2026-11-01")
        #expect(Search.facts(Self.report) == "T914 · t-0833 · +3 journal lines")
        #expect(Search.facts(Self.task) == "")
        #expect(Search.summary(AppAPI.SearchResult(project: "p", hits: [Self.note], total: 1)) == "1 hit")
        #expect(Search.summary(AppAPI.SearchResult(project: "p", hits: [Self.note, Self.task], total: 9)) == "2 hits of 9")
        #expect(Search.summary(AppAPI.SearchResult(project: "p", total: 0)) == "No hits")
        #expect(Search.kind("inbox").label == "Inbox" && Search.kind("odd").label == "Odd")
        let p = Search.params(project: "web", words: "  wombat  ", kind: "notes")
        #expect(p.words == "wombat" && p.in == ["notes"] && p.limit == 50)
        #expect(Search.params(project: "web", words: "x").in.isEmpty)
        let tl = AppAPI.SearchTimeline(project: "p", at: Self.report, before: [Self.task], after: [Self.note])
        let rows = Search.rows(tl)
        #expect(rows.map(\.id) == ["T914", "t-0833/report.3#next", "note:relay#decided"] && rows.map(\.at) == [false, true, false])
    }

    @MainActor @Test func model() async {
        let calls = Search.Calls(
            query: { p in
                if p.words == "boom" { throw AppAPI.Error(code: "internal", message: "search: broke") }
                return AppAPI.SearchResult(project: p.project, hits: [Self.note, Self.report], total: 2, terms: [p.words])
            },
            timeline: { p in AppAPI.SearchTimeline(project: p.project, at: Self.report, before: [Self.task]) },
            show: { p in
                AppAPI.SearchShown(project: p.project, items: p.ids.map {
                    $0 == Self.report.id ? AppAPI.SearchText(hit: Self.report, text: "## Next\n- ship", bytes: 15)
                        : AppAPI.SearchText(hit: AppAPI.SearchHit(id: $0, kind: "", title: "", time: Self.at, path: ""), error: "no such chunk")
                })
            })
        let m = SearchModel(project: "web", calls: calls)
        #expect(!m.canSearch)
        await m.search()
        #expect(m.result == nil)
        m.words = "wombat"
        await m.search()
        let hits = m.result?.hits.map(\.id)
        #expect(hits == [Self.note.id, Self.report.id] && !m.searching && m.failure == nil)
        await m.show(Self.report)
        let text = m.text?.text
        #expect(m.shown == Self.report && text == "## Next\n- ship" && m.timeline?.before.count == 1 && m.shownFailure == nil)
        await m.show(Self.note)
        #expect(m.text == nil && m.shownFailure == "no such chunk" && m.timeline != nil)
        m.close()
        #expect(m.shown == nil && m.timeline == nil)
        m.words = "boom"
        await m.search()
        #expect(m.result == nil && m.failure == "search: broke")
        m.switchProject("api")
        #expect(m.project == "api" && m.failure == nil)
    }
}

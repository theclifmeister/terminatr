import Foundation
import Testing
@testable import TerminatrKit

// A project's daily numbers (app API 58): gated on the hello, so a tm or
// a server without them hides what draws them; read again only when the
// done count moves.

@Test func historyNeedsAPI58AndTheCap() {
    let caps = AppAPI.Server(version: "0.83.0", protocol: 39, caps: ["history", "peer-history"])
    var h = AppAPI.Hello(appApi: 58, tm: "0.83.0", build: "", methods: ["project.history", "peer.history"], server: caps)
    #expect(Compatibility.readsHistory(h) && Compatibility.readsPeerHistory(h))
    h.appApi = 57
    #expect(!Compatibility.readsHistory(h) && !Compatibility.readsPeerHistory(h))
    h.appApi = 58
    h.server = AppAPI.Server(version: "0.82.0", protocol: 38, caps: [])
    #expect(!Compatibility.readsHistory(h) && !Compatibility.readsPeerHistory(h))
    h.server = caps
    h.methods = []
    #expect(!Compatibility.readsHistory(h) && !Compatibility.readsPeerHistory(h))
}

@Test func anOlderPairedServersNoPagesReadsAsNotShared() {
    let state: PeerPages.State<Int> = PeerPages.state(AppAPI.Error(code: "no-pages", message: "the paired server's tm shares no history"))
    #expect(state == .notShared)
}

@Test func historyIsReadAgainOnlyWhenTheDoneCountChanges() {
    var reads = HistoryReads()
    let asked = [("web", 3), ("web", 3), ("api", 3), ("web", 4)].map { reads.needsRead($0.0, done: $0.1) }
    #expect(asked == [true, false, true, true])
    reads.forget("web")
    let again = reads.needsRead("web", done: 4)
    #expect(again)
}

@Test func historyDecodesAnEmptyProject() throws {
    let h = try JSONDecoder().decode(AppAPI.History.self, from: Data(#"{"done":0,"open":0,"dropped":0,"days":[],"timed":0}"#.utf8))
    #expect(h.days.isEmpty && h.bestDay == nil && h.typical.isEmpty && h.firstRecord.isEmpty)
}

@Test func historyDecodesTheFacts() throws {
    let h = try JSONDecoder().decode(AppAPI.History.self, from: Data(#"{"first_record":"2026-10-05","done":3,"open":1,"dropped":0,"days":[{"day":"2026-10-05","done":3}],"best_day":{"day":"2026-10-05","done":3},"timed":12,"typical_seconds":2040,"typical":"34 minutes"}"#.utf8))
    #expect(h.firstRecord == "2026-10-05" && h.bestDay?.done == 3 && h.typicalSeconds == 2040 && h.typical == "34 minutes")
}

// Which tags count as releases (app API 65).

@Test func releaseTagsNeedAPI65AndTheCap() {
    let caps = AppAPI.Server(version: "0.84.0", protocol: 39, caps: ["release-tags"])
    var h = AppAPI.Hello(appApi: 65, tm: "0.84.0", build: "", methods: ["release.tags", "release.tags.set"], server: caps)
    #expect(Compatibility.setsReleaseTags(h))
    h.appApi = 64
    #expect(!Compatibility.setsReleaseTags(h))
    h.appApi = 65
    h.server = AppAPI.Server(version: "0.83.0", protocol: 39, caps: [])
    #expect(!Compatibility.setsReleaseTags(h))
    h.server = caps
    h.methods = []
    #expect(!Compatibility.setsReleaseTags(h))
}

@Test func releasesDecodeAndAreNeverSummed() throws {
    let json = #"""
    {"done":1,"open":0,"dropped":0,"days":[],"timed":0,"releases":[
      {"repo":"terminatr","count":128,"releases":[{"tag":"v0.82.1","version":"0.82.1","at":"2026-11-06T05:03:00Z"}]},
      {"repo":"site","count":6,"releases":[]},
      {"repo":"mono","count":5,"parts":[{"part":"api","count":3},{"part":"web","count":2}],"releases":[{"tag":"api/v1.2","part":"api","version":"1.2","at":"2026-11-06T05:03:00Z"}]}
    ]}
    """#
    let h = try JSONDecoder().decode(AppAPI.History.self, from: Data(json.utf8))
    #expect(ReleaseCounts.words(h.releases) == "terminatr 128 · site 6 · mono api 3 · mono web 2")
    #expect(h.releases[2].releases[0].part == "api")
    #expect(h.releases[0].parts.isEmpty && h.releases[0].releases[0].part.isEmpty)
    #expect(ReleaseCounts.words([]).isEmpty)
}

@Test func anOlderTmsHistoryHasNoReleases() throws {
    let h = try JSONDecoder().decode(AppAPI.History.self, from: Data(#"{"done":0,"open":0,"dropped":0,"days":[],"timed":0}"#.utf8))
    #expect(h.releases.isEmpty)
}

@Test func releaseTagsFormReadsAndWritesEachMode() {
    func tags(_ mode: String, _ patterns: [String] = [], own: Bool = true) -> AppAPI.ReleaseTags {
        AppAPI.ReleaseTags(project: "web", mode: mode, patterns: patterns, own: own, allProjects: "version tags")
    }
    #expect(ReleaseTagsForm(tags("rule", own: false)).mode == .follow)
    #expect(ReleaseTagsForm(tags("rule")).mode == .rule)
    #expect(ReleaseTagsForm(tags("none")).mode == .none)
    var f = ReleaseTagsForm(tags("patterns", ["prod-*", "v*"]))
    #expect(f.mode == .patterns && f.text == "prod-* v*" && f.canSave)
    f.text = " prod-*, deploy-*\n"
    let p = f.params(project: "web")
    #expect(p.mode == "patterns" && p.patterns == ["prod-*", "deploy-*"])
    f.text = ""
    #expect(!f.canSave)
    f.text = "none"
    #expect(!f.canSave)
    f.mode = .none
    let none = f.params(project: "web")
    #expect(f.canSave && none.mode == "none" && none.patterns.isEmpty)
    #expect(f.title(.follow) == "As all projects (version tags)")
}

// The Pulse (app API 69): tm decides the bars, the Kit says them.

private func bar(_ from: String, done: Int, released: Bool = false) -> AppAPI.PulseBar {
    AppAPI.PulseBar(from: from, to: from, label: from, done: done, released: released, tip: "\(from): \(done) done")
}

@Test func noPulseNoBand() throws {
    #expect(PulseBand(nil) == nil)
    // 9 done tasks, or an older tm: the field isn't there.
    let old = #"{"done":9,"open":0,"dropped":0,"days":[],"timed":0}"#
    let h = try JSONDecoder().decode(AppAPI.History.self, from: Data(old.utf8))
    #expect(h.pulse == nil && PulseBand(h) == nil)
    var empty = h
    empty.pulse = AppAPI.HistoryPulse(unit: "day", span: "7 days")
    #expect(PulseBand(empty) == nil)
}

@Test func pulseFactsLeaveOutWhatIsNotThere() throws {
    var h = AppAPI.History(done: 957, open: 0, dropped: 0, timed: 20, typical: "34 minutes")
    h.pulse = AppAPI.HistoryPulse(unit: "day", span: "33 days", releases: "128 releases", bars: [bar("a", done: 1, released: true)])
    var b = try #require(PulseBand(h))
    #expect(b.facts == "33 days · 128 releases" && b.hasDots && b.done == 957)
    // A plain folder or no tags: bars only, no empty dot row, no words.
    h.pulse = AppAPI.HistoryPulse(unit: "day", span: "33 days", bars: [bar("a", done: 1)])
    b = try #require(PulseBand(h))
    #expect(b.facts == "33 days" && !b.hasDots)
    // Slow: weeks, with the typical time, as the TUI says it.
    h.typical = "2 days"
    h.pulse = AppAPI.HistoryPulse(unit: "week", span: "5 months", bars: [bar("a", done: 1)])
    b = try #require(PulseBand(h))
    #expect(b.facts == "5 months, by week · typical 2 days")
    // Several repos: tm's words, each repo's, never a total.
    h.typical = ""
    h.pulse = AppAPI.HistoryPulse(unit: "day", span: "33 days", releases: "terminatr 128 · site 6 releases", bars: [bar("a", done: 1, released: true)])
    #expect(try #require(PulseBand(h)).facts == "33 days · terminatr 128 · site 6 releases")
}

@Test func pulseBarsFilterAndScale() throws {
    var h = AppAPI.History(done: 11, open: 0, dropped: 0, timed: 0)
    h.pulse = AppAPI.HistoryPulse(unit: "day", span: "7 days", bars: [bar("a", done: 1), bar("b", done: 0, released: true), bar("c", done: 10)])
    let b = try #require(PulseBand(h))
    #expect(b.bar(from: "a")?.done == 1)
    // No tasks to list on a day with none, and a bar gone ends the filter.
    #expect(b.bar(from: "b") == nil && b.bar(from: "z") == nil && b.bar(from: nil) == nil)
    #expect(b.height(b.bars[2], of: 36) == 36 && b.height(b.bars[1], of: 36) == 0)
    #expect(b.height(b.bars[0], of: 36) == 3.6)
    var small = h
    small.pulse?.bars[2].done = 1000
    #expect(try #require(PulseBand(small)).height(small.pulse!.bars[0], of: 36) == 2)
}

// The History view (app API 76): tm's points and words, with no part for what
// a project hasn't.

private func curve(_ totals: [Int], ticks: [AppAPI.CurveTicks] = [], unit: String = "day") -> AppAPI.HistoryCurve {
    let points = totals.enumerated().map { i, t in
        AppAPI.CurvePoint(from: "2026-10-\(10 + i)", to: "2026-10-\(10 + i)", label: "\(10 + i) Oct", done: i == 0 ? t : t - totals[i - 1], total: t, tip: "tip \(i)")
    }
    return AppAPI.HistoryCurve(unit: unit, span: "\(totals.count) days", range: "10 Oct – 12 Oct", points: points,
                               best: AppAPI.CurveBest(done: 2, word: "best day", when: "12 Oct"), ticks: ticks)
}

private func history(_ c: AppAPI.HistoryCurve?, releases: [AppAPI.HistoryRepo] = [], chapters: [AppAPI.HistoryChapter] = [], typical: String = "") -> AppAPI.History {
    AppAPI.History(firstRecord: "2026-10-10", done: c?.points.last?.total ?? 0, open: 1, dropped: 0, days: [], timed: 0, typical: typical, releases: releases, curve: c, chapters: chapters, chapterDone: 1)
}

@Test func historyPageNeedsACurve() {
    #expect(HistoryPage(nil) == nil)
    #expect(HistoryPage(history(nil)) == nil)
    #expect(HistoryPage(history(curve([3]))) != nil)
}

@Test func aPlainFolderHasNoReleaseFactsOrRows() throws {
    let page = try #require(HistoryPage(history(curve([1, 3]))))
    #expect(page.facts.map(\.word) == ["done", "of work", "best day, 12 Oct"])
    #expect(page.tickRows(repo: "").isEmpty)
    #expect(page.chapters.isEmpty)
    #expect(page.flags(width: 800).isEmpty)
}

@Test func severalReposHaveARowAndACountEachNeverAddedUp() throws {
    let ticks = [AppAPI.CurveTicks(repo: "app", points: [0]), AppAPI.CurveTicks(repo: "site", points: [1])]
    let repos = [AppAPI.HistoryRepo(repo: "app", count: 128, releases: []), AppAPI.HistoryRepo(repo: "site", count: 1, releases: [])]
    let page = try #require(HistoryPage(history(curve([1, 3], ticks: ticks), releases: repos)))
    #expect(page.facts.contains(.init(value: "128", word: "app releases")))
    #expect(page.facts.contains(.init(value: "1", word: "site release")))
    #expect(!page.filtersRepos)
    #expect(page.tickRows(repo: "").map(\.repo) == ["app", "site"])
}

@Test func beyondThreeReposAFilterPicksTheRow() throws {
    let ticks = ["a", "b", "c", "d"].enumerated().map { AppAPI.CurveTicks(repo: $1, points: [$0]) }
    let page = try #require(HistoryPage(history(curve([1, 2, 3, 4], ticks: ticks))))
    #expect(page.filtersRepos)
    #expect(page.tickRows(repo: "") == [.init(repo: "", points: [0, 1, 2, 3])])
    #expect(page.tickRows(repo: "c") == [.init(repo: "c", points: [2])])
}

@Test func flagsKeepTheBiggestAndNeverTouch() throws {
    func chapter(_ title: String, _ done: Int, flag: Int) -> AppAPI.HistoryChapter {
        AppAPI.HistoryChapter(name: title, title: title, done: done, total: done, flag: flag)
    }
    let page = try #require(HistoryPage(history(curve(Array(1...40)), chapters: [
        chapter("Programs: a director over project coordinators", 30, flag: 30),
        chapter("Small", 1, flag: 30),
        chapter("Tiny", 1, flag: 30),
        chapter("Open", 0, flag: 0),
    ])))
    let flags = page.flags(width: 400)
    #expect(flags.count == 2, "two lines at one point, the biggest first; none for what hasn't finished")
    #expect(flags.first { !$0.high }?.name == "Programs")
    #expect(!flags.contains { $0.name == "Open" })
    #expect(page.flags(width: 400, most: 1).count == 1)
}

@Test func flagsAtDifferentHeightsStillClearEachOther() throws {
    func chapter(_ title: String, _ done: Int, flag: Int) -> AppAPI.HistoryChapter {
        AppAPI.HistoryChapter(name: title, title: title, done: done, total: done, flag: flag)
    }
    // The curve rises about 10 points between them, so their lower lines would
    // touch though they are on different heights.
    let page = try #require(HistoryPage(history(curve([10, 20, 30, 40, 100]), chapters: [
        chapter("Agent framework", 9, flag: 2), chapter("Mods with a long name", 3, flag: 3),
    ])))
    let flags = page.flags(width: 500, height: 96)
    try #require(flags.count == 2)
    func y(_ f: HistoryPage.Flag) -> Double {
        96 * (1 - Double(page.curve.points[f.point].total) / 100) - HistoryPage.rise(high: f.high)
    }
    #expect(abs(y(flags[0]) - y(flags[1])) >= 14)
}

@Test func aNameReadingRightwardNeverCrossesTheCurve() throws {
    func chapter(_ title: String, flag: Int) -> AppAPI.HistoryChapter {
        AppAPI.HistoryChapter(name: title, title: title, done: 1, total: 1, flag: flag)
    }
    // A steep curve, and a flag too near the left edge to read leftward.
    let page = try #require(HistoryPage(history(curve([1, 50, 100]), chapters: [chapter("Agent framework", flag: 1)])))
    #expect(page.flags(width: 120, height: 190).isEmpty)
    // A flat one leaves it room above.
    let flat = try #require(HistoryPage(history(curve([99, 99, 100]), chapters: [chapter("Agent framework", flag: 1)])))
    #expect(flat.flags(width: 200, height: 190).count == 1)
}

@Test func aShortCurveNamesEveryPoint() throws {
    let page = try #require(HistoryPage(history(curve([1, 2, 3]))))
    #expect(page.axis(width: 800) == [0, 1, 2])
    #expect(page.axis(width: 60).count == 2)
}

@Test func theCurveHasRoundLinesAndAnAxisThatDoesntTouch() throws {
    let page = try #require(HistoryPage(history(curve(Array(stride(from: 29, through: 957, by: 29))))))
    #expect(page.gridLines == [0, 250, 500, 750])
    #expect(page.axis(width: 800) == [0, 7, 14, 21, 28])
    #expect(page.axis(width: 150).count <= 3)
    let one = try #require(HistoryPage(history(curve([3]))))
    #expect(one.axis(width: 800) == [0])
    #expect(one.gridLines == [0, 1, 2])
}

@Test func aChaptersProgress() {
    #expect(HistoryPage.progress(AppAPI.HistoryChapter(name: "n", title: "t", done: 3, total: 4)) == 0.75)
    #expect(HistoryPage.progress(AppAPI.HistoryChapter(name: "n", title: "t", done: 0, total: 0)) == 0)
}

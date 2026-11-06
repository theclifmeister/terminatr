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

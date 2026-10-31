import Foundation
import Testing
@testable import TerminatrKit

// The phone's pairings (T852): one per server, in pairing order, and a
// server paired directly shown once, not again through another's link.

@Test func pairingsAddLastAndReplaceInPlace() {
    var ids = Pairings.adding("aa", to: [])
    ids = Pairings.adding("bb", to: ids)
    #expect(ids == ["aa", "bb"])
    // Pairing again with aa keeps it first.
    #expect(Pairings.adding("aa", to: ids) == ["aa", "bb"])
    #expect(Pairings.removing("aa", from: ids) == ["bb"])
    #expect(Pairings.removing("cc", from: ids) == ["aa", "bb"])
}

@Test func peersPairedDirectlyAreShownOnce() {
    let direct = [Pairings.Direct(id: "ABCD", name: "studio")]
    let byID = AppAPI.Peer(alias: "s", name: "workshop", id: "abcd", state: "up")
    let other = AppAPI.Peer(alias: "mini", name: "mini", id: "ef01", state: "up")
    // A tm without ids: its name, whatever its alias here.
    let byName = AppAPI.Peer(alias: "st", name: "Studio", state: "up")
    let unnamed = AppAPI.Peer(alias: "studio", state: "up")
    #expect(Pairings.isDirect(byID, direct))
    #expect(!Pairings.isDirect(other, direct))
    #expect(Pairings.isDirect(byName, direct))
    #expect(!Pairings.isDirect(unnamed, direct))
    // An id decides: the same name with another id is another server.
    #expect(!Pairings.isDirect(AppAPI.Peer(alias: "x", name: "studio", id: "9999", state: "up"), direct))
    #expect(Pairings.shown([byID, other, byName, unnamed], direct).map(\.alias) == ["mini", "studio"])
    #expect(Pairings.shown([byID], []).map(\.alias) == ["s"])
}

@Test func needsMergeAcrossServers() {
    func need(_ id: String, _ kind: Need.Kind) -> Need {
        Need(id: id, kind: kind, project: "p", what: id, detail: "")
    }
    let merged = Need.merge([
        ("studio", [need("q1", .question), need("r1", .review)]),
        ("mini", [need("a2", .approval), need("r2", .review)]),
    ])
    #expect(merged.map { "\($0.server) \($0.need.id)" } == ["studio q1", "mini a2", "studio r1", "mini r2"])
}

@Test func linkLogsArePerPairing() throws {
    let d = try #require(UserDefaults(suiteName: "PairingsTests-\(UUID())"))
    var l = LinkLog()
    l.dropped(at: Date(timeIntervalSince1970: 1000), .ping, detail: "")
    l.save(d)
    LinkLog.move(to: "aa", d)
    #expect(d.data(forKey: LinkLog.key) == nil)
    #expect(LinkLog.load(pairing: "aa", d).drops.count == 1)
    #expect(LinkLog.load(pairing: "bb", d).drops.isEmpty)
    LinkLog.forget(pairing: "aa", d)
    #expect(LinkLog.load(pairing: "aa", d).drops.isEmpty)
}

@Test func liveWorkMergesAcrossServers() throws {
    var a = AppAPI.Session(id: "s-1", role: "thread", project: "p", cols: 80, rows: 24, created: Date(timeIntervalSince1970: 0), clients: 0, state: "working")
    var b = a
    b.id = "s-2"
    a.turnSince = Date(timeIntervalSince1970: 100)
    b.turnSince = Date(timeIntervalSince1970: 50)
    let one = try #require(LiveWork.of([a, a]))
    let two = try #require(LiveWork.of([b]))
    let m = try #require(LiveWork.merge([("studio", one), ("mini", two)]))
    #expect(m.server == "mini")
    #expect(m.work.count == 3)
    #expect(m.work.lead.id == "s-2")
    #expect(LiveWork.merge([(String, LiveWork)]()) == nil)
}

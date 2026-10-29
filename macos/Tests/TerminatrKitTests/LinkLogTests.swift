import Foundation
import Testing
@testable import TerminatrKit

// The phone's link drops and redials (T859).

private let dropAt = Date(timeIntervalSince1970: 1_000_000)

@Test func dropIsLoggedUntilTheLinkIsBack() {
    var l = LinkLog()
    #expect(!l.isDown)
    l.dropped(at: dropAt, .ping, detail: "the relay didn't answer a ping")
    #expect(l.isDown)
    // The redials failing while it is down are no new drops.
    l.dropped(at: dropAt.addingTimeInterval(1), .error, detail: "connection refused")
    l.dropped(at: dropAt.addingTimeInterval(3), .asleep)
    #expect(l.drops.count == 1)
    #expect(l.drops[0].cause == .ping)
    l.up(at: dropAt.addingTimeInterval(4))
    #expect(!l.isDown)
    #expect(l.drops[0].down == 4)
    // Up again while up: nothing changes.
    l.up(at: dropAt.addingTimeInterval(9))
    #expect(l.drops[0].down == 4)
}

@Test func logKeepsTheNewestAndSurvivesALaunch() throws {
    var l = LinkLog()
    for i in 0..<(LinkLog.max + 5) {
        l.dropped(at: dropAt.addingTimeInterval(Double(i) * 60), .relay, detail: "peer-gone")
        l.up(at: dropAt.addingTimeInterval(Double(i) * 60 + 1))
    }
    #expect(l.drops.count == LinkLog.max)
    #expect(l.drops.first?.at == dropAt.addingTimeInterval(Double(LinkLog.max + 4) * 60))

    let d = try #require(UserDefaults(suiteName: "LinkLogTests"))
    d.removePersistentDomain(forName: "LinkLogTests")
    #expect(LinkLog.load(d) == LinkLog())
    l.save(d)
    #expect(LinkLog.load(d) == l)
    d.removePersistentDomain(forName: "LinkLogTests")
}

@Test func tallyCountsTheDayByCause() {
    var l = LinkLog()
    let causes: [LinkLog.Cause] = [.background, .ping, .ping, .path]
    for (i, c) in causes.enumerated() {
        l.dropped(at: dropAt.addingTimeInterval(Double(i) * 3600), c)
        l.up(at: dropAt.addingTimeInterval(Double(i) * 3600 + 2))
    }
    let t = l.tally(since: dropAt.addingTimeInterval(1800))
    #expect(t.map(\.0) == [.ping, .path])
    #expect(t.map(\.1) == [2, 1])
}

@Test func backoffStartsAtOnceAndDoublesToThirty() {
    #expect((0...8).map(LinkKeep.backoff) == [0, 1, 2, 4, 8, 16, 30, 30, 30])
}

@Test func networkChangesRedialOrCheck() {
    let wifi = LinkKeep.Path(online: true, via: "wifi"), cell = LinkKeep.Path(online: true, via: "cellular")
    let off = LinkKeep.Path(online: false, via: "")
    // The monitor's first word, and the same path again: nothing.
    #expect(LinkKeep.onPath(from: nil, to: wifi, up: true) == .nothing)
    #expect(LinkKeep.onPath(from: wifi, to: wifi, up: false) == .nothing)
    // Gone: the ping finds out; nothing to dial on.
    #expect(LinkKeep.onPath(from: wifi, to: off, up: true) == .nothing)
    // Back: dial now, or check the link that's up.
    #expect(LinkKeep.onPath(from: off, to: wifi, up: false) == .redial)
    #expect(LinkKeep.onPath(from: off, to: cell, up: true) == .check)
    // Wi-Fi to cellular: the link went out on Wi-Fi.
    #expect(LinkKeep.onPath(from: wifi, to: cell, up: true) == .reconnect)
    #expect(LinkKeep.onPath(from: cell, to: wifi, up: false) == .redial)
}

// A conversation read again after a redial keeps its place (T859).

private func reset(_ items: [AppAPI.TranscriptItem], before: String = "") -> Transcript {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: items, before: before))
    return t
}

private let goPrompt = AppAPI.TranscriptItem(kind: "prompt", text: "go")
private let bashCall = AppAPI.TranscriptItem(kind: "tool", tool: "Bash", call: "c1")
private let running = AppAPI.TranscriptItem(kind: "text", text: "Running it.")

@Test func rejoinKeepsTheIdsAndAddsTheNew() {
    var t = reset([goPrompt, bashCall], before: "cur")
    t.prepend(AppAPI.TranscriptPage(items: [AppAPI.TranscriptItem(kind: "note", text: "start")]))
    let ids = t.entries.map(\.id)
    // The tail again, the call's result in now, and one more.
    let fresh = reset([bashCall, AppAPI.TranscriptItem(kind: "result", text: "ok", call: "c1", lines: 1), running], before: "cur2")
    t.rejoin(fresh)
    #expect(t.entries.map(\.kind) == [.note, .prompt, .tool, .text])
    #expect(Array(t.entries.map(\.id).prefix(3)) == ids)
    #expect(t.entries[2].result?.text == "ok")
    #expect(Set(t.entries.map(\.id)).count == 4)
    // The page read before stays: its cursor too.
    #expect(t.before == "")
}

@Test func rejoinWithNothingInCommonStartsOver() {
    var t = reset([goPrompt, bashCall])
    let fresh = reset([AppAPI.TranscriptItem(kind: "prompt", text: "another session")], before: "c")
    t.rejoin(fresh)
    #expect(t == fresh)
    // Nor when what follows the match differs.
    var u = reset([goPrompt, bashCall, running])
    let other = reset([bashCall, AppAPI.TranscriptItem(kind: "text", text: "something else")])
    u.rejoin(other)
    #expect(u == other)
}

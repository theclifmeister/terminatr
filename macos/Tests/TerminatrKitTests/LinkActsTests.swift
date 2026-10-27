import Foundation
import Testing
@testable import TerminatrKit

func linked(_ needs: [AppAPI.LinkNeed]) -> AppAPI.Project {
    AppAPI.Project(slug: "web", active: true, usage: noUsage, linkNeeds: needs)
}

/// A project's link needs are needs of their own, each with what its
/// buttons send; a held message's words stay text (R9).
@Test func linkNeeds() throws {
    let needs = Need.of(projects: [linked([
        .init(kind: "held", peer: "api", id: "M-3", created: t0, title: "Ship the schema", bytes: 2048),
        .init(kind: "file", peer: "mini/docs", shown: "Studio/docs", id: "X-AB-2", created: t0, file: "spec.pdf", size: 3 << 20),
        .init(kind: "request", peer: "mini/docs", shown: "Studio/docs", id: "R-AB-1", created: t0, allow: "send"),
        .init(kind: "later", peer: "api", id: "Z-1", created: t0),
    ])], sessions: [])
    #expect(needs.map(\.id) == ["link:web:M-3", "link:web:X-AB-2", "link:web:R-AB-1"])
    #expect(needs.allSatisfy { $0.kind == .link && $0.project == "web" })
    let held = needs[0], file = needs[1], ask = needs[2]
    #expect(held.title == "web · M-3 to api waits for your OK")
    #expect(held.detail == "Message · 2.0 KB")
    #expect(held.text == "Ship the schema" && !held.title.contains("schema"))
    #expect(held.link?.params(yes: true) == AppAPI.LinkActParams(project: "web", action: "approve", id: "M-3"))
    #expect(held.link?.params(yes: false) == AppAPI.LinkActParams(project: "web", action: "drop", id: "M-3"))
    #expect(held.link?.declineMessage == "Drop M-3 to api? It is never sent.")
    #expect(file.title == "web · Studio/docs offers a file" && file.detail == "spec.pdf · 3.0 MB")
    #expect(file.link?.params(yes: true).action == "accept-file")
    #expect(file.link?.params(yes: false).action == "decline-file")
    #expect(file.link?.declineMessage == "Decline spec.pdf (3.0 MB) from Studio/docs? Studio/docs hears of it.")
    // Send there is receive here.
    #expect(ask.detail == "It asks for receive only on this side")
    let l = try #require(ask.link)
    #expect(l.yes == "Accept…" && l.no == "Decline…")
    #expect(l.directions.map(\.words) == ["receive only, ask first", "receive only", "both ways, ask first", "both ways",
                                          "send only, ask first", "send only"])
    #expect(l.params(yes: true, direction: l.directions[0]) == AppAPI.LinkActParams(project: "web", action: "accept", id: "R-AB-1", allow: "receive", ask: true))
    #expect(l.params(yes: false) == AppAPI.LinkActParams(project: "web", action: "decline", id: "R-AB-1"))
}

/// Each share request waiting is a need its buttons answer; a tm that
/// sends only their count keeps the one need that opens.
@Test func shareNeeds() throws {
    let q = AppAPI.ShareRequest(id: "S-AB-4", for: "acme", note: "the API docs", created: t0)
    let peer = AppAPI.Peer(alias: "mini", name: "Studio", state: "up", shareWaits: 1, shareRequests: [q])
    let needs = Need.of(projects: [], sessions: [], peers: [peer])
    let n = try #require(needs.first)
    #expect(needs.count == 1 && n.id == "share:mini:S-AB-4" && n.kind == .network && n.project == "")
    #expect(n.detail == "For its acme · Studio" && n.text == "the API docs")
    let l = try #require(n.link)
    #expect(l.kind == .share && l.alias == "mini" && l.other == "Studio")
    #expect(l.shareMessage("web").hasPrefix("Share web with every paired server? Studio asked for a project (S-AB-4, for its acme)."))
    #expect(l.declineMessage == "Decline Studio's request (S-AB-4)? Nothing is shared; Studio hears of it.")
    let old = Need.of(projects: [], sessions: [], peers: [AppAPI.Peer(alias: "mini", state: "up", shareWaits: 2)])
    #expect(old.map(\.id) == ["share:mini"] && old.first?.link == nil)
    let projects = [AppAPI.Project(slug: "web", active: true, usage: noUsage),
                    AppAPI.Project(slug: "api", active: true, shared: true, usage: noUsage),
                    AppAPI.Project(slug: "old", active: false, usage: noUsage),
                    AppAPI.Project(slug: "docs", active: true, usage: noUsage)]
    #expect(LinkAct.shareable(projects) == ["docs", "web"])
}

@Test func linkOutcome() {
    let act = LinkAct(kind: .held, project: "web", id: "M-3", other: "api")
    var o = LinkOutcome(act: act, title: "web · M-3 to api waits for your OK")
    #expect(o.holdsButtons && o.text == "Answering…")
    o.phase = .done("sent M-3 to api")
    #expect(o.holdsButtons && o.text == "Sent M-3 to api.")
    o.phase = .done("dropped M-3; it is never sent")
    #expect(o.text == "Dropped M-3; it is never sent.")
    o.phase = .failed("M-3 isn't held")
    #expect(!o.holdsButtons && o.text == "M-3 isn't held")
    #expect(LinkAct.size(640) == "640 bytes" && LinkAct.size(1126) == "1.1 KB")
}

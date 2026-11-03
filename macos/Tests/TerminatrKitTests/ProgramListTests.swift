import Foundation
import Testing
@testable import TerminatrKit

// The iPhone's program rows (B19): a program is one row placed as the
// TUI's buildTree places it, its local members under it alone, its
// needs its director's and its members' counted once.

private func project(_ slug: String, pinned: Bool = false) -> AppAPI.Project {
    AppAPI.Project(slug: slug, active: true, pinned: pinned, usage: noUsage)
}

private func program(_ slug: String, _ members: [(String, String)]) -> AppAPI.Program {
    AppAPI.Program(slug: slug, members: members.map { AppAPI.ProgramMember(project: $0.1, server: $0.0) }, authority: "propose")
}

private func need(_ id: String, _ project: String, task: String = "") -> Need {
    Need(id: id, kind: .review, project: project, what: id, detail: "", task: task)
}

private let acme = program("acme", [("", "acme-web"), ("mini", "acme-ios"), ("", "gone")])

@Test func programMembersAndStandalone() {
    let zed = program("zed", [("", "acme-web"), ("", "zed-cli")])
    // acme-web is acme's: the first program that lists it.
    #expect(ProgramList.members([acme, zed]) == ["acme-web": "acme", "gone": "acme", "zed-cli": "zed"])
    let ps = [project("acme-web"), project("billing"), project("zed-cli")]
    #expect(ProgramList.standalone(ps, programs: [acme, zed]).map(\.slug) == ["billing"])
    #expect(ProgramList.standalone(ps, programs: []).map(\.slug) == ["acme-web", "billing", "zed-cli"])
    #expect(ProgramList.key(acme.members[1]) == "mini/acme-ios")
}

@Test func programNeedsCountedOnce() {
    let needs = [need("a", "acme-web"), need("b", "billing"), need("c", "mini/acme-ios"), need("d", "acme"),
                 need("a", "acme-web"), need("e", "other/acme-ios"), need("f", "acme-ios")]
    // Its director's (acme), its local member's and its paired one's
    // under that server's alias; not another server's acme-ios, nor
    // this one's.
    #expect(ProgramList.needs(of: acme, in: needs).map(\.id) == ["a", "c", "d"])
}

@Test func programNeedsRanked() {
    let needs = [need("a", "acme-web", task: "T1"), need("b", "acme-web", task: "T2"), need("c", "acme-web", task: "T3"),
                 need("d", "acme-web")]
    let prio = ["T1": "could", "T2": "must", "T3": "should"]
    #expect(ProgramList.ranked(needs) { prio[$0.task] ?? "" }.map(\.id) == ["b", "c", "a", "d"])
}

@Test func programEntriesOrder() {
    // As the TUI's buildTree: pinned projects first, then a program
    // before the first unpinned project after it by slug, the rest last.
    let active = [project("zeta", pinned: true), project("alpha"), project("delta")]
    let progs = [program("beta", []), program("omega", [])]
    #expect(ProgramList.entries(active, programs: progs).map(\.slug) == ["zeta", "alpha", "beta", "delta", "omega"])
    #expect(ProgramList.entries([], programs: progs).map(\.slug) == ["beta", "omega"])
    // needs_first: what needs the user first, below the pinned.
    let first = ProgramList.entries(active, programs: progs, needsFirst: true) { ["omega", "delta"].contains($0.slug) }
    #expect(first.map(\.slug) == ["zeta", "delta", "omega", "alpha", "beta"])
}

@Test func programPairedMemberFromTheMirror() {
    let peers = [AppAPI.Peer(alias: "mini", state: "up", projects: [
        AppAPI.PeerProject(project: "acme-ios", needsYou: 2, threads: 1, ready: 0, program: "acme"),
    ])]
    #expect(ProgramList.peerProject(acme.members[1], in: peers)?.needsYou == 2)
    #expect(ProgramList.peerProject(acme.members[0], in: peers) == nil)
    #expect(ProgramList.peerProject(AppAPI.ProgramMember(project: "acme-ios", server: "far"), in: peers) == nil)
}

@Test func programPairedMemberHiddenFromItsServer() {
    let peer = AppAPI.Peer(alias: "mini", state: "up", projects: [
        AppAPI.PeerProject(project: "acme-ios", needsYou: 2, threads: 1, ready: 0, program: "acme"),
        AppAPI.PeerProject(project: "other", needsYou: 0, threads: 0, ready: 0),
        AppAPI.PeerProject(project: "listed", needsYou: 0, threads: 0, ready: 0),
    ])
    #expect(ProgramList.standalone(peer, programs: [acme]).map(\.project) == ["other", "listed"])
    let both = program("p", [("mini", "listed")])
    #expect(ProgramList.standalone(peer, programs: [acme, both]).map(\.project) == ["other"])
    #expect(ProgramList.standalone(peer, programs: []).map(\.project) == ["other", "listed"])
}

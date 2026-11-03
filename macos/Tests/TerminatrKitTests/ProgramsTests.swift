import Foundation
import Testing
@testable import TerminatrKit

// Programs in the Mac app (the programs design §11.1, §12): the rows'
// order is pinned by rowsAsTheTUI; these pin the rest.

private let acme = AppAPI.Program(slug: "acme", members: [AppAPI.ProgramMember(project: "web"),
                                                          AppAPI.ProgramMember(project: "api", server: "mini")],
                                  authority: "propose", questions: 1)

@Test func programNeedsCountOnce() {
    let n = { (id: String, project: String) in Need(id: id, kind: .question, project: project, what: id, detail: "") }
    // Its own, its local member's and its paired member's; not another
    // project's, nor a same-named project on another server.
    let needs = [n("a", "acme"), n("b", "web"), n("c", "mini/api"), n("d", "other"), n("e", "far/api"), n("f", "api")]
    #expect(SidebarTree.needs(of: acme, in: needs).map(\.id) == ["a", "b", "c"])
    // The program's questions are one need of its own, once.
    let all = Need.of(projects: [], sessions: [], programs: [acme])
    #expect(all.map(\.id) == ["program-questions:acme"])
    #expect(all.first?.title == "acme · director has a question")
    // Off: no programs, no such need.
    #expect(Need.of(projects: [], sessions: []).isEmpty)
}

@Test func pairedMemberTag() {
    #expect(SidebarTree.pairedMembers([acme]) == ["mini/api": "acme"])
    #expect(SidebarTree.members([acme], projects: [AppAPI.Project(slug: "web", active: true, usage: noUsage)]) == ["web": "acme"])
}

@Test func directorRef() throws {
    let ref = SessionRef.director("acme")
    #expect(!ref.isCoordinator && ref.who == "director")
    #expect(ref != SessionRef(project: "acme"))
    #expect(ref.description == "acme#director")
    // It survives the saved navigation; an old save reads as before.
    let back = try JSONDecoder().decode(SessionRef.self, from: JSONEncoder().encode(ref))
    #expect(back == ref)
    let old = try JSONDecoder().decode(SessionRef.self, from: Data(#"{"project":"p","thread":""}"#.utf8))
    #expect(old == SessionRef(project: "p"))
    let s = AppAPI.Session(id: "s-d", role: "director", project: "acme", cols: 80, rows: 24, created: t0, clients: 0)
    #expect(SessionRef(s) == ref)
    #expect(ref.session(in: [], sessions: [s]) == "s-d")
    // Its dashboard is its program's, which exists while listed.
    let sel = Selection.session(ref)
    #expect(sel.dashboard == .program("acme") && sel.isProgram)
    #expect(sel.exists(projects: [], peers: [], programs: [acme]))
    #expect(!sel.exists(projects: [], peers: []))
    #expect(Selection.route(project: "acme", projects: [], sessions: [], peers: [], programs: [acme]) == .program("acme"))
}

@Test func programFolds() {
    var nav = Navigation()
    #expect(SidebarTree.opens(acme, nav: nav))
    nav.collapsed.insert(Navigation.programKey("acme"))
    #expect(!SidebarTree.opens(acme, nav: nav))
    // Its director's pane shown keeps it open, so the List has the row.
    nav.select(.session(.director("acme")))
    #expect(SidebarTree.opens(acme, nav: nav))
    // A paired member starts folded under its program, and folds there
    // apart from its server.
    let key = Navigation.memberKey(program: "acme", server: "mini", project: "api")
    #expect(!SidebarTree.opens(member: "mini", "api", of: acme, nav: nav))
    nav.unfold(key)
    #expect(SidebarTree.opens(member: "mini", "api", of: acme, nav: nav))
    nav.fold(key)
    #expect(!SidebarTree.opens(member: "mini", "api", of: acme, nav: nav))
    #expect(!nav.collapsed.contains(Navigation.sectionKey(server: "mini", project: "api")))
    // A local member starts folded too; a standalone project unfolded.
    let web = AppAPI.Project(slug: "web", active: true, pinned: false, usage: noUsage)
    #expect(SidebarTree.opens(web, nav: Navigation()))
    #expect(!SidebarTree.opens(web, nav: Navigation(), member: true))
    var open = Navigation()
    open.unfold(Navigation.sectionKey(server: "", project: "web"))
    #expect(SidebarTree.opens(web, nav: open, member: true))
    // Folded again by the user, it stays folded once it leaves.
    open.fold(Navigation.sectionKey(server: "", project: "web"))
    #expect(!SidebarTree.opens(web, nav: open))
}

@Test func programActs() {
    let need = { (id: String, kind: String, program: Bool) in
        AppAPI.ProgramNeed(need: AppAPI.PeerNeed(id: id, kind: kind, session: "s-1", task: "T4", title: "SSO"),
                           project: program ? "acme" : "api", server: program ? "" : "mini", program: program)
    }
    #expect(ProgramFacts.act(need("ask:Q2", "ask", true)) == .answer(ref: "Q2", question: nil))
    #expect(ProgramFacts.act(need("umbrella:T4", "umbrella-review", true)) == .umbrella(task: "T4"))
    // A paired member's: its project there, not this server's session.
    #expect(ProgramFacts.act(need("x", "review", false)) == .open(project: "mini/api", session: ""))
}

@Test func programWords() {
    var g = acme
    g.parallelThreads = 4
    #expect(ProgramFacts.line(g, working: 1) == "propose · slots 1/4")
    g.paused = true
    #expect(ProgramFacts.line(g, working: nil) == "propose · slots 0/4 · paused")
    let m = AppAPI.ProgramMember(project: "api", server: "mini", state: "invited")
    #expect(ProgramFacts.child(m, project: nil, peer: nil, needs: nil) == "invited · not paired")
    let peer = AppAPI.Peer(alias: "mini", state: "up")
    #expect(ProgramFacts.child(m, project: nil, peer: peer, needs: 2) == "invited · not shared · 2 need you")
}

@Test func demoProgramDecodes() async throws {
    // program.get flattens its Program, as Go's JSON does (the schema
    // generator inlines embedded structs).
    let b = try await demoBridge()
    let d = try await b.call(AppAPI.Methods.programGet, AppAPI.ProgramParams(program: "acme"))
    #expect(d.slug == "acme" && d.members.count == 2 && d.umbrellas.count == 1 && d.waits.count == 1)
    let projects = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true, lean: true)).result
    #expect(projects.programs.map(\.slug) == ["acme"])
    let peers = try await b.call(AppAPI.Methods.peerList).peers
    // acme-web hangs under its program, not on its own.
    let entries = SidebarTree.entries(projects.projects, programs: projects.programs, peers: peers)
    #expect(entries.map(\.slug).contains("acme") && !entries.map(\.slug).contains("acme-web"))
}

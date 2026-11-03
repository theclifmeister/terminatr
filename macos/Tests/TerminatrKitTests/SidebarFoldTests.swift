import Foundation
import Testing
@testable import TerminatrKit

@Suite("Program member folds")
struct SidebarFoldTests {
    private let g = AppAPI.Program(slug: "acme", authority: "")

    @Test("A local member's key is its project's own, a paired one's is the program's")
    func keys() {
        #expect(SidebarTree.foldKey(member: "web", of: g) == Navigation.sectionKey(server: "", project: "web"))
        #expect(SidebarTree.foldKey(member: "ios", alias: "mini", of: g) == Navigation.memberKey(program: "acme", server: "mini", project: "ios"))
    }

    @Test("Toggling unfolds a folded member and folds it again")
    func toggle() {
        let k = SidebarTree.foldKey(member: "web", of: g)
        var nav = Navigation()
        nav.toggle(k, open: nav.unfolded.contains(k))
        #expect(nav.unfolded.contains(k))
        nav.toggle(k, open: nav.unfolded.contains(k))
        #expect(!nav.unfolded.contains(k))
        #expect(nav.collapsed.contains(k))
    }

    @Test("The fold persists like the Mac's")
    func persists() {
        let k = SidebarTree.foldKey(member: "ios", alias: "mini", of: g)
        var nav = Navigation()
        nav.unfold(k)
        let back = Navigation.restore(nav.encoded())
        #expect(back.unfolded.contains(k))
        #expect(SidebarTree.opens(member: "mini", "ios", of: g, nav: back))
    }

    @Test("A member's own key does not unfold the same project standalone-folded elsewhere")
    func apart() {
        var nav = Navigation()
        nav.unfold(SidebarTree.foldKey(member: "ios", alias: "mini", of: g))
        #expect(!nav.unfolded.contains(Navigation.sectionKey(server: "mini", project: "ios")))
    }

    @Test("A program is a section of its own; a folded member keeps its row, an unfolded one lists its rows")
    func sections() {
        let g = AppAPI.Program(slug: "acme", members: [
            AppAPI.ProgramMember(project: "web"), AppAPI.ProgramMember(project: "api"),
        ], authority: "")
        let rows = ["g:acme", "r:acme", "p:api", "p:web", "c:web", "t:web/t-1", "p:solo", "c:solo", "d:inactive"]
        let s: [SidebarSection] = SidebarTree.sections(rows, programs: [g])
        #expect(s.map { $0.id } == ["g:acme", "p:solo", "d:inactive"])
        #expect(s[0].title == "acme")
        #expect(s[0].rows == ["g:acme", "r:acme", "p:api", "p:web", "c:web", "t:web/t-1"])
        #expect(s[1].rows == ["c:solo"])
    }
}

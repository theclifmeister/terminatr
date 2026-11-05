import Testing
import TerminatrKit

@Suite struct SessionRoleTests {
    @Test func aRefNamesItsRole() {
        #expect(SessionRef(project: "web").role == .coordinator)
        #expect(SessionRef(project: "web", server: "studio").role == .coordinator)
        #expect(SessionRef.director("acme").role == .director)
        #expect(SessionRef(project: "web", thread: "t-1").role == nil)
    }

    @Test func eachRoleHasItsOwnWordAndMark() {
        #expect(SessionRole.coordinator.word == "Coordinator")
        #expect(SessionRole.director.word == "Director")
        #expect(SessionRole.coordinator.symbol != SessionRole.director.symbol)
    }

    /// A role and a thread at a depth share one edge, which is also the
    /// name's of the project they sit under.
    @Test func theGridHasOneTextEdgePerDepth() {
        #expect(SidebarGrid.textEdge(depth: 0) == 37)
        #expect(SidebarGrid.textEdge(depth: 1) == 49)
        // A project's coordinator sits where a program's director does.
        #expect(SidebarGrid.contents(of: 0) == SidebarGrid.step)
        #expect(SidebarGrid.contents(of: SidebarGrid.step) == 2 * SidebarGrid.step)
    }
}

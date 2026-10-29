import Foundation
import Testing
@testable import TerminatrKit

// The iPad's sidebar (T849) sections the TUI's rows, in their order.

@Test func sidebarSections() {
    let rows = ["g:", "p:acme-web", "c:acme-web", "t:acme-web/t-2", "t:acme-web/t-1", "p:billing", "c:billing",
                "g:laptop", "r:laptop/docs", "rc:laptop/docs", "rt:laptop/docs/t-9",
                "d:inactive", "p:old", "p:older"]
    #expect(SidebarTree.sections(rows) == [
        SidebarSection(id: "p:acme-web", title: "acme-web", rows: ["c:acme-web", "t:acme-web/t-2", "t:acme-web/t-1"]),
        SidebarSection(id: "p:billing", title: "billing", rows: ["c:billing"]),
        SidebarSection(id: "r:laptop/docs", title: "docs · laptop", rows: ["rc:laptop/docs", "rt:laptop/docs/t-9"]),
        SidebarSection(id: "d:inactive", title: "Inactive", rows: ["p:old", "p:older"]),
    ])
    // A folded project still has its section, with no rows.
    #expect(SidebarTree.sections(["p:a", "p:b", "c:b"]).map(\.rows) == [[], ["c:b"]])
    #expect(SidebarTree.sections([]).isEmpty)
    #expect(SidebarTree.split("rt:laptop/docs/t-9") == ("rt", "laptop/docs/t-9"))
}

import Foundation
import Testing
@testable import TerminatrKit

// The menu bar's dropdown and notifications (T748 milestone F): the
// first needs and a line for the rest, the active projects with
// something going under their ⌘ numbers, and no notification for the
// session shown in front.

private func project(_ slug: String, threads: [AppAPI.Thread] = []) -> AppAPI.Project {
    AppAPI.Project(slug: slug, active: true, threads: threads, usage: noUsage)
}

private func thread(_ id: String, done: Bool = false) -> AppAPI.Thread {
    AppAPI.Thread(record: AppAPI.ThreadRecord(id: id, title: id, agent: "claude", worktree: "/w", state: "working",
                                              session: "", created: t0, reports: 0, reportAcked: 0, done: done,
                                              usage: noUsage))
}

private func need(_ id: String, _ kind: Need.Kind, project: String, session: String = "") -> Need {
    Need(id: id, kind: kind, project: project, what: id, detail: "", session: session)
}

@Test func menuNeeds() {
    let all = [need("a", .approval, project: "terminatr"), need("b", .question, project: "terminatr"),
               need("c", .blocked, project: "toe"), need("d", .blocked, project: "foodperfect"),
               need("e", .review, project: "terminatr"), need("f", .review, project: "toe")]
    let (shown, rest) = MenuBarList.needs(all)
    #expect(shown.map(\.id) == ["a", "b", "c"])
    #expect(rest.map(\.id) == ["d", "e", "f"])
    #expect(SidebarTree.moreNeeds(rest) == "+3 more: 1 blocked, 2 review")
    #expect(MenuBarList.projects(of: rest) == "foodperfect, terminatr, toe")

    let few = MenuBarList.needs(Array(all.prefix(2)))
    #expect(few.shown.count == 2 && few.rest.isEmpty)
}

@Test func menuProjects() {
    // Active ones in the sidebar's order: idle ones (no open thread, no
    // need) are counted, the rest keep the Go menu's ⌘ numbers.
    let active = [project("cowork"), project("demo", threads: [thread("t-1")]),
                  project("foodperfect"), project("mods", threads: [thread("t-2", done: true)]), project("toe")]
    let needs = [need("r", .review, project: "foodperfect"), need("b", .blocked, project: "toe")]
    let (shown, more) = MenuBarList.projects(active, needs: needs)
    #expect(shown == [.init(index: 1, slug: "demo"), .init(index: 2, slug: "foodperfect"), .init(index: 4, slug: "toe")])
    #expect(more == 2)
    #expect(MenuBarList.others(more: more, inactive: 3) == "2 more active · 3 inactive")
    #expect(MenuBarList.others(more: 1, inactive: 0) == "1 more active")
    #expect(MenuBarList.others(more: 0, inactive: 3) == "3 inactive")
    #expect(MenuBarList.others(more: 0, inactive: 0) == nil)

    // At most projectsShown, the busy ones past it counted too.
    let many = (0..<9).map { project("p\($0)", threads: [thread("t-\($0)")]) }
    let cut = MenuBarList.projects(many, needs: [])
    #expect(cut.shown.count == MenuBarList.projectsShown)
    #expect(cut.more == 9 - MenuBarList.projectsShown)
}

@Test func noNotificationInFront() {
    let fresh = [need("q", .question, project: "p", session: "s-1"), need("a", .approval, project: "p", session: "s-2"),
                 need("r", .review, project: "p")]
    #expect(NeedTracker.notified(fresh, front: nil).map(\.id) == ["q", "a", "r"])
    #expect(NeedTracker.notified(fresh, front: "s-1").map(\.id) == ["a", "r"])
}

@Test func fallbackQuestionNotifies() {
    // A question with no parsed menu is a fresh need like any other.
    var t = NeedTracker()
    _ = t.update([])
    let needs = Need.of(projects: [], sessions: [session("s-1", state: "blocked", reason: "question")])
    let (fresh, _) = t.update(needs)
    #expect(fresh.map(\.id) == ["question:s-1"])
    #expect(NeedTracker.notified(fresh, front: "s-2").count == 1)
}

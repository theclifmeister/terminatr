import Foundation
import Testing
@testable import TerminatrKit

// The window's keys (T748 milestone E): ⌥⌘↓ / ⌥⌘↑ through the needs,
// the sidebar's type-to-select and its filter.

private func need(_ id: String, project: String) -> Need {
    Need(id: id, kind: .blocked, project: project, what: id, detail: "")
}

@Test func stepThroughNeeds() {
    // a and b both show project p's dashboard; c shows q's.
    let needs = [need("a", project: "p"), need("b", project: "p"), need("c", project: "q")]
    let route = { (n: Need) -> Selection? in .project(n.project) }
    func step(_ by: Int, last: String?, shown: Selection?) -> String? {
        Need.step(needs, by: by, last: last, shown: shown, route: route)?.id
    }
    // Nothing of theirs shown: down starts at the first, up at the last.
    #expect(step(1, last: nil, shown: nil) == "a")
    #expect(step(-1, last: nil, shown: .project("x")) == "c")
    // From what the window shows, wrapping round.
    #expect(step(1, last: nil, shown: .project("q")) == "a")
    #expect(step(-1, last: nil, shown: .project("q")) == "b")
    // Two needs show p: the last one stepped to tells which.
    #expect(step(1, last: "a", shown: .project("p")) == "b")
    #expect(step(1, last: "b", shown: .project("p")) == "c")
    #expect(step(-1, last: "b", shown: .project("p")) == "a")
    // The last one is no longer what the window shows: from what it does.
    #expect(step(1, last: "c", shown: .project("p")) == "b")
    #expect(Need.step([], by: 1, last: nil, shown: nil, route: route) == nil)
}

@Test func typeSelect() {
    let rows = ["Coordinator", "demo", "Docs pass", "notes", "Network popup", "webshop"]
    // One letter: the next row after the current one that begins with it.
    #expect(TypeSelect.match("n", in: rows, from: 0) == 3)
    #expect(TypeSelect.match("n", in: rows, from: 3) == 4)
    #expect(TypeSelect.match("n", in: rows, from: 4) == 3)
    // A word: the first from the current one on, regardless of case.
    #expect(TypeSelect.match("do", in: rows, from: 0) == 2)
    #expect(TypeSelect.match("de", in: rows, from: 1) == 1)
    #expect(TypeSelect.match("net", in: rows, from: nil) == 4)
    // The same letter again cycles through its rows.
    #expect(TypeSelect.match("nn", in: rows, from: 3) == 4)
    #expect(TypeSelect.match("x", in: rows, from: 0) == nil)

    var t = TypeSelect()
    #expect(t.type("N", now: t0) == "n")
    #expect(t.type("e", now: t0.addingTimeInterval(0.5)) == "ne")
    // After a pause, a new word.
    #expect(t.type("d", now: t0.addingTimeInterval(2)) == "d")
}

@Test func filterMatches() {
    #expect(SidebarTree.matches("", "anything"))
    #expect(SidebarTree.matches("cache", "T9 Cache eviction"))
    #expect(SidebarTree.matches("evic t9", "T9 Cache eviction"))
    #expect(SidebarTree.matches("cafe", "Café menu"))
    #expect(!SidebarTree.matches("cache disk", "T9 Cache eviction"))
    // Every word, in any field.
    #expect(SidebarTree.matches("demo popup", "demo", "Network popup polish"))

    let p = AppAPI.Project(slug: "demo", active: true,
                           threads: [thread("t-1", task: AppAPI.Task(id: 9, ref: "T9", title: "Cache eviction", status: "started")),
                                     thread("t-2")],
                           usage: noUsage)
    #expect(SidebarTree.threads(of: p, matching: "t9").map(\.record.id) == ["t-1"])
    #expect(SidebarTree.threads(of: p, matching: "t-2").map(\.record.id) == ["t-2"])
    #expect(SidebarTree.threads(of: p, matching: "thread").count == 2)
}

@Test func foldProjects() {
    let p = AppAPI.Project(slug: "p", active: true, usage: noUsage)
    let key = Navigation.sectionKey(server: "", project: "p")
    var nav = Navigation(selection: .project("p"))
    #expect(SidebarTree.opens(p, nav: nav, needs: 0))
    // ← folds the selected project; → opens one that has no needs.
    nav.fold(key)
    #expect(!SidebarTree.opens(p, nav: nav, needs: 1))
    nav.select(.project("q"))
    nav.unfold(key)
    #expect(SidebarTree.opens(p, nav: nav, needs: 0))
    // Folded, it still lists the session the window shows.
    nav.fold(key)
    nav.select(.session(SessionRef(project: "p", thread: "t-1")))
    #expect(SidebarTree.opens(p, nav: nav, needs: 0))
}

@Test func findInConversation() {
    func entry(_ id: Int, _ kind: Transcript.Entry.Kind, _ text: String, tool: String = "", result: String = "") -> Transcript.Entry {
        Transcript.Entry(id: id, kind: kind, text: text, truncated: false, tool: tool, call: "",
                         result: result.isEmpty ? nil : Transcript.Result(text: result, lines: 1, error: false))
    }
    let entries = [entry(0, .prompt, "Add a cache"), entry(1, .text, "The **Cache** is in place."),
                   entry(2, .tool, "git push", tool: "Bash", result: "rejected"), entry(3, .note, "Résumé")]
    #expect(Transcript.find("cache", in: entries) == [0, 1])
    #expect(Transcript.find("bash", in: entries) == [2])
    #expect(Transcript.find("rejected", in: entries) == [2])
    #expect(Transcript.find("resume", in: entries) == [3])
    #expect(Transcript.find(" ", in: entries).isEmpty)
}

import Foundation
import Testing
@testable import TerminatrKit

@Suite struct DesignsTests {
    let vault = AppAPI.Design(
        name: "vault", title: "Obsidian vault", status: "building", task: "T2", taskStatus: "review",
        artifact: "https://example.com/vault", tasks: ["T1", "T9"], tasksDone: 1, answers: ["YAML"],
        questions: [AppAPI.DesignQuestion(id: "Q3", header: "Canvas", question: "Columns by depth?")],
        build: [AppAPI.DesignTask(id: "T9", title: "Groundwork", status: "done", archived: true),
                AppAPI.DesignTask(id: "T1", title: "Notes", status: "started", after: ["T9"])])

    @Test func words() {
        #expect(Designs.built(vault) == "1/2 built")
        #expect(Designs.progress(vault) == 0.5)
        #expect(Designs.designTask(vault) == "T2 · review")
        #expect(Designs.facts(vault) == ["design task T2 · review", "1 open question", "1 answer"])
        #expect(Designs.page(vault)?.host == "example.com")
        #expect(Designs.after(vault.build[1]) == "after T9")
        #expect(Designs.after(vault.build[0]) == "")
        let bare = AppAPI.Design(name: "net", title: "net", status: "designing", tasksDone: 0)
        #expect(Designs.built(bare) == "no build tasks")
        #expect(Designs.progress(bare) == 0)
        #expect(Designs.facts(bare) == ["0 answers"])
        #expect(Designs.page(AppAPI.Design(name: "x", title: "x", status: "done", artifact: "http://example.com", tasksDone: 0)) == nil)
    }

    @Test func order() {
        let done = AppAPI.Design(name: "a", title: "a", status: "done", tasksDone: 0)
        let designing = AppAPI.Design(name: "b", title: "b", status: "designing", tasksDone: 0)
        let building = AppAPI.Design(name: "c", title: "c", status: "building", tasksDone: 0)
        #expect(Designs.ordered([done, designing, building, vault]).map(\.name) == ["vault", "c", "b", "a"])
        #expect(Designs.Status("odd") == .planned)
        #expect(Designs.tone(vault) == .question)
        #expect(Designs.tone(building) == .working && Designs.tone(done) == .done && Designs.tone(designing) == .idle)
        #expect(Designs.tone(task: vault.build[1]) == .working)
    }

    /// The done and archived designs show only when asked for (T872);
    /// an archived one sorts last and says so.
    @Test func hidden() {
        let done = AppAPI.Design(name: "a", title: "a", status: "done", tasksDone: 0)
        let shelved = AppAPI.Design(name: "b", title: "b", status: "planned", tasksDone: 0, archived: true)
        let building = AppAPI.Design(name: "c", title: "c", status: "building", tasksDone: 0)
        let all = [done, shelved, building]
        #expect(Designs.shown(all, all: false).map(\.name) == ["c"])
        #expect(Designs.ordered(Designs.shown(all, all: true)).map(\.name) == ["c", "a", "b"])
        #expect(Designs.toggle(all, all: false) == "Show 2 done or archived")
        #expect(Designs.toggle(all, all: true) == "Hide done and archived")
        #expect(Designs.toggle([building], all: false) == "")
        #expect(Designs.facts(shelved) == ["archived", "0 answers"])
        // An older tm's design.list has no archived: it reads as false.
        let old = try! JSONDecoder().decode(AppAPI.Design.self, from: Data(#"{"name":"x","title":"x","status":"planned","tasks_done":0}"#.utf8))
        #expect(!old.archived)
    }

    @Test func compatibility() {
        var h = AppAPI.Hello(appApi: 31, tm: "1", build: "", capabilities: [], methods: ["design.list"])
        h.server = AppAPI.Server(version: "1", protocol: 28, caps: ["designs"])
        #expect(Compatibility.listsDesigns(h))
        h.server?.caps = []
        #expect(!Compatibility.listsDesigns(h))
    }
}

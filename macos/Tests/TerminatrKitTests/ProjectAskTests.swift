import Foundation
import Testing
@testable import TerminatrKit

// The project controls (app API 13): Save & Clear, Activate and
// Deactivate, asked first in the TUI's words, and the coordinator's
// context use as its sidebar row shows it.

private func session(_ id: String, role: String, project: String = "demo", thread: String = "", task: String = "",
                     server: String = "", context: Int = 0, window: Int = 0) -> AppAPI.Session {
    AppAPI.Session(id: id, role: role, project: project, thread: thread, task: task, cols: 80, rows: 24, created: t0,
                   clients: 0, context: context, contextWindow: window, server: server)
}

@Test func deactivateAsksWhileSomethingRuns() {
    let on = AppAPI.Project(slug: "demo", active: true, usage: noUsage)
    let running = [session("s-1", role: "coordinator"), session("s-2", role: "thread", thread: "t-2", task: "T5"),
                   session("s-3", role: "thread", thread: "t-3"), session("s-4", role: "coordinator", project: "other"),
                   session("mini/s-5", role: "thread", thread: "t-5", server: "mini"), session("s-6", role: "shell")]
    let a = ProjectAsk.toggle(on, sessions: running)
    #expect(a == .deactivate(project: "demo", running: ["its coordinator", "T5", "t-3"]))
    #expect(a.asks)
    #expect(a.message == "This stops its coordinator, T5 and t-3 now; they resume when you activate it.")
    #expect(a.verb == "Deactivate" && a.title == "Deactivate demo?")
    // Nothing runs: done at once, as the TUI's space.
    let idle = ProjectAsk.toggle(on, sessions: [])
    #expect(!idle.asks)
    let off = AppAPI.Project(slug: "demo", active: false, usage: noUsage)
    #expect(ProjectAsk.toggle(off, sessions: running) == .activate(project: "demo"))
    #expect(!ProjectAsk.activate(project: "demo").asks)
}

@Test func saveClearAlwaysAsks() {
    let a = ProjectAsk.saveClear(project: "demo")
    #expect(a.asks && a.verb == "Save & Clear" && a.project == "demo")
    #expect(a.message.contains("save what it learned"))
}

@Test func activeResultWords() {
    #expect(ProjectAsk.words(AppAPI.ProjectActive(changed: true, resumed: ["coordinator"]), project: "demo") == nil)
    #expect(ProjectAsk.words(AppAPI.ProjectActive(changed: true, lost: ["t-1", "t-2"]), project: "demo")
        == "demo is active, but t-1 and t-2 couldn't be resumed: their records were dropped.")
}

@Test func contextUseAsTheTUI() {
    #expect(ContextUse.of(nil, hint: 40) == nil)
    #expect(ContextUse.of(session("s", role: "coordinator"), hint: 40) == nil)
    let low = ContextUse.of(session("s", role: "coordinator", context: 30_000, window: 200_000), hint: 40)
    #expect(low == ContextUse(percent: 15, level: .plain) && low?.tone == nil && low?.word == "15%")
    let hinted = ContextUse.of(session("s", role: "coordinator", context: 80_000, window: 200_000), hint: 40)
    #expect(hinted?.level == .hint && hinted?.tone == .approval)
    // The hint off: plain until 80%, high from there whatever the hint.
    #expect(ContextUse.of(session("s", role: "coordinator", context: 150_000, window: 200_000), hint: 0)?.level == .plain)
    let high = ContextUse.of(session("s", role: "coordinator", context: 160_000, window: 200_000), hint: 0)
    #expect(high?.level == .high && high?.tone == .attention)
}

@Test func projectControlsNeedAPI13() {
    var h = AppAPI.Hello(appApi: 13, tm: "v0.57.0", build: "x")
    #expect(!Compatibility.controlsProjects(h))
    h.methods = [AppAPI.Methods.coordinatorClear.name, AppAPI.Methods.projectActive.name]
    #expect(Compatibility.controlsProjects(h))
    h.appApi = 12
    #expect(!Compatibility.controlsProjects(h))
}

@Test func paletteProjectActions() {
    let p = AppAPI.Project(slug: "demo", active: true, usage: noUsage)
    let coord = session("s-1", role: "coordinator", context: 90_000, window: 200_000)
    let rows = Palette.projectActions(p, coordinator: coord, sessions: [coord], hint: 40)
    #expect(rows.map(\.title) == ["Save & clear demo's coordinator", "Deactivate demo"])
    #expect(rows[0].detail == "context 45%" && rows[0].tone == .approval)
    #expect(rows[0].command == .ask(.saveClear(project: "demo")))
    #expect(Palette.search(rows, "saveclear").map(\.id) == ["cmd:saveclear"])
    // No coordinator runs: no Save & Clear.
    #expect(Palette.projectActions(p, coordinator: nil, sessions: [], hint: 40).map(\.command) == [.ask(.deactivate(project: "demo", running: []))])
}

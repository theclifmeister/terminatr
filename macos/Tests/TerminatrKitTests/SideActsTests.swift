import Foundation
import Testing
@testable import TerminatrKit

// The sidebar's and the sessions' acts (app API 22), as the TUI's P, +,
// O, prefix+r and M: what asks first and in which words, which sessions
// can be adopted, and the forms' params.

private func session(_ id: String, role: String, agent: String = "", project: String = "", title: String = "",
                     state: String = "", remote: Bool = false, server: String = "") -> AppAPI.Session {
    AppAPI.Session(id: id, role: role, agent: agent, project: project, title: title, cols: 80, rows: 24, created: t0,
                   clients: 0, state: state, remoteControl: remote, server: server)
}

private func link(_ peer: String, send: Bool) -> AppAPI.Link {
    AppAPI.Link(peer: peer, state: "linked", hasMine: true, hasTheirs: true, send: send, receive: true, unread: 0, held: 0)
}

@Test func pinNeverAsks() {
    let p = AppAPI.Project(slug: "demo", active: true, usage: noUsage)
    let a = ProjectAsk.pinToggle(p)
    #expect(a == .pin(project: "demo", on: true) && !a.asks && a.verb == "Pin to Top" && a.isSideAct)
    let pinned = AppAPI.Project(slug: "demo", active: true, pinned: true, usage: noUsage)
    #expect(ProjectAsk.pinToggle(pinned) == .pin(project: "demo", on: false))
    #expect(ProjectAsk.pinToggle(pinned).verb == "Unpin")
}

@Test func remoteAsksInTheTUIsWords() {
    let off = ProjectAsk.remoteToggle(session("s-1", role: "coordinator", project: "demo"))
    #expect(off == .remote(project: "demo", session: "s-1", on: true) && off.asks)
    #expect(off.message == "Turn remote control on for demo, so it can be continued from another device?")
    let on = ProjectAsk.remoteToggle(session("s-1", role: "coordinator", project: "demo", remote: true))
    #expect(on.message == "Turn remote control off for demo?" && on.verb == "Turn Remote Control Off")
}

@Test func remoteResultWords() {
    #expect(ProjectAsk.words(AppAPI.Remote(remoteControl: true, how: "prompted"), project: "demo")
        == "Remote control on for demo: listed there as \"demo\"; until the coordinator is started anew.")
    #expect(ProjectAsk.words(AppAPI.Remote(remoteControl: false, how: "unchanged", held: true), project: "demo")
        == "Remote control is already off for demo; it stays off until the coordinator is started anew.")
    #expect(ProjectAsk.words(AppAPI.Remote(remoteControl: true, how: "restarted"), project: "demo")
        .hasPrefix("Remote control on for demo: the coordinator restarted"))
}

@Test func adoptableSessions() {
    let sessions = [
        session("s-1", role: "shell", agent: "claude", title: "Fix login"),
        session("s-2", role: "shell"),  // no agent
        session("s-3", role: "shell", agent: "codex", state: "exited"),
        session("s-4", role: "thread", agent: "claude", project: "demo"),
        session("mini/s-5", role: "shell", agent: "claude", server: "mini"),
    ]
    #expect(ProjectAsk.adoptable(sessions).map(\.id) == ["s-1"])
    #expect(ProjectAsk.adoptWhat(sessions[0]) == "claude · Fix login")
}

@Test func adoptAgainWithdraws() {
    let s = session("s-1", role: "shell", agent: "claude")
    let a = ProjectAsk.adoptToggle(s, project: "demo", items: [])
    #expect(a == .adopt(project: "demo", session: "s-1", what: "claude") && a.asks)
    #expect(a.message == "Adopt s-1 as a thread of demo? The coordinator links it to a task and briefs it.\n\nclaude")
    let asked = [AppAPI.Item(id: "i-1", kind: "adopt", subject: "s-1", created: t0, summary: "", needsUser: false)]
    let w = ProjectAsk.adoptToggle(s, project: "demo", items: asked)
    #expect(w == .withdrawAdopt(project: "demo", session: "s-1"))
    #expect(w.message == "Withdraw your go-ahead to adopt s-1 as a thread of demo? The coordinator can't adopt it then.")
}

@Test func newProjectForm() {
    var f = NewProjectForm(name: "  Demo App ")
    #expect(f.canMake && f.params == AppAPI.ProjectNewParams(project: "Demo App"))
    f.repo = "/src/app"
    #expect(f.params.repos == ["/src/app"])
    #expect(!NewProjectForm(name: " ").canMake && !NewProjectForm(name: "a\nb").canMake)
    #expect(NewProjectForm.done(AppAPI.ProjectNew(project: "demo-app", noAgent: true)).contains("no agent is installed"))
}

@Test func linkMessageForm() {
    let p = AppAPI.Project(slug: "demo", active: true, usage: noUsage,
                           links: [link("api", send: true), link("docs", send: false), link("mini/web", send: true)])
    #expect(LinkMessageForm.writable(p).map(\.peer) == ["api", "mini/web"])
    #expect(LinkMessageForm.writable(p, first: "mini/web").map(\.peer) == ["mini/web", "api"])
    var f = LinkMessageForm(peer: "api", text: "  ")
    #expect(!f.canSend)
    f.text = " hello \n"
    #expect(f.canSend && f.params(project: "demo") == AppAPI.LinkSendParams(project: "demo", peer: "api", text: "hello"))
}

@Test func sideActsNeedAPI22AndTheServer() {
    let caps = AppAPI.Server(version: "0.58.0", protocol: 23, caps: ["side-acts"])
    var h = AppAPI.Hello(appApi: 22, tm: "0.58.0", build: "", capabilities: [],
                         methods: [AppAPI.Methods.projectPin.name, AppAPI.Methods.sessionRemote.name], server: caps)
    #expect(Compatibility.actsOnSidebar(h) && Compatibility.controlsRemote(h))
    // An older server lacks the methods, though the bridge lists them.
    h.server?.caps = []
    #expect(!Compatibility.actsOnSidebar(h) && Compatibility.controlsRemote(h))
    // A device's link below write lists neither.
    h.server?.caps = ["side-acts"]
    h.methods = []
    #expect(!Compatibility.actsOnSidebar(h) && !Compatibility.controlsRemote(h))
    h = AppAPI.Hello(appApi: 21, tm: "0.57.0", build: "", capabilities: [], methods: [AppAPI.Methods.projectPin.name], server: caps)
    #expect(!Compatibility.actsOnSidebar(h))
}

@Test func paletteSideActions() {
    let p = AppAPI.Project(slug: "demo", active: true, usage: noUsage, links: [link("api", send: true)])
    let coord = session("s-1", role: "coordinator", project: "demo")
    let rows = Palette.sideActions(p, coordinator: coord, remote: true)
    #expect(rows.map(\.id) == ["cmd:pin", "cmd:remote", "cmd:writelink"])
    #expect(rows[2].command == .writeLink(project: "demo"))
    #expect(Palette.sideActions(AppAPI.Project(slug: "demo", active: true, usage: noUsage), coordinator: coord, remote: false)
        .map(\.id) == ["cmd:pin"])
}

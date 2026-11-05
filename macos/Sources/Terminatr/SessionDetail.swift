import SwiftUI
import TerminatrKit
import TerminatrUI

/// A coordinator or thread in the main pane (design T734 §6, T746): the
/// tab, its switch in the window's title row (T1000). It opens on the
/// conversation, and falls back to the terminal, saying why, when the
/// conversation can't be shown (an old server, a Codex agent, a
/// transcript tm can't read). A paired server's session opens the same
/// way while that server shares its conversation (RemoteConversation),
/// else on the terminal alone, its pane through the server's peer link.
/// The model, and its streams, are the app's
/// (AppModel.model): they outlive the selection for a few switches.
struct SessionDetail: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.controlActiveState) private var active
    let model: SessionModel

    var body: some View {
        let session = app.session(of: model)
        let state = app.conversationState(model, session)
        VStack(spacing: 0) {
            notice(state)
            if model.sessionID == nil {
                NotRunning(ref: model.ref)
            } else {
                switch model.tab {
                case .conversation:
                    ConversationTab(model: model, session: session, state: state)
                case .terminal:
                    TerminalTab(model: model, session: session)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: app.bridge == nil) { model.follow(app) }
        .onChange(of: app.sessionID(model.ref)) { model.follow(app) }
        .onChange(of: model.ref.isRemote ? app.remoteConversation(model.ref) : nil) { model.follow(app) }
        .onChange(of: state, initial: true) { _, s in model.settle(s) }
        // In the key window of the app in front: its needs don't notify.
        .onChange(of: active, initial: true) { _, a in app.shown(model.ref, inFront: a == .key) }
        .onDisappear { app.shown(model.ref, inFront: false) }
    }

    /// Why the conversation can't be shown, when it can't.
    @ViewBuilder
    private func notice(_ state: ConversationState) -> some View {
        let terminal = model.tab == .terminal
        if model.sessionID != nil, case .unavailable(let why) = state {
            Label {
                Text(terminal ? "Showing the terminal. \(why)" : why)
            } icon: {
                Image(systemName: model.ref.isRemote ? "network" : "rectangle.on.rectangle.slash")
            }
            .font(theme.body(.subheadline))
            .foregroundStyle(theme.muted)
            .padding(.horizontal, Page.edge)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(alignment: .bottom) { theme.lineSoft.frame(height: 1) }
            .accessibilityElement(children: .combine)
        }
    }
}

/// A session the user opened in a window of its own (Open in New
/// Window), beside the main one: the session and its inspector, with
/// streams of its own.
struct SessionWindow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let ref: SessionRef
    @State private var model: SessionModel
    @State private var inspector = true
    @AppStorage("inspectorPanelWidth") private var panelWidth = InspectorColumn.ideal

    init(ref: SessionRef) {
        self.ref = ref
        _model = State(initialValue: SessionModel(ref: ref))
    }

    var body: some View {
        GeometryReader { g in
            let f = WindowColumns.fit(window: g.size.width, sidebar: nil, inspector: inspector ? panelWidth : nil)
            MainArea(selection: .session(ref), own: model, buttons: true, inspector: $inspector,
                     panelWidth: f.inspector, panelWidthSaved: $panelWidth) {
                VStack(spacing: 0) {
                    if app.lost { ConnectionBanner() }
                    SessionDetail(model: model)
                        .opacity(app.lost ? MainWindow.dimmed : 1)
                }
            } panel: {
                Inspector(model: model, session: app.session(of: model))
            }
            .padding(CGFloat(WindowColumns.edge))
        }
        .background(theme.chrome)
        .overlay { WindowEdge() }
        .background { WindowButtons() }
        .ignoresSafeArea()
        .foregroundStyle(theme.ink)
        .navigationTitle(app.title(ref))
        .onDisappear { model.stop() }
    }
}

/// The ref names no running session: the thread is done or stopped,
/// the coordinator isn't running (Start starts it, project.open), the
/// director isn't (Start starts it, program.open), or the bridge is
/// down.
struct NotRunning: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let ref: SessionRef

    var body: some View {
        VStack(spacing: 8) {
            Text(headline)
                .font(theme.title(.headline, .semibold))
            Text(detail)
                .foregroundStyle(theme.muted)
                .multilineTextAlignment(.center)
            if ref.isCoordinator, !ref.isRemote, app.bridge != nil, let p = app.project(ref.project) {
                if p.active, app.sleep(of: p.slug) != .awake {
                    WakeButton(project: p.slug)
                        .padding(.top, 4)
                } else if !p.active, app.controlsProjects {
                    Button("Activate \(p.slug)") { app.ask(.activate(project: p.slug)) }
                        .buttonStyle(.flatPrimary)
                        .padding(.top, 4)
                } else {
                    StartButton(project: p, title: "Start the Coordinator")
                        .padding(.top, 4)
                }
            }
            if ref.director, app.bridge != nil, app.opensDirector, app.program(ref.project) != nil {
                StartDirectorButton(program: ref.project)
                    .padding(.top, 4)
            }
        }
        .padding(28)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var headline: String {
        if app.bridge == nil { return "Not connected" }
        if ref.isRemote {
            if case .closed = app.remotePane(ref) { return "\(ref.server)'s pane can't open" }
            return ref.isCoordinator ? "The coordinator isn't running on \(ref.server)" : "This thread has no session on \(ref.server)"
        }
        if ref.director { return "The director isn't running" }
        if ref.isCoordinator {
            switch app.sleep(of: ref.project) {
            case .asleep: return "The coordinator is asleep"
            case .waking: return "Waking the coordinator"
            case .awake: return "The coordinator isn't running"
            }
        }
        return "This thread has no session"
    }

    private var detail: String {
        if app.bridge == nil { return "The window follows the session again once tm app bridge is back." }
        if ref.isRemote {
            if case .closed(let why) = app.remotePane(ref) { return why }
            return "The window shows it once \(ref.server) lists it again."
        }
        if ref.director {
            if app.opensDirector, app.program(ref.project) != nil {
                return "Start it with the program's director agent, as enter on its row in tm's console does."
            }
            return "Open it in tm's console: enter on its row under \(ref.project). The window shows it once it runs."
        }
        if ref.isCoordinator, app.sleep(of: ref.project) != .awake {
            return "It slept after a long idle, its conversation kept: opening it wakes it, as enter on its row in tm's console does."
        }
        if ref.isCoordinator {
            return app.project(ref.project)?.active == false
                ? app.controlsProjects ? "The project is inactive: activate it to start its coordinator and resume its threads."
                    : "The project is inactive: tm project activate \(ref.project) turns it on."
                : "Start it with the project's agent and model, as tm project open does."
        }
        if let t = app.thread(ref), t.record.done { return "The thread is done." }
        return "Its agent isn't running. The window shows it once it runs."
    }
}


/// A sleeping coordinator's Wake (T980): project.open resumes it under
/// its session id, its conversation kept; it spins while it wakes, as
/// opening it from the sidebar does (AppModel.opened).
struct WakeButton: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    var title = "Wake the Coordinator"
    @State private var error: String?

    var body: some View {
        VStack(spacing: 4) {
            Button {
                wake()
            } label: {
                if app.sleep(of: project) == .waking {
                    ProgressView().controlSize(.small)
                } else {
                    Label(title, systemImage: "sun.max")
                }
            }
            .buttonStyle(.flatPrimary)
            .disabled(app.sleep(of: project) == .waking)
            .help("Resumes it with its conversation, as tm project open does")
            if let error {
                Text(error)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
                    .multilineTextAlignment(.center)
            }
        }
    }

    private func wake() {
        error = nil
        Task {
            do {
                try await app.wake(project)
            } catch {
                self.error = Dashboard.words(error)
            }
        }
    }
}

/// The director pane's Start: starts program's director (program.open),
/// as enter on its row in tm's console does; a refusal (programs off, a
/// project with the program's name) shows under it.
struct StartDirectorButton: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let program: String
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        VStack(spacing: 4) {
            Button {
                start()
            } label: {
                if busy {
                    ProgressView().controlSize(.small)
                } else {
                    Text("Start the Director")
                }
            }
            .buttonStyle(.flatPrimary)
            .disabled(busy)
            .help("Starts the director with the program's director agent, as tm program open does")
            if let error {
                Text(error)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
                    .multilineTextAlignment(.center)
            }
        }
    }

    private func start() {
        busy = true
        error = nil
        Task {
            do {
                try await app.startDirector(program)
            } catch {
                self.error = Dashboard.words(error)
            }
            busy = false
        }
    }
}

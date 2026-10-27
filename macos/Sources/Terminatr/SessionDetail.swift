import SwiftUI
import TerminatrKit

/// A coordinator or thread in the window's detail (design T734 §6,
/// T746): the tab, with its switch in the title bar beside the window's
/// title (the session's, once, T752). It opens on the
/// conversation, and falls back to the terminal, saying why, when the
/// conversation can't be shown (an old server, a Codex agent, a
/// transcript tm can't read). A paired server's session opens on the
/// terminal alone: its pane comes through the server's peer link, its
/// transcript never does. The model, and its streams, are the app's
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
        .onChange(of: state, initial: true) { _, s in model.settle(s) }
        // In the key window of the app in front: its needs don't notify.
        .onChange(of: active, initial: true) { _, a in app.shown(model.ref, inFront: a == .key) }
        .onDisappear { app.shown(model.ref, inFront: false) }
        .toolbar {
            // At the title bar's end: beside the title (.principal) it
            // cuts the title at the window's narrowest.
            ToolbarItem(placement: .primaryAction) {
                Picker("View", selection: Binding(get: { model.tab }, set: { model.pick($0) })) {
                    ForEach(SessionTab.allCases, id: \.self) { t in
                        Text(t.rawValue).tag(t)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .fixedSize()
                .disabled(model.sessionID == nil || model.ref.isRemote)
                .help("Conversation or Terminal (⌘⇧T)")
            }
        }
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
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: terminal ? TerminalTab.corner : 9))
            .accessibilityElement(children: .combine)
            // Over the terminal, at its box's edges.
            .padding(.horizontal, terminal ? TerminalTab.edge : 20)
            .padding(.top, 12)
            .padding(.bottom, 4)
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

    init(ref: SessionRef) {
        self.ref = ref
        _model = State(initialValue: SessionModel(ref: ref))
    }

    var body: some View {
        SessionDetail(model: model)
            .opacity(app.lost ? MainWindow.dimmed : 1)
            .safeAreaInset(edge: .top) {
                if app.lost { ConnectionBanner() }
            }
            .inspector(isPresented: $inspector) {
                Inspector(model: model, session: app.session(of: model))
                    .inspectorColumnWidth(min: InspectorColumn.min, ideal: InspectorColumn.min, max: InspectorColumn.max)
                    .background { Frost() }
            }
            .foregroundStyle(theme.ink)
            .background { Frost(\.glass, bar: true) }
            .navigationTitle(app.title(ref))
            .navigationSubtitle(app.subtitle(ref))
            .glassToolbar()
            .onDisappear { model.stop() }
    }
}

/// The ref names no running session: the thread is done or stopped,
/// the coordinator isn't running (Start starts it, project.open), or
/// the bridge is down.
struct NotRunning: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let ref: SessionRef

    var body: some View {
        VStack(spacing: 8) {
            Text(headline)
                .font(theme.title(.title, .heavy))
            Text(detail)
                .foregroundStyle(theme.muted)
                .multilineTextAlignment(.center)
            if ref.isCoordinator, !ref.isRemote, app.bridge != nil, let p = app.project(ref.project) {
                StartButton(project: p, title: "Start the Coordinator")
                    .padding(.top, 4)
            }
        }
        .padding(28)
        .frosted(RoundedRectangle(cornerRadius: 18))
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var headline: String {
        if app.bridge == nil { return "Not connected" }
        if ref.isRemote {
            if case .closed = app.remotePane(ref) { return "\(ref.server)'s pane can't open" }
            return ref.isCoordinator ? "The coordinator isn't running on \(ref.server)" : "This thread has no session on \(ref.server)"
        }
        return ref.isCoordinator ? "The coordinator isn't running" : "This thread has no session"
    }

    private var detail: String {
        if app.bridge == nil { return "The window follows the session again once tm app bridge is back." }
        if ref.isRemote {
            if case .closed(let why) = app.remotePane(ref) { return why }
            return "The window shows it once \(ref.server) lists it again."
        }
        if ref.isCoordinator {
            return app.project(ref.project)?.active == false
                ? "The project is inactive: tm project activate \(ref.project) turns it on."
                : "Start it with the project's agent and model, as tm project open does."
        }
        if let t = app.thread(ref), t.record.done { return "The thread is done." }
        return "Its agent isn't running. The window shows it once it runs."
    }
}


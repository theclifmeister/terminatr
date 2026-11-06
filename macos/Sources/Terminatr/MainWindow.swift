import SwiftUI
import TerminatrKit
import TerminatrUI

/// The app's one window (design T746, framed as the flat look's reference
/// since T1000): the sidebar of servers, projects and their coordinators
/// and threads; beside it the title row, the main pane with a project's
/// pages or a session, the right panel, Files (⌘⌥E) or the inspector
/// (⌘⌥I), and the status bar;
/// over them the command palette (⌘K). The menu bar, notifications and
/// cards change what it shows, never open another window, unless the user
/// asks for one (Open in New Window). The sidebar and the right panel keep
/// the widths they were left at.
struct MainWindow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @AppStorage("sidebarWidth") private var sidebarWidth = WindowColumns.sidebarIdeal
    @AppStorage("inspectorPanelWidth") private var panelWidth = InspectorColumn.ideal
    /// The column the user sized last: the last to give way in a narrow window.
    @State private var sizedLast: WindowColumns.Column?
    /// The needs rotor's entries are the sidebar's need rows.
    @Namespace private var needsRotor

    var body: some View {
        @Bindable var app = app
        GeometryReader { g in
            let shown = app.panelShown(app.nav.selection) != nil
            let f = WindowColumns.fit(window: g.size.width, sidebar: app.sidebarShown ? sidebarWidth : nil, inspector: shown ? panelWidth : nil, keep: sizedLast)
            let sidebarOn = app.sidebarShown
            HStack(spacing: 0) {
                if let s = f.sidebar {
                    Sidebar(needsRotor: needsRotor)
                        .sidebarText()
                        .opacity(app.lost ? Self.dimmed : 1)
                        .frame(width: CGFloat(s) - 1)
                        .windowSection(.sidebar)
                    theme.line
                        .frame(width: 1)
                        .overlay {
                            SplitHandle(width: 7, value: $sidebarWidth, shown: s, sign: 1, range: WindowColumns.sidebarMin...WindowColumns.limit(.sidebar, window: g.size.width, sidebarShown: true, inspectorShown: shown), name: "Sidebar width", sized: { sizedLast = .sidebar })
                        }
                        .zIndex(1)
                }
                MainArea(selection: app.nav.selection, files: app.nav.selection.map { app.filesModel($0) }, buttons: f.sidebar == nil,
                         shownPanel: app.panelShown(app.nav.selection), press: { app.press($0) },
                         panelWidth: f.inspector, panelWidthSaved: $panelWidth,
                         panelLimit: WindowColumns.limit(.inspector, window: g.size.width, sidebarShown: sidebarOn, inspectorShown: true),
                         panelSized: { sizedLast = .inspector }) {
                    VStack(spacing: 0) {
                        if app.lost { ConnectionBanner() }
                        detail.opacity(app.lost ? Self.dimmed : 1)
                    }
                } panel: {
                    InspectorPanel(selection: app.nav.selection)
                        .opacity(app.lost ? Self.dimmed : 1)
                }
            }
            .padding(CGFloat(WindowColumns.edge))
            .animation(.easeInOut(duration: 0.18), value: app.sidebarShown)
        }
        .background(theme.chrome)
        .overlay { WindowEdge() }
        .background { WindowButtons() }
        .ignoresSafeArea()
        .ownsSectionFocus(paused: app.palette)
        .tint(theme.action)
        .foregroundStyle(theme.ink)
        // VoiceOver's rotor (VO-U) lists what needs the user, from anywhere
        // in the window; picking one lands on its row in the sidebar's list.
        .accessibilityRotor("Needs you") {
            // Only while the list is unfolded: folded, no row is there to
            // land on (the fold row says the count).
            ForEach(app.nav.needsOpen ? app.needs : []) { n in
                AccessibilityRotorEntry(Text(n.title), id: n.id, in: needsRotor)
            }
        }
        // Opening a sleeping coordinator wakes it (T980).
        .onChange(of: app.nav.selection) { _, s in app.opened(s) }
        // Hidden, for the Window menu, Mission Control and VoiceOver. No
        // subtitle: one makes AppKit lay out the window's buttons afresh.
        .navigationTitle(app.title(app.nav.selection))
        .overlay(alignment: .top) {
            if app.palette {
                ZStack(alignment: .top) {
                    Color.black.opacity(0.2)
                        .ignoresSafeArea()
                        .onTapGesture { app.palette = false }
                    CommandPalette()
                        .padding(.top, 64)
                }
            }
        }
        .sheet(isPresented: $app.shortcuts) { ShortcutsSheet() }
        .sheet(isPresented: $app.welcome) { FirstRunSheet() }
        .sheet(isPresented: $app.pairServer) { PairServerSheet() }
        .sheet(isPresented: Binding(get: { app.newTask != nil }, set: { if !$0 { app.newTask = nil } })) {
            if let p = app.newTask {
                NewTaskSheet(project: p)
            }
        }
        .sheet(item: $app.taskShown) { TaskSheet(shown: $0) }
        .sheet(isPresented: $app.searchShown) {
            if let m = app.search {
                SearchSheet(model: m)
            }
        }
        .sheet(isPresented: $app.newProject) { NewProjectSheet() }
        .sheet(isPresented: Binding(get: { app.linkMessage != nil }, set: { if !$0 { app.linkMessage = nil } })) {
            if let m = app.linkMessage {
                LinkMessageSheet(project: m.project, form: LinkMessageForm(peer: m.peer))
            }
        }
        // Save & Clear, Activate and Deactivate, remote control and
        // adopting, from wherever the user picked them: asked here, in
        // the TUI's words.
        .confirmationDialog(app.asking?.title ?? "", isPresented: Binding(get: { app.asking != nil }, set: { if !$0 { app.asking = nil } }),
                            presenting: app.asking) { a in
            Button(a.verb) { app.act(a) }
            Button("Cancel", role: .cancel) {}
        } message: { a in
            Text(a.message)
        }
        .alert(app.askSaid?.title ?? "", isPresented: Binding(get: { app.askSaid != nil }, set: { if !$0 { app.askSaid = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(app.askSaid?.text ?? "")
        }
    }

    /// What the window keeps while the bridge is down shows so (lost).
    static let dimmed = 0.5

    @ViewBuilder
    private var detail: some View {
        switch app.nav.selection {
        case .session(let ref):
            SessionDetail(model: app.model(ref))
                .id(ref)
        case .project(let slug, let server) where server.isEmpty:
            if let p = app.project(slug) {
                ProjectPages(project: p)
                    .id(slug)
            } else {
                Placeholder(phase: app.phase, empty: app.projects.isEmpty)
            }
        case .project(let slug, let server):
            if let peer = app.peer(server), let p = peer.projects.first(where: { $0.project == slug }) {
                RemoteProjectPages(peer: peer, project: p)
                    .id(server + "/" + slug)
            } else {
                Placeholder(phase: app.phase, empty: app.projects.isEmpty)
            }
        case .program(let slug):
            if let g = app.program(slug) {
                ProgramPage(program: g)
                    .id("program:" + slug)
            } else {
                Placeholder(phase: app.phase, empty: app.projects.isEmpty)
            }
        case nil:
            Placeholder(phase: app.phase, empty: app.projects.isEmpty)
        }
    }
}

/// A context menu's Open in New Window: the session beside the main
/// window, for the user who wants two side by side.
struct NewWindowButton: View {
    @Environment(\.openWindow) private var openWindow
    let ref: SessionRef

    var body: some View {
        Button("Open in New Window") {
            openWindow(id: TerminatrApp.sessionWindow, value: ref)
        }
    }
}

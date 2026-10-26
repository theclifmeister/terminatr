import SwiftUI
import TerminatrKit

/// The app's one window (design T746): the sidebar of servers, projects
/// and their coordinators and threads; in the detail a project's
/// dashboard or a session; the session's inspector in a trailing column
/// (⌘⌥I); over them the command palette (⌘K). The menu bar,
/// notifications and cards change what it shows, never open another
/// window, unless the user asks for one (Open in New Window).
struct MainWindow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        @Bindable var app = app
        NavigationSplitView {
            Sidebar()
                .opacity(app.lost ? Self.dimmed : 1)
                .navigationSplitViewColumnWidth(min: 210, ideal: 240, max: 340)
        } detail: {
            detail
                .opacity(app.lost ? Self.dimmed : 1)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .safeAreaInset(edge: .top) {
                    if app.lost { ConnectionBanner() }
                }
                .background { Frost(\.glass, bar: true) }
                // A project has no inspector: its hiding isn't the user's.
                .inspector(isPresented: Binding(get: { app.inspector && app.selectedRef != nil },
                                                set: { if app.selectedRef != nil { app.inspector = $0 } })) {
                    inspector
                        .inspectorColumnWidth(min: 240, ideal: 270, max: 360)
                        .background { Frost() }
                }
        }
        .tint(theme.action)
        .foregroundStyle(theme.ink)
        .navigationTitle(app.title(app.nav.selection))
        .navigationSubtitle(app.subtitle(app.nav.selection))
        // The detail's glass shows through: no opaque band (T752).
        .toolbarBackground(.hidden, for: .windowToolbar)
        .toolbar {
            ToolbarItemGroup(placement: .navigation) {
                Button { app.goBack() } label: { Label("Back", systemImage: "chevron.backward") }
                    .disabled(!app.nav.canGoBack)
                    .help("Back (⌘[)")
                Button { app.goForward() } label: { Label("Forward", systemImage: "chevron.forward") }
                    .disabled(!app.nav.canGoForward)
                    .help("Forward (⌘])")
            }
            // Only when they don't fit: all well, it takes no room.
            if app.isConnected, app.compatibility.message != nil {
                ToolbarItem(placement: .primaryAction) { VersionBadge() }
            }
            if app.selectedRef != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button { app.inspector.toggle() } label: { Label("Inspector", systemImage: "sidebar.right") }
                        .help(app.inspector ? "Hide the inspector (⌘⌥I)" : "Show the inspector (⌘⌥I)")
                }
            }
        }
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
                ProjectDashboard(project: p)
            } else {
                Placeholder(phase: app.phase, empty: app.projects.isEmpty)
            }
        case .project(let slug, let server):
            if let peer = app.peer(server), let p = peer.projects.first(where: { $0.project == slug }) {
                RemoteProjectDashboard(peer: peer, project: p)
            } else {
                Placeholder(phase: app.phase, empty: app.projects.isEmpty)
            }
        case nil:
            Placeholder(phase: app.phase, empty: app.projects.isEmpty)
        }
    }

    @ViewBuilder
    private var inspector: some View {
        if let ref = app.selectedRef {
            let m = app.model(ref)
            Inspector(model: m, session: app.session(of: m))
                .id(ref)
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

/// A panel's ground: the theme's mesh under the system's frost and a
/// tint of the theme, as Bloom draws its panels. The sidebar and the
/// inspector take sidebarTint; the detail takes glass (frost in light,
/// smoked glass in dark) and, with bar, the title bar's band over it.
struct Frost: View {
    @Environment(\.theme) private var theme
    let tint: KeyPath<Theme, Color>
    let bar: Bool

    init(_ tint: KeyPath<Theme, Color> = \.sidebarTint, bar: Bool = false) {
        self.tint = tint
        self.bar = bar
    }

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .top) {
                Backdrop()
                Rectangle().fill(.ultraThinMaterial)
                theme[keyPath: tint]
                if bar {
                    theme.bar
                        .frame(height: g.safeAreaInsets.top)
                        .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
                }
            }
            .ignoresSafeArea()
        }
    }
}

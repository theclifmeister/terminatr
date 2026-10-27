import SwiftUI
import TerminatrKit

/// The app's one window (design T746): the sidebar of servers, projects
/// and their coordinators and threads; in the detail a project's
/// dashboard or a session; the session's inspector in a trailing column
/// (⌘⌥I); over them the command palette (⌘K). The menu bar,
/// notifications and cards change what it shows, never open another
/// window, unless the user asks for one (Open in New Window). The
/// toolbar has Search (⌘K), Needs you with its count and the inspector's
/// toggle (T748 milestone G); the inspector keeps the width it was left
/// at.
struct MainWindow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    /// The inspector's width as it was left (InspectorWidth saves it),
    /// read once: its ideal stays put while the user drags it.
    private static let inspectorWidth = InspectorColumn.ideal(saved: UserDefaults.standard.object(forKey: inspectorWidthKey) as? Double)
    static let inspectorWidthKey = "inspectorWidth"
    /// The needs rotor's entries are the sidebar's need rows.
    @Namespace private var needsRotor

    var body: some View {
        @Bindable var app = app
        NavigationSplitView {
            Sidebar(needsRotor: needsRotor)
                .sidebarText()
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
                        .inspectorColumnWidth(min: InspectorColumn.min, ideal: Self.inspectorWidth, max: InspectorColumn.max)
                        .background { Frost() }
                        .background(InspectorWidth(key: Self.inspectorWidthKey))
                }
        }
        .tint(theme.action)
        .foregroundStyle(theme.ink)
        // VoiceOver's rotor (VO-U) lists what needs the user, from anywhere
        // in the window; picking one unfolds the sidebar's list to it.
        .accessibilityRotor("Needs you") {
            ForEach(app.needs) { n in
                AccessibilityRotorEntry(Text(n.title), id: n.id, in: needsRotor) {
                    app.nav.collapsed.remove(Navigation.needsSection)
                    app.nav.unfolded.insert(Navigation.moreKey())
                }
            }
        }
        .navigationTitle(app.title(app.nav.selection))
        .navigationSubtitle(app.subtitle(app.nav.selection))
        // The detail's glass shows through: no opaque band (T752);
        // on macOS 26 the scroll edge effect under it (T764).
        .glassToolbar()
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
            ToolbarItemGroup(placement: .primaryAction) {
                Button { app.palette.toggle() } label: { Label("Search", systemImage: "magnifyingglass") }
                    .help("Search projects, threads, tasks and needs (⌘K)")
                NeedsMenu()
                Button { app.inspector.toggle() } label: { Label("Inspector", systemImage: "sidebar.right") }
                    .disabled(app.selectedRef == nil)
                    .help(app.selectedRef == nil ? "A session's inspector (⌘⌥I)"
                        : app.inspector ? "Hide the inspector (⌘⌥I)" : "Show the inspector (⌘⌥I)")
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
        .sheet(isPresented: Binding(get: { app.newTask != nil }, set: { if !$0 { app.newTask = nil } })) {
            if let p = app.newTask {
                NewTaskSheet(project: p)
            }
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

/// The toolbar's Needs you: its count on a flag, and a menu of the
/// needs, each showing what it names; Next Need (⌥⌘↓) first.
struct NeedsMenu: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        Menu {
            Button("Next Need") { app.stepNeed(1) }
                .disabled(app.needs.isEmpty)
            if !app.needs.isEmpty {
                Divider()
                ForEach(app.needs) { n in
                    Button("\(n.title) · \(n.detail.isEmpty ? n.project : n.detail)") {
                        app.open(project: n.project, session: n.session)
                    }
                }
            }
        } label: {
            // The count beside the flag, as Bloom's toolbar has it (⚑ 6).
            Label("\(app.needs.count)", systemImage: app.needs.isEmpty ? "flag" : "flag.fill")
                .labelStyle(.titleAndIcon)
                .monospacedDigit()
        }
        .menuIndicator(.hidden)
        .help(app.needs.isEmpty ? "Nothing needs you" : "\(app.needs.count) need you: ⌥⌘↓ shows the next")
        .accessibilityLabel("Needs you, \(app.needs.count)")
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

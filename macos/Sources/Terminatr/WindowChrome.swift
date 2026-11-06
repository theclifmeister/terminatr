import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

/// The flat look's frame round the main pane (T1000), to the reference's
/// measure: a title row of 44 and its line over the panes, the gutter
/// with the framed main pane and the right panel, the status bar
/// of 30 under them. Replaces the window's toolbar and split view: their
/// items and columns can't reach these places (the trial in T1000).
struct MainArea<Detail: View, Panel: View>: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    /// What the window shows, for the title row and the pane's header.
    let selection: Selection?
    /// A session window's own model, else the app's for selection.
    var own: SessionModel?
    /// The selection's Files model: the panel's, and the pane's file tabs.
    var files: FilesModel?
    /// Room in the title row for the window's buttons, when no sidebar
    /// has them.
    var buttons = false
    /// What the right panel shows.
    let shownPanel: RightPanel?
    /// Files or Inspector pressed.
    let press: (RightPanel) -> Void
    /// The panel's width, or nil while it hides.
    let panelWidth: Double?
    @Binding var panelWidthSaved: Double
    @ViewBuilder var detail: Detail
    @ViewBuilder var panel: Panel

    var body: some View {
        VStack(spacing: 0) {
            TitleRow(selection: selection, own: own, files: files, buttons: buttons, shownPanel: shownPanel, press: press)
            HStack(spacing: 0) {
                MainPane(selection: selection, own: own, file: files?.pane.current) {
                    if let files, let f = files.pane.current {
                        FilePane(model: files, file: f)
                            .id(f.id)
                    } else {
                        detail
                    }
                }
                .windowSection(.main, anchor: true)
                .focusBorder(.main)
                .padding(.leading, CGFloat(WindowColumns.gap))
                if let panelWidth {
                    SplitHandle(width: CGFloat(WindowColumns.gap), value: $panelWidthSaved, sign: -1, range: InspectorColumn.min...InspectorColumn.max, name: "Panel width")
                    Group {
                        if shownPanel == .files, let files, let place = app.filesPlace(selection) {
                            FilesPanel(model: files, place: place, own: own)
                                .opacity(app.lost ? MainWindow.dimmed : 1)
                                .windowSection(.files)
                                .focusBorder(.files)
                        } else {
                            panel
                                .windowSection(.inspector, anchor: true)
                                .focusBorder(.inspector)
                        }
                    }
                    .frame(width: CGFloat(panelWidth))
                }
            }
            .padding(.trailing, CGFloat(WindowColumns.trailing))
            .padding(.vertical, CGFloat(WindowColumns.gap))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(theme.gutter)
            StatusBar()
        }
        // A tab of the session's or project's own, however it's picked,
        // comes back from a file's.
        .onChange(of: ownTab) { files?.pane.shown = nil }
    }

    private var ownTab: String {
        switch selection {
        case .session(let ref): (own ?? app.model(ref)).tab.rawValue
        case .project: app.projectTab.rawValue
        default: ""
        }
    }
}

// MARK: the title row

/// The title row: a session's or a project's tabs, then back and forward,
/// + for a new task, Files and Inspector. Empty parts of it move the
/// window, as a title bar does.
struct TitleRow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let selection: Selection?
    var own: SessionModel?
    var files: FilesModel?
    var buttons = false
    let shownPanel: RightPanel?
    let press: (RightPanel) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            if buttons {
                Color.clear.frame(width: 76, height: 1)
                if !app.sidebarShown && own == nil {
                    IconButton(symbol: "sidebar.left", name: "Show the sidebar", help: "Show the sidebar (⌃⌘S)") { app.toggleSidebar() }
                        .padding(.top, 13)
                        .padding(.trailing, 4)
                }
            }
            // The tabs lose their icons, then their room, before the
            // buttons at the row's end do.
            ViewThatFits(in: .horizontal) {
                tabs(icons: true, files: files?.pane.tabs ?? [])
                tabs(icons: false, files: files?.pane.tabs ?? [])
                // Too many file tabs for the row: the one shown alone.
                tabs(icons: false, files: files?.pane.current.map { [$0] } ?? [])
            }
            .padding(.leading, 11)
            .padding(.top, 6)
            Spacer(minLength: 8)
            if own == nil {
                IconButton(symbol: "chevron.backward", name: "Back", help: "Back (⌘[)") { app.goBack() }
                    .disabled(!app.nav.canGoBack)
                    .padding(.top, 13)
                    .padding(.trailing, 7)
                IconButton(symbol: "chevron.forward", name: "Forward", help: "Forward (⌘])") { app.goForward() }
                    .disabled(!app.nav.canGoForward)
                    .padding(.top, 13)
                    .padding(.trailing, 11)
                IconButton(symbol: "plus", name: "New task", help: "New task (⌘N)") { app.showNewTask() }
                    .disabled(!app.canAddTask || app.boardProject == nil)
                    .padding(.top, 13)
                    .padding(.trailing, 11)
            }
            filesButton
                .padding(.top, 6)
                .padding(.trailing, 1)
            PillButton(symbol: "sidebar.right", title: "Inspector", on: shownPanel == .inspector,
                       help: shownPanel == .inspector ? "Hide the inspector (⌘⌥I)" : "Show the inspector (⌘⌥I)") { press(.inspector) }
                .disabled(selection == nil)
                .padding(.top, 6)
                .padding(.trailing, 10)
        }
        .frame(height: 44, alignment: .top)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background { WindowDragArea() }
        .padding(.bottom, 1)
        .background(theme.chrome)
        .overlay(alignment: .bottom) { theme.lineSoft.frame(height: 1) }
    }

    private func tabs(icons: Bool, files opened: [OpenedFile]) -> some View {
        let reading = files?.pane.shown != nil
        return HStack(spacing: 10) {
            switch selection {
            case .session(let ref):
                let m = own ?? app.model(ref)
                HStack(spacing: 10) {
                    ForEach(SessionTab.allCases, id: \.self) { t in
                        TabButton(title: t.rawValue, symbol: t == .conversation ? "bubble.left" : "terminal", icons: icons, on: !reading && m.tab == t) {
                            files?.pane.shown = nil
                            m.pick(t)
                        }
                    }
                }
                .disabled(m.sessionID == nil)
                .help("Conversation or Terminal (⇧⌘T)")
            case .project(_, let server):
                let tabs = server.isEmpty ? app.projectTabs : ProjectTab.paired
                ForEach(tabs) { t in
                    TabButton(title: t.rawValue, symbol: t.symbol, icons: icons, on: !reading && app.projectTab == t) {
                        files?.pane.shown = nil
                        app.projectTab = t
                    }
                }
            case .program:
                TabButton(title: "Program", symbol: "square.stack.3d.up", icons: icons, on: true) {}
            case nil:
                EmptyView()
            }
            ForEach(opened) { f in
                FileTabButton(file: f, icons: icons, on: files?.pane.shown == f.id) {
                    files?.pane.shown = f.id
                } close: {
                    files?.pane.close(f.id)
                }
            }
        }
    }

    /// Shows or hides the right panel's Files view, everywhere a thread or
    /// project has files. Off on a director and a program, which have no
    /// repo.
    private var filesButton: some View {
        let has = app.filesPlace(selection) != nil
        return PillButton(symbol: "doc", title: "Files", on: shownPanel == .files,
                          help: !has ? "A director has no repo: its projects' threads have the files"
                              : shownPanel == .files ? "Hide the files (⌘⌥E)" : "Show the files (⌘⌥E)") {
            press(.files)
        }
        .disabled(!has)
    }
}

extension ProjectTab {
    var symbol: String {
        switch self {
        case .overview: "info.circle"
        case .board: "list.bullet.rectangle"
        case .memory: "bookmark"
        case .library: "books.vertical"
        case .mods: "puzzlepiece.extension"
        }
    }
}

/// A tab in the title row: 31 high, 6 round, 11 at its sides.
private struct TabButton: View {
    @Environment(\.theme) private var theme
    let title: String
    let symbol: String
    let icons: Bool
    let on: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 7) {
                if icons {
                    BarIcon(symbol: symbol)
                }
                Text(title)
                    .font(theme.sized(13))
            }
            .padding(.horizontal, 11)
            .frame(height: 31)
            .foregroundStyle(on ? theme.ink : theme.muted)
            .background(on ? theme.tab : .clear, in: RoundedRectangle(cornerRadius: 6))
            .contentShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
        .fixedSize()
        .accessibilityLabel(title)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// A file Open in pane added: a tab with its name and a way to close it.
private struct FileTabButton: View {
    @Environment(\.theme) private var theme
    let file: OpenedFile
    let icons: Bool
    let on: Bool
    let action: () -> Void
    let close: () -> Void

    var body: some View {
        HStack(spacing: 5) {
            Button(action: action) {
                HStack(spacing: 7) {
                    if icons {
                        BarIcon(symbol: "doc")
                    }
                    Text(file.name)
                        .font(theme.sized(13))
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .frame(maxWidth: 170)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(file.name)
            .accessibilityAddTraits(on ? .isSelected : [])
            Button(action: close) {
                Image(systemName: "xmark")
                    .font(.system(size: 9, weight: .semibold))
                    .frame(width: 14, height: 14)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Closes this tab")
            .accessibilityLabel("Close \(file.name)")
        }
        .padding(.leading, 11)
        .padding(.trailing, 7)
        .frame(height: 31)
        .foregroundStyle(on ? theme.ink : theme.muted)
        .background(on ? theme.tab : .clear, in: RoundedRectangle(cornerRadius: 6))
        .fixedSize()
        .help(file.path)
    }
}

/// Files and Inspector: 30 high, 6 round, 9 at their sides, on the
/// selection's fill while what they show does.
private struct PillButton: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    let symbol: String
    let title: String
    let on: Bool
    let help: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                BarIcon(symbol: symbol)
                Text(title)
                    .font(theme.sized(13))
            }
            .padding(.horizontal, 9)
            .frame(height: 30)
            .foregroundStyle(on ? theme.ink : enabled ? theme.muted : theme.faint)
            .background(on ? theme.selection : .clear, in: RoundedRectangle(cornerRadius: 6))
            .contentShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help(help)
        .accessibilityLabel(title)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// An icon alone in a bar: 18 square, muted, its name for VoiceOver.
struct IconButton: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    let symbol: String
    let name: String
    let help: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            BarIcon(symbol: symbol)
                .frame(width: 18, height: 18)
                .foregroundStyle(enabled ? theme.muted : theme.faint)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(name)
    }
}

/// A bar's icon at the page's 15 points.
struct BarIcon: View {
    let symbol: String

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: 13))
            .frame(width: 15, height: 15)
            .accessibilityHidden(true)
    }
}

// MARK: the main pane

/// The main pane: 8 round with its line, and a header naming what's open.
/// MainArea marks it as a section, which gives it the focus line.
struct MainPane<Content: View>: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let selection: Selection?
    var own: SessionModel?
    /// The file tab it shows, for its header.
    var file: OpenedFile?
    @ViewBuilder var content: Content

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 8)
        VStack(spacing: 0) {
            if let h = header {
                PaneHeader(symbol: h.symbol, title: h.title, detail: h.detail)
            }
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                // A pane's buttons are the page's small ones (T1002).
                .buttonStyle(.flat)
        }
        .background(theme.pane)
        .clipShape(shape)
        .overlay { shape.strokeBorder(theme.line, lineWidth: 1).allowsHitTesting(false) }
    }

    private var header: (symbol: String, title: String, detail: String)? {
        if let file {
            let who = app.filesPlace(selection)?.label ?? ""
            return ("doc", file.path, who.isEmpty ? "read only" : "\(who) · read only")
        }
        switch selection {
        case .session(let ref):
            let m = own ?? app.model(ref)
            return Self.head(ref, tab: m.tab, agent: app.session(of: m)?.agent ?? ref.session(in: app.peers)?.agent ?? "",
                             task: ref.isRemote ? ref.session(in: app.peers)?.task : app.thread(ref)?.task?.ref,
                             title: app.thread(ref).map { $0.task?.title ?? $0.record.title } ?? "")
        case .project(let slug, let server):
            let tab = app.projectTab
            return (tab.symbol, tab.rawValue, server.isEmpty ? slug : "\(server) › \(slug)")
        case .program(let slug):
            return ("square.stack.3d.up", "Program", slug)
        case nil:
            return nil
        }
    }

    /// A session's header: the conversation's who and what, the
    /// terminal's agent and session.
    private static func head(_ ref: SessionRef, tab: SessionTab, agent: String, task: String?, title: String) -> (symbol: String, title: String, detail: String) {
        let who = ref.director ? "Director" : ref.isCoordinator ? "Coordinator" : (task?.isEmpty == false ? task! : ref.thread)
        let place = ref.isRemote ? "\(ref.server) › \(ref.project)" : ref.project
        switch tab {
        case .conversation:
            return (ref.role?.symbol ?? "bubble.left", who, ref.isCoordinator || ref.director ? place : title.isEmpty ? place : title)
        case .terminal:
            return ("terminal", agent.isEmpty ? who : agent, ref.isCoordinator || ref.director ? place : ref.thread)
        }
    }
}

/// A pane's header: 34 high with its line.
private struct PaneHeader: View {
    @Environment(\.theme) private var theme
    let symbol: String
    let title: String
    let detail: String

    var body: some View {
        HStack(spacing: 8) {
            BarIcon(symbol: symbol)
                .foregroundStyle(theme.muted)
            Text(title)
                .font(theme.sized(13, .medium))
                .lineLimit(1)
                .layoutPriority(1)
            Text(detail)
                .font(theme.sized(13))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 13)
        .frame(height: 33)
        .frame(maxWidth: .infinity)
        .padding(.bottom, 1)
        .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

// MARK: the status bar

/// The status bar: the servers, threads at work and what needs the user,
/// each a menu of them, a version that doesn't fit when one doesn't, and ⌘K.
struct StatusBar: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        let f = StatusFacts.of(projects: app.projects, sessions: app.sessions, peers: app.peers, needs: app.needs.count)
        HStack(spacing: 17) {
            ServersMenu(facts: f)
            WorkingMenu(facts: f)
            NeedsMenu(facts: f)
            Spacer(minLength: 8)
            if app.isConnected, app.compatibility.message != nil {
                VersionBadge()
            }
            Button { app.palette.toggle() } label: {
                HStack(spacing: 6) {
                    Text("⌘K").foregroundStyle(theme.faint)
                    Text("commands")
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Search projects, threads, tasks and needs (⌘K)")
            .accessibilityLabel("Commands, command K")
        }
        .font(theme.sized(13))
        .foregroundStyle(theme.muted)
        .padding(.horizontal, 15)
        .frame(height: 29)
        .frame(maxWidth: .infinity)
        .padding(.top, 1)
        .background(theme.chrome)
        .overlay(alignment: .top) { theme.lineSoft.frame(height: 1) }
    }
}

/// A status's dot.
struct Dot: View {
    let colour: Color

    var body: some View {
        Circle()
            .fill(colour)
            .frame(width: 7, height: 7)
            .accessibilityHidden(true)
    }
}

/// A count of the status bar that opens a menu: the one look, keys and
/// VoiceOver of its three (a native menu: arrows, return, escape). Its rows
/// hold still while it is open, as a row that changes closes an open menu
/// (a server's ping does every few seconds), and read the time afresh every
/// few seconds while it is shut, as nothing else draws them while the model
/// is quiet.
struct StatusMenu<Rows, Mark: View, Items: View>: View {
    let word: String
    let help: String
    let spoken: String
    let rows: (Date) -> Rows
    @ViewBuilder let mark: Mark
    @ViewBuilder let items: (Rows) -> Items
    @State private var now = Date()
    /// The rows while a menu is open.
    @State private var held: Rows?

    var body: some View {
        let current = rows(now)
        Menu {
            items(held ?? current)
        } label: {
            HStack(spacing: 6) {
                mark
                Text(word)
                    .monospacedDigit()
            }
            .contentShape(Rectangle())
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .help(help)
        .accessibilityLabel(spoken)
        .task {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(15))
                if held == nil { now = Date() }
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: NSMenu.didBeginTrackingNotification)) { _ in
            held = current
        }
        .onReceive(NotificationCenter.default.publisher(for: NSMenu.didEndTrackingNotification)) { _ in
            held = nil
            now = Date()
        }
    }
}

/// The status bar's servers: this Mac and the paired ones, each with its
/// connection and tm; choosing one goes to it in the sidebar. Pair a
/// server last, as in the sidebar's foot.
struct ServersMenu: View {
    @Environment(AppModel.self) private var app
    let facts: StatusFacts

    var body: some View {
        StatusMenu(word: app.isConnected ? facts.server : "Not connected",
                   help: app.isConnected ? "This server and the ones paired with it" : "The bridge to tm is down",
                   spoken: "Servers, \(facts.servers)",
                   rows: { now in ServerList.of(connected: app.isConnected, version: app.hello?.tm ?? "", peers: app.peers, rtt: app.peerRTT, now: now) }) {
            BarIcon(symbol: "display")
        } items: { servers in
            ForEach(servers) { s in
                Button(s.text) { app.go(toServer: s.alias) }
            }
            if servers.count == 1 {
                Button("No paired servers") {}
                    .disabled(true)
            }
            Divider()
            Button("Pair a server") { app.pairServer = true }
        }
    }
}

/// The status bar's threads at work: its count, and a menu of them by
/// project, each with how long its turn has run, then the coordinators and
/// directors at work, which the count leaves out.
struct WorkingMenu: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let facts: StatusFacts

    var body: some View {
        StatusMenu(word: facts.workingWord, help: "Threads whose agents are working", spoken: "Working, \(facts.working)",
                   rows: { now in (list: WorkingList.of(projects: app.projects, sessions: app.sessions, peers: app.peers, programs: app.programs), now: now) }) {
            Dot(colour: facts.working > 0 ? theme.good : theme.faint)
        } items: { r in
            if r.list.isEmpty {
                Button("Nothing is working") {}
                    .disabled(true)
            }
            ForEach(r.list.threads) { g in
                Section(g.name) { rows(g.rows, r.now) }
            }
            if !r.list.roles.isEmpty {
                Section("Coordinators and directors") { rows(r.list.roles, r.now) }
            }
        }
    }

    private func rows(_ rows: [WorkingRow], _ now: Date) -> some View {
        ForEach(rows) { r in
            Button(r.text(now: now)) { app.select(r.selection) }
        }
    }
}

/// The status bar's needs: its count, and a menu of the needs, each
/// showing what it names; Next Need (⌥⌘↓) first.
struct NeedsMenu: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let facts: StatusFacts

    var body: some View {
        StatusMenu(word: facts.needsWord,
                   help: app.needs.isEmpty ? "Nothing needs you" : "\(facts.needsWord): ⌥⌘↓ shows the next",
                   spoken: "Needs you, \(app.needs.count)",
                   rows: { _ in app.needs }) {
            Dot(colour: app.needs.first.map { theme.colour($0.kind) } ?? theme.faint)
        } items: { needs in
            Button("Next Need") { app.stepNeed(1) }
                .disabled(needs.isEmpty)
            if !needs.isEmpty {
                Divider()
                ForEach(needs) { n in
                    Button("\(n.title) · \(n.detail.isEmpty ? n.project : n.detail)") {
                        app.open(project: n.project, session: n.session)
                    }
                }
            }
        }
    }
}

// MARK: dragging and the window

/// A column's edge, dragged: value follows the pointer, in range.
struct SplitHandle: View {
    let width: CGFloat
    @Binding var value: Double
    /// -1 for a column to the handle's right.
    let sign: Double
    let range: ClosedRange<Double>
    let name: String
    @State private var start: Double?

    var body: some View {
        Color.clear
            .frame(width: width)
            .frame(maxHeight: .infinity)
            .contentShape(Rectangle())
            .onHover { inside in
                if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() }
            }
            .gesture(
                DragGesture(minimumDistance: 1, coordinateSpace: .global)
                    .onChanged { d in
                        let from = start ?? value
                        start = from
                        value = min(range.upperBound, max(range.lowerBound, from + sign * d.translation.width)).rounded()
                    }
                    .onEnded { _ in start = nil }
            )
            .accessibilityHidden(true)
    }
}

/// Puts the window's three buttons where the reference has them: 22
/// apart, 14 from the window's left and 20 from its top. AppKit lays them
/// out again as the window changes, so this does it again when it does.
struct WindowButtons: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView { Placer() }
    func updateNSView(_ nsView: NSView, context: Context) {}

    private final class Placer: NSView {
        private var watching: [NSObjectProtocol] = []

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            watching.forEach(NotificationCenter.default.removeObserver)
            watching = []
            guard let window else { return }
            // After the pass that moved them, which would undo it.
            let again: @Sendable (Notification) -> Void = { [weak self] _ in
                DispatchQueue.main.async { MainActor.assumeIsolated { self?.place() } }
            }
            for name in [NSWindow.didResizeNotification, NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification,
                         NSWindow.didExitFullScreenNotification, NSWindow.didEndLiveResizeNotification] {
                watching.append(NotificationCenter.default.addObserver(forName: name, object: window, queue: .main, using: again))
            }
            for b in Self.kinds {
                guard let v = window.standardWindowButton(b) else { continue }
                v.postsFrameChangedNotifications = true
                watching.append(NotificationCenter.default.addObserver(forName: NSView.frameDidChangeNotification, object: v, queue: .main, using: again))
            }
            DispatchQueue.main.async { [weak self] in self?.place() }
        }

        private static let kinds: [NSWindow.ButtonType] = [.closeButton, .miniaturizeButton, .zoomButton]

        private func place() {
            guard let window, !window.styleMask.contains(.fullScreen) else { return }
            for (i, b) in Self.kinds.enumerated() {
                guard let v = window.standardWindowButton(b), let s = v.superview else { continue }
                let at = NSPoint(x: 14 + 22 * CGFloat(i), y: s.bounds.height - 19 - v.frame.height)
                if v.frame.origin != at { v.setFrameOrigin(at) }
            }
        }
    }
}

/// The window's line, which the reference has and macOS 26's own edge
/// doesn't draw, on the system's corner: 16 round (measured), 10 before.
struct WindowEdge: View {
    @Environment(\.theme) private var theme

    var body: some View {
        let radius: CGFloat = if #available(macOS 26, *) { 16 } else { 10 }
        RoundedRectangle(cornerRadius: radius)
            .strokeBorder(theme.line, lineWidth: 1)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

/// An empty part of the title row or the sidebar's head: it moves the
/// window, and a double click does what the user set for a title bar.
struct WindowDragArea: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView { Area() }
    func updateNSView(_ nsView: NSView, context: Context) {}

    private final class Area: NSView {
        override var mouseDownCanMoveWindow: Bool { false }

        override func mouseDown(with event: NSEvent) {
            if event.clickCount == 2 {
                let action = UserDefaults.standard.string(forKey: "AppleActionOnDoubleClick")
                if action == "Minimize" { window?.miniaturize(nil) } else if action != "None" { window?.zoom(nil) }
                return
            }
            window?.performDrag(with: event)
        }
    }
}

// MARK: pairing a server

/// The sidebar's Pair a server: pairing servers is tm's (tm peer pair and
/// join), which the app API doesn't carry, so this says how.
struct PairServerSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme

    var body: some View {
        FlatSheet(width: 480) {
            SheetTitle("Pair a server")
        } content: {
            SheetBody {
                Text("Pairing happens in tm, on both servers. On this one, run:")
                    .foregroundStyle(theme.muted)
                CommandText("tm peer pair --relay <relay> --as <name>")
                Text("It prints a code. On the other server, run:")
                    .foregroundStyle(theme.muted)
                CommandText("tm peer join <relay>/<code> --as <name>")
                Text("The other server then lists here under its name, with what it shares.")
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        } footer: {
            SheetEscape { dismiss() }
            Button("Done") { dismiss() }
                .buttonStyle(.flatPrimary)
                .keyboardShortcut(.defaultAction)
        }
    }
}

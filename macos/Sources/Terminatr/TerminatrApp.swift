import AppKit
import SwiftUI
import TerminatrKit

/// terminatr for Mac (design T734, T746): the coordinators and threads
/// of the tm server, through tm app bridge. One window shows them all: a
/// sidebar of the projects, their coordinators and threads, a project's
/// dashboard or a session beside it. The menu bar lists what needs you,
/// notifications tell it as it comes, and both show it in that window.
/// A session opens in a window of its own only when the user asks.
@main
struct TerminatrApp: App {
    @NSApplicationDelegateAdaptor private var delegate: AppDelegate

    static let mainWindow = "main"
    static let sessionWindow = "session"

    init() {
        Fonts.register()
    }

    var body: some Scene {
        Window("terminatr", id: Self.mainWindow) {
            MainWindow()
                .environment(delegate.model)
                .themed()
                .frame(minWidth: 980, minHeight: 540)
                .background(WindowFrame(name: "main"))
                .task { delegate.model.start() }
        }
        .defaultSize(width: 1280, height: 800)
        .commands {
            FileCommands(model: delegate.model)
            ViewCommands(model: delegate.model)
            GoCommands(model: delegate.model)
        }

        // Only Open in New Window opens one: no New Window command.
        WindowGroup("Session", id: Self.sessionWindow, for: SessionRef.self) { $ref in
            if let ref {
                SessionWindow(ref: ref)
                    .environment(delegate.model)
                    .themed()
                    .frame(minWidth: 760, minHeight: 480)
                    .task { delegate.model.start() }
            }
        }
        .defaultSize(width: 1100, height: 720)
        .commandsRemoved()

        MenuBarExtra {
            MenuBarView()
                .environment(delegate.model)
                .themed()
        } label: {
            MenuBarLabel()
                .environment(delegate.model)
        }
        .menuBarExtraStyle(.window)

        Settings {
            SettingsView()
                .environment(delegate.model)
        }
    }
}

/// Owns the model and the notifier from launch: a notification's button
/// can start the app, and its answer must find them.
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let model = AppModel()
    private(set) lazy var notifier = Notifier(model: model)

    func applicationWillFinishLaunching(_ notification: Notification) {
        _ = notifier
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Here, not before: SwiftUI makes the app a regular one as it
        // finishes launching.
        DockIcon.apply()
        Appearance.apply()
        model.start()
        Task {
            // The first-run sheet, once: not for one who answered macOS
            // about notifications already (an earlier release asked).
            let asked = await Notifier.permission().map { $0 != .notDetermined } ?? false
            if FirstRun.shows(done: UserDefaults.standard.bool(forKey: FirstRun.doneKey), notificationsAsked: asked) {
                model.welcome = true
            }
        }
    }

    /// Closing the window leaves the menu bar running.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }
}

/// Where the app shows (T748 F1): the menu bar and the Dock, with the
/// needs count as the Dock icon's badge; or the menu bar only, an
/// accessory app with no Dock icon.
enum DockIcon {
    static let key = "dockIcon"

    static var on: Bool {
        // bool(forKey:) reads a launch argument's "NO" too.
        UserDefaults.standard.object(forKey: key) == nil || UserDefaults.standard.bool(forKey: key)
    }

    @MainActor static func apply() {
        NSApp.setActivationPolicy(on ? .regular : .accessory)
    }
}

/// The File menu (T748 milestone G): New Task… (⌘N) adds a task to the
/// shown project's board, Open in New Window (⇧⌘O) opens the shown
/// session beside the main window, Close (⌘W) the key window.
struct FileCommands: Commands {
    let model: AppModel
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("New Task…") { model.showNewTask() }
                .keyboardShortcut("n")
                .disabled(!model.canAddTask || model.boardProject == nil)
            Button("Open in New Window") {
                if let ref = model.selectedRef { openWindow(id: TerminatrApp.sessionWindow, value: ref) }
            }
            .keyboardShortcut("o", modifiers: [.command, .shift])
            .disabled(model.selectedRef == nil)
        }
    }
}

/// The View menu's own items, after the sidebar's: the inspector,
/// Conversation or Terminal, the inactive projects, the sidebar's
/// density and the text size.
struct ViewCommands: Commands {
    let model: AppModel

    var body: some Commands {
        CommandGroup(after: .sidebar) {
            Button(model.inspector ? "Hide Inspector" : "Show Inspector") { model.inspector.toggle() }
                .keyboardShortcut("i", modifiers: [.command, .option])
                .disabled(model.selectedRef == nil)
            Button("Conversation or Terminal") { model.switchTab() }
                .keyboardShortcut("t", modifiers: [.command, .shift])
                .disabled(model.selectedRef == nil)
            Divider()
            Button(model.nav.showInactive ? "Hide Inactive Projects" : "Show Inactive Projects") { model.nav.showInactive.toggle() }
                .keyboardShortcut("h", modifiers: [.command, .shift])
                .disabled(model.inactiveProjects.isEmpty)
            DensityPicker(label: "Sidebar Density")
            Divider()
            Button("Make Text Bigger") { TextSize.change(by: 1) }
                .keyboardShortcut("+")
            Button("Make Text Smaller") { TextSize.change(by: -1) }
                .keyboardShortcut("-")
            Button("Actual Size") { TextSize.change(by: 0) }
                .keyboardShortcut("0")
            Divider()
        }
    }
}

/// The sidebar's density, a menu of three in View and a picker in
/// Settings.
struct DensityPicker: View {
    let label: String
    @AppStorage(Density.key) private var density = Density.regular.rawValue

    var body: some View {
        Picker(label, selection: $density) {
            ForEach(Density.allCases, id: \.self) { d in
                Text(d.label).tag(d.rawValue)
            }
        }
    }
}

/// The Go menu and the session's keys (design T746, T748 milestone E):
/// ⌘1–⌘9 the active projects, ⌘[ and ⌘] back and forward, ⌘K the
/// command palette, ⌥⌘↓ and ⌥⌘↑ the next and previous need, ⌘L the
/// composer, ⌘↩ the open card's first choice, ⌘F find in the
/// conversation, ⌥⌘F the sidebar's filter; Help has the tm docs and
/// Keyboard Shortcuts (⌘/), which lists them (Shortcuts).
struct GoCommands: Commands {
    let model: AppModel

    /// Help's pages of tm's docs.
    static let docs: [(String, String)] = [
        ("terminatr Guide", "https://github.com/theclifmeister/terminatr#readme"),
        ("tm Reference", "https://github.com/theclifmeister/terminatr/blob/main/docs/SPEC.md"),
        ("Operations", "https://github.com/theclifmeister/terminatr/blob/main/docs/OPERATIONS.md"),
        ("Release Notes", "https://github.com/theclifmeister/terminatr/releases"),
    ]

    var body: some Commands {
        CommandGroup(replacing: .textEditing) {
            Button("Find in Conversation…") { model.findInConversation() }
                .keyboardShortcut("f")
                .disabled(model.selectedRef == nil || model.palette)
            Button("Find Next") { model.findInConversation(1) }
                .keyboardShortcut("g")
                .disabled(model.selectedRef == nil || model.palette)
            Button("Find Previous") { model.findInConversation(-1) }
                .keyboardShortcut("g", modifiers: [.command, .shift])
                .disabled(model.selectedRef == nil || model.palette)
            Button("Filter Sidebar") { model.focusFilter() }
                .keyboardShortcut("f", modifiers: [.command, .option])
        }
        CommandGroup(replacing: .help) {
            Button("Getting Started") { model.welcome = true }
            ForEach(Self.docs, id: \.0) { title, url in
                Button(title) {
                    if let u = URL(string: url) { NSWorkspace.shared.open(u) }
                }
            }
            Divider()
            Button("Keyboard Shortcuts") { model.shortcuts = true }
                .keyboardShortcut("/")
        }
        CommandMenu("Go") {
            Button("Command Palette…") { model.palette.toggle() }
                .keyboardShortcut("k")
            Divider()
            Button("Next Need") { model.stepNeed(1) }
                .keyboardShortcut(.downArrow, modifiers: [.command, .option])
                .disabled(model.needs.isEmpty)
            Button("Previous Need") { model.stepNeed(-1) }
                .keyboardShortcut(.upArrow, modifiers: [.command, .option])
                .disabled(model.needs.isEmpty)
            // Off while the palette searches: its own ⌘↩ acts on its row.
            Button("Take First Choice") { model.takeFirstChoice() }
                .keyboardShortcut(.return)
                .disabled(model.palette ? !model.paletteActing : !model.openCard)
            Divider()
            Button("Back") { model.goBack() }
                .keyboardShortcut("[")
                .disabled(!model.nav.canGoBack)
            Button("Forward") { model.goForward() }
                .keyboardShortcut("]")
                .disabled(!model.nav.canGoForward)
            Divider()
            Button("Composer") { model.focusComposer() }
                .keyboardShortcut("l")
                .disabled(model.selectedRef == nil)
            Divider()
            ForEach(Array(model.activeProjects.prefix(9).enumerated()), id: \.element.slug) { i, p in
                Button(p.slug) { model.selectProject(i) }
                    .keyboardShortcut(KeyEquivalent(Character("\(i + 1)")))
            }
        }
    }
}

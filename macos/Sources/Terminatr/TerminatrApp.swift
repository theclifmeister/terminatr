import AppKit
import ServiceManagement
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
                .frame(minWidth: 980, minHeight: 540)
                .task { delegate.model.start() }
        }
        .defaultSize(width: 1280, height: 800)
        .commands { GoCommands(model: delegate.model) }

        // Only Open in New Window opens one: no New Window command.
        WindowGroup("Session", id: Self.sessionWindow, for: SessionRef.self) { $ref in
            if let ref {
                SessionWindow(ref: ref)
                    .environment(delegate.model)
                    .frame(minWidth: 760, minHeight: 480)
                    .task { delegate.model.start() }
            }
        }
        .defaultSize(width: 1100, height: 720)
        .commandsRemoved()

        MenuBarExtra {
            MenuBarView()
                .environment(delegate.model)
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
        model.start()
        Task { await notifier.authorize() }
    }

    /// Closing the window leaves the menu bar running.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }
}

/// The Go menu and the session's keys (design T746): ⌘1–⌘9 the active
/// projects, ⌘[ and ⌘] back and forward, ⌘L the composer, ⌘⇧T
/// Conversation or Terminal, ⌘⌥I the inspector, ⇧⌘H the inactive
/// projects.
struct GoCommands: Commands {
    let model: AppModel

    var body: some Commands {
        CommandGroup(after: .sidebar) {
            Button(model.inspector ? "Hide Inspector" : "Show Inspector") { model.inspector.toggle() }
                .keyboardShortcut("i", modifiers: [.command, .option])
                .disabled(model.selectedRef == nil)
            Button("Conversation or Terminal") { model.switchTab() }
                .keyboardShortcut("t", modifiers: [.command, .shift])
                .disabled(model.selectedRef == nil)
            Button(model.nav.showInactive ? "Hide Inactive Projects" : "Show Inactive Projects") { model.nav.showInactive.toggle() }
                .keyboardShortcut("h", modifiers: [.command, .shift])
                .disabled(model.inactiveProjects.isEmpty)
        }
        CommandMenu("Go") {
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

/// Where the app finds tm, what notifies, and open at login.
struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @AppStorage(AppModel.tmPathKey) private var tmPath = ""
    @AppStorage(Notifier.showTextKey) private var showText = false
    @AppStorage(Notifier.noticesKey) private var notices = true

    var body: some View {
        Form {
            Section("terminatr") {
                TextField("tm path", text: $tmPath, prompt: Text("/opt/homebrew/bin/tm"))
                Text("Empty: the Homebrew prefixes, then ~/.local/bin.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Button("Reconnect") { model.connect() }
                LoginItemToggle()
            }
            Section {
                ForEach(Need.Kind.allCases, id: \.self) { k in
                    KindToggle(kind: k)
                }
                Toggle("Mods' notices", isOn: $notices)
            } header: {
                Text("Notify when")
            } footer: {
                if !Bundle.main.isApp {
                    Text("Notifications need the app bundle (make -C macos run), not swift run.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Section {
                Toggle("Show what agents ask", isOn: $showText)
            } footer: {
                Text("Off: a notification names the project and the kind of need, never the question's words. A question's options still show as its buttons.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .frame(width: 460)
    }
}

private struct KindToggle: View {
    let kind: Need.Kind
    @AppStorage private var on: Bool

    init(kind: Need.Kind) {
        self.kind = kind
        _on = AppStorage(wrappedValue: true, Notifier.key(kind))
    }

    var body: some View {
        Toggle(Self.label(kind), isOn: $on)
    }

    static func label(_ k: Need.Kind) -> String {
        switch k {
        case .question: "An agent asks a question"
        case .approval: "An agent needs approval"
        case .review: "A task is ready to review"
        case .blocked: "A task is blocked"
        case .checks: "A PR's checks fail"
        case .held: "A prompt queue is held"
        case .network: "A paired server needs you"
        }
    }
}

/// Open at login (SMAppService.mainApp): the user may have to allow it
/// in System Settings › Login Items. The server keeps its own launchd
/// job either way.
private struct LoginItemToggle: View {
    @State private var status = SMAppService.mainApp.status
    @State private var error: String?

    var body: some View {
        Toggle("Open at login", isOn: Binding(
            get: { status == .enabled || status == .requiresApproval },
            set: { set($0) }
        ))
        .disabled(!Bundle.main.isApp)
        if status == .requiresApproval {
            HStack {
                Text("Allow terminatr in Login Items to finish.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Button("Open Login Items") { SMAppService.openSystemSettingsLoginItems() }
                    .buttonStyle(.link)
            }
        }
        if let error {
            Text(error)
                .font(.caption)
                .foregroundStyle(.red)
        }
    }

    private func set(_ on: Bool) {
        do {
            if on {
                try SMAppService.mainApp.register()
            } else {
                try SMAppService.mainApp.unregister()
            }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        status = SMAppService.mainApp.status
    }
}

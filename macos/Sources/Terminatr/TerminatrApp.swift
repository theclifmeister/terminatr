import AppKit
import ServiceManagement
import SwiftUI
import TerminatrKit

/// terminatr for Mac (design T734): the coordinators and threads of the
/// tm server, through tm app bridge. The projects window lists the
/// projects and their threads; the menu bar lists what needs you, and
/// notifications tell it as it comes.
@main
struct TerminatrApp: App {
    @NSApplicationDelegateAdaptor private var delegate: AppDelegate

    static let projectsWindow = "projects"

    init() {
        Fonts.register()
    }

    var body: some Scene {
        Window("terminatr", id: Self.projectsWindow) {
            ProjectsView()
                .environment(delegate.model)
                .frame(minWidth: 720, minHeight: 440)
                .task { delegate.model.start() }
        }
        .windowStyle(.hiddenTitleBar)
        .defaultSize(width: 980, height: 620)

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

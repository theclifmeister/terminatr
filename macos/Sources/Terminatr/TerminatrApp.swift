import SwiftUI
import TerminatrKit

/// terminatr for Mac (design T734): the coordinators and threads of the
/// tm server, through tm app bridge. The projects window lists the
/// projects and their threads; each coordinator and thread opens in a
/// window of its own.
@main
struct TerminatrApp: App {
    @State private var model = AppModel()

    init() {
        Fonts.register()
    }

    var body: some Scene {
        Window("terminatr", id: "projects") {
            ProjectsView()
                .environment(model)
                .frame(minWidth: 720, minHeight: 440)
                .task { model.start() }
        }
        .windowStyle(.hiddenTitleBar)
        .defaultSize(width: 980, height: 620)

        WindowGroup("Session", id: "session", for: SessionRef.self) { $ref in
            SessionWindow(ref: $ref)
                .environment(model)
                .frame(minWidth: 860, minHeight: 520)
                .task { model.start() }
        }
        .windowStyle(.hiddenTitleBar)
        .defaultSize(width: 1240, height: 780)

        Settings {
            SettingsView()
                .environment(model)
        }
    }
}

/// Where the app finds tm: empty means the Homebrew prefixes, then
/// ~/.local/bin.
struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @AppStorage(AppModel.tmPathKey) private var tmPath = ""

    var body: some View {
        Form {
            TextField("tm path", text: $tmPath, prompt: Text("/opt/homebrew/bin/tm"))
            Text("Empty: the Homebrew prefixes, then ~/.local/bin.")
                .font(.caption)
                .foregroundStyle(.secondary)
            Button("Reconnect") { model.connect() }
        }
        .padding(20)
        .frame(width: 420)
    }
}

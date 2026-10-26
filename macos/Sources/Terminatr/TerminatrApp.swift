import SwiftUI
import TerminatrKit

/// terminatr for Mac (design T734): the coordinators and threads of the
/// tm server, through tm app bridge. This first window lists the
/// projects and their threads.
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

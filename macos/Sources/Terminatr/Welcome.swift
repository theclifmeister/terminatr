import AppKit
import SwiftUI
import TerminatrKit
import UserNotifications

/// The first-run sheet (T748 milestone H): tm found, or how to install
/// it; the server running; and what the notifications are for, before
/// macOS asks for them. It shows once (FirstRun.shows), and again from
/// Help › Getting Started.
struct FirstRunSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    @AppStorage(AppModel.tmPathKey) private var tmPath = ""

    var body: some View {
        let tm = Result { try TMLocator(configured: tmPath).locate() }
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Welcome to terminatr")
                    .font(theme.title(.sheetTitle, .heavy))
                Text("The app shows the projects, coordinators and threads of the tm server on this Mac, and tells you when an agent needs you.")
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            switch tm {
            case .success(let url):
                Step(mark: .done, title: "tm is installed") {
                    Text(url.path)
                        .font(theme.mono(.subheadline))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .textSelection(.enabled)
                }
            case .failure:
                Step(mark: .problem, title: "Install tm first") {
                    Text("The app runs the tm on this Mac. Install it with Homebrew:")
                        .foregroundStyle(theme.muted)
                    CommandText(FirstRun.homebrew)
                    Text("or with the install script, into ~/.local/bin:")
                        .foregroundStyle(theme.muted)
                    CommandText(FirstRun.script)
                    HStack {
                        Button("Look Again") { app.connect() }
                        SettingsLink { Text("Set Its Path…") }
                    }
                }
            }
            server(tmFound: (try? tm.get()) != nil)
            Step(mark: .info, title: "Notifications") {
                Text("terminatr tells you when an agent asks or needs approval, a task waits for review or is blocked, or a PR's checks fail: the project and the kind of need, not the agent's words. Settings chooses which.")
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                NotificationPermission()
            }
            HStack {
                Spacer()
                Button("Get Started") { dismiss() }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
            }
        }
        .font(theme.body(.body))
        .padding(24)
        .frame(width: 600)
        .onDisappear { UserDefaults.standard.set(true, forKey: FirstRun.doneKey) }
    }

    @ViewBuilder
    private func server(tmFound: Bool) -> some View {
        switch app.phase {
        case .connected(let h):
            Step(mark: .done, title: "The server is running") {
                Text("tm \(h.tm)" + (h.server.map { " · server \($0.version)" } ?? ""))
                    .font(theme.mono(.subheadline))
                    .foregroundStyle(theme.muted)
            }
        case .connecting:
            Step(mark: .busy, title: "Starting the server…") {
                Text("The app starts it when none runs, and it keeps running when the app quits.")
                    .foregroundStyle(theme.muted)
            }
        case .failed(let why):
            Step(mark: tmFound ? .problem : .waiting, title: tmFound ? "The server can't be reached" : "The server starts once tm is installed") {
                if tmFound {
                    Text(why)
                        .foregroundStyle(theme.muted)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                    Button("Retry") { app.connect() }
                    .accessibilityLabel("Retry the connection to terminatr")
                }
            }
        }
    }
}

/// One step of the first-run sheet: its mark, its title and what it
/// says under it.
private struct Step<Content: View>: View {
    enum Mark { case done, busy, problem, waiting, info }

    @Environment(\.theme) private var theme
    let mark: Mark
    let title: String
    @ViewBuilder let content: Content

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            icon
                .frame(width: 18)
                .accessibilityLabel(markWord)
            VStack(alignment: .leading, spacing: 6) {
                Text(title)
                    .font(theme.title(.headline, .bold))
                content
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(12)
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.edge, lineWidth: 1))
    }

    /// What VoiceOver says of the step's mark.
    private var markWord: String {
        switch mark {
        case .done: "Done"
        case .busy: "In progress"
        case .problem: "Problem"
        case .waiting: "Waiting"
        case .info: "Note"
        }
    }

    @ViewBuilder
    private var icon: some View {
        switch mark {
        case .done: Image(systemName: "checkmark.circle.fill").foregroundStyle(theme.good)
        case .busy: ProgressView().controlSize(.small)
        case .problem: Image(systemName: "exclamationmark.circle.fill").foregroundStyle(theme.attention)
        case .waiting: Image(systemName: "circle.dashed").foregroundStyle(theme.muted)
        case .info: Image(systemName: "bell.fill").foregroundStyle(theme.action)
        }
    }
}

/// Whether macOS lets the app notify, and the button that asks it.
struct NotificationPermission: View {
    @Environment(\.theme) private var theme
    @State private var status: UNAuthorizationStatus?
    @State private var known = false

    var body: some View {
        HStack(spacing: 8) {
            if !known {
                ProgressView().controlSize(.small)
            } else if let status {
                switch status {
                case .notDetermined:
                    Button("Allow Notifications…") {
                        Task {
                            _ = await Notifier.authorize()
                            await refresh()
                        }
                    }
                    .accessibilityLabel("Allow notifications from terminatr")
                    .accessibilityHint("macOS asks you next")
                    Text("macOS asks you next.")
                        .foregroundStyle(theme.muted)
                case .denied:
                    Text("Off in System Settings")
                        .foregroundStyle(theme.muted)
                    Button("Notification Settings…") {
                        let id = Bundle.main.bundleIdentifier ?? ""
                        if let url = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=\(id)") {
                            NSWorkspace.shared.open(url)
                        }
                    }
                    .accessibilityLabel("Open terminatr's notification settings")
                default:
                    Label("On", systemImage: "checkmark")
                        .foregroundStyle(theme.good)
                        .accessibilityLabel("Notifications are on")
                }
            } else {
                Text("They need the app bundle (make -C macos run).")
                    .foregroundStyle(theme.muted)
            }
        }
        .task { await refresh() }
    }

    private func refresh() async {
        status = await Notifier.permission()
        known = true
    }
}

/// Settings' row: whether notifications are on, and asking for them.
struct NotificationsRow: View {
    var body: some View {
        LabeledContent("Notifications") { NotificationPermission() }
    }
}

/// A command to run in a terminal: selectable, with a copy button.
struct CommandText: View {
    @Environment(\.theme) private var theme
    let command: String
    @State private var copied = false

    init(_ command: String) {
        self.command = command
    }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(command)
                .font(theme.mono(.subheadline))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(command, forType: .string)
                copied = true
            } label: {
                Image(systemName: copied ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(.borderless)
            .help("Copy")
            .accessibilityLabel("Copy \(command)")
            .accessibilityValue(copied ? "copied" : "")
        }
        .padding(8)
        .background(theme.command, in: RoundedRectangle(cornerRadius: 8))
    }
}

/// Over the window while the bridge is down after it connected: what
/// it shows stays, dimmed, under this (T748 milestone H).
struct ConnectionBanner: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: 10) {
            if case .failed(let why) = app.phase {
                Image(systemName: "bolt.horizontal.circle.fill")
                    .foregroundStyle(theme.attention)
                    .font(theme.body(.title3))
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Lost the connection to terminatr")
                        .font(theme.body(.body, .semibold))
                    Text("What shows is from before. \(why)")
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                        .lineLimit(2)
                        .textSelection(.enabled)
                }
                Spacer(minLength: 8)
                Button("Retry") { app.connect() }
                    .accessibilityLabel("Retry the connection to terminatr")
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
            } else {
                ProgressView().controlSize(.small)
                Text("Reconnecting to terminatr…")
                    .font(theme.body(.body, .semibold))
                Spacer(minLength: 8)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frosted(RoundedRectangle(cornerRadius: 12))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.edge, lineWidth: 1))
        .padding(.horizontal, 20)
        .padding(.top, 10)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Connection")
    }
}

/// The toolbar's word when this app and tm don't fit (Compatibility):
/// the whole message, and tm update, in its popover.
struct VersionBadge: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var open = false

    var body: some View {
        if let headline = app.compatibility.headline, let message = app.compatibility.message {
            Button { open.toggle() } label: {
                Label {
                    Text(headline)
                } icon: {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(theme.attention)
                        .accessibilityHidden(true)
                }
                .labelStyle(.titleAndIcon)
            }
            .help(message)
            .accessibilityLabel("\(headline): version mismatch")
            .accessibilityHint("Shows what doesn't fit and the command that fixes it")
            .popover(isPresented: $open, arrowEdge: .bottom) {
                VStack(alignment: .leading, spacing: 10) {
                    Text(message)
                        .fixedSize(horizontal: false, vertical: true)
                    CommandText("tm update")
                }
                .font(theme.body(.body))
                .padding(16)
                .frame(width: 320)
            }
        }
    }
}

/// A server with no projects (T748 H1): what a project is, the command
/// that makes one, and what comes after.
struct NoProjects: View {
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("No projects yet")
                .font(theme.title(.largeTitle, .heavy))
            Text("A project is a goal and the repositories its agents work in. Make one in a terminal:")
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            CommandText(FirstRun.newProject)
            Text("It shows here at once. Start its coordinator and tell it what to build: it plans the tasks and starts a thread for each.")
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Text("tm doctor checks that tm and an agent are set up.")
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.muted)
        }
        // A set width: with maxWidth its texts' ideal width made the
        // window's content taller than the window, the sidebar's top
        // out of sight.
        .frame(width: 500, alignment: .leading)
    }
}

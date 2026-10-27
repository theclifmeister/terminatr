import AppKit
import ServiceManagement
import SwiftUI
import TerminatrKit

/// Settings in tabs (T748 milestone G): General (where the app finds tm,
/// where it shows, open at login), Notifications, Appearance (theme,
/// sidebar density, text size) and Advanced (the bridge's log,
/// Reconnect).
struct SettingsView: View {
    enum Tab: String {
        case general, notifications, appearance, advanced
    }

    @AppStorage("settingsTab") private var tab = Tab.general.rawValue

    var body: some View {
        TabView(selection: $tab) {
            GeneralSettings()
                .tabItem { Label("General", systemImage: "gearshape") }
                .tag(Tab.general.rawValue)
            NotificationSettings()
                .tabItem { Label("Notifications", systemImage: "bell.badge") }
                .tag(Tab.notifications.rawValue)
            AppearanceSettings()
                .tabItem { Label("Appearance", systemImage: "paintpalette") }
                .tag(Tab.appearance.rawValue)
            AdvancedSettings()
                .tabItem { Label("Advanced", systemImage: "wrench.and.screwdriver") }
                .tag(Tab.advanced.rawValue)
        }
        .frame(width: 500)
    }
}

/// Where the app finds tm, where it shows, and open at login.
private struct GeneralSettings: View {
    @Environment(AppModel.self) private var model
    @AppStorage(AppModel.tmPathKey) private var tmPath = ""
    @AppStorage(DockIcon.key) private var dockIcon = true

    var body: some View {
        Form {
            Section {
                TextField("tm path", text: $tmPath, prompt: Text("/opt/homebrew/bin/tm"))
                    .onSubmit { model.connect() }
            } footer: {
                Text("Empty: the Homebrew prefixes, then ~/.local/bin. A new path takes effect with Reconnect (Advanced) or ↩.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Section {
                LoginItemToggle()
                Picker("Show terminatr in", selection: $dockIcon) {
                    Text("Menu bar and Dock").tag(true)
                    Text("Menu bar only").tag(false)
                }
                .onChange(of: dockIcon) {
                    DockIcon.apply()
                    model.showBadge()
                    // Leaving the Dock deactivates the app: Settings stays in front.
                    NSApp.activate()
                }
            } footer: {
                Text("In the Dock, the icon's badge counts what needs you.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// What notifies, what a notification says, and quiet times.
private struct NotificationSettings: View {
    @AppStorage(Notifier.showTextKey) private var showText = false
    @AppStorage(Notifier.noticesKey) private var notices = true

    var body: some View {
        Form {
            Section {
                NotificationsRow()
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
            Section {
                LabeledContent("Quiet times") {
                    Button("Focus Settings…") {
                        if let url = URL(string: "x-apple.systempreferences:com.apple.Focus-Settings.extension") {
                            NSWorkspace.shared.open(url)
                        }
                    }
                }
            } footer: {
                Text("terminatr follows macOS Focus: while a Focus is on, its notifications wait in Notification Centre unless that Focus allows terminatr.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// The theme (also View › Theme and the palette); light, dark or the
/// system's; the sidebar's density; the text size (also View › Make
/// Text Bigger ⌘+ and Smaller ⌘-).
private struct AppearanceSettings: View {
    @AppStorage(Appearance.key) private var appearance = Appearance.system.rawValue
    @AppStorage(ThemeName.key) private var theme = ThemeName.bloom.rawValue
    @AppStorage(TextSize.key) private var scale = TextSize.standard

    var body: some View {
        Form {
            Section {
                ThemePicker(selection: $theme)
            } header: {
                Text("Theme")
            } footer: {
                Text("\(ThemeName.of(theme).label): \(ThemeName.of(theme).mood.lowercased()). Every window, the menu bar and the terminal follow it.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Section {
                Picker("Appearance", selection: $appearance) {
                    ForEach(Appearance.allCases, id: \.self) { a in
                        Text(a.label).tag(a.rawValue)
                    }
                }
                .pickerStyle(.segmented)
                .onChange(of: appearance) { Appearance.apply() }
                DensityPicker(label: "Sidebar density")
            } footer: {
                Text("The theme in light or dark; System follows macOS.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Section {
                LabeledContent("Text size") {
                    HStack(spacing: 8) {
                        Text("A").font(.system(size: 10)).accessibilityHidden(true)
                        Slider(value: Binding(get: { TextSize.of(scale) }, set: { scale = $0 }),
                               in: TextSize.steps.first!...TextSize.steps.last!, step: 0.05)
                            .frame(width: 180)
                        Text("A").font(.system(size: 16)).accessibilityHidden(true)
                        Text(TextSize.label(TextSize.of(scale)))
                            .monospacedDigit()
                            .frame(width: 44, alignment: .trailing)
                            .foregroundStyle(.secondary)
                    }
                }
                HStack {
                    Spacer()
                    Button("Actual Size") { scale = TextSize.standard }
                        .disabled(TextSize.of(scale) == TextSize.standard)
                }
            } footer: {
                Text("Every text in the window and the menu bar, from \(TextSize.label(TextSize.steps.first!)) to \(TextSize.label(TextSize.steps.last!)). The terminal keeps its own size.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// The themes as swatches, each a small window of its own: the mesh, a
/// card with a title and a muted line, a filled pill and the status
/// dots, drawn in the appearance Settings is in.
struct ThemePicker: View {
    @Binding var selection: String

    var body: some View {
        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 10), count: 4), spacing: 12) {
            ForEach(ThemeName.allCases, id: \.self) { t in
                ThemeSwatch(name: t, selected: ThemeName.of(selection) == t) { selection = t.rawValue }
            }
        }
        .padding(.vertical, 4)
    }
}

struct ThemeSwatch: View {
    let name: ThemeName
    let selected: Bool
    let pick: () -> Void
    @Environment(\.colorSchemeContrast) private var contrast

    var body: some View {
        let theme = contrast == .increased ? Theme.named(name).increasedContrast() : Theme.named(name)
        Button(action: pick) {
            VStack(spacing: 5) {
                ThemePreview()
                    .environment(\.theme, theme)
                    .frame(height: 64)
                    .clipShape(RoundedRectangle(cornerRadius: 9))
                    .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(Color.primary.opacity(0.15), lineWidth: 1))
                    .padding(3)
                    .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(selected ? Color.accentColor : .clear, lineWidth: 2.5))
                Text(name.label)
                    .font(.callout.weight(selected ? .semibold : .regular))
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(name.mood)
        .accessibilityLabel("\(name.label): \(name.mood)")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A theme in little: its mesh, a frosted card with a title and a muted
/// line, a pill and the dots.
struct ThemePreview: View {
    @Environment(\.theme) private var theme

    var body: some View {
        ZStack {
            Backdrop()
            VStack(alignment: .leading, spacing: 3) {
                Text("Aa")
                    .font(theme.title(.headline, .heavy))
                    .foregroundStyle(theme.ink)
                theme.muted.frame(width: 40, height: 3).clipShape(Capsule())
                HStack(spacing: 4) {
                    Text("run")
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(theme.onAction)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(theme.action, in: Capsule())
                    ForEach(Array([theme.good, theme.attention, theme.approval].enumerated()), id: \.0) { _, c in
                        Circle().fill(c).frame(width: 6, height: 6)
                    }
                }
            }
            .padding(7)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 7))
            .background(theme.glass, in: RoundedRectangle(cornerRadius: 7))
            .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(theme.edge, lineWidth: 1))
            .padding(9)
        }
        .accessibilityHidden(true)
    }
}

/// The bridge's state and log, and Reconnect.
private struct AdvancedSettings: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Form {
            Section {
                LabeledContent("Bridge") {
                    Text(state)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.trailing)
                }
                HStack {
                    Spacer()
                    Button("Reconnect") { model.connect() }
                }
            } footer: {
                Text("Reconnect starts tm app bridge again: after a new tm path, or an update of tm.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Section {
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 3) {
                            ForEach(model.log.entries) { e in
                                Text("\(Self.time.string(from: e.time))  \(e.text)")
                                    .font(.system(.caption, design: .monospaced))
                                    .textSelection(.enabled)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .id(e.id)
                            }
                        }
                        .padding(6)
                    }
                    .frame(height: 160)
                    .onChange(of: model.log.entries.last?.id) { _, id in
                        if let id { proxy.scrollTo(id, anchor: .bottom) }
                    }
                }
                HStack {
                    Spacer()
                    Button("Copy Log") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(model.log.text { Self.time.string(from: $0) }, forType: .string)
                    }
                    .disabled(model.log.entries.isEmpty)
                }
            } header: {
                Text("Bridge log")
            }
        }
        .formStyle(.grouped)
        .fixedSize(horizontal: false, vertical: true)
    }

    private var state: String {
        switch model.phase {
        case .connecting: "Connecting…"
        case .connected(let h): "Connected to tm \(h.tm)" + (h.server.map { ", server \($0.version)" } ?? "")
        case .failed(let why): "Not connected: \(why)"
        }
    }

    private static let time: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm:ss"
        return f
    }()
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
        case .link: "A link waits for you (a held message, a file, a link request)"
        case .refusal: "The guard refused a call"
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

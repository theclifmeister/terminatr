import SwiftUI
import TerminatrKit
import TerminatrUI
import UIKit

/// Settings (T808): the theme, as the Mac's Settings › Appearance picks
/// it (ThemePicker, the same keys), and light, dark or the system's.
/// Text follows the phone's Dynamic Type, so there is no size of its
/// own. Paired with a server, it names the server, its link and the
/// phone's level on it, and ends with Forget this server (T843). From
/// the Projects tab's gear.
struct SettingsScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var typeSize
    @AppStorage(ThemeName.key) private var theme = ThemeName.bloom.rawValue
    @AppStorage(Appearance.key) private var appearance = Appearance.system.rawValue

    var body: some View {
        NavigationStack {
            Form {
                if case .server = model.source { ServerSection() }
                Section {
                    ThemePicker(selection: $theme, columns: typeSize.isAccessibilitySize ? 2 : 3)
                } header: {
                    Text("Theme")
                } footer: {
                    Text("\(ThemeName.of(theme).label): \(ThemeName.of(theme).mood.lowercased()).")
                }
                Section {
                    Picker("Appearance", selection: $appearance) {
                        ForEach(Appearance.allCases, id: \.self) { a in
                            Text(a.label).tag(a.rawValue)
                        }
                    }
                    .pickerStyle(.segmented)
                } header: {
                    Text("Appearance")
                } footer: {
                    Text("The theme in light or dark; System follows the phone. Text follows the phone's text size.")
                }
                if case .server = model.source { ForgetSection() }
            }
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .backdrop()
        }
    }
}

/// The app's content with the user's theme (Bloom unless they picked
/// another) in their light or dark, its text scaled with Dynamic Type,
/// and with Increase Contrast its opaque panels: what the Mac's Themed
/// does, the phone's text size standing in for Settings › Text size.
struct Themed: ViewModifier {
    @AppStorage(ThemeName.key) private var name = ThemeName.bloom.rawValue
    @AppStorage(Appearance.key) private var appearance = Appearance.system.rawValue
    @Environment(\.colorSchemeContrast) private var contrast
    @Environment(\.dynamicTypeSize) private var typeSize

    func body(content: Content) -> some View {
        let picked = Theme.named(ThemeName.of(name))
        var theme = contrast == .increased ? picked.increasedContrast() : picked
        theme.scale = Self.scale(typeSize)
        return content
            .environment(\.theme, theme)
            .tint(theme.action)
            .preferredColorScheme(Self.scheme(Appearance.of(appearance)))
    }

    /// The theme's fonts are sized in points (TextStyle), which Dynamic
    /// Type leaves alone: they take the body's growth at this size
    /// instead, 1 at the standard Large.
    static func scale(_ size: DynamicTypeSize) -> CGFloat {
        let traits = UITraitCollection(preferredContentSizeCategory: UIContentSizeCategory(size))
        return UIFontMetrics(forTextStyle: .body).scaledValue(for: 17, compatibleWith: traits) / 17
    }

    static func scheme(_ a: Appearance) -> ColorScheme? {
        switch a {
        case .system: nil
        case .light: .light
        case .dark: .dark
        }
    }
}

extension View {
    func themed() -> some View {
        modifier(Themed())
    }
}

/// The paired server: its name, its link and the phone's level on it.
struct ServerSection: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme

    var body: some View {
        Section {
            LabeledContent("Name", value: model.serverName)
            LabeledContent("Link") {
                let (icon, text) = model.linkWords
                HStack(spacing: 6) {
                    if model.link == .connecting {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: icon)
                            .foregroundStyle(model.link.down ? theme.attention : theme.action)
                            .accessibilityHidden(true)
                    }
                    Text(text)
                        .multilineTextAlignment(.trailing)
                }
            }
            LabeledContent("Level", value: level)
        } header: {
            Text("Server")
        } footer: {
            Text("What the phone may do there; tm device access on the computer changes it.")
        }
    }

    /// The level the link runs at, by its methods: write, answer or view.
    private var level: String {
        if model.writes { return "Answers and prompts" }
        if model.answers { return "Answers" }
        return model.link == .up ? "View only" : "—"
    }
}

/// Forget this server: a red button, the screen's last, asked again.
struct ForgetSection: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var forgetting = false

    var body: some View {
        Section {
            Button(role: .destructive) { forgetting = true } label: {
                Text("Forget this server")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .tint(.red)
            .controlSize(.large)
            .listRowInsets(EdgeInsets())
            .listRowBackground(Color.clear)
            .confirmationDialog("Forget \(model.serverName)?", isPresented: $forgetting, titleVisibility: .visible) {
                Button("Forget", role: .destructive) {
                    dismiss()
                    Task { await model.forget() }
                }
            } message: {
                Text("The phone forgets the server and its own key. To link again, pair again; on the computer, tm device remove takes the phone off.")
            }
        }
    }
}

extension PhoneModel.Link {
    /// Asleep or offline, or lost.
    var down: Bool {
        switch self {
        case .asleep, .lost: true
        case .connecting, .up: false
        }
    }
}

extension PhoneModel {
    /// The paired server's link in words, with its symbol.
    var linkWords: (String, String) {
        switch link {
        case .connecting: ("antenna.radiowaves.left.and.right", "Linking…")
        case .asleep: ("moon.zzz", "Asleep or offline")
        case .up: ("checkmark.circle.fill", "Linked")
        case .lost(let why): ("exclamationmark.triangle", "Lost: \(why)")
        }
    }
}

/// Opens Settings: Root's sheet, from the Projects tab's gear.
struct ShowSettings: Sendable {
    var action: @MainActor @Sendable () -> Void = {}

    @MainActor func callAsFunction() { action() }
}

private struct ShowSettingsKey: EnvironmentKey {
    static let defaultValue = ShowSettings()
}

extension EnvironmentValues {
    var showSettings: ShowSettings {
        get { self[ShowSettingsKey.self] }
        set { self[ShowSettingsKey.self] = newValue }
    }
}

/// The gear that opens Settings.
struct SettingsButton: View {
    @Environment(\.showSettings) private var showSettings

    var body: some View {
        Button { showSettings() } label: {
            Label("Settings", systemImage: "gearshape")
        }
    }
}

import SwiftUI
import TerminatrKit
import TerminatrUI
import UIKit

/// Settings (T808): the theme, as the Mac's Settings › Appearance picks
/// it (ThemePicker, the same keys), and light, dark or the system's.
/// Text follows the phone's Dynamic Type, so there is no size of its
/// own. From the Projects tab's gear and the link banner's menu.
struct SettingsScreen: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var typeSize
    @AppStorage(ThemeName.key) private var theme = ThemeName.bloom.rawValue
    @AppStorage(Appearance.key) private var appearance = Appearance.system.rawValue

    var body: some View {
        NavigationStack {
            Form {
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

/// Opens Settings: Root's sheet, from the Projects tab's gear and the
/// link banner's menu.
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

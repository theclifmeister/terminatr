import SwiftUI
import TerminatrKit
import TerminatrUI
import UIKit

/// Settings (T808; redesigned in T850): on the sheet's glass, the paired
/// server first (ServerCard) and its link's drops (Connection, T859; the
/// demo's made up), then the themes as large live previews
/// (ThemeCarousel, the same keys as the Mac's Settings › Appearance),
/// light, dark or the system's, the Haptics switch (T851), and with a
/// server Forget this server, red and last (T843). Text follows the
/// phone's Dynamic Type, so there is no size of its own. From the Projects tab's gear.
struct SettingsScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    @AppStorage(ThemeName.key) private var name = ThemeName.bloom.rawValue
    @AppStorage(Appearance.key) private var appearance = Appearance.system.rawValue
    @AppStorage(Haptics.key) private var haptics = true

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Spacing.row) {
                SheetHeader("Settings", trailing: {
                    Button("Done") { dismiss() }
                        .buttonStyle(.primary())
                        .fixedSize()
                        .keyboardShortcut(.defaultAction)
                })
                if case .server = model.source { ServerSection() }
                if model.source != .none { ConnectionSection() }
                GroupLabel("Theme")
                ThemeCarousel(selection: $name)
                note("\(ThemeName.of(name).label): \(ThemeName.of(name).mood.lowercased()).")
                GroupLabel("Appearance")
                Segments(Appearance.allCases.map { (id: $0.rawValue, label: $0.label) }, selection: $appearance)
                    .padding(.horizontal, Spacing.gutter)
                note("The theme in light or dark; System follows the phone. Text follows the phone's text size.")
                GroupLabel("Feedback")
                SwitchCard("Haptics", detail: "A tap on picking an option or a chip, and when the server answers.", isOn: $haptics)
                if case .server = model.source { ForgetSection() }
            }
            .frame(maxWidth: 640)
            .frame(maxWidth: .infinity)
            .padding(.bottom, 32)
        }
        .sheetGlass()
    }

    private func note(_ s: String) -> some View {
        Text(s)
            .font(theme.body(.footnote))
            .foregroundStyle(theme.muted)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, Spacing.gutter + 6)
    }
}

/// The app's content with the user's theme (Bloom unless they picked
/// another) in their light or dark, its text scaled with Dynamic Type,
/// and with Increase Contrast its opaque panels: what the Mac's Themed
/// does, the phone's text size standing in for Settings › Text size.
/// Settings' Haptics switch reaches every screen from here (\.haptics).
struct Themed: ViewModifier {
    @AppStorage(ThemeName.key) private var name = ThemeName.bloom.rawValue
    @AppStorage(Appearance.key) private var appearance = Appearance.system.rawValue
    @AppStorage(Haptics.key) private var haptics = true
    @Environment(\.colorSchemeContrast) private var contrast
    @Environment(\.dynamicTypeSize) private var typeSize

    func body(content: Content) -> some View {
        let picked = Theme.named(ThemeName.of(name))
        var theme = contrast == .increased ? picked.increasedContrast() : picked
        theme.scale = Self.scale(typeSize)
        theme.titleGrowth = 0.35
        return content
            .environment(\.theme, theme)
            .environment(\.haptics, haptics)
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
        VStack(alignment: .leading, spacing: Spacing.tight) {
            ServerCard(name: model.serverName, link: model.linkWords.1, linking: model.link == .connecting,
                       down: model.link.down, level: level.word, levelSpoken: level.spoken)
            Text("What the phone may do there; tm device access on the computer changes it.")
                .font(theme.body(.footnote))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 6)
        }
        .padding(.horizontal, Spacing.gutter)
    }

    /// The level the link runs at, by its methods: write, answer or view.
    private var level: (word: String, spoken: String) {
        if model.writes { return ("write", "Level: answers and prompts") }
        if model.answers { return ("answer", "Level: answers") }
        return model.link == .up ? ("view", "Level: view only") : ("", "")
    }
}

/// The link's last drops (LinkLog): when, why, and how long until it
/// was up again, under a line of the day's count by cause.
struct ConnectionSection: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    /// The drops shown; the log keeps LinkLog.max.
    static let shown = 6

    var body: some View {
        let drops = model.log.drops.prefix(Self.shown)
        GroupLabel("Connection")
        VStack(alignment: .leading, spacing: Spacing.tight) {
            Text(summary)
                .font(theme.body(.headline, .semibold))
                .foregroundStyle(theme.ink)
            if drops.isEmpty {
                Text("No drops since the link came up.")
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
            }
            ForEach(drops) { d in
                VStack(alignment: .leading, spacing: 1) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(d.at, format: .dateTime.hour().minute())
                            .font(theme.mono(.subheadline))
                            .foregroundStyle(theme.muted)
                        Text(d.cause.words)
                            .font(theme.body(.subheadline, .semibold))
                            .foregroundStyle(theme.ink)
                        Spacer(minLength: 8)
                        Text(Self.down(d))
                            .font(theme.body(.subheadline))
                            .foregroundStyle(theme.muted)
                    }
                    if !d.detail.isEmpty {
                        Text(d.detail)
                            .font(theme.body(.footnote))
                            .foregroundStyle(theme.muted)
                            .lineLimit(2)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Spacing.card)
        .padding(.vertical, Spacing.row)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
        .padding(.horizontal, Spacing.gutter)
    }

    /// "Linked · 3 drops today" and the like.
    private var summary: String {
        let state = model.source == .demo ? "Demo" : model.linkWords.1
        let n = model.log.drops.filter { Calendar.current.isDateInToday($0.at) }.count
        return n == 0 ? state : "\(state) · \(n) \(n == 1 ? "drop" : "drops") today"
    }

    /// How long the link was down: "back in 4 s", or "down" while it is.
    static func down(_ d: LinkLog.Drop) -> String {
        guard let s = d.down else { return "down" }
        if s < 60 { return "back in \(max(1, Int(s.rounded()))) s" }
        if s < 3600 { return "back in \(Int((s / 60).rounded())) min" }
        return "back in \(Int((s / 3600).rounded())) h"
    }
}

/// Forget this server: a red button, the screen's last, asked again.
struct ForgetSection: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var forgetting = false

    var body: some View {
        Button(role: .destructive) { forgetting = true } label: {
            Text("Forget this server")
        }
        .buttonStyle(.primary(.attention))
        .padding(.horizontal, Spacing.gutter)
        .padding(.top, Spacing.gutter)
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

import SwiftUI
import TerminatrUI

/// terminatr.dev's pages the app links to (T910): App Review 5.1.1(i)
/// asks for the privacy policy inside the app, as well as in App Store
/// Connect (AppStore/en-US/privacy_url.txt, the same address). The pages
/// are the site's (site/, T909).
enum Site {
    static let home = URL(string: "https://terminatr.dev")!
    static let privacy = URL(string: "https://terminatr.dev/privacy")!
    static let support = URL(string: "https://terminatr.dev/support")!
}

/// The app's version and build, "0.71.0 (2340)": the tm release it came
/// with and that tag's commit count (README.md, TestFlight).
enum AppVersion {
    static var words: String {
        let info = Bundle.main.infoDictionary ?? [:]
        let version = info["CFBundleShortVersionString"] as? String ?? "?"
        let build = info["CFBundleVersion"] as? String ?? "?"
        return "\(version) (\(build))"
    }
}

/// Settings → About (T910): the privacy policy, support and
/// terminatr.dev, each opening in Safari, and the version.
struct AboutSection: View {
    @Environment(\.theme) private var theme

    var body: some View {
        GroupLabel("About")
        VStack(spacing: 0) {
            row("Privacy policy", symbol: "hand.raised", url: Site.privacy)
            Divider().padding(.leading, Spacing.card)
            row("Support", symbol: "questionmark.circle", url: Site.support)
            Divider().padding(.leading, Spacing.card)
            row("terminatr.dev", symbol: "globe", url: Site.home)
            Divider().padding(.leading, Spacing.card)
            HStack {
                Text("Version")
                    .font(theme.body(.headline, .semibold))
                    .foregroundStyle(theme.ink)
                Spacer(minLength: 8)
                Text(AppVersion.words)
                    .font(theme.mono(.subheadline))
                    .foregroundStyle(theme.muted)
                    .textSelection(.enabled)
            }
            .padding(.horizontal, Spacing.card)
            .frame(minHeight: tapTarget)
            .accessibilityElement(children: .combine)
        }
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
        .padding(.horizontal, Spacing.gutter)
    }

    private func row(_ title: String, symbol: String, url: URL) -> some View {
        Link(destination: url) {
            HStack(spacing: Spacing.item) {
                Label(title, systemImage: symbol)
                    .font(theme.body(.headline, .semibold))
                    .foregroundStyle(theme.ink)
                Spacer(minLength: 8)
                Image(systemName: "arrow.up.right")
                    .font(theme.body(.footnote, .semibold))
                    .foregroundStyle(theme.muted)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, Spacing.card)
            .frame(minHeight: tapTarget)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityHint("Opens in Safari")
    }
}

/// The welcome screen's footer (T910): the privacy policy and support,
/// for someone who hasn't paired yet and so can't reach Settings.
struct WelcomeLinks: View {
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: Spacing.card) {
            Link("Privacy policy", destination: Site.privacy)
            Text("·").foregroundStyle(theme.muted).accessibilityHidden(true)
            Link("Support", destination: Site.support)
        }
        .font(theme.body(.footnote, .medium))
        .tint(theme.muted)
        .foregroundStyle(theme.muted)
        .frame(maxWidth: .infinity)
        .frame(minHeight: tapTarget)
    }
}

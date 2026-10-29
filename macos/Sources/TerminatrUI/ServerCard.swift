import SwiftUI
import TerminatrKit

/// The paired server, first in the phone's Settings (T850): a gradient
/// monogram, the server's name in the display face, its link as a dot
/// (or a spinner while it links) and words, and the device's level as a
/// mono chip on a wash of good. A deck card on the sheet's glass
/// ("a sheet card" in ThemeColours.checks).
public struct ServerCard: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let name: String
    let link: String
    let linking: Bool
    let down: Bool
    let level: String
    let levelSpoken: String

    /// level: the chip's word ("write", "answer", "view"), empty for
    /// none; levelSpoken says it to VoiceOver.
    public init(name: String, link: String, linking: Bool = false, down: Bool = false, level: String, levelSpoken: String) {
        self.name = name
        self.link = link
        self.linking = linking
        self.down = down
        self.level = level
        self.levelSpoken = levelSpoken
    }

    public var body: some View {
        let row = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: Spacing.row))
            : AnyLayout(HStackLayout(alignment: .center, spacing: Spacing.row))
        row {
            monogram
            VStack(alignment: .leading, spacing: 3) {
                Text(name)
                    .font(theme.title(.title, .bold))
                    .foregroundStyle(theme.ink)
                    .lines(1)
                HStack(spacing: Spacing.tight) {
                    if linking {
                        ProgressView().controlSize(.mini)
                    } else {
                        Circle()
                            .fill(down ? theme.attention : theme.good)
                            .frame(width: 8 * theme.scale, height: 8 * theme.scale)
                    }
                    Text(link)
                        .font(theme.body(.subheadline, .medium))
                        .foregroundStyle(theme.muted)
                        .lines(2)
                }
            }
            .accessibilityElement(children: .combine)
            if !typeSize.isAccessibilitySize { Spacer(minLength: 0) }
            if !level.isEmpty {
                Text(level)
                    .font(theme.mono(.caption, .medium))
                    .foregroundStyle(theme.ink)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 5)
                    .background(theme.wash(.done), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(theme.good.opacity(0.5), lineWidth: 1))
                    .fixedSize()
                    .accessibilityLabel(levelSpoken)
            }
        }
        .padding(Spacing.card)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.card, style: .continuous))
    }

    private var monogram: some View {
        let s = 46 * theme.scale
        return Image(systemName: "wave.3.right")
            .font(.system(size: s * 0.42, weight: .semibold))
            .foregroundStyle(theme.onAction)
            .frame(width: s, height: s)
            .background(LinearGradient(colors: [theme.good, theme.action], startPoint: .topLeading, endPoint: .bottomTrailing),
                        in: RoundedRectangle(cornerRadius: Radius.glyph * s / 38, style: .continuous))
            .accessibilityHidden(true)
    }
}

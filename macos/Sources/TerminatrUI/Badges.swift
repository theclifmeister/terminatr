import SwiftUI
import TerminatrKit

/// A state's pill (T845): its word in mono ("allow?", "asks",
/// "review"), the tone's ink on the tone. Idle's is ink on idle.
public struct ToneBadge: View {
    @Environment(\.theme) private var theme
    let word: String
    let tone: Attention.Tone

    public init(_ word: String, tone: Attention.Tone) {
        self.word = word
        self.tone = tone
    }

    public var body: some View {
        Text(word)
            .font(theme.mono(.caption, .semibold))
            .foregroundStyle(theme.on(tone))
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(theme.colour(tone), in: Capsule())
    }
}

/// A count in the attention colour, the system's red badge's place: a
/// tab's, a header's. Its digits roll when it changes.
public struct CountBadge: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    let count: Int
    let style: TextStyle

    public init(_ count: Int, style: TextStyle = .caption) {
        self.count = count
        self.style = style
    }

    public var body: some View {
        Text(count, format: .number)
            .font(theme.mono(style, .semibold))
            .monospacedDigit()
            .contentTransition(.numericText(value: Double(count)))
            .animation(still ? nil : .snappy, value: count)
            .foregroundStyle(theme.onAttention)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, theme.size(style) * 0.55)
            .padding(.vertical, theme.size(style) * 0.25)
            .frame(minWidth: theme.size(style) * 1.6)
            .background(theme.attention, in: Capsule())
    }
}

/// What waits in a project, on its card: the count in a round orb of
/// the attention colour with a soft halo.
public struct NeedsOrb: View {
    @Environment(\.theme) private var theme
    let count: Int

    public init(_ count: Int) {
        self.count = count
    }

    public var body: some View {
        let s = 34 * theme.scale
        Text(count, format: .number)
            .font(theme.mono(.title3, .semibold))
            .monospacedDigit()
            .contentTransition(.numericText(value: Double(count)))
            .foregroundStyle(theme.onAttention)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 6)
            .frame(minWidth: s, minHeight: s)
            .background(theme.attention, in: Capsule())
            .background(theme.wash(.attention, 0.22), in: Capsule().inset(by: -4))
            .accessibilityElement()
            .accessibilityLabel(count == 1 ? "1 waits on you" : "\(count) wait on you")
    }
}

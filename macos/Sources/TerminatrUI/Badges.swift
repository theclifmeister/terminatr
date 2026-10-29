import SwiftUI
import TerminatrKit

/// A state's pill (T845): its word in mono ("allow?", "asks",
/// "review"), the tone's ink on the tone. Idle's is ink on idle.
/// Compact, it fits under a header's title (SessionTitle).
public struct ToneBadge: View {
    @Environment(\.theme) private var theme
    let word: String
    let tone: Attention.Tone
    let compact: Bool

    public init(_ word: String, tone: Attention.Tone, compact: Bool = false) {
        self.word = word
        self.tone = tone
        self.compact = compact
    }

    public var body: some View {
        Text(word)
            .font(theme.mono(compact ? .caption2 : .caption, .semibold))
            .foregroundStyle(theme.on(tone))
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, compact ? 6 : 8)
            .padding(.vertical, compact ? 1 : 4)
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

/// A session's context use (T868): a small ring filled to it and its
/// percent in mono, muted, the approvals' amber from [ui] context_hint
/// and the attention red when nearly full (ContextUse.tone), as the
/// TUI's coordinator row warns. Compact, it fits under a header's
/// title. VoiceOver says "context 72% used".
public struct ContextGauge: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    let use: ContextUse
    let compact: Bool

    public init(_ use: ContextUse, compact: Bool = false) {
        self.use = use
        self.compact = compact
    }

    public var body: some View {
        let colour = use.tone.map { theme.colour($0) } ?? theme.muted
        let style: TextStyle = compact ? .caption2 : .caption
        let side = theme.size(style) * 0.95
        HStack(spacing: compact ? 3 : 4) {
            ZStack {
                Circle().stroke(theme.ink.opacity(ThemeColours.inkWash * 2), lineWidth: side * 0.2)
                Circle()
                    .trim(from: 0, to: use.fraction)
                    .stroke(colour, style: StrokeStyle(lineWidth: side * 0.2, lineCap: .round))
                    .rotationEffect(.degrees(-90))
            }
            .frame(width: side, height: side)
            Text(use.word)
                .font(theme.mono(style, use.tone == nil ? .medium : .semibold))
                .monospacedDigit()
                .contentTransition(.numericText(value: Double(use.percent)))
                .foregroundStyle(colour)
                .lineLimit(1)
        }
        .animation(still ? nil : .snappy, value: use.percent)
        .fixedSize()
        .padding(.horizontal, compact ? 5 : 8)
        .padding(.vertical, compact ? 1 : 4)
        .background(use.tone.map { theme.wash($0) } ?? theme.ink.opacity(ThemeColours.inkWash), in: Capsule())
        .accessibilityElement()
        .accessibilityLabel(use.spoken)
    }
}

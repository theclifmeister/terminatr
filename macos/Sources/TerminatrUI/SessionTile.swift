import SwiftUI
import TerminatrKit

/// A session's row (T846), in place of its dotted line: its StateGlyph,
/// its task ref in mono and its title, the why line under them, and at
/// the end what waits on the user as a ToneBadge ("allow?", "asks",
/// "review"), else how long its turn has run, else a PR mark, after its
/// context use as a ContextGauge when it reports one (T868). Rows sit
/// in a TileGroup, a hairline over each after the first (rule). At
/// accessibility sizes the glyph and the badge go over the title, which
/// keeps the row's width. Dormant: an inactive project's, all muted.
public struct SessionTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let label: String
    let ref: String?
    let state: Attention
    let busy: Busy?
    let pr: Int
    let context: ContextUse?
    let dormant: Bool
    let rule: Bool
    let selected: Bool

    public init(_ label: String, ref: String? = nil, state: Attention, busy: Busy? = nil, pr: Int = 0, context: ContextUse? = nil,
                dormant: Bool = false, rule: Bool = false, selected: Bool = false) {
        self.label = label
        self.ref = ref
        self.state = state
        self.busy = busy
        self.pr = pr
        self.context = context
        self.dormant = dormant
        self.rule = rule
        self.selected = selected
    }

    public var body: some View {
        let busy = dormant ? nil : busy
        let tone: Attention.Tone = dormant ? .idle : state.tone
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: Spacing.item) {
                    HStack(spacing: Spacing.row) {
                        StateGlyph(tone, filled: dormant ? false : nil, side: 32)
                        Spacer(minLength: 0)
                        end(busy)
                    }
                    words
                }
            } else {
                HStack(spacing: Spacing.row + 2) {
                    StateGlyph(tone, filled: dormant ? false : nil, side: 32)
                    words
                    Spacer(minLength: Spacing.hair)
                    end(busy)
                }
            }
        }
        .padding(.horizontal, Spacing.card)
        .padding(.vertical, Spacing.row)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
        .background(selected ? theme.wash(.working) : .clear)
        .overlay(alignment: .top) {
            if rule {
                Rectangle()
                    .fill(theme.line)
                    .frame(height: theme.contrast ? 1 : 0.5)
                    .padding(.leading, Spacing.card)
            }
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([ref, label].compactMap { $0 }.joined(separator: " "))
        .accessibilityValue(dormant ? "inactive" : [busy.map { $0.spoken(now: Date()) } ?? state.spoken, context?.spoken,
                                                     pr > 0 ? "PR \(pr)" : nil]
            .compactMap { $0 }.joined(separator: ", "))
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var words: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline, spacing: Spacing.tight) {
                if let ref {
                    Text(ref)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(dormant ? theme.muted : theme.action)
                }
                Text(label)
                    .font(theme.body(.body, .semibold))
                    .foregroundStyle(dormant ? theme.muted : theme.ink)
                    .lines(1)
            }
            if !state.why.isEmpty, !dormant {
                Text(state.why)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
                    .lines(1)
            }
        }
    }

    /// The context use, then what waits on the user, else the turn's
    /// time, else the PR.
    @ViewBuilder private func end(_ busy: Busy?) -> some View {
        if !dormant, let context {
            ContextGauge(context)
        }
        if !dormant, state.waits {
            ToneBadge(state.word, tone: state.tone)
        } else if let busy {
            TimelineView(.periodic(from: .now, by: 1)) { tl in
                if let t = busy.elapsed(now: tl.date) { quiet(t) }
            }
            .fixedSize()
        } else if !dormant, pr > 0 {
            quiet("PR")
        }
    }

    /// A quiet pill: muted mono on a wash of ink.
    private func quiet(_ text: String) -> some View {
        Text(text)
            .font(theme.mono(.caption, .medium))
            .monospacedDigit()
            .foregroundStyle(theme.muted)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(theme.ink.opacity(ThemeColours.inkWash), in: Capsule())
    }
}

/// A run of SessionTiles in one deck card (T846), clipped to its
/// corners.
public struct TileGroup<Content: View>: View {
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.tile, style: .continuous)
        VStack(spacing: 0) { content }
            .clipShape(shape)
            .surface(.card, in: shape)
    }
}

/// A count on a project's screen (T846): its numeral in the display
/// face over its word, on glass: "2 on deck".
public struct StatTile: View {
    @Environment(\.theme) private var theme
    let count: Int
    let label: String

    public init(_ count: Int, _ label: String) {
        self.count = count
        self.label = label
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(count, format: .number)
                .font(theme.title(.sheetTitle, .bold))
                .monospacedDigit()
                .contentTransition(.numericText(value: Double(count)))
                .foregroundStyle(theme.ink)
            Text(label)
                .font(theme.body(.footnote, .medium))
                .foregroundStyle(theme.muted)
                .lines(1)
        }
        .padding(.horizontal, Spacing.card - 2)
        .padding(.vertical, Spacing.row)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
        .surface(.glass, in: RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(count) \(label)")
    }
}

/// One of a project's pages (T846): its symbol in the action colour
/// over its name, on glass; at accessibility sizes beside it.
public struct PageTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let title: String
    let symbol: String
    let selected: Bool

    public init(_ title: String, symbol: String, selected: Bool = false) {
        self.title = title
        self.symbol = symbol
        self.selected = selected
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row, style: .continuous)
        let wide = typeSize.isAccessibilitySize
        let layout = wide ? AnyLayout(HStackLayout(spacing: Spacing.row)) : AnyLayout(VStackLayout(spacing: Spacing.tight))
        layout {
            Image(systemName: symbol)
                .font(theme.body(.title3, .medium))
                .foregroundStyle(theme.action)
                // One height for every symbol, so the names line up.
                .frame(height: theme.size(.title3) * 1.5)
                .accessibilityHidden(true)
            Text(title)
                .font(theme.body(.footnote, .semibold))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .padding(.horizontal, Spacing.tight)
        .padding(.vertical, Spacing.row)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: wide ? .leading : .center)
        .padding(.leading, wide ? Spacing.card - Spacing.tight : 0)
        .surface(.glass, in: shape)
        .overlay {
            if selected { shape.strokeBorder(theme.action, lineWidth: 2) }
        }
        .contentShape(shape)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// Tiles side by side, or one over another at accessibility sizes.
public struct TileRow<Content: View>: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(spacing: Spacing.item))
            : AnyLayout(HStackLayout(alignment: .top, spacing: Spacing.item))
        layout { content }
    }
}

/// A heading over a run of cards or tiles on the mesh (T846): its
/// title in the display face, large enough for the mesh (ThemeColours
/// .checks, "a header's title"), and a detail on glass: "Threads", "5
/// · 3 wait on you".
public struct SectionHeading: View {
    @Environment(\.theme) private var theme
    let title: String
    let detail: String

    public init(_ title: String, detail: String = "") {
        self.title = title
        self.detail = detail
    }

    public var body: some View {
        HStack(alignment: .center, spacing: Spacing.item) {
            Text(title)
                .font(theme.title(.title, .bold))
                .foregroundStyle(theme.ink)
                .lines(1)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            if !detail.isEmpty {
                Text(detail)
                    .font(theme.mono(.caption, .medium))
                    .foregroundStyle(theme.muted)
                    .lines(1)
                    .padding(.horizontal, Spacing.item + 1)
                    .padding(.vertical, Spacing.hair)
                    .surface(.glass, in: Capsule())
            }
        }
        .padding(.top, Spacing.item)
    }
}

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
/// A coordinator's or director's (role, T1003) has its RoleMark where a
/// thread's ref starts and its word in medium ink; a thread stays quieter,
/// its ref muted and its title regular. The context use ends the row, so
/// it and the status before it keep their places as the state changes.
public struct SessionTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let label: String
    let ref: String?
    let role: SessionRole?
    let state: Attention
    let busy: Busy?
    let pr: Int
    let context: ContextUse?
    let dormant: Bool
    let rule: Bool
    let selected: Bool

    public init(_ label: String, ref: String? = nil, role: SessionRole? = nil, state: Attention, busy: Busy? = nil, pr: Int = 0, context: ContextUse? = nil,
                dormant: Bool = false, rule: Bool = false, selected: Bool = false) {
        self.label = label
        self.ref = ref
        self.role = role
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
                if let role {
                    RoleMark(role, points: CGFloat(TextStyle.body.points) * 0.9, alignment: .leading, colour: dormant ? theme.muted : theme.ink)
                }
                if let ref {
                    Text(ref)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(theme.muted)
                }
                Text(label)
                    .font(theme.body(.body, role != nil ? .medium : .regular))
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

    /// What waits on the user, else the turn's time, else the PR; then
    /// the context use.
    @ViewBuilder private func end(_ busy: Busy?) -> some View {
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
        if !dormant, let context {
            ContextGauge(context)
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

/// A run of SessionTiles in one soft panel (T846, T996), clipped to its
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
            .surface(in: shape)
    }
}

/// A count on a project's screen (T846): its numeral in the display
/// face over its word, on a soft panel: "2 on deck".
public struct StatTile: View {
    @Environment(\.theme) private var theme
    /// nil: a count not known here (a paired server's board it doesn't
    /// share, T886), shown as –.
    let count: Int?
    let label: String

    public init(_ count: Int?, _ label: String) {
        self.count = count
        self.label = label
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Group {
                if let count {
                    Text(count, format: .number)
                        .contentTransition(.numericText(value: Double(count)))
                } else {
                    Text("–")
                }
            }
                .font(theme.title(.sheetTitle, .bold))
                .monospacedDigit()
                .foregroundStyle(count == nil ? theme.muted : theme.ink)
            Text(label)
                .font(theme.body(.footnote, .medium))
                .foregroundStyle(theme.muted)
                .lines(1)
        }
        .padding(.horizontal, Spacing.card - 2)
        .padding(.vertical, Spacing.row)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
        .surface(in: RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(count.map { "\($0) \(label)" } ?? "\(label), not shared")
    }
}

/// One of a project's pages, a cell of a PageRow (T846, T996): its symbol
/// in the ink over its name; at accessibility sizes beside it, a hairline
/// over each after the first (rule). Open beside the list, ringed in the
/// action colour.
public struct PageTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let title: String
    let symbol: String
    let selected: Bool
    let rule: Bool

    public init(_ title: String, symbol: String, selected: Bool = false, rule: Bool = false) {
        self.title = title
        self.symbol = symbol
        self.selected = selected
        self.rule = rule
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row, style: .continuous)
        let wide = typeSize.isAccessibilitySize
        let layout = wide ? AnyLayout(HStackLayout(spacing: Spacing.row)) : AnyLayout(VStackLayout(spacing: 2))
        layout {
            Image(systemName: symbol)
                .font(theme.body(.subheadline, .regular))
                .foregroundStyle(theme.ink)
                // One height for every symbol, so the names line up.
                .frame(height: theme.size(.subheadline) * 1.25)
                .accessibilityHidden(true)
            Text(title)
                .font(theme.body(.caption, .medium))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .padding(.horizontal, Spacing.hair)
        .padding(.vertical, 3)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: wide ? .leading : .center)
        .padding(.leading, wide ? Spacing.card - Spacing.hair : 0)
        .overlay {
            if selected { shape.strokeBorder(theme.action, lineWidth: 2) }
        }
        .overlay(alignment: .top) {
            if rule, wide { Hairline().padding(.leading, Spacing.card) }
        }
        .contentShape(shape)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A project's pages in one soft panel (T996): equal cells side by side,
/// or a list at accessibility sizes.
public struct PageRow<Content: View>: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(spacing: 0))
            : AnyLayout(HStackLayout(alignment: .top, spacing: 0))
        layout { content }
            .surface(in: RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
    }
}

/// The inset rule between a panel's rows.
struct Hairline: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Rectangle()
            .fill(theme.line)
            .frame(height: theme.contrast ? 1 : 0.5)
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

/// A heading over a run of cards or tiles (T846, T996): a small, quiet
/// uppercase label on the phone's veil, and a detail plain beside it:
/// "THREADS", "5 · 3 wait on you".
public struct SectionHeading: View {
    @Environment(\.theme) private var theme
    let title: String
    let detail: String

    public init(_ title: String, detail: String = "") {
        self.title = title
        self.detail = detail
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Spacing.item) {
            Text(title)
                .font(theme.body(.caption))
                .foregroundStyle(theme.muted)
                .lines(1)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            if !detail.isEmpty {
                Text(detail)
                    .font(theme.mono(.caption, .medium))
                    .foregroundStyle(theme.muted)
                    .lines(1)
            }
        }
        .padding(.top, Spacing.row)
    }
}

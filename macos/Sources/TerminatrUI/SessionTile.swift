import SwiftUI
import TerminatrKit

/// A session's row in a TileGroup (T846), to the design page's list
/// (T1001, T1008): its state's dot, its task ref in mono and its title, the why
/// line under them (the user's own, over the page), and at the end what waits
/// on the user as a Chip ("allow?", "asks", "review"), else Working and how
/// long its turn has run, else a PR mark, before its context use when it is
/// shown. A soft line over each
/// after the first (rule). At accessibility sizes the dot and the end go
/// over the title, which keeps the row's width. Dormant: an inactive
/// project's, all muted. A project's coordinator and a program's director
/// have their own row (LeadRow); a role here (a paired server's list) has
/// its RoleMark where a thread's ref starts and its word in medium ink.
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

    /// What can't be read on the selection's fill is ink there (ThemeColours.checks).
    private var quiet: Color { selected ? theme.ink : theme.muted }

    public var body: some View {
        let busy = dormant ? nil : busy
        let tone: Attention.Tone = dormant ? .idle : state.tone
        // Centred on its one line (`.list`); with a why, on the title's.
        let lift = 4 * theme.scale
        let two = !state.why.isEmpty && !dormant
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: Spacing.hair) {
                    HStack(spacing: Spacing.row) {
                        FlatDot(tone)
                        Spacer(minLength: 0)
                        end(busy)
                    }
                    words
                }
            } else {
                HStack(alignment: two ? .firstTextBaseline : .center, spacing: Spacing.row) {
                    FlatDot(tone)
                        .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + lift }
                    words
                    Spacer(minLength: Spacing.hair)
                    end(busy)
                }
            }
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 7)
        .frame(maxWidth: .infinity, minHeight: Flat.listRow * theme.scale, alignment: .leading)
        .background(selected ? theme.selection : .clear)
        .overlay(alignment: .top) {
            if rule { SoftLine() }
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
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .firstTextBaseline, spacing: Spacing.row) {
                if let role {
                    RoleMark(role, points: 12, alignment: .leading, colour: dormant ? theme.muted : theme.ink)
                }
                if let ref {
                    Text(ref)
                        .font(.system(size: 12.5 * theme.scale, design: .monospaced))
                        .foregroundStyle(quiet)
                }
                Text(label)
                    .font(theme.sized(Flat.listText, role != nil ? .medium : .regular))
                    .foregroundStyle(dormant ? theme.muted : theme.ink)
                    .lines(1)
            }
            if !state.why.isEmpty, !dormant {
                Text(state.why)
                    .font(theme.sized(12.5))
                    .foregroundStyle(quiet)
                    .lines(1)
                    .pageLeading(12.5)
            }
        }
    }

    /// What waits on the user, else the turn's time, else the PR; then
    /// the context use.
    @ViewBuilder private func end(_ busy: Busy?) -> some View {
        if !dormant, state.waits {
            Chip(state.word, colour: theme.colour(state.tone))
        } else if let busy {
            TimelineView(.periodic(from: .now, by: 1)) { tl in
                small(busy.elapsed(now: tl.date).map { "Working · \($0)" } ?? "Working")
            }
            .fixedSize()
        } else if !dormant, pr > 0 {
            Chip("PR")
        }
        if !dormant, let context {
            ContextGauge(context)
        }
    }

    private func small(_ text: String) -> some View {
        Text(text)
            .font(theme.sized(12.5))
            .monospacedDigit()
            .foregroundStyle(quiet)
            .lineLimit(1)
            .fixedSize()
    }
}

/// A run of SessionTiles in one flat panel (T846, T996, T1001), clipped to
/// its 8 round corners.
public struct TileGroup<Content: View>: View {
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        VStack(spacing: 0) { content }
            .panelGround()
    }
}

/// One of a project's pages, a cell of a PageRow (T846, T996, T1001): its
/// symbol in ink over its name at 11.5; at accessibility sizes beside it, a
/// soft line over each after the first (rule). Open beside the list, on the
/// selection's fill.
public struct PageTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let title: String
    let glyph: FlatGlyph
    let selected: Bool
    let rule: Bool

    public init(_ title: String, glyph: FlatGlyph, selected: Bool = false, rule: Bool = false) {
        self.title = title
        self.glyph = glyph
        self.selected = selected
        self.rule = rule
    }

    public var body: some View {
        let wide = typeSize.isAccessibilitySize
        let layout = wide ? AnyLayout(HStackLayout(spacing: Spacing.row)) : AnyLayout(VStackLayout(spacing: 3))
        layout {
            FlatIcon(glyph, side: 17)
                .foregroundStyle(theme.ink)
            Text(title)
                .font(theme.sized(11.5))
                .foregroundStyle(selected ? theme.ink : theme.muted)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .pageLeading(11.5)
        }
        .padding(.horizontal, Spacing.hair)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: wide ? .leading : .center)
        .padding(.leading, wide ? 13 - Spacing.hair : 0)
        .background(selected ? theme.selection : .clear)
        .overlay(alignment: .top) {
            if rule, wide { SoftLine() }
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A project's pages in one flat panel (T996, T1001): equal cells side by
/// side, or a list at accessibility sizes.
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
            .panelGround()
    }
}

/// The line between a list's rows, across the whole row (`.list`).
struct SoftLine: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Rectangle()
            .fill(theme.contrast ? theme.line : theme.lineSoft)
            .frame(height: 1)
    }
}

/// The inset rule between a panel's rows.
struct Hairline: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Rectangle()
            .fill(theme.contrast ? theme.line : theme.lineSoft)
            .frame(height: 1)
    }
}

/// A heading over a run of cards or tiles (T846, T996, T1001): a 12 muted
/// label in sentence case, its detail after it on the one line:
/// "Threads · 5 · 3 wait on you".
public struct SectionHeading: View {
    @Environment(\.theme) private var theme
    let title: String
    let detail: String

    public init(_ title: String, detail: String = "") {
        self.title = title
        self.detail = detail
    }

    public var body: some View {
        Text(detail.isEmpty ? title : "\(title) · \(detail)")
            .font(theme.sized(12))
            .foregroundStyle(theme.muted)
            .lines(1)
            .pageLeading(12)
            .accessibilityAddTraits(.isHeader)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.top, Spacing.item)
    }
}

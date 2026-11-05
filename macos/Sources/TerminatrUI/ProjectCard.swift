import SwiftUI
import TerminatrKit

/// A project's monogram (T846): its letters (Monogram.letters) in the
/// display face, the action's ink on the action's colour.
public struct ProjectMonogram: View {
    @Environment(\.theme) private var theme
    let name: String
    /// The side at 100%; the theme's scale grows it with Dynamic Type.
    let side: CGFloat

    public init(_ name: String, side: CGFloat = 40) {
        self.name = name
        self.side = side
    }

    public var body: some View {
        let s = side * theme.scale
        Text(Monogram.letters(name))
            .font(theme.title(side >= 48 ? .sheetTitle : side >= 40 ? .title2 : .headline, .bold))
            .foregroundStyle(theme.onAction)
            .lineLimit(1)
            .frame(width: s, height: s)
            .background(
                theme.action,
                in: RoundedRectangle(cornerRadius: Radius.glyph * side / 38, style: .continuous)
            )
            .accessibilityHidden(true)
    }
}

/// A project's threads at a glance (T846): one segment per thread in
/// its tone, sorted as Attention.Tone.order, and under it a legend of
/// the counts with a dot of each tone, so the words carry the state and
/// not colour alone. The segments slide as states change (still with
/// Reduce Motion). At accessibility sizes the legend is a list.
public struct ThreadMeter: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.accessibilityReduceMotion) private var still
    let tones: [Attention.Tone]

    public init(_ tones: [Attention.Tone]) {
        self.tones = tones
    }

    public var body: some View {
        let tally = Attention.Tone.tally(tones)
        VStack(alignment: .leading, spacing: Spacing.item) {
            if tones.isEmpty {
                Text("No threads")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            } else {
                HStack(spacing: 3) {
                    ForEach(Array(tally.flatMap { t in Array(repeating: t.tone, count: t.count) }.enumerated()), id: \.offset) { _, t in
                        Capsule()
                            .fill(theme.colour(t))
                            .frame(maxWidth: .infinity)
                    }
                }
                .frame(height: 6 * theme.scale)
                .animation(still ? nil : .spring(response: 0.4, dampingFraction: 0.85), value: tones)
                let legend = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: Spacing.hair))
                    : AnyLayout(Flow(spacing: Spacing.row))
                legend {
                    ForEach(tally, id: \.tone) { t in
                        HStack(spacing: 5) {
                            Circle()
                                .fill(theme.colour(t.tone))
                                .frame(width: 7 * theme.scale, height: 7 * theme.scale)
                            Text(t.tone.counted(t.count))
                                .lineLimit(1)
                        }
                        .font(theme.body(.footnote, .medium))
                        .foregroundStyle(theme.muted)
                    }
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Threads")
        .accessibilityValue(tones.isEmpty ? "none" : tally.map { "\($0.count) \($0.tone.spoken)" }.joined(separator: ", "))
    }
}

/// A project on the Projects screen (T846), a row of a ProjectPanel
/// (T996): its monogram, its name in the display face (a pin when
/// pinned), its coordinator's state with the glyph's symbol or, working,
/// the turning mark, what waits on the user as a NeedsOrb, and its
/// threads' ThreadMeter. Compact (T881): the monogram two lines tall,
/// and without threads no meter, "no threads" ending the coordinator's
/// line instead. Dim: a paired server's project it can't reach, or an
/// inactive one. Selected: open beside the list, ringed in the action
/// colour (projectRow). At accessibility sizes the orb goes over the name.
public struct ProjectCard: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let name: String
    let coordinator: Attention
    let needs: Int
    let threads: [Attention.Tone]
    let pinned: Bool
    let dim: Bool
    let selected: Bool
    let rule: Bool
    /// A dim word after the name: what holds it ("· acme", its program)
    /// or why it doesn't open.
    let tag: String

    public init(name: String, coordinator: Attention, needs: Int, threads: [Attention.Tone], pinned: Bool = false,
                dim: Bool = false, selected: Bool = false, rule: Bool = false, tag: String = "") {
        self.name = name
        self.tag = tag
        self.coordinator = coordinator
        self.needs = needs
        self.threads = threads
        self.pinned = pinned
        self.dim = dim
        self.selected = selected
        self.rule = rule
    }

    public var body: some View {
        let stacked = typeSize.isAccessibilitySize
        VStack(alignment: .leading, spacing: Spacing.item) {
            HStack(alignment: stacked ? .top : .center, spacing: Spacing.row) {
                ProjectMonogram(name, side: 32)
                if !stacked { title }
                Spacer(minLength: 0)
                if needs > 0 { NeedsOrb(needs) }
            }
            if stacked { title }
            if !threads.isEmpty { ThreadMeter(threads) }
        }
        .opacity(dim ? 0.55 : 1)
        .projectRow(selected: selected, rule: rule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([name, tag.isEmpty ? nil : tag, pinned ? "pinned" : nil].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue(spoken)
    }

    private var title: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(spacing: Spacing.tight) {
                Text(name)
                    .font(theme.title(.title3, .bold))
                    .foregroundStyle(theme.ink)
                    .lines(1)
                if !tag.isEmpty {
                    Text("· " + tag)
                        .font(theme.body(.subheadline, .medium))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                        .fixedSize()
                }
                if pinned {
                    Image(systemName: "pin.fill")
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.muted)
                }
            }
            HStack(spacing: Spacing.tight) {
                if coordinator.tone == .working {
                    BusyMark(size: 6 * theme.scale)
                } else {
                    Image(systemName: coordinator.tone.symbol)
                        .foregroundStyle(coordinator.tone == .idle ? theme.muted : theme.colour(coordinator.tone))
                }
                // The state is cut before "no threads" is; at accessibility
                // sizes one text, wrapped at its words.
                Group {
                    if typeSize.isAccessibilitySize {
                        Text(line + (threads.isEmpty ? " · no threads" : ""))
                            .lines(1)
                    } else {
                        HStack(spacing: 0) {
                            Text(line)
                                .lines(1)
                            if threads.isEmpty {
                                Text(" · no threads")
                                    .lineLimit(1)
                                    .fixedSize()
                            }
                        }
                    }
                }
                .foregroundStyle(theme.muted)
            }
            .font(theme.body(.subheadline))
        }
    }

    /// "Coordinator · reading T15's report", else its state.
    private var line: String {
        "Coordinator · " + (coordinator.why.isEmpty ? coordinator.word : coordinator.why)
    }

    private var spoken: String {
        let threads = Attention.Tone.tally(threads).map { "\($0.count) \($0.tone.spoken)" }
        let threadWords = threads.isEmpty ? "no threads" : "threads: " + threads.joined(separator: ", ")
        return ["coordinator \(coordinator.spoken)", needs > 0 ? "\(needs) need you" : nil, threadWords]
            .compactMap { $0 }.joined(separator: "; ")
    }
}

/// The rows of a group on the Projects screen and a program's page, in
/// one soft panel (T996).
public struct ProjectPanel<Content: View>: View {
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        VStack(spacing: 0) { content }
            .padding(Spacing.hair)
            .surface(in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }
}

extension View {
    /// A ProjectPanel's row: its padding and tap target, the ring when
    /// selected, inside the panel's corners, and an inset hairline over it
    /// (rule) lined up with the row's words.
    public func projectRow(selected: Bool, rule: Bool) -> some View {
        modifier(ProjectRow(selected: selected, rule: rule))
    }
}

private struct ProjectRow: ViewModifier {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let selected: Bool
    let rule: Bool

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row, style: .continuous)
        // The monogram's side and its gap; stacked, the words start at the edge.
        let words = typeSize.isAccessibilitySize ? 0 : 32 * theme.scale + Spacing.row
        content
            .padding(Spacing.row)
            .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
            .overlay(alignment: .top) {
                if rule, !selected {
                    Hairline()
                        .padding(.leading, Spacing.row + words)
                        .padding(.trailing, Spacing.row)
                }
            }
            .overlay {
                if selected { shape.strokeBorder(theme.action, lineWidth: 2.5) }
            }
            .contentShape(shape)
    }
}

/// A project's screen's header (T846), in place of its title bar's
/// title: its monogram and name in the display face, the screen's one big
/// title (T996), and its goal as plain text under it (GoalLine).
public struct ProjectHeader: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let name: String
    let goal: String

    public init(_ name: String, goal: String = "") {
        self.name = name
        self.goal = goal
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: Spacing.item) {
            let head = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: Spacing.item))
                : AnyLayout(HStackLayout(alignment: .center, spacing: Spacing.row + 2))
            head {
                ProjectMonogram(name, side: 40)
                Text(name)
                    .font(theme.title(.display, .bold))
                    .foregroundStyle(theme.ink)
                    .lines(1)
                    .minimumScaleFactor(0.7)
                    .accessibilityAddTraits(.isHeader)
            }
            if !goal.isEmpty {
                GoalLine(goal: goal)
                    // Another project's goal starts folded.
                    .id(goal)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// A goal as plain muted text (T996): the most of it that fits two lines
/// (at accessibility sizes, its first sentence), a More after it when
/// that isn't all of it, which opens the whole in place.
private struct GoalLine: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.accessibilityReduceMotion) private var still
    let goal: String
    @State private var open = false

    /// Tall enough for More's row to reach the least tap target.
    private static let reach: CGFloat = 13

    var body: some View {
        let lead = GoalLead(goal)
        Group {
            if open {
                text(lead.whole)
            } else if typeSize.isAccessibilitySize {
                if lead.hasMore { more(lead.sentence) } else { text(lead.whole) }
            } else {
                // 3 × the size is between two lines' height and three's.
                LimitedHeight(limit: theme.size(.subheadline) * 3) {
                    ViewThatFits(in: .vertical) {
                        text(lead.whole)
                        if lead.hasMore { more(lead.sentence) }
                        ForEach(lead.cuts, id: \.self) { more($0) }
                    }
                }
            }
        }
        .font(theme.body(.subheadline, .medium))
        .foregroundStyle(theme.muted)
    }

    private func text(_ s: String) -> some View {
        Text(verbatim: s)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func more(_ s: String) -> some View {
        Button {
            withAnimation(still ? nil : .snappy) { open = true }
        } label: {
            Text("\(Text(verbatim: s)) \(Text("More").fontWeight(.semibold).foregroundStyle(theme.ink))")
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, Self.reach)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.vertical, -Self.reach)
        .accessibilityHint("Shows the whole goal")
    }
}

/// Offers its one view a height it needn't fill, so a ViewThatFits in it
/// has a height to fit and still takes only the height of what it picks.
private struct LimitedHeight: Layout {
    let limit: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        subviews.first?.sizeThatFits(ProposedViewSize(width: proposal.width, height: limit)) ?? .zero
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        subviews.first?.place(at: bounds.origin, proposal: ProposedViewSize(width: bounds.width, height: limit))
    }
}

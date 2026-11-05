import SwiftUI
import TerminatrKit

/// A project's monogram (T846): its letters (Monogram.letters), the
/// action's ink on the action's colour, 5 round at 24, as the design page's
/// tile.
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
            .font(theme.sized(side >= 30 ? side * 13 / 30 : side * 11 / 24, .semibold))
            .foregroundStyle(theme.onAction)
            .lineLimit(1)
            .frame(width: s, height: s)
            .background(theme.action, in: RoundedRectangle(cornerRadius: side >= 30 ? Radius.row : 5))
            .accessibilityHidden(true)
    }
}

/// A project on the Projects screen (T846), a row of a ProjectPanel
/// (T996), to the design page's measure (T1001): its monogram, its name (a
/// pin when pinned), its coordinator's line (RoleLine) with its threads in
/// words, and what waits on the user as a NeedsOrb. Dim: a paired server's
/// project it can't reach, or an inactive one. Selected: open beside the
/// list, on the selection's fill. At accessibility sizes the monogram and
/// the orb go over the words.
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
            HStack(spacing: Spacing.row) {
                ProjectMonogram(name, side: 24)
                if !stacked { title }
                Spacer(minLength: 0)
                if needs > 0 { NeedsOrb(needs) }
            }
            if stacked { title }
        }
        .opacity(dim ? 0.55 : 1)
        .projectRow(selected: selected, rule: rule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([name, tag.isEmpty ? nil : tag, pinned ? "pinned" : nil].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue(spoken)
    }

    private var title: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .firstTextBaseline, spacing: Spacing.hair) {
                Text(name)
                    .font(theme.sized(14.5, .medium))
                    .foregroundStyle(theme.ink)
                    .lines(1)
                    .pageLeading(14.5)
                if !tag.isEmpty {
                    Text(tag)
                        .font(theme.sized(12.5))
                        .foregroundStyle(selected ? theme.ink : theme.muted)
                        .lineLimit(1)
                        .fixedSize()
                }
                if pinned {
                    Image(systemName: "pin.fill")
                        .font(.system(size: 10 * theme.scale))
                        .foregroundStyle(selected ? theme.ink : theme.muted)
                }
            }
            RoleLine(.coordinator, state: coordinator, dot: .strongest(coordinator.tone, threads),
                     after: " · " + Attention.Tone.summary(threads), selected: selected)
        }
    }

    private var spoken: String {
        let threads = Attention.Tone.tally(threads).map { "\($0.count) \($0.tone.spoken)" }
        let threadWords = threads.isEmpty ? "no threads" : "threads: " + threads.joined(separator: ", ")
        return ["coordinator \(coordinator.spoken)", needs > 0 ? "\(needs) need you" : nil, threadWords]
            .compactMap { $0 }.joined(separator: "; ")
    }
}

/// The rows of a group on the Projects screen and a program's page, in
/// one flat panel (T996, T1001), clipped to its 8 round corners.
public struct ProjectPanel<Content: View>: View {
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        VStack(spacing: 0) { content }
            .panelGround()
    }
}

extension View {
    /// A panel's ground, 8 round: the pane's colour and its line behind its
    /// rows, as the design page's inset shadow, so a selected row's fill
    /// reaches the panel's edge over it.
    func panelGround() -> some View {
        modifier(PanelGround())
    }

    /// A ProjectPanel's row: 50 high at least, the selection's fill when
    /// selected, and a soft line over it (rule) lined up with the row's
    /// words.
    public func projectRow(selected: Bool, rule: Bool) -> some View {
        modifier(ProjectRow(selected: selected, rule: rule))
    }
}

private struct PanelGround: ViewModifier {
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: Radius.tile)
        content
            .background {
                ZStack {
                    shape.fill(theme.pane)
                    shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1)
                }
            }
            .clipShape(shape)
    }
}

private struct ProjectRow: ViewModifier {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let selected: Bool
    let rule: Bool

    func body(content: Content) -> some View {
        // The line starts 2 before the words (`.irow::before`, at 43); stacked,
        // the words start at the edge.
        let words = typeSize.isAccessibilitySize ? 0 : 24 * theme.scale + Spacing.row - 2
        content
            .padding(.horizontal, 11)
            .padding(.vertical, 7)
            .frame(maxWidth: .infinity, minHeight: 50 * theme.scale, alignment: .leading)
            .background(selected ? theme.selection : .clear)
            .overlay(alignment: .top) {
                if rule {
                    Hairline()
                        .padding(.leading, 11 + words)
                }
            }
            .contentShape(Rectangle())
    }
}

/// A project's screen's header (T846), in place of its title bar's
/// title: its monogram and name, the screen's one big title (T996), and
/// its goal as plain text under it (GoalLine).
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
                ProjectMonogram(name, side: 30)
                Text(name)
                    .font(theme.display(24))
                    .foregroundStyle(theme.ink)
                    .lines(1)
                    .minimumScaleFactor(0.7)
                    .pageLeading(24)
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
                LimitedHeight(limit: 13.5 * theme.scale * 3) {
                    ViewThatFits(in: .vertical) {
                        text(lead.whole)
                        if lead.hasMore { more(lead.sentence) }
                        ForEach(lead.cuts, id: \.self) { more($0) }
                    }
                }
            }
        }
        .font(theme.sized(13.5))
        .foregroundStyle(theme.muted)
        .pageLeading(13.5)
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
            Text("\(Text(verbatim: s)) \(Text("More").foregroundStyle(theme.action))")
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

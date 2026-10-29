import SwiftUI
import TerminatrKit

/// A project's monogram (T846): its letters (Monogram.letters) in the
/// display face, the prompt's ink on the prompt's gradient.
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
            .font(theme.title(side >= 48 ? .sheetTitle : .title2, .bold))
            .foregroundStyle(theme.onPrompt)
            .lineLimit(1)
            .frame(width: s, height: s)
            .background(
                LinearGradient(colors: theme.prompt, startPoint: .topLeading, endPoint: .bottomTrailing),
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

/// A project on the Projects screen (T846), in place of its row: a deck
/// card with its monogram, its name in the display face (a pin when
/// pinned), its coordinator's state with the glyph's symbol or, working,
/// the turning mark, what waits on the user as a NeedsOrb, and its
/// threads' ThreadMeter. Dim: a paired server's project it can't reach,
/// or an inactive one. Selected: open beside the list, ringed in the
/// action colour. At accessibility sizes the orb goes over the name.
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

    public init(name: String, coordinator: Attention, needs: Int, threads: [Attention.Tone], pinned: Bool = false,
                dim: Bool = false, selected: Bool = false) {
        self.name = name
        self.coordinator = coordinator
        self.needs = needs
        self.threads = threads
        self.pinned = pinned
        self.dim = dim
        self.selected = selected
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.card, style: .continuous)
        let stacked = typeSize.isAccessibilitySize
        VStack(alignment: .leading, spacing: Spacing.card) {
            HStack(alignment: stacked ? .top : .center, spacing: Spacing.row + 2) {
                ProjectMonogram(name)
                if !stacked { title }
                Spacer(minLength: 0)
                if needs > 0 { NeedsOrb(needs) }
            }
            if stacked { title }
            ThreadMeter(threads)
        }
        .padding(Spacing.card + 2)
        .frame(maxWidth: .infinity, alignment: .leading)
        .opacity(dim ? 0.55 : 1)
        .surface(.card, in: shape)
        .overlay {
            if selected { shape.strokeBorder(theme.action, lineWidth: 2.5) }
        }
        .contentShape(shape)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(pinned ? "\(name), pinned" : name)
        .accessibilityValue(spoken)
    }

    private var title: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: Spacing.tight) {
                Text(name)
                    .font(theme.title(.largeTitle, .bold))
                    .foregroundStyle(theme.ink)
                    .lines(1)
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
                Text(line)
                    .foregroundStyle(theme.muted)
                    .lines(1)
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

/// A project's screen's header (T846), in place of its title bar's
/// title: its monogram and name in the display face on the mesh, and
/// its goal on glass (the mesh is no place for small text).
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
        VStack(alignment: .leading, spacing: Spacing.row) {
            let head = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: Spacing.item))
                : AnyLayout(HStackLayout(alignment: .center, spacing: Spacing.card))
            head {
                ProjectMonogram(name, side: 52)
                Text(name)
                    .font(theme.title(.hero, .heavy))
                    .tracking(-0.5)
                    .foregroundStyle(theme.ink)
                    .lines(1)
                    .minimumScaleFactor(0.7)
                    .accessibilityAddTraits(.isHeader)
            }
            if !goal.isEmpty {
                Text(goal)
                    .font(theme.body(.subheadline, .medium))
                    .foregroundStyle(theme.muted)
                    .lines(2)
                    .padding(.horizontal, Spacing.row + 2)
                    .padding(.vertical, Spacing.item)
                    .surface(.glass, in: RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

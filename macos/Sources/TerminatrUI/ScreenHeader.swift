import SwiftUI
import TerminatrKit

/// A screen's header (T845), in place of the large-title bar: its
/// trailing circle buttons, the title in the display face with a count
/// of what waits, and a line on glass (the link's state). The title is
/// the one text on the raw mesh, large enough for it (ThemeColours.checks,
/// "a header's title"). At accessibility sizes the count goes under the
/// title.
public struct ScreenHeader<Trailing: View>: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let title: String
    let count: Int
    let subtitle: String
    let symbol: String?
    let trailing: Trailing

    public init(_ title: String, count: Int = 0, subtitle: String = "", symbol: String? = nil, @ViewBuilder trailing: () -> Trailing) {
        self.title = title
        self.count = count
        self.subtitle = subtitle
        self.symbol = symbol
        self.trailing = trailing()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: Spacing.item) {
            HStack(spacing: Spacing.item) {
                Spacer(minLength: 0)
                trailing
            }
            .frame(minHeight: tapTarget)
            let head = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: Spacing.tight))
                : AnyLayout(HStackLayout(alignment: .center, spacing: Spacing.row))
            head {
                Text(title)
                    .font(theme.title(.hero, .heavy))
                    .tracking(-0.5)
                    .foregroundStyle(theme.ink)
                    .lines(1)
                    .accessibilityAddTraits(.isHeader)
                if count > 0 {
                    CountBadge(count, style: .title3)
                        .accessibilityLabel(count == 1 ? "1 waits" : "\(count) wait")
                }
            }
            if !subtitle.isEmpty {
                Label {
                    Text(subtitle).lines(1)
                } icon: {
                    if let symbol { Image(systemName: symbol) }
                }
                .font(theme.body(.subheadline, .medium))
                .foregroundStyle(theme.muted)
                .padding(.horizontal, Spacing.row)
                .padding(.vertical, Spacing.tight)
                .surface(.glass, in: Capsule())
            }
        }
        .padding(.horizontal, Spacing.gutter)
        .padding(.bottom, Spacing.row)
    }
}

extension ScreenHeader where Trailing == EmptyView {
    public init(_ title: String, count: Int = 0, subtitle: String = "", symbol: String? = nil) {
        self.init(title, count: count, subtitle: subtitle, symbol: symbol) { EmptyView() }
    }
}

/// One of a header's filter chips: "1 approval", a tone's dot before
/// it; nil tone for "All".
public struct PulseChip<ID: Hashable>: Identifiable {
    public let id: ID
    public let label: String
    public let tone: Attention.Tone?

    public init(id: ID, label: String, tone: Attention.Tone? = nil) {
        self.id = id
        self.label = label
        self.tone = tone
    }
}

/// A header's filter chips (T845): a row of glass capsules, the picked
/// one filled in ink. It scrolls sideways, and at accessibility sizes
/// wraps instead. Picking one is a selection tap.
public struct PulseChips<ID: Hashable>: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let chips: [PulseChip<ID>]
    @Binding var selection: ID

    public init(_ chips: [PulseChip<ID>], selection: Binding<ID>) {
        self.chips = chips
        _selection = selection
    }

    public var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                Flow(spacing: Spacing.item) { row }
                    .padding(.horizontal, Spacing.gutter)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Spacing.item) { row }
                        .padding(.horizontal, Spacing.gutter)
                        .padding(.vertical, 2)
                }
            }
        }
        .feedback(.selection, trigger: selection)
    }

    private var row: some View {
        ForEach(chips) { c in
            let on = c.id == selection
            Button { selection = c.id } label: {
                HStack(spacing: 7) {
                    if let t = c.tone {
                        Circle()
                            .fill(theme.colour(t))
                            .frame(width: 8 * theme.scale, height: 8 * theme.scale)
                            .padding(3)
                            .background(theme.wash(t, 0.25), in: Circle())
                    }
                    Text(c.label)
                        .lineLimit(1)
                }
                .font(theme.body(.subheadline, .semibold))
                .foregroundStyle(on ? theme.base : theme.ink)
                .padding(.horizontal, 12)
                .frame(minHeight: tapTarget)
                .background { chip(on) }
                .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(on ? .isSelected : [])
        }
    }

    @ViewBuilder private func chip(_ on: Bool) -> some View {
        if on {
            Capsule().fill(theme.ink)
        } else {
            Color.clear.surface(.glass, in: Capsule())
        }
    }
}

/// Views in rows, each as wide as it needs, a new row when one is full.
struct Flow: Layout {
    var spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let rows = arrange(proposal.width ?? .infinity, subviews)
        let w = rows.map { $0.width }.max() ?? 0
        let h = rows.map(\.height).reduce(0, +) + spacing * CGFloat(max(rows.count - 1, 0))
        return CGSize(width: w, height: h)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for r in arrange(bounds.width, subviews) {
            var x = bounds.minX
            for i in r.items {
                let s = subviews[i].sizeThatFits(.unspecified)
                subviews[i].place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(s))
                x += s.width + spacing
            }
            y += r.height + spacing
        }
    }

    private struct Row {
        var items: [Int] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    private func arrange(_ width: CGFloat, _ subviews: Subviews) -> [Row] {
        var rows: [Row] = [Row()]
        for i in subviews.indices {
            let s = subviews[i].sizeThatFits(.unspecified)
            let gap = rows[rows.count - 1].items.isEmpty ? 0 : spacing
            if rows[rows.count - 1].width + gap + s.width > width, !rows[rows.count - 1].items.isEmpty {
                rows.append(Row())
            }
            let g = rows[rows.count - 1].items.isEmpty ? 0 : spacing
            rows[rows.count - 1].items.append(i)
            rows[rows.count - 1].width += g + s.width
            rows[rows.count - 1].height = max(rows[rows.count - 1].height, s.height)
        }
        return rows
    }
}

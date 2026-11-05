import SwiftUI
import TerminatrKit

/// A screen's header (T845), in place of the large-title bar: the title
/// in the display face with a count of what waits, its trailing circle
/// buttons centred on the title's line (T854), and the link's state as a
/// plain line under it (T996), on the phone's veil. A pane's title, below
/// a project's name (ProjectHeader) in size. At accessibility sizes the
/// count goes under the title.
public struct ScreenHeader<Trailing: View>: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let title: String
    let count: Int
    let subtitle: String
    let symbol: String?
    let gutter: CGFloat
    let top: CGFloat
    let bottom: CGFloat
    let trailing: Trailing

    public init(_ title: String, count: Int = 0, subtitle: String = "", symbol: String? = nil, gutter: CGFloat = Spacing.gutter,
                top: CGFloat = Spacing.row, bottom: CGFloat = Spacing.row, @ViewBuilder trailing: () -> Trailing) {
        self.gutter = gutter
        self.top = top
        self.bottom = bottom
        self.title = title
        self.count = count
        self.subtitle = subtitle
        self.symbol = symbol
        self.trailing = trailing()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .titleLine, spacing: Spacing.item) {
                let head = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: Spacing.tight))
                    : AnyLayout(HStackLayout(alignment: .center, spacing: Spacing.row))
                head {
                    Text(title)
                        .font(theme.display(22))
                        .foregroundStyle(theme.ink)
                        .lines(1)
                        .pageLeading(22)
                        .accessibilityAddTraits(.isHeader)
                        // Its first line's middle: wrapped at accessibility
                        // sizes, the lines below add what the baselines span.
                        .alignmentGuide(.titleLine) { d in
                            (d.height - (d[.lastTextBaseline] - d[.firstTextBaseline])) / 2
                        }
                    if count > 0 {
                        CountBadge(count, style: .caption)
                            .accessibilityLabel(count == 1 ? "1 waits" : "\(count) wait")
                    }
                }
                .layoutPriority(1)
                Spacer(minLength: 0)
                trailing
                    .alignmentGuide(.titleLine) { $0[VerticalAlignment.center] }
            }
            if !subtitle.isEmpty {
                // The title's line and this one's are 4 apart (`.sub`).
                HStack(spacing: Spacing.tight) {
                    if let symbol { Image(systemName: symbol) }
                    Text(subtitle).lines(1)
                }
                .font(theme.sized(13))
                .foregroundStyle(theme.muted)
                .pageLeading(13)
                .padding(.top, Spacing.hair)
            }
        }
        .padding(.horizontal, gutter)
        .padding(.top, top)
        .padding(.bottom, bottom)
    }
}

extension VerticalAlignment {
    /// The middle of a header's title's first line, its trailing
    /// buttons centred on it (T854).
    private enum TitleLine: AlignmentID {
        static func defaultValue(in d: ViewDimensions) -> CGFloat { d[VerticalAlignment.center] }
    }

    static let titleLine = VerticalAlignment(TitleLine.self)
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

/// A header's filter chips (T845, T1001): a row of 6 round chips, the
/// picked one filled in ink, 44 pt to tap. It scrolls sideways, and at accessibility sizes
/// wraps instead. Picking one is a selection tap.
public struct PulseChips<ID: Hashable>: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let chips: [PulseChip<ID>]
    @Binding var selection: ID
    let gutter: CGFloat

    public init(_ chips: [PulseChip<ID>], selection: Binding<ID>, gutter: CGFloat = Spacing.gutter) {
        self.chips = chips
        self.gutter = gutter
        _selection = selection
    }

    public var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                Flow(spacing: Spacing.item) { row }
                    .padding(.horizontal, gutter)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Spacing.item) { row }
                        .padding(.horizontal, gutter)
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
                    if let t = c.tone { FlatDot(t) }
                    Text(c.label)
                        .lineLimit(1)
                }
                .font(theme.sized(Flat.label))
                .foregroundStyle(on ? theme.pane : theme.ink)
                .padding(.horizontal, 10)
                .frame(minHeight: 30 * theme.scale)
                .background { chip(on) }
                .padding(.vertical, 7)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(on ? .isSelected : [])
        }
    }

    @ViewBuilder private func chip(_ on: Bool) -> some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        if on {
            shape.fill(theme.ink)
        } else {
            Color.clear.surface(in: shape)
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
            // A point of slack: placed, its bounds can come out a hair
            // under the width it measured itself at, and the last item
            // would wrap onto a line its height never counted.
            if rows[rows.count - 1].width + gap + s.width > width + 1, !rows[rows.count - 1].items.isEmpty {
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

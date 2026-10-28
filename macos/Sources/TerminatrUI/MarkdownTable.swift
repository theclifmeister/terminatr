import SwiftUI
import TerminatrKit

/// A pipe table from an agent's Markdown: the header on the command
/// tint, rules in the theme's line. It fits the width it has, wrapping
/// its widest columns first; past what wrapping can do (columns of
/// TableLayout.floor) it scrolls sideways. VoiceOver reads it a row at
/// a time, each cell after its column's header.
struct MarkdownTable: View {
    @Environment(\.theme) private var theme
    let table: SafeMarkdown.Table
    @State private var width: CGFloat?

    var body: some View {
        ScrollView(.horizontal) {
            TableLayout(columns: table.align.count, fit: width) {
                ForEach(Array(table.header.enumerated()), id: \.offset) { c, s in
                    cell(s, c).font(theme.body(.subheadline, .semibold)).background(theme.command)
                }
                ForEach(Array(table.rows.enumerated()), id: \.offset) { _, row in
                    ForEach(Array(row.enumerated()), id: \.offset) { c, s in
                        cell(s, c).font(theme.body(.subheadline)).background(theme.agentCard)
                    }
                }
            }
            .background(theme.line)
            .clipShape(RoundedRectangle(cornerRadius: 6))
        }
        .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .accessibilityRepresentation { reading }
    }

    private func cell(_ s: AttributedString, _ column: Int) -> some View {
        Text(s)
            .multilineTextAlignment(textAlignment(column))
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 8)
            .padding(.vertical, 5)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: alignment(column))
    }

    private func alignment(_ column: Int) -> Alignment {
        switch table.align[column] {
        case .center: .top
        case .right: .topTrailing
        case .none, .left: .topLeading
        }
    }

    private func textAlignment(_ column: Int) -> TextAlignment {
        switch table.align[column] {
        case .center: .center
        case .right: .trailing
        case .none, .left: .leading
        }
    }

    /// What VoiceOver reads: the table, then a row at a time, a cell as
    /// "header: value".
    private var reading: some View {
        VStack {
            Text(table.header.map { String($0.characters) }.joined(separator: ", "))
                .accessibilityLabel("Columns")
                .accessibilityValue(table.header.map { String($0.characters) }.joined(separator: ", "))
            ForEach(Array(table.rows.enumerated()), id: \.offset) { r, row in
                VStack {
                    ForEach(Array(row.enumerated()), id: \.offset) { c, s in
                        Text(Self.spoken(header: table.header[c], s))
                    }
                }
                .accessibilityElement(children: .contain)
                .accessibilityLabel("Row \(r + 1)")
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Table, \(table.rows.count) rows, \(table.align.count) columns")
    }

    static func spoken(header: AttributedString, _ s: AttributedString) -> AttributedString {
        header.characters.isEmpty ? s : header + AttributedString(": ") + s
    }
}

/// Cells in rows of columns, a point of the background between them
/// for rules. A column is as wide as its widest cell, up to ceiling;
/// when the columns are wider than fit, the widest give way first,
/// down to floor (or their own width), and their cells wrap.
struct TableLayout: Layout {
    let columns: Int
    /// The width to fit, or nil to take every column's own width.
    let fit: CGFloat?
    static let ceiling: CGFloat = 360
    static let floor: CGFloat = 96
    static let rule: CGFloat = 1

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let (w, h) = measure(subviews)
        return CGSize(width: w.reduce(0, +) + gaps(w.count), height: h.reduce(0, +) + gaps(h.count))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let (w, h) = measure(subviews)
        var y = bounds.minY + Self.rule
        for (r, height) in h.enumerated() {
            var x = bounds.minX + Self.rule
            for (c, width) in w.enumerated() {
                let i = r * columns + c
                guard i < subviews.count else { return }
                subviews[i].place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(width: width, height: height))
                x += width + Self.rule
            }
            y += height + Self.rule
        }
    }

    private func gaps(_ n: Int) -> CGFloat { CGFloat(n + 1) * Self.rule }

    private func measure(_ subviews: Subviews) -> (widths: [CGFloat], heights: [CGFloat]) {
        guard columns > 0 else { return ([], []) }
        var ideal = Array(repeating: CGFloat(0), count: columns)
        for (i, v) in subviews.enumerated() {
            ideal[i % columns] = max(ideal[i % columns], v.sizeThatFits(.unspecified).width.rounded(.up))
        }
        let widths = SafeMarkdown.Table.widths(ideal.map { min($0, Self.ceiling) }, fit: fit.map { $0 - gaps(columns) }, floor: Self.floor)
        let rows = (subviews.count + columns - 1) / columns
        var heights = Array(repeating: CGFloat(0), count: rows)
        for (i, v) in subviews.enumerated() {
            heights[i / columns] = max(heights[i / columns], v.sizeThatFits(ProposedViewSize(width: widths[i % columns], height: nil)).height.rounded(.up))
        }
        return (widths, heights)
    }
}

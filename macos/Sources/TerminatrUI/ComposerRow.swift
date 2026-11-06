import SwiftUI
import TerminatrKit

/// A composer's field and its buttons (T1033): the first subview is the
/// field, the rest the buttons. Side by side while the field keeps
/// leastField; else the buttons take a row under the field, at its
/// trailing edge. A Layout, not ViewThatFits: the field stays the same
/// view when the row changes, so it keeps its focus and caret.
public struct ComposerRow: Layout {
    let leastField: CGFloat
    let spacing: CGFloat

    public init(leastField: CGFloat, spacing: CGFloat = 8) {
        self.leastField = leastField
        self.spacing = spacing
    }

    public func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let field = subviews.first else { return .zero }
        let row = buttons(subviews)
        let width = proposal.width ?? field.sizeThatFits(.unspecified).width + row.width
        if ComposerFit.stacks(width: width, buttons: row.width, leastField: leastField), !row.sizes.isEmpty {
            let f = field.sizeThatFits(ProposedViewSize(width: width, height: nil))
            return CGSize(width: max(f.width, row.width - spacing), height: f.height + spacing + row.height)
        }
        let f = field.sizeThatFits(ProposedViewSize(width: width - row.width, height: nil))
        return CGSize(width: f.width + row.width, height: max(f.height, row.height))
    }

    public func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard let field = subviews.first else { return }
        let row = buttons(subviews)
        let stacked = ComposerFit.stacks(width: bounds.width, buttons: row.width, leastField: leastField) && !row.sizes.isEmpty
        let fieldWidth = stacked ? bounds.width : bounds.width - row.width
        let f = field.sizeThatFits(ProposedViewSize(width: fieldWidth, height: nil))
        field.place(at: bounds.origin, proposal: ProposedViewSize(width: fieldWidth, height: f.height))
        var x = stacked ? bounds.maxX - (row.width - spacing) : bounds.minX + fieldWidth + spacing
        let y = stacked ? bounds.minY + f.height + spacing : bounds.minY
        for (button, size) in zip(subviews.dropFirst(), row.sizes) {
            button.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
        }
    }

    /// The buttons' sizes, their row's height, and its width with a gap
    /// before each: what they take from a row beside the field.
    private func buttons(_ subviews: Subviews) -> (sizes: [CGSize], width: CGFloat, height: CGFloat) {
        let sizes = subviews.dropFirst().map { $0.sizeThatFits(.unspecified) }
        return (sizes, sizes.reduce(0) { $0 + $1.width + spacing }, sizes.map(\.height).max() ?? 0)
    }
}

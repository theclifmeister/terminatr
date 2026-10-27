import SwiftUI

/// The detail's pages fill the window's width, with no centred column
/// (T767).
enum Page {
    /// The side inset of a session's conversation, its cards, composer
    /// and terminal box, so the Conversation and Terminal tabs line up.
    static let edge: CGFloat = 14
    /// A dashboard card's least width: its grid adds a column for each.
    static let card: CGFloat = 420
}

/// A dashboard's cards side by side as the window widens: as many
/// columns as Page.card fits, each card its column's width.
struct CardGrid<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: Page.card), spacing: 12, alignment: .top)],
                  alignment: .leading, spacing: 10) {
            content
        }
    }
}

/// A split view column's content at the column's size (T777): its least
/// width is min and its height any, whatever the content would take, so
/// the column's least and greatest sizes never change with what it
/// shows. Content wider than the column runs past its edge rather than
/// widening it.
struct ColumnFit: Layout {
    let min: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        CGSize(width: Swift.max(min, proposal.width ?? min), height: proposal.height ?? 0)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for s in subviews {
            s.place(at: bounds.origin, anchor: .topLeading, proposal: ProposedViewSize(bounds.size))
        }
    }
}

extension View {
    /// This view as a split view column's content, at least min wide
    /// (ColumnFit).
    func column(min: Double) -> some View {
        ColumnFit(min: min) { self }
    }
}

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

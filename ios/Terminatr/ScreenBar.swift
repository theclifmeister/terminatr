import SwiftUI
import TerminatrUI

/// A screen's own bar (T925), in place of the navigation bar's buttons:
/// Back, when there is somewhere to go back to, and the screen's
/// trailing circle buttons, pinned over its top, laid out by the
/// screen's gutter as its content is. The navigation bar keeps its own
/// margins whatever the screen's: 16 pt on an iPhone, 10 pt in an
/// iPad's column, and in a column on an open iPhone Duo the window's
/// inset on the inner edge, which the content ignores (InnerEdge), so
/// a trailing button there sat 21 pt short of the content's edge. The
/// screen keeps its title, for VoiceOver and the screen it opens.
struct ScreenBar<Trailing: View>: ViewModifier {
    @Environment(\.back) private var back
    @Environment(\.isPresented) private var pushed
    @Environment(\.dismiss) private var dismiss
    @ViewBuilder let trailing: () -> Trailing

    func body(content: Content) -> some View {
        if #available(iOS 26, *) {
            content
                .safeAreaBar(edge: .top, spacing: 0) { row }
                .toolbar(.hidden, for: .navigationBar)
        } else {
            content
                .safeAreaInset(edge: .top, spacing: 0) { row }
                .toolbar(.hidden, for: .navigationBar)
        }
    }

    private var row: some View {
        HStack(spacing: Spacing.tight) {
            // Back to the list before this one in the columns (Columns
            // sets it), else out of the stack it was pushed on; at the
            // root of a column, none.
            if let go = back?.action ?? (pushed ? { dismiss() } : nil) {
                Button("Back", systemImage: "chevron.left", action: go)
                    .buttonStyle(.circle)
            }
            Spacer(minLength: 0)
            trailing()
                .buttonStyle(.circle)
        }
        .padding(.horizontal, Spacing.gutter)
        .padding(.vertical, Spacing.tight)
    }
}

extension View {
    /// The screen's own bar (ScreenBar): Back and trailing's buttons.
    func screenBar<Trailing: View>(@ViewBuilder trailing: @escaping () -> Trailing) -> some View {
        modifier(ScreenBar(trailing: trailing))
    }
}

/// Goes back to the list before this one in the columns (Columns).
struct GoBack {
    let action: () -> Void
}

extension EnvironmentValues {
    /// The columns' Back, nil outside them or with no list before.
    @Entry var back: GoBack?
}

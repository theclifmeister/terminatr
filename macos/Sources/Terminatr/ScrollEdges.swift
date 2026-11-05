import SwiftUI
import TerminatrUI

/// A scroll view's edges (T764; flat since T999): what scrolls under a
/// bar at its foot (footBar) is never read through it, and the system's
/// scroll edge effect is off.
struct ScrollEdges: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 26, *) {
            content.scrollEdgeEffectHidden(true, for: .top)
        } else {
            content
        }
    }
}

extension View {
    /// A scroll view's edges, flat.
    func scrollEdges() -> some View {
        modifier(ScrollEdges())
    }

    /// bar at this scroll view's foot: the content ends above it. Solid,
    /// the pane's colour with a line along its top.
    func footBar<Bar: View>(_ bar: Bar) -> some View {
        safeAreaInset(edge: .bottom, spacing: 0) {
            FootBar(bar: bar)
        }
    }
}

private struct FootBar<Bar: View>: View {
    @Environment(\.theme) private var theme
    let bar: Bar

    var body: some View {
        bar
            .frame(maxWidth: .infinity)
            .background(theme.pane)
            .overlay(alignment: .top) { theme.line.frame(height: 1) }
    }
}


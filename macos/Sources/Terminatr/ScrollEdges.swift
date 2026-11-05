import SwiftUI
import TerminatrUI

/// A scroll view's edges under the window's bars (T764; flat since T999):
/// what scrolls under the toolbar, or under a bar at its foot (footBar),
/// is never read through them. The title bar is a solid band with a line,
/// over the scrolling content, and the system's scroll edge effect is off.
struct ScrollEdges: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 26, *) {
            content
                .scrollEdgeEffectHidden(true, for: .top)
                .overlay(alignment: .top) { TitleBand() }
        } else {
            content.overlay(alignment: .top) { TitleBand() }
        }
    }
}

extension View {
    /// The edges of a scroll view that sits under the toolbar.
    func scrollEdges() -> some View {
        modifier(ScrollEdges())
    }

    /// A window's toolbar on its solid colour.
    func solidToolbar() -> some View {
        modifier(SolidToolbar())
    }

    /// bar at this scroll view's foot: the content ends above it. Solid,
    /// the pane's colour with a line along its top.
    func footBar<Bar: View>(_ bar: Bar) -> some View {
        safeAreaInset(edge: .bottom, spacing: 0) {
            FootBar(bar: bar)
        }
    }
}

private struct SolidToolbar: ViewModifier {
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        content
            .toolbarBackground(theme.chrome, for: .windowToolbar)
            .toolbarBackground(.visible, for: .windowToolbar)
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

/// The title bar's band: the window's colour, as high as the safe area's
/// top, with its soft line.
struct TitleBand: View {
    @Environment(\.theme) private var theme

    var body: some View {
        GeometryReader { g in
            theme.chrome
                .frame(height: g.safeAreaInsets.top)
                .overlay(alignment: .bottom) { theme.lineSoft.frame(height: 1) }
                .ignoresSafeArea()
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

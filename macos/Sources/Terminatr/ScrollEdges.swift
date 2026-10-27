import SwiftUI

/// A scroll view's edges under the window's bars (T764): what scrolls
/// under the toolbar, or under a bar at its foot (footBar), is never
/// read through them. On macOS 26 the system's scroll edge effect,
/// hard: the soft one leaves the text under the title legible in
/// light mode. On 14 and 15, Bloom's bar fading into the content
/// (EdgeFade).
struct ScrollEdges: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 26, *) {
            content.scrollEdgeEffectStyle(.hard, for: .top)
        } else {
            content.overlay(alignment: .top) { EdgeFade(edge: .top) }
        }
    }
}

extension View {
    /// The edges of a scroll view that sits under the toolbar.
    func scrollEdges() -> some View {
        modifier(ScrollEdges())
    }

    /// A window's toolbar over the detail's glass. Before macOS 26 its
    /// band is hidden, so the glass shows through (T752) and EdgeFade
    /// draws the edge; on 26 the system keeps it, as it draws nothing
    /// there but the scroll edge effect, which hiding it turns off.
    @ViewBuilder
    func glassToolbar() -> some View {
        if #available(macOS 26, *) {
            self
        } else {
            toolbarBackground(.hidden, for: .windowToolbar)
        }
    }

    /// bar at this scroll view's foot: the content ends above it and
    /// scrolls behind it, fading out within the bar's top (EdgeFade).
    /// Not macOS 26's safe area bar: its edge effect cuts the content
    /// hard at the bar's top over the fade, the line T764 removes.
    func footBar<Bar: View>(_ bar: Bar) -> some View {
        safeAreaInset(edge: .bottom, spacing: 0) {
            bar
                .frame(maxWidth: .infinity)
                .padding(.top, EdgeFade.fade)
                .background { EdgeFade(edge: .bottom) }
        }
    }
}

/// An edge's ground (the top's before macOS 26, the foot bar's): the
/// bar material and the theme's bar, opaque where the toolbar (top) or
/// the bar (bottom) is and fading into the content over fade points:
/// below the toolbar, within the bar's top. With Increase Contrast or
/// Reduce Transparency, opaque: the theme's bar over its base.
struct EdgeFade: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency
    let edge: VerticalEdge

    static let fade: CGFloat = 16

    var body: some View {
        GeometryReader { g in
            // The top's toolbar is the safe area; the bottom's bar is
            // the view's own height.
            let height = edge == .top ? g.safeAreaInsets.top + Self.fade : g.size.height
            let solid = height - Self.fade
            let stop = height > 0 ? solid / height : 0
            ground
                .mask {
                    LinearGradient(stops: [.init(color: .black, location: 0), .init(color: .black, location: stop),
                                           .init(color: .clear, location: 1)],
                                   startPoint: edge == .top ? .top : .bottom, endPoint: edge == .top ? .bottom : .top)
                }
                .frame(height: height)
                .frame(maxHeight: .infinity, alignment: edge == .top ? .top : .bottom)
                .ignoresSafeArea()
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    @ViewBuilder
    private var ground: some View {
        if theme.contrast || reduceTransparency {
            // With Increase Contrast the bar is opaque: the title bar's.
            ZStack {
                theme.base
                theme.bar
            }
        } else {
            ZStack {
                Rectangle().fill(.bar)
                theme.bar
            }
        }
    }
}

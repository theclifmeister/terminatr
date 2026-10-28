import SwiftUI

/// The theme's mesh behind the window's panels: a MeshGradient on
/// macOS 15+ and iOS 18+, blurred radial gradients before. Reduce Transparency hides
/// it and leaves the plain base colour. The Mac's windows and the
/// theme swatches in both apps' settings draw it.
public struct Backdrop: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

    public init() {}

    public var body: some View {
        ZStack {
            theme.base
            if !reduceTransparency {
                if #available(macOS 15, iOS 18, *) {
                    mesh
                } else {
                    radials
                }
            }
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }

    /// 3×3: the corners and the centre are the theme's colours, the
    /// edges' midpoints the base, so the colours bloom from the corners.
    @available(macOS 15, iOS 18, *)
    private var mesh: some View {
        let m = theme.mesh, b = theme.base
        return MeshGradient(
            width: 3, height: 3,
            points: [
                [0, 0], [0.5, 0], [1, 0],
                [0, 0.5], [0.55, 0.45], [1, 0.5],
                [0, 1], [0.5, 1], [1, 1],
            ],
            colors: [
                m[0], b, m[1],
                b, m[4], b,
                m[2], b, m[3],
            ]
        )
    }

    private var radials: some View {
        GeometryReader { g in
            let r = max(g.size.width, g.size.height) * 0.6
            ZStack {
                ForEach(Array(zip(theme.mesh.indices, Self.centres)), id: \.0) { i, c in
                    RadialGradient(colors: [theme.mesh[i], theme.mesh[i].opacity(0)], center: c, startRadius: 0, endRadius: r)
                }
            }
            .blur(radius: 40)
        }
    }

    private static let centres: [UnitPoint] = [
        UnitPoint(x: 0.08, y: 0.12), UnitPoint(x: 0.92, y: 0.08),
        UnitPoint(x: 0.12, y: 0.92), UnitPoint(x: 0.85, y: 0.95),
        UnitPoint(x: 0.5, y: 0.5),
    ]
}

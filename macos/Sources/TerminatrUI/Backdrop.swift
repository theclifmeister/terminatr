import SwiftUI

/// The theme's mesh behind the window's panels: a MeshGradient on
/// macOS 15+ and iOS 18+, blurred radial gradients before. Reduce Transparency hides
/// it and leaves the plain base colour. The Mac's windows and the
/// theme swatches in both apps' settings draw it. Drifting (the phone's
/// Welcome, T850), its inner points wander on a 30 s loop, still with
/// Reduce Motion and in Low Power Mode.
public struct Backdrop: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency
    @Environment(\.accessibilityReduceMotion) private var still
    @State private var lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
    let drifting: Bool

    public init(drifting: Bool = false) {
        self.drifting = drifting
    }

    public var body: some View {
        ZStack {
            theme.base
            if !reduceTransparency {
                if #available(macOS 15, iOS 18, *) {
                    if drifting {
                        TimelineView(.animation(minimumInterval: 1 / 20, paused: still || lowPower)) { tl in
                            mesh(at: still || lowPower ? 0 : tl.date.timeIntervalSinceReferenceDate)
                        }
                    } else {
                        mesh(at: 0)
                    }
                } else {
                    radials
                }
            }
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
        .onReceive(NotificationCenter.default.publisher(for: .NSProcessInfoPowerStateDidChange)) { _ in
            lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
        }
    }

    /// 3×3: the corners and the centre are the theme's colours, the
    /// edges' midpoints the base, so the colours bloom from the corners.
    /// At t (seconds) the centre and the midpoints sit off their places
    /// along a 30 s loop; at 0, where they rest.
    @available(macOS 15, iOS 18, *)
    private func mesh(at t: TimeInterval) -> some View {
        let m = theme.mesh, b = theme.base
        let a = t.truncatingRemainder(dividingBy: 30) / 30 * 2 * .pi
        func d(_ phase: Double, _ r: Double = 0.08) -> Float { t == 0 ? 0 : Float(sin(a + phase) * r) }
        return MeshGradient(
            width: 3, height: 3,
            points: [
                [0, 0], [0.5 + d(0), 0], [1, 0],
                [0, 0.5 + d(1.3)], [0.55 + d(2.1, 0.12), 0.45 + d(0.4, 0.12)], [1, 0.5 + d(3.7)],
                [0, 1], [0.5 + d(5.1), 1], [1, 1],
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

import SwiftUI
import TerminatrKit

/// A card's glow: still, as it always was, unless the card waits on the
/// user (key set). Then it breathes, and the newest such card's edge
/// runs round it. With Reduce Motion, or while the window is in the
/// back, hidden or the card off screen, it stands still.
struct Glow: ViewModifier {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    @Environment(\.scenePhase) private var phase
    #if os(macOS)
    @Environment(\.controlActiveState) private var window
    #endif
    let key: String?
    let colour: Color
    let shape: RoundedRectangle
    @State private var shown = false
    private var cards: WaitingCards { .shared }

    /// One breath, in seconds.
    static let breath = 3.6
    /// One lap of the edge, in seconds.
    static let lap = 5.0
    /// The laps a leading card runs in front of the user before it
    /// counts as seen.
    static let laps = 3.0

    func body(content: Content) -> some View {
        if let key, !still {
            let lead = cards.lead == key
            let paused = !shown || !front
            // Breathing is slow: 15 frames a second is enough. The edge
            // moves further each frame and takes 30, on its own clock
            // over the shadow, so the card's shadow isn't drawn again
            // for each of its frames.
            TimelineView(.animation(minimumInterval: 1 / 15, paused: paused)) { tl in
                let b = Self.swell(at: tl.date.timeIntervalSinceReferenceDate)
                content
                    .shadow(color: colour.opacity(0.2 + 0.3 * b), radius: 9 + 10 * b, y: 6)
            }
            .overlay {
                if lead {
                    TimelineView(.animation(minimumInterval: 1 / 30, paused: paused)) { tl in
                        edge(at: tl.date.timeIntervalSinceReferenceDate)
                    }
                }
            }
            .onAppear { show(key) }
            .onDisappear { hide(key) }
            .task(id: lead && !paused) {
                guard lead, !paused else { return }
                try? await Task.sleep(for: .seconds(Self.lap * Self.laps))
                if !Task.isCancelled { cards.see(key) }
            }
            #if os(macOS)
            .onHover { if $0 { cards.see(key) } }
            #endif
        } else {
            content.shadow(color: colour.opacity(0.25), radius: 12, y: 6)
        }
    }

    /// The window is in front: the key window on the Mac, the active
    /// scene on the phone.
    private var front: Bool {
        #if os(macOS)
        window == .key && phase != .background
        #else
        phase == .active
        #endif
    }

    private func show(_ key: String) {
        guard !shown else { return }
        shown = true
        cards.show(key)
    }

    private func hide(_ key: String) {
        guard shown else { return }
        shown = false
        cards.hide(key)
    }

    /// A light band in the card's colour going round its edge, a comet:
    /// a faded tail and a bright head. Its pieces are the edge's own
    /// path, trimmed and stroked in one colour each: a gradient over the
    /// card's whole frame costs several times as much to draw.
    private func edge(at t: Double) -> some View {
        let head = t.truncatingRemainder(dividingBy: Self.lap) / Self.lap
        let width: CGFloat = theme.contrast ? 2.5 : 2
        let path = shape.inset(by: width / 2)
        return ZStack {
            ForEach(0..<Self.tail, id: \.self) { i in
                // The head's piece last and brightest.
                let end = head - Double(Self.tail - 1 - i) * Self.piece
                let c = i == Self.tail - 1 && theme.contrast ? theme.ink : colour
                Lapped(base: path, from: end - Self.piece, to: end)
                    .stroke(c.opacity(Double(i + 1) / Double(Self.tail)), style: StrokeStyle(lineWidth: width, lineCap: .butt))
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    /// The band's pieces, and each one's share of the edge.
    static let tail = 6
    static let piece = 0.02

    /// The breath at a time, 0 (out) to 1 (in), eased: every card
    /// breathes together.
    static func swell(at t: Double) -> Double {
        (1 - cos(t.truncatingRemainder(dividingBy: breath) / breath * 2 * .pi)) / 2
    }
}

/// A piece of a shape's outline, from one point to another as shares of
/// a lap: across the outline's start when the piece is.
private struct Lapped<S: Shape>: Shape {
    let base: S
    let from: Double
    let to: Double

    func path(in rect: CGRect) -> Path {
        let whole = base.path(in: rect)
        let f = from - floor(from), t = to - floor(from)
        guard t > 1 else { return whole.trimmedPath(from: f, to: t) }
        var p = whole.trimmedPath(from: f, to: 1)
        p.addPath(whole.trimmedPath(from: 0, to: t - 1))
        return p
    }
}

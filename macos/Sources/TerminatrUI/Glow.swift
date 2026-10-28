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
            // moves further each frame and takes 30.
            TimelineView(.animation(minimumInterval: lead ? 1 / 30 : 1 / 15, paused: paused)) { tl in
                let t = tl.date.timeIntervalSinceReferenceDate
                let b = Self.swell(at: t)
                content
                    .overlay {
                        if lead { edge(at: t) }
                    }
                    .shadow(color: colour.opacity(0.2 + 0.3 * b), radius: 9 + 10 * b, y: 6)
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
    /// a faded tail and a bright head.
    private func edge(at t: Double) -> some View {
        let turn = t.truncatingRemainder(dividingBy: Self.lap) / Self.lap * 360
        let head = theme.contrast ? theme.ink : colour
        return shape
            .strokeBorder(
                AngularGradient(
                    stops: [
                        .init(color: colour.opacity(0), location: 0),
                        .init(color: colour.opacity(0), location: 0.8),
                        .init(color: colour.opacity(0.7), location: 0.94),
                        .init(color: head, location: 0.985),
                        .init(color: colour.opacity(0), location: 1),
                    ],
                    center: .center,
                    angle: .degrees(turn)
                ),
                lineWidth: theme.contrast ? 2.5 : 2
            )
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    /// The breath at a time, 0 (out) to 1 (in), eased: every card
    /// breathes together.
    static func swell(at t: Double) -> Double {
        (1 - cos(t.truncatingRemainder(dividingBy: breath) / breath * 2 * .pi)) / 2
    }
}

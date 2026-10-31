import SwiftUI
import TerminatrKit
import UIKit

/// Jump to latest's glide (T889, Glide) on the conversation's scroll
/// view: it stops a flick's momentum where it is, then moves the offset
/// each frame toward the end as that frame has it (content size and
/// insets now, the rows a LazyVStack just laid out included), until it
/// lands. Each frame it lays the rows at the new offset out at once and
/// keeps the offset off the far side of the end they leave (T896): UIKit
/// doesn't clamp an offset it is given, and one past the content shows
/// only the background. The user's finger on the scroll ends it.
@MainActor final class Glider: NSObject {
    /// The conversation's UIScrollView (ScrollViewHook finds it).
    weak var scrollView: UIScrollView?
    private var link: CADisplayLink?
    private var glide: Glide?
    private var last: CFTimeInterval?
    private var done: (() -> Void)?

    /// Whether a glide runs.
    var running: Bool { link != nil }

    /// Glides to the end, then calls done (also when the user takes
    /// hold, or stop() cuts it). False with no scroll view to drive: the
    /// caller scrolls as it can.
    func start(instant: Bool, done: @escaping () -> Void) -> Bool {
        stop()
        guard let sv = scrollView, sv.window != nil else { return false }
        // Where the flick has it now, and no momentum left.
        sv.setContentOffset(sv.contentOffset, animated: false)
        let from = sv.contentOffset.y
        var g = Glide(offset: from, target: Self.end(sv), visible: sv.bounds.height, instant: instant)
        // Its start laid out, the end can move a long way (T896): after a
        // fast flick up the content still counts the rows near the end at
        // the LazyVStack's estimates, and a coordinator's, tm's one-line
        // prompts between tall cards, are far shorter. Aimed again at the
        // end they leave, it glides from lead screens above that, rather
        // than start past it and snap back.
        for _ in 0..<3 {
            let end = Self.end(sv)
            set(sv, g.offset)
            sv.layoutIfNeeded()
            let now = Self.end(sv)
            if abs(now - end) < 1 { break }
            g = Glide(offset: from, target: now, visible: sv.bounds.height, instant: instant)
        }
        place(sv, &g)
        glide = g
        self.done = done
        last = nil
        let l = CADisplayLink(target: self, selector: #selector(tick(_:)))
        l.add(to: .main, forMode: .common)
        link = l
        return true
    }

    /// Ends a glide where it is.
    func stop() {
        link?.invalidate()
        link = nil
        glide = nil
        let d = done
        done = nil
        d?()
    }

    /// The offset at the end: the content's foot at the view's, past
    /// the bars inset over it (UIKit's adjustedContentInset: the safe
    /// area's and the composer's).
    static func end(_ sv: UIScrollView) -> Double {
        max(-sv.adjustedContentInset.top, sv.contentSize.height + sv.adjustedContentInset.bottom - sv.bounds.height)
    }

    private func set(_ sv: UIScrollView, _ y: Double) {
        if abs(sv.contentOffset.y - y) > 0.01 { sv.contentOffset = CGPoint(x: sv.contentOffset.x, y: y) }
    }

    /// Puts the glide's offset on the scroll view, and the rows there
    /// laid out now, this frame: rows shorter than the LazyVStack
    /// estimated pull the end up, and an offset past it goes back onto
    /// it before the frame draws. A few rounds: each can bring rows of
    /// its own.
    private func place(_ sv: UIScrollView, _ g: inout Glide) {
        set(sv, g.offset)
        for _ in 0..<3 {
            sv.layoutIfNeeded()
            guard g.clamp(to: Self.end(sv)) else { return }
            set(sv, g.offset)
        }
    }

    @objc private func tick(_ l: CADisplayLink) {
        guard let sv = scrollView, sv.window != nil, !sv.isTracking, var g = glide else { return stop() }
        let dt = last.map { l.timestamp - $0 } ?? (l.targetTimestamp - l.timestamp)
        last = l.timestamp
        _ = g.step(to: Self.end(sv), dt: dt)
        place(sv, &g)
        glide = g
        if g.landed { stop() }
    }
}

/// Finds the UIScrollView its view sits in, for the Glider: put in the
/// scroll view's content.
struct ScrollViewHook: UIViewRepresentable {
    let glider: Glider

    func makeUIView(context: Context) -> Hook {
        let v = Hook()
        v.isUserInteractionEnabled = false
        v.glider = glider
        return v
    }

    func updateUIView(_ v: Hook, context: Context) {
        v.glider = glider
        v.find()
    }

    final class Hook: UIView {
        weak var glider: Glider?

        override func didMoveToWindow() {
            super.didMoveToWindow()
            find()
        }

        func find() {
            var s = superview
            while let v = s {
                if let sv = v as? UIScrollView {
                    glider?.scrollView = sv
                    return
                }
                s = v.superview
            }
        }
    }
}

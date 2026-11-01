import SwiftUI
import TerminatrKit
import UIKit

/// Jump to latest's glide (T889, Glide) on the conversation's scroll
/// view: it stops a flick's momentum where it is, then moves the offset
/// each frame toward the end as that frame has it (content size and
/// insets now, the rows a LazyVStack just laid out included), until it
/// lands. While Glide aims its start (T896: the rows there lay out a
/// frame on, and can move the end up past it) a still image of the view
/// covers it, so the start never shows past the end. The user's finger
/// on the scroll ends it. The follower goes to the end by one too, an
/// instant one (T901), renewed as more output comes.
@MainActor final class Glider: NSObject {
    /// The conversation's UIScrollView (ScrollViewHook finds it).
    weak var scrollView: UIScrollView?
    private var link: CADisplayLink?
    private var glide: Glide?
    private var last: CFTimeInterval?
    private var done: (() -> Void)?
    /// The still image over the scroll view while the start is aimed.
    private var cover: UIView?

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
        let g = Glide(offset: sv.contentOffset.y, target: Self.end(sv), visible: sv.bounds.height, instant: instant)
        if g.covered, let still = sv.snapshotView(afterScreenUpdates: false) {
            still.frame = sv.frame
            still.isUserInteractionEnabled = false
            sv.superview?.insertSubview(still, aboveSubview: sv)
            cover = still
        }
        glide = g
        set(sv, g.offset)
        self.done = done
        last = nil
        let l = CADisplayLink(target: self, selector: #selector(tick(_:)))
        l.add(to: .main, forMode: .common)
        link = l
        return true
    }

    /// More output came while a glide runs (T901): it keeps to the end
    /// until that holds again (Glide.renew).
    func renew() {
        glide?.renew()
    }

    /// Ends a glide where it is.
    func stop() {
        link?.invalidate()
        link = nil
        glide = nil
        uncover()
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

    private func uncover() {
        cover?.removeFromSuperview()
        cover = nil
    }

    @objc private func tick(_ l: CADisplayLink) {
        guard let sv = scrollView, sv.window != nil, !sv.isTracking, var g = glide else { return stop() }
        let dt = last.map { l.timestamp - $0 } ?? (l.targetTimestamp - l.timestamp)
        last = l.timestamp
        let landed = g.step(to: Self.end(sv), dt: dt)
        glide = g
        set(sv, g.offset)
        if !g.covered { uncover() }
        if landed { stop() }
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

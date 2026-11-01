import AppKit
import SwiftUI
import TerminatrKit

/// Jump to latest's glide (T895, Glide; the iPhone's is T889) on the
/// conversation's NSScrollView: each display frame it moves the clip
/// view toward the end as that frame has it (the document's height and
/// the insets now, the rows a LazyVStack just laid out included), until
/// it lands. SwiftUI's animated scrollTo aims once, at an end the
/// LazyVStack only estimates, and stops short of it.
///
/// A trackpad flick's momentum comes as scroll events after the finger
/// lifts, and an NSScrollView applies each: tapped mid-flick, they would
/// carry the glide back up. So the momentum of a gesture that was in
/// flight when a glide started is stilled (its events pass with no
/// delta, so SwiftUI still sees the scroll come to rest) until it ends,
/// also after the glide has landed. A new touch on the trackpad, a mouse
/// wheel or a drag of the scroller ends a glide where it is: the user
/// took hold.
///
/// While Glide aims its start (T896: the rows there lay out a frame on,
/// and move the end) the start only moves down (Glide's aimsUp off):
/// no still image covers it here, as an NSView's cached image of
/// SwiftUI's layers can come out blank. The follower goes to the end by
/// one too, an instant one (T901), renewed as more output comes.
@MainActor final class Glider: NSObject {
    /// The conversation's NSScrollView (ScrollViewHook finds it).
    weak var scrollView: NSScrollView?
    private var link: CADisplayLink?
    private var glide: Glide?
    private var last: CFTimeInterval?
    /// The offset the glide last set: one that differs at the next
    /// frame was moved by SwiftUI's layout (Glide.moved).
    private var placed: Double?
    private var done: (() -> Void)?
    /// The scroll events' monitor: momentum stilled, the user's own
    /// scroll ending a glide.
    private var monitor: Any?
    /// Whether a flick's momentum is stilled, and since when.
    private var stilling: CFTimeInterval?

    /// Whether a glide runs.
    var running: Bool { link != nil }

    /// Momentum is stilled this long at most: a flick's is over well
    /// within it, and a missed end event must not still the next.
    static let stillLongest = 3.0

    /// Glides to the end, then calls done (also when the user takes
    /// hold, or stop() cuts it). False with no scroll view to drive: the
    /// caller scrolls as it can.
    func start(instant: Bool, done: @escaping () -> Void) -> Bool {
        stop()
        guard let sv = scrollView, sv.window != nil else { return false }
        watch()
        if !instant { stilling = CACurrentMediaTime() }
        let g = Glide(offset: Self.offset(sv), target: Self.end(sv), visible: sv.contentView.bounds.height, instant: instant, aimsUp: false)
        glide = g
        placed = nil
        set(sv, g.offset)
        self.done = done
        last = nil
        let l = sv.displayLink(target: self, selector: #selector(tick(_:)))
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
        let d = done
        done = nil
        d?()
    }

    /// Ends the glide and lets go of the scroll events: the view leaves.
    func leave() {
        stop()
        stilling = nil
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }

    /// The scroll offset, down from the top (a SwiftUI scroll view's
    /// document is flipped).
    static func offset(_ sv: NSScrollView) -> Double {
        sv.contentView.bounds.origin.y
    }

    /// The offset at the end: the document's foot at the clip view's,
    /// past the insets of the bars over it (footBar's). Not the clip
    /// view's constrainBoundsRect: SwiftUI's answers 0, the top, for an
    /// origin far down.
    static func end(_ sv: NSScrollView) -> Double {
        let clip = sv.contentView
        let doc = sv.documentView?.frame.height ?? 0
        return max(-clip.contentInsets.top, doc + clip.contentInsets.bottom - clip.bounds.height)
    }

    private func set(_ sv: NSScrollView, _ y: Double) {
        let clip = sv.contentView
        placed = y
        guard abs(clip.bounds.origin.y - y) > 0.01 else { return }
        clip.scroll(to: NSPoint(x: clip.bounds.origin.x, y: y))
        sv.reflectScrolledClipView(clip)
    }

    /// The scroll events of the app, seen before any view: the user's
    /// own scroll ends a glide, and the momentum of a flick that was in
    /// flight is stilled.
    private func watch() {
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] e in
            self?.scrolled(e) ?? e
        }
    }

    private func scrolled(_ e: NSEvent) -> NSEvent? {
        guard let sv = scrollView, e.window === sv.window else { return e }
        if !e.momentumPhase.isEmpty {
            guard let since = stilling, CACurrentMediaTime() - since < Self.stillLongest else {
                stilling = nil
                return e
            }
            if e.momentumPhase.contains(.ended) || e.momentumPhase.contains(.cancelled) { stilling = nil }
            return Self.still(e)
        }
        // A finger on the trackpad, or a mouse wheel's notch (no phase):
        // the user's scroll from here.
        if e.phase.contains(.began) || e.phase.contains(.mayBegin) || e.phase.isEmpty {
            stilling = nil
            if running { stop() }
        }
        return e
    }

    /// The event with no movement: the scroll view sees the momentum's
    /// phases (and so comes to rest) but doesn't move.
    static func still(_ e: NSEvent) -> NSEvent? {
        guard let c = e.cgEvent?.copy() else { return nil }
        for f: CGEventField in [.scrollWheelEventDeltaAxis1, .scrollWheelEventDeltaAxis2,
                                .scrollWheelEventPointDeltaAxis1, .scrollWheelEventPointDeltaAxis2,
                                .scrollWheelEventFixedPtDeltaAxis1, .scrollWheelEventFixedPtDeltaAxis2] {
            c.setIntegerValueField(f, value: 0)
            c.setDoubleValueField(f, value: 0)
        }
        return NSEvent(cgEvent: c)
    }

    /// Whether the user holds the scroller: a mouse button down over it.
    private static func holdsScroller(_ sv: NSScrollView) -> Bool {
        guard NSEvent.pressedMouseButtons != 0, let bar = sv.verticalScroller, let w = sv.window else { return false }
        let p = bar.convert(w.mouseLocationOutsideOfEventStream, from: nil)
        return bar.bounds.contains(p)
    }

    @objc private func tick(_ l: CADisplayLink) {
        guard let sv = scrollView, sv.window != nil, !Self.holdsScroller(sv), var g = glide else { return stop() }
        let dt = last.map { l.timestamp - $0 } ?? (l.targetTimestamp - l.timestamp)
        last = l.timestamp
        if let placed, abs(Self.offset(sv) - placed) > 0.5 { g.moved(by: Self.offset(sv) - placed) }
        let landed = g.step(to: Self.end(sv), dt: dt)
        glide = g
        set(sv, g.offset)
        if landed { stop() }
    }
}

/// Finds the NSScrollView its view sits in, for the Glider: put in the
/// scroll view's content.
struct ScrollViewHook: NSViewRepresentable {
    let glider: Glider

    func makeNSView(context: Context) -> Hook {
        let v = Hook()
        v.glider = glider
        return v
    }

    func updateNSView(_ v: Hook, context: Context) {
        v.glider = glider
        v.find()
    }

    final class Hook: NSView {
        weak var glider: Glider?

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            find()
        }

        /// It takes no clicks: they go to the transcript under it.
        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        func find() {
            if let sv = enclosingScrollView { glider?.scrollView = sv }
        }
    }
}

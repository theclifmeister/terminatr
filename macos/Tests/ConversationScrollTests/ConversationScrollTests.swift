import AppKit
import os
import SwiftUI
import Testing
import TerminatrKit
@testable import TerminatrUI

// The conversation's scroll on the Mac (T976), the cases the iPhone's UI
// tests drive, in a real window: the conversation's way of scrolling
// (ConversationScroll over a VStack of exact rows, the follower's
// scrollTo of the end row) in an NSHostingView, its NSScrollView's
// geometry read back after each change. The end shows when the clip
// view's foot is at the document's, and never past it.
//
// While the display sleeps (a locked screen that is lit is fine) a wheel
// event does nothing and an animated scroll never ticks: both need the
// display's frames. Layout, geometry and an unanimated scrollTo don't, so
// the tests use those, and only the test with a real wheel and an animated
// jump needs a lit display. Load only makes things late: the tests wait
// for what must happen (Harness.until, atEnd), and settle by the clock
// only to see that something doesn't.

/// The rows, as a transcript grows.
@MainActor @Observable
final class Rows {
    var heights: [Double]
    init(_ n: Int) { heights = (0..<n).map { [24, 60, 140, 32, 300, 48][$0 % 6] } }
}

struct Conversation: View {
    let rows: Rows
    let follow: Follower
    /// Bumped to press Jump to latest.
    let jumps: Int
    /// Changed to scroll up to this row, without a display's frames.
    let up: Int
    /// Whether the jump animates, as the conversation's does.
    let animated: Bool

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    ForEach(Array(rows.heights.enumerated()), id: \.offset) { i, h in
                        Text("Message \(i)").frame(maxWidth: .infinity, minHeight: h, alignment: .topLeading).id(i)
                    }
                    // The foot's padding is the end row's, as the
                    // conversation's (T807).
                    Color.clear.frame(height: 1).padding(.bottom, 14).id("end")
                }
                .padding([.horizontal, .top], 14)
            }
            .modifier(ConversationScroll(follow: follow, newest: rows.heights.count - 1, screen: "test") {
                proxy.scrollTo("end", anchor: .bottom)
            })
            .onChange(of: rows.heights) {
                if follow.arrived(rows.heights.indices) { proxy.scrollTo("end", anchor: .bottom) }
            }
            .onChange(of: up) { proxy.scrollTo(up, anchor: .bottom) }
            .onChange(of: jumps) {
                follow.jump(newest: rows.heights.count - 1)
                withAnimation(animated ? .default : nil) { proxy.scrollTo("end", anchor: .bottom) }
            }
        }
    }
}

/// Whether the main display is awake, its frames ticking.
func displayLit() -> Bool {
    let main = CGMainDisplayID()
    return CGDisplayIsActive(main) != 0 && CGDisplayIsAsleep(main) == 0
}

@MainActor
final class Harness {
    let rows: Rows
    let follow = Follower()
    let window: NSWindow
    let host: NSHostingView<Conversation>
    /// A real wheel and an animated jump: these need a lit display.
    let real: Bool
    /// Called on each turn of the run loop while settling.
    var tick: () -> Void = {}
    var jumps = 0
    var up = -1

    init(rows n: Int, real: Bool = false) {
        _ = NSApplication.shared
        rows = Rows(n)
        self.real = real
        host = NSHostingView(rootView: Conversation(rows: rows, follow: follow, jumps: 0, up: -1, animated: real))
        window = NSWindow(contentRect: NSRect(x: -4000, y: -4000, width: 500, height: 600), styleMask: [.titled, .resizable],
                          backing: .buffered, defer: false)
        window.contentView = host
        window.orderFrontRegardless()
    }

    private func update() {
        host.rootView = Conversation(rows: rows, follow: follow, jumps: jumps, up: up, animated: real)
    }

    func jump() {
        jumps += 1
        update()
    }

    var scroll: NSScrollView? {
        var stack: [NSView] = [host]
        while let v = stack.popLast() {
            if let s = v as? NSScrollView { return s }
            stack.append(contentsOf: v.subviews)
        }
        return nil
    }

    /// The clip view's foot from the document's top, and the document's
    /// height (the flipped document: y grows down).
    var foot: (shown: Double, content: Double) {
        guard let s = scroll, let doc = s.documentView else { return (0, 0) }
        let clip = s.contentView.bounds
        return (clip.maxY - s.contentInsets.bottom, doc.frame.height)
    }

    /// Spins the main run loop: SwiftUI lays out and scrolls.
    func settle(_ seconds: Double = 0.6) {
        let until = Date().addingTimeInterval(seconds)
        while Date() < until {
            RunLoop.main.run(until: Date().addingTimeInterval(0.02))
            tick()
        }
    }

    /// Spins the run loop until the condition holds, or for a long time
    /// on a loaded machine; the expectation after it says which. A real
    /// scroll doesn't wait on a dark display: nothing would come.
    func until(_ condition: () -> Bool) {
        let deadline = Date().addingTimeInterval(15)
        while !condition(), Date() < deadline, !real || displayLit() {
            settle(0.02)
        }
    }

    /// Spins the run loop until the view has stopped moving.
    func rest() {
        var last = foot.shown
        var still = Date()
        until {
            if foot.shown != last {
                last = foot.shown
                still = Date()
            }
            return Date().timeIntervalSince(still) > 0.3
        }
    }

    /// At the end, and not past it.
    func atEnd(_ what: String, sourceLocation: SourceLocation = #_sourceLocation) {
        until { foot.content > 0 && abs(foot.shown - foot.content) <= 2 }
        let f = foot
        #expect(f.content > 0, "\(what): no content", sourceLocation: sourceLocation)
        #expect(f.shown <= f.content + 2, "\(what): past the end, \(f)", sourceLocation: sourceLocation)
        #expect(f.shown >= f.content - 2, "\(what): short of the end, \(f), following \(follow.following), \(follow.geometry)", sourceLocation: sourceLocation)
    }

    /// Scrolled up by the user. Real: mouse wheel events, as a wheel
    /// sends them (setting the clip view's bounds would be undone by
    /// SwiftUI, which keeps its own position). Otherwise SwiftUI's
    /// scrollTo of a row some 3000 points up, which Follow can't tell
    /// from a wheel's: the offset went up.
    func scrollUp() {
        guard real else {
            up = rows.heights.count - 30
            update()
            return
        }
        guard let s = scroll else { return }
        var left = 4000.0
        while left > 0 {
            let step = min(left, 400)
            left -= step
            guard let cg = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 1, wheel1: Int32(step), wheel2: 0, wheel3: 0),
                  let e = NSEvent(cgEvent: cg) else { return }
            s.scrollWheel(with: e)
            settle(0.03)
        }
    }
}

@MainActor @Test func opensStreamsAndResizesAtTheEnd() throws {
    let h = Harness(rows: 200)
    h.settle(1)
    h.atEnd("opened")
    for i in 0..<15 {
        h.rows.heights.append([24, 300, 60][i % 3])
        h.settle(0.15)
        h.atEnd("streamed \(i)")
    }
    // The last one grows as it streams.
    for i in 0..<8 {
        h.rows.heights[h.rows.heights.count - 1] += 40
        h.settle(0.15)
        h.atEnd("grew \(i)")
    }
    // The window narrower, wider, shorter, taller: a fold, a rotation.
    for size in [NSSize(width: 320, height: 600), NSSize(width: 900, height: 400), NSSize(width: 500, height: 900), NSSize(width: 500, height: 600)] {
        h.window.setContentSize(size)
        h.settle(0.5)
        h.atEnd("resized \(size)")
    }
}

/// Scrolled up, the screen stays as output comes; a jump lands at the
/// end and follows again.
@MainActor func scrollUpStayThenJump(_ h: Harness) {
    h.atEnd("opened")
    for round in 0..<3 {
        h.scrollUp()
        h.until { !h.follow.following }
        h.rest()
        #expect(!h.follow.following, "round \(round): scrolled up")
        let before = h.foot.shown
        // Output while scrolled up: the screen stays.
        h.rows.heights.append(200)
        h.until { h.follow.unseen == 1 }
        h.settle(0.4)
        #expect(abs(h.foot.shown - before) < 2, "round \(round): moved from \(before) to \(h.foot.shown)")
        #expect(h.follow.unseen == 1)
        h.jump()
        h.atEnd("round \(round): jumped")
        // At rest: a jump still under way takes the next scroll up for
        // its momentum (Follow.jumping).
        h.rest()
        #expect(h.follow.following)
        #expect(h.follow.breaches == 0)
    }
}

@MainActor @Test func scrolledUpStaysThenJumpLandsAtTheEnd() throws {
    scrollUpStayThenJump(Harness(rows: 200))
}

/// The same with a real wheel and the animated jump, on a lit display.
/// The test keeps the display from idling off; if it goes dark anyway (a
/// lock, a closed lid), what fails from then on says nothing of the
/// scroll.
@MainActor @Test(.enabled(if: displayLit(), "needs a lit display: a wheel and an animation need its frames"))
func wheelStaysThenAnimatedJumpLandsAtTheEnd() throws {
    let activity = ProcessInfo.processInfo.beginActivity(options: [.userInitiated, .idleDisplaySleepDisabled], reason: "a wheel and an animation need the display's frames")
    defer { ProcessInfo.processInfo.endActivity(activity) }
    let h = Harness(rows: 200, real: true)
    let dark = OSAllocatedUnfairLock(initialState: false)
    h.tick = {
        if !displayLit() { dark.withLock { $0 = true } }
    }
    h.tick()
    withKnownIssue("the display went dark", isIntermittent: true) {
        scrollUpStayThenJump(h)
    } matching: { _ in
        dark.withLock { $0 }
    }
}

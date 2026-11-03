import AppKit
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
            .onChange(of: jumps) {
                follow.jump(newest: rows.heights.count - 1)
                withAnimation { proxy.scrollTo("end", anchor: .bottom) }
            }
        }
    }
}

@MainActor
final class Harness {
    let rows: Rows
    let follow = Follower()
    let window: NSWindow
    let host: NSHostingView<Conversation>
    var jumps = 0

    init(rows n: Int) {
        _ = NSApplication.shared
        rows = Rows(n)
        host = NSHostingView(rootView: Conversation(rows: rows, follow: follow, jumps: 0))
        window = NSWindow(contentRect: NSRect(x: -4000, y: -4000, width: 500, height: 600), styleMask: [.titled, .resizable],
                          backing: .buffered, defer: false)
        window.contentView = host
        window.orderFrontRegardless()
    }

    func jump() {
        jumps += 1
        host.rootView = Conversation(rows: rows, follow: follow, jumps: jumps)
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
        }
    }

    /// At the end, and not past it.
    func atEnd(_ what: String, sourceLocation: SourceLocation = #_sourceLocation) {
        let f = foot
        #expect(f.content > 0, "\(what): no content", sourceLocation: sourceLocation)
        #expect(f.shown <= f.content + 2, "\(what): past the end, \(f)", sourceLocation: sourceLocation)
        #expect(f.shown >= f.content - 2, "\(what): short of the end, \(f), following \(follow.following), \(follow.geometry)", sourceLocation: sourceLocation)
    }

    /// Scrolled up by the user: mouse wheel events, as a wheel sends
    /// them (setting the clip view's bounds would be undone by SwiftUI,
    /// which keeps its own position).
    func scrollUp(_ by: Double) {
        guard let s = scroll else { return }
        var left = by
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

@MainActor @Test func scrolledUpStaysThenJumpLandsAtTheEnd() throws {
    let h = Harness(rows: 200)
    h.settle(1)
    h.atEnd("opened")
    for round in 0..<3 {
        h.scrollUp(4000)
        h.settle(0.5)
        #expect(!h.follow.following, "round \(round): scrolled up")
        let before = h.foot.shown
        // Output while scrolled up: the screen stays.
        h.rows.heights.append(200)
        h.settle(0.4)
        #expect(abs(h.foot.shown - before) < 2, "round \(round): moved from \(before) to \(h.foot.shown)")
        #expect(h.follow.unseen == 1)
        h.jump()
        h.settle(1.2)
        h.atEnd("round \(round): jumped")
        #expect(h.follow.following)
        #expect(h.follow.breaches == 0)
    }
}

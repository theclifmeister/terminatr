import ObjectiveC
import XCTest

/// The conversation's scroll (T976): whatever happens to the screen,
/// its last message is on it. The demo with the scroll fixture
/// (-scroll-fixture YES, ScrollFixture): acme-web's coordinator has a
/// long conversation, t-0104 streams a new answer every 0.35 s.
///
/// `make -C ios ui-test DEVICE=…` runs it (ITERATIONS times each, 20 by
/// default) on an iPhone, an iPad or an iPhone Duo; on a Duo opened in
/// Xcode's Device Hub, on its inner display (simctl and XCTest can't
/// open or close one, so a fold while the app runs is by hand).
final class ConversationScrollTests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    // MARK: The cases

    /// Opening a coordinator's long conversation: at its end.
    @MainActor func testOpensAtTheEnd() {
        let app = launch("session:acme-web")
        lastOnScreen(app, "opened")
    }

    /// To the terminal and back, to another session and back.
    @MainActor func testSwitchingSessions() {
        let app = launch("session:acme-web")
        lastOnScreen(app, "opened")
        app.buttons["Terminal"].firstMatch.tap()
        app.buttons["Conversation"].firstMatch.tap()
        lastOnScreen(app, "back from the terminal")
        for _ in 0..<2 {
            back(app)
            open(app, row: "Fix the flaky cart test")
            lastOnScreen(app, "a streaming thread")
            back(app)
            open(app, row: "Coordinator")
            lastOnScreen(app, "the coordinator again")
        }
    }

    /// To the home screen and back.
    @MainActor func testBackgroundAndForeground() {
        let app = launch("session:acme-web")
        lastOnScreen(app, "opened")
        for i in 0..<2 {
            XCUIDevice.shared.press(.home)
            sleep(2)
            app.activate()
            lastOnScreen(app, "foreground \(i)")
        }
    }

    /// Turned to landscape and back (an iPhone stays portrait: the iPad
    /// and the Duo turn).
    @MainActor func testRotating() {
        let app = launch("session:acme-web")
        lastOnScreen(app, "opened")
        for o in [UIDeviceOrientation.landscapeLeft, .portrait, .landscapeRight, .portrait] {
            XCUIDevice.shared.orientation = o
            sleep(1)
            lastOnScreen(app, "turned \(o.rawValue)")
        }
    }

    /// The keyboard opening over the composer, and closing.
    @MainActor func testKeyboard() {
        let app = launch("session:acme-web")
        lastOnScreen(app, "opened")
        let field = app.textViews.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the composer")
        for i in 0..<2 {
            field.tap()
            XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 10), "the keyboard")
            lastOnScreen(app, "keyboard open \(i)")
            field.typeText("Hello")
            lastOnScreen(app, "typed \(i)")
            dismissKeyboard(app)
            // The drag that folds it scrolled up a little: back down.
            let jump = jumpButton(app)
            if jump.waitForExistence(timeout: 2) { jump.tap() }
            lastOnScreen(app, "keyboard closed \(i)")
        }
    }

    /// A thread whose answers keep coming: each time, the newest shows.
    @MainActor func testStreaming() {
        let app = launch("session:acme-web/t-0104")
        lastOnScreen(app, "opened")
        for i in 0..<16 {
            usleep(700_000)
            lastOnScreen(app, "streamed \(i)", settle: false)
        }
    }

    /// A fast flick up, then Jump to latest while it still moves, and
    /// after it rests: at the end both ways.
    @MainActor func testFlickAndJump() {
        let app = launch("session:acme-web")
        lastOnScreen(app, "opened")
        let conv = app.scrollViews["conversation"]
        for i in 0..<3 {
            conv.swipeDown(velocity: .fast)
            Quiescence.skip = true
            conv.swipeDown(velocity: .fast)
            let jump = jumpButton(app)
            if jump.waitForExistence(timeout: 2) { jump.tap() }
            Quiescence.skip = false
            lastOnScreen(app, "jumped mid-flick \(i)")
            conv.swipeDown(velocity: .fast)
            conv.swipeDown(velocity: .fast)
            XCTAssertTrue(jumpButton(app).waitForExistence(timeout: 5), "Jump to latest after a flick up")
            jumpButton(app).tap()
            lastOnScreen(app, "jumped at rest \(i)")
        }
    }

    /// The streaming thread scrolled up stays where it is, then Jump to
    /// latest lands on the newest as more keeps coming.
    @MainActor func testJumpWhileStreaming() {
        let app = launch("session:acme-web/t-0104")
        lastOnScreen(app, "opened")
        let conv = app.scrollViews["conversation"]
        conv.swipeDown(velocity: .fast)
        if !jumpButton(app).waitForExistence(timeout: 2) { conv.swipeDown(velocity: .fast) }
        XCTAssertTrue(jumpButton(app).waitForExistence(timeout: 5), "Jump to latest")
        sleep(2)
        jumpButton(app).tap()
        for i in 0..<5 {
            usleep(700_000)
            lastOnScreen(app, "following again \(i)", settle: false)
        }
    }

    // MARK: Helpers

    @MainActor private func launch(_ open: String) -> XCUIApplication {
        _ = Quiescence.installed
        let app = XCUIApplication()
        app.launchArguments = ["-demo", "YES", "-scroll-fixture", "YES", "-open", open]
        XCUIDevice.shared.orientation = .portrait
        app.launch()
        return app
    }

    @MainActor private func jumpButton(_ app: XCUIApplication) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Jump to latest'")).firstMatch
    }

    @MainActor private func back(_ app: XCUIApplication) {
        let b = app.buttons["BackButton"].firstMatch
        if b.waitForExistence(timeout: 5), b.isHittable {
            b.tap()
            return
        }
        // A column layout (the iPad, the opened Duo) has the list beside
        // it, or behind Show the list (the iPad upright).
        let list = app.buttons["Show the list"].firstMatch
        if list.exists, list.isHittable { list.tap() }
    }

    /// Opens the session whose row says words, from the project screen.
    @MainActor private func open(_ app: XCUIApplication, row words: String) {
        let p = NSPredicate(format: "label CONTAINS %@", words)
        let row = app.buttons.matching(p).firstMatch
        if !row.waitForExistence(timeout: 5) {
            app.swipeUp()
        }
        XCTAssertTrue(row.waitForExistence(timeout: 10), "the row \(words)")
        row.tap()
    }

    @MainActor private func dismissKeyboard(_ app: XCUIApplication) {
        let kb = app.keyboards.firstMatch
        let hide = kb.buttons["Hide keyboard"]
        if hide.exists {
            // By its place: the iPad's keyboard refuses the scroll to it
            // that a plain tap asks first.
            hide.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        } else {
            // A drag down the conversation, from what shows of it above
            // the composer into the keyboard, folds it (interactively,
            // T857); on the Duo's outer screen that is a sliver.
            let area = seen(app)
            let window = app.windows.firstMatch.frame
            let origin = app.coordinate(withNormalizedOffset: .zero)
            let from = origin.withOffset(CGVector(dx: area.midX, dy: area.minY + min(20, area.height / 2)))
            from.press(forDuration: 0.05, thenDragTo: origin.withOffset(CGVector(dx: area.midX, dy: window.maxY - 4)))
        }
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: kb)
        wait(for: [gone], timeout: 10)
    }

    /// The part of the conversation the user sees: its scroll view in
    /// the window, under the bars over it, above the composer and the
    /// keyboard.
    @MainActor private func seen(_ app: XCUIApplication) -> CGRect {
        var r = app.scrollViews["conversation"].frame.intersection(app.windows.firstMatch.frame)
        let bar = app.navigationBars.firstMatch
        if bar.exists, bar.frame.minX < r.midX, bar.frame.maxX > r.midX, bar.frame.maxY > r.minY, bar.frame.minY <= r.minY + 1 {
            r = CGRect(x: r.minX, y: bar.frame.maxY, width: r.width, height: max(0, r.maxY - bar.frame.maxY))
        }
        var foot = r.maxY
        let field = app.textViews.firstMatch
        if field.exists, field.frame.minY < foot { foot = field.frame.minY - 12 }
        let kb = app.keyboards.firstMatch
        if kb.exists, kb.frame.minY < foot { foot = kb.frame.minY }
        return CGRect(x: r.minX, y: r.minY, width: r.width, height: max(0, foot - r.minY))
    }

    /// The last message's frame on the screen (all its parts).
    /// One snapshot of the conversation, so a streamed row coming
    /// between a query and its frames can't fail it.
    @MainActor private func last(_ app: XCUIApplication) -> CGRect {
        guard let root = try? app.scrollViews["conversation"].snapshot() else { return .null }
        var r = CGRect.null
        var stack: [any XCUIElementSnapshot] = [root]
        while let n = stack.popLast() {
            if n.identifier == "conversation.last", !n.frame.isEmpty { r = r.union(n.frame) }
            stack.append(contentsOf: n.children)
        }
        return r
    }

    /// The last message is on the screen, near its foot: not off the
    /// end above the composer (the blank screen, T976), not left up the
    /// conversation. Settled, it waits for the layout to come to rest.
    @MainActor private func lastOnScreen(_ app: XCUIApplication, _ what: String, settle: Bool = true,
                                         file: StaticString = #filePath, line: UInt = #line) {
        let conv = app.scrollViews["conversation"]
        XCTAssertTrue(conv.waitForExistence(timeout: 20), "\(what): no conversation", file: file, line: line)
        var l = CGRect.null
        let until = Date().addingTimeInterval(20)
        while Date() < until {
            l = last(app)
            if !l.isNull { break }
            usleep(200_000)
        }
        if settle {
            let rest = Date().addingTimeInterval(6)
            while Date() < rest {
                usleep(300_000)
                let again = last(app)
                if again == l { break }
                l = again
            }
        }
        let area = seen(app)
        let shown = l.intersection(area)
        let onScreen = !l.isNull && !shown.isNull && shown.height >= min(20, l.height)
        // At the end: the last message's foot within the lower part of
        // what is seen (the working line and cards come after it), or
        // the message taller than the screen.
        let atEnd = onScreen && (l.maxY >= area.minY + area.height * 0.25 || l.height >= area.height * 0.75)
        if !(onScreen && atEnd) {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = what
            shot.lifetime = .keepAlways
            add(shot)
        }
        XCTAssertTrue(onScreen, "\(what): the last message \(l) isn't on the screen \(area)", file: file, line: line)
        XCTAssertTrue(atEnd, "\(what): the last message \(l) isn't at the end of the screen \(area)", file: file, line: line)
    }
}

/// XCUITest waits for the app to be idle, animations done, before each
/// event: a tap after a flick would wait out its momentum. While skip is
/// set it doesn't (XCUIApplication's private _waitForQuiescenceAsPreEvent:),
/// so Jump to latest is tapped mid-flick, as a user does.
enum Quiescence {
    nonisolated(unsafe) static var skip = false

    static let installed: Void = {
        let sel = NSSelectorFromString("_waitForQuiescenceAsPreEvent:")
        guard let m = class_getInstanceMethod(XCUIApplication.self, sel) else { return }
        typealias Wait = @convention(c) (AnyObject, Selector, Bool) -> Void
        let original = unsafeBitCast(method_getImplementation(m), to: Wait.self)
        let block: @convention(block) (AnyObject, Bool) -> Void = { app, pre in
            if Quiescence.skip { return }
            original(app, sel, pre)
        }
        method_setImplementation(m, imp_implementationWithBlock(block))
    }()
}

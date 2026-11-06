import XCTest

/// The screens' own bars (T1001): they hide the navigation bar, so going
/// back by its button and by the swipe from the edge, the session's tabs and
/// the board's filter and folds are the screens' own. On a phone's stack: an
/// iPad's columns have a list beside them.
final class FlatNavTests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom == .pad, "the columns go back by their own list")
    }

    @MainActor private func launch(_ open: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-demo", "YES", "-open", open]
        app.launch()
        return app
    }

    /// The main row opens the session, its tabs switch, Back goes back.
    @MainActor func testBackButtonAndTabs() {
        let app = launch("project:acme-web")
        let coordinator = app.buttons["Coordinator"].firstMatch
        XCTAssertTrue(coordinator.waitForExistence(timeout: 10))
        coordinator.tap()
        XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 10))
        app.buttons["Inspector"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Project"].firstMatch.waitForExistence(timeout: 5))
        app.buttons["Terminal"].firstMatch.tap()
        app.buttons["Conversation"].firstMatch.tap()
        app.buttons["BackButton"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Overview"].firstMatch.waitForExistence(timeout: 5))
    }

    /// A swipe from the leading edge goes back with the bar hidden.
    @MainActor func testEdgeSwipeBack() {
        let app = launch("board:acme-web")
        XCTAssertTrue(app.textFields["Filter by id or title"].firstMatch.waitForExistence(timeout: 10))
        let edge = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5))
        edge.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5)))
        XCTAssertTrue(app.buttons["Overview"].firstMatch.waitForExistence(timeout: 5), "the swipe didn't go back")
    }

    /// The filter in the screen filters; a fold opens.
    @MainActor func testBoardFilterAndFold() {
        let app = launch("board:acme-web")
        let filter = app.textFields["Filter by id or title"].firstMatch
        XCTAssertTrue(filter.waitForExistence(timeout: 10))
        app.buttons["Backlog · 3"].firstMatch.tap()
        filter.tap()
        filter.typeText("Gift")
        XCTAssertTrue(app.staticTexts["Gift cards at checkout"].firstMatch.waitForExistence(timeout: 5))
    }
}


/// The rail and Back (T1071): the tab bar, or a Duo's rail, stays on every
/// session's tab, and every screen reached by navigation has one Back, also
/// where it opened with nothing pushed under it. Columns (an iPad, an open
/// Duo) keep theirs too.
final class RailAndBackTests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    private var phone: Bool { UIDevice.current.userInterfaceIdiom == .phone }

    @MainActor private func launch(_ open: String) -> XCUIApplication {
        let app = XCUIApplication()
        // -sidebar NO: an iPad's open sidebar lies over the list.
        app.launchArguments += ["-demo", "YES", "-sidebar", "NO", "-open", open]
        app.launch()
        return app
    }

    @MainActor private func back(_ app: XCUIApplication) -> XCUIElement {
        app.buttons["BackButton"].firstMatch
    }

    /// Once the push under way has settled.
    @MainActor private func hittable(_ element: XCUIElement) -> Bool {
        let settled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "isHittable == true"), object: element)
        return XCTWaiter.wait(for: [settled], timeout: 5) == .completed
    }

    /// Needs you, Projects and Search on a session's three tabs, for a
    /// coordinator, a thread and a question opened from Needs you.
    @MainActor func testRailStaysOnEverySession() throws {
        try XCTSkipUnless(phone, "an iPad's sidebar folds away by its own rule")
        for route in ["session:acme-web", "session:acme-web/t-0104", "needs need:acme-web/t-0103"] {
            let app = launch(route)
            XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 10), route)
            for page in ["Conversation", "Terminal", "Inspector"] {
                app.buttons[page].firstMatch.tap()
                for item in ["Needs you", "Projects"] {
                    XCTAssertTrue(hittable(app.buttons[item].firstMatch), "\(item) on \(page) of \(route)")
                }
                // Not isHittable: the conversation scrolls under the bar, and
                // the accessibility hit test finds its rows there.
                XCTAssertTrue(back(app).exists, "Back on \(page) of \(route)")
            }
            back(app).tap()
            XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForNonExistence(timeout: 5), "Back left \(route)")
            app.terminate()
        }
    }

    /// A paired server's session, opened from its project: the rail and Back.
    @MainActor func testPairedSession() throws {
        try XCTSkipUnless(phone, "an iPad's sidebar folds away by its own rule")
        let app = launch("peer:studio/acme-api")
        XCTAssertTrue(back(app).waitForExistence(timeout: 10))
        let coordinator = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Coordinator")).firstMatch
        XCTAssertTrue(coordinator.waitForExistence(timeout: 10))
        coordinator.tap()
        XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 10))
        for page in ["Conversation", "Terminal", "Inspector"] {
            app.buttons[page].firstMatch.tap()
            XCTAssertTrue(hittable(app.buttons["Projects"].firstMatch), page)
            XCTAssertTrue(back(app).exists, page)
        }
        back(app).tap()
        XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForNonExistence(timeout: 5))
        XCTAssertTrue(back(app).exists, "Back on the paired project")
    }

    /// From a session the rail goes to Projects' list, and back to the session.
    @MainActor func testRailLeavesAndReturnsToSession() throws {
        try XCTSkipUnless(phone, "an iPad's sidebar folds away by its own rule")
        let app = launch("needs session:acme-web")
        XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 10))
        app.buttons["Projects"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Projects"].firstMatch.waitForExistence(timeout: 5))
        app.buttons["Needs you"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 5))
    }

    /// Each kind of screen a deep link opens has Back; on a phone taps of
    /// it reach the tab's root, which has none.
    @MainActor func testBackOnEveryRoute() {
        for route in ["project:acme-web", "overview:acme-web", "board:acme-web", "memory:acme-web", "memory:acme-web/CONTEXT.md",
                      "library:acme-web", "mods:acme-web", "task:acme-web/T12", "design:acme-web/one-page-checkout",
                      "session:acme-web", "session:acme-web/t-0104", "need:acme-web/t-0103",
                      "search:acme-web/t-0103/report.1#report", "needs session:acme-web", "needs need:acme-web/t-0103"] {
            let app = launch(route)
            XCTAssertTrue(back(app).waitForExistence(timeout: 10), "no Back on \(route)")
            guard phone else {
                app.terminate()
                continue
            }
            var taps = 0
            while back(app).exists, taps < 5 {
                back(app).tap()
                taps += 1
            }
            XCTAssertFalse(back(app).exists, "Back still on the root from \(route)")
            app.terminate()
        }
    }

    /// A tab's root has nothing to go back to.
    @MainActor func testNoBackOnRoots() {
        let app = launch("projects")
        XCTAssertTrue(app.staticTexts["Projects"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(back(app).exists)
    }

    /// Opening and leaving a coordinator and a thread again and again: the
    /// freeze of 0.82.0 came on opening a conversation (T1026).
    @MainActor func testRepeatedOpens() {
        let app = launch("project:acme-web")
        for _ in 1...15 {
            for row in ["Coordinator", "T14"] {
                let link = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ OR label CONTAINS %@", row, " \(row) ")).firstMatch
                XCTAssertTrue(link.waitForExistence(timeout: 10), row)
                link.tap()
                XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 10), row)
                app.buttons["Terminal"].firstMatch.tap()
                app.buttons["Conversation"].firstMatch.tap()
                back(app).tap()
                XCTAssertTrue(app.buttons["Overview"].firstMatch.waitForExistence(timeout: 10), row)
            }
        }
    }
}

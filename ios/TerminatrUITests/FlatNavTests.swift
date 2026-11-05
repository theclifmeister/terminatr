import XCTest

/// The screens' own bars (T1001): they hide the navigation bar, so going
/// back by its button and by the swipe from the edge, the session's tabs and
/// the board's filter and folds are the screens' own. On a phone's stack: an
/// iPad's columns have a list beside them. Then the app's own bar, rail and
/// sidebar (T1005): what TabView and .sidebarAdaptable gave it.
final class FlatNavTests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    private func phoneOnly() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom == .pad, "the columns go back by their own list")
    }

    private func padOnly() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom != .pad, "the iPad's sidebar")
    }

    /// The keyboard once it has come up and stopped, or nil when the
    /// Simulator's hardware keyboard keeps it off screen.
    @MainActor private func keyboard(_ app: XCUIApplication) -> XCUIElement? {
        let keys = app.keyboards.firstMatch
        guard keys.waitForExistence(timeout: 5) else { return nil }
        var last = CGFloat.infinity
        let settled = NSPredicate { _, _ in
            let y = keys.frame.minY
            defer { last = y }
            return y < app.frame.maxY && abs(y - last) < 0.5
        }
        let waited = XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: settled, object: nil)], timeout: 4)
        return waited == .completed ? keys : nil
    }

    private func nav(_ app: XCUIApplication, _ name: String) -> XCUIElement {
        app.buttons["nav." + name].firstMatch
    }

    @MainActor private func launch(_ open: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-demo", "YES", "-open", open]
        app.launch()
        return app
    }

    /// The main row opens the session, its tabs switch, Back goes back.
    @MainActor func testBackButtonAndTabs() throws {
        try phoneOnly()
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
    @MainActor func testEdgeSwipeBack() throws {
        try phoneOnly()
        let app = launch("board:acme-web")
        XCTAssertTrue(app.textFields["Filter by id or title"].firstMatch.waitForExistence(timeout: 10))
        let edge = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5))
        edge.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5)))
        XCTAssertTrue(app.buttons["Overview"].firstMatch.waitForExistence(timeout: 5), "the swipe didn't go back")
    }

    /// The filter in the screen filters; a fold opens.
    @MainActor func testBoardFilterAndFold() throws {
        try phoneOnly()
        let app = launch("board:acme-web")
        let filter = app.textFields["Filter by id or title"].firstMatch
        XCTAssertTrue(filter.waitForExistence(timeout: 10))
        app.buttons["Backlog · 3"].firstMatch.tap()
        filter.tap()
        filter.typeText("Gift")
        XCTAssertTrue(app.staticTexts["Gift cards at checkout"].firstMatch.waitForExistence(timeout: 5))
    }

    /// The three places, each read by VoiceOver with its count and whether
    /// it's the one showing.
    @MainActor func testBarLabelsCountAndSelected() throws {
        try phoneOnly()
        let app = launch("project:acme-web")
        let needs = nav(app, "Needs you"), projects = nav(app, "Projects")
        XCTAssertTrue(projects.waitForExistence(timeout: 10))
        XCTAssertTrue(nav(app, "Search").exists)
        XCTAssertEqual(needs.label, "Needs you")
        XCTAssertEqual(needs.value as? String, "5")
        XCTAssertTrue(projects.isSelected)
        XCTAssertFalse(needs.isSelected)
        needs.tap()
        XCTAssertTrue(needs.isSelected)
        XCTAssertFalse(projects.isSelected)
    }

    /// Each place keeps its stack and its scroll place while another shows;
    /// the place showing, chosen again, goes back to its root.
    @MainActor func testPlacesKeepTheirStackAndScroll() throws {
        try phoneOnly()
        let app = launch("project:acme-web")
        let row = app.buttons["Overview"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        let top = row.frame.minY
        app.swipeUp()
        let y = row.frame.minY
        XCTAssertLessThan(y, top, "the page didn't scroll")
        nav(app, "Needs you").tap()
        XCTAssertFalse(row.exists)
        nav(app, "Projects").tap()
        XCTAssertTrue(row.waitForExistence(timeout: 5), "the stack went")
        XCTAssertEqual(row.frame.minY, y, accuracy: 1, "the scroll place went")
        nav(app, "Projects").tap()
        XCTAssertTrue(app.staticTexts["This server"].firstMatch.waitForExistence(timeout: 5), "didn't pop to the root")
        XCTAssertFalse(row.exists)
    }

    /// At its root, the place showing chosen again goes to its top; the
    /// largest text makes Projects long enough to scroll.
    @MainActor func testRetapAtTheRootGoesToTheTop() throws {
        try phoneOnly()
        let app = XCUIApplication()
        app.launchArguments += ["-demo", "YES", "-open", "projects", "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"]
        app.launch()
        let title = app.staticTexts["This server"].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        let top = title.frame.minY
        app.swipeUp()
        XCTAssertLessThan(title.frame.minY, top, "the page didn't scroll")
        nav(app, "Projects").tap()
        let expected = NSPredicate { _, _ in abs(title.frame.minY - top) < 1 }
        wait(for: [XCTNSPredicateExpectation(predicate: expected, object: nil)], timeout: 5)
    }

    /// A session has the bottom for its composer; back from it, the bar returns.
    @MainActor func testSessionHidesTheBar() throws {
        try phoneOnly()
        let app = launch("project:acme-web")
        let coordinator = app.buttons["Coordinator"].firstMatch
        XCTAssertTrue(coordinator.waitForExistence(timeout: 10))
        coordinator.tap()
        XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(nav(app, "Projects").exists)
        app.buttons["BackButton"].firstMatch.tap()
        XCTAssertTrue(nav(app, "Projects").waitForExistence(timeout: 5))
    }

    /// The keyboard covers the bar and the page avoids it: over a tab's
    /// page, and in a session with its composer.
    @MainActor func testKeyboardAvoidance() throws {
        try phoneOnly()
        let app = launch("board:acme-web")
        let filter = app.textFields["Filter by id or title"].firstMatch
        XCTAssertTrue(filter.waitForExistence(timeout: 10))
        filter.tap()
        let bottom = nav(app, "Projects").frame.minY
        if let keys = keyboard(app) {
            XCTAssertLessThanOrEqual(filter.frame.maxY, keys.frame.minY)
        }
        XCTAssertEqual(nav(app, "Projects").frame.minY, bottom, accuracy: 1, "the bar rode up on the keyboard")

        let session = launch("session:acme-web/t-0101")
        let field = session.textViews.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        if let k = keyboard(session) {
            XCTAssertLessThanOrEqual(field.frame.maxY, k.frame.minY)
        }
    }

    /// A session's composer over the keyboard beside the iPad's rail.
    @MainActor func testKeyboardOnTheIPad() throws {
        try padOnly()
        let app = launch("session:acme-web/t-0101")
        let field = app.textViews.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        if let keys = keyboard(app) {
            XCTAssertLessThanOrEqual(field.frame.maxY, keys.frame.minY)
        }
        XCTAssertTrue(field.isHittable, "the layout swapped under the keyboard")
    }

    /// The sidebar's items, tree and member folds, its fold kept across
    /// launches; closed, the rail has the three places and opens it again.
    @MainActor func testSidebarRowsFoldsAndShowHide() throws {
        try padOnly()
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchArguments += ["-demo", "YES", "-open", "projects"]
        app.launch()
        let needs = nav(app, "Needs you"), projects = nav(app, "Projects")
        XCTAssertTrue(projects.waitForExistence(timeout: 10))
        XCTAssertTrue(projects.isSelected)
        XCTAssertEqual(needs.value as? String, "5")
        XCTAssertTrue(app.buttons["Director"].firstMatch.exists)

        // A member's threads fold, and stay so over a launch.
        let show = app.buttons["Show threads"].firstMatch
        XCTAssertTrue(show.exists)
        let shown = app.buttons.matching(identifier: "Show threads").count
        show.tap()
        XCTAssertTrue(app.buttons["Hide threads"].firstMatch.waitForExistence(timeout: 5))
        app.terminate()
        app.launch()
        let hide = app.buttons["Hide threads"].firstMatch
        XCTAssertTrue(hide.waitForExistence(timeout: 10), "the fold wasn't kept")
        hide.tap()
        XCTAssertEqual(app.buttons.matching(identifier: "Show threads").count, shown)

        // Closed, the rail; its button opens the sidebar again.
        nav(app, "sidebar.close").tap()
        XCTAssertFalse(app.buttons["Director"].firstMatch.waitForExistence(timeout: 1))
        XCTAssertTrue(nav(app, "Projects").isSelected)
        nav(app, "sidebar.open").tap()
        XCTAssertTrue(app.buttons["Director"].firstMatch.waitForExistence(timeout: 5))

        // A session's row opens it; upright it has the whole width, so the
        // sidebar folds; opened again, its row is the selected one.
        let row = app.buttons["T7 Invoice PDF export"].firstMatch
        XCTAssertTrue(row.exists)
        row.tap()
        XCTAssertTrue(app.buttons["Inspector"].firstMatch.waitForExistence(timeout: 10))
        let open = nav(app, "sidebar.open")
        XCTAssertTrue(open.waitForExistence(timeout: 5), "the sidebar didn't fold for the session")
        open.tap()
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        XCTAssertTrue(row.isSelected)
    }
}

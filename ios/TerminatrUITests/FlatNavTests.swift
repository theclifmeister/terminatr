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

import XCTest

/// Create › Review › tap a stop (mock data): its details pane, "Swap for something similar" and
/// "Remove stop" from it, and the short line when the details can't be had.
///
/// `TEST_RUNNER_SQ_SHOTS_DIR=<dir> xcodebuild test …` also saves review.png, pane.png,
/// pane-actions.png and after-remove.png there.
@MainActor
final class StopDetailUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    /// Tap stop 1 → the pane shows its title at once and its description once loaded → "Swap for
    /// something similar" closes it and opens the swap sheet. Reopen it → "Remove stop" takes the
    /// stop out (the route has one stop fewer, and "Undo" shows).
    func testTapAStopForDetailsThenSwapAndRemove() {
        let app = launchSignedIn()
        openReview(app)
        shot(app, "review")

        let rooftop = element(app, type: .button, labelBeginsWith: "Skyline Park rooftop")
        rooftop.tapWhenReady(timeout)
        let title = app.descendants(matching: .any)["stopDetail.title"]
        expect(title)
        XCTAssertTrue(title.label.hasPrefix("Skyline Park rooftop"), "Pane title: \(title.label)")
        let about = app.descendants(matching: .any)["stopDetail.about"]
        expect(about)
        XCTAssertTrue(about.label.hasPrefix("Mini golf, carnival games"), "Description: \(about.label)")
        // The route's times, whether it's fixed, the hours that day, and the way there.
        expect(element(app, labelContains: "Arrive 2:24 PM · leave 3:44 PM"))
        expect(element(app, labelContains: "Drop in any time while it's open"))
        expect(app.buttons["Directions"])
        if shotsDir != nil {
            // Let the map tiles come in, then show the rest of the pane (the actions stay pinned).
            Thread.sleep(forTimeInterval: 2)
            shot(app, "pane")
            about.swipeUp()
            Thread.sleep(forTimeInterval: 1)
            shot(app, "pane-actions")
        }

        app.buttons["Swap for something similar"].tapWhenReady(timeout)
        expectGone(title)
        let swapSheet = element(app, labelBeginsWith: "Swap Skyline Park rooftop")
        expect(swapSheet)
        // Close the swap sheet from its backdrop.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.06)).tap()
        expectGone(swapSheet)

        rooftop.tapWhenReady(timeout)
        expect(title)
        app.buttons["Remove stop"].tapWhenReady(timeout)
        expectGone(title)
        expectGone(app.descendants(matching: .any)["Reorder Skyline Park rooftop"])
        let first = app.descendants(matching: .any)["Reorder Krog Street Tunnel murals"]
        expect(first)
        XCTAssertEqual(first.value as? String, "Stop 1 of 2")
        expect(app.buttons["Undo"])
        shot(app, "after-remove")
    }

    /// `-SQMockFail activity`: the pane keeps what the stop says and puts one short line and
    /// "Try again" where the details would be.
    func testDetailsThatCantLoadSaySo() {
        let app = launchSignedIn(["-SQMockFail", "activity"])
        openReview(app)

        element(app, type: .button, labelBeginsWith: "Krog Street Tunnel murals").tapWhenReady(timeout)
        expect(app.descendants(matching: .any)["stopDetail.title"])
        expect(app.staticTexts["More details aren't available right now."])
        expect(app.buttons["Try again"])
        expect(element(app, labelContains: "Arrive 3:58 PM · leave 4:58 PM"))
        expect(app.buttons["Swap for something similar"])
        expect(app.buttons["Remove stop"])
    }

    // MARK: Helpers

    /// Login → + → Next ×3: Review with its options and the first option's route timed.
    private func openReview(_ app: XCUIApplication) {
        app.buttons["tab.plan"].tapWhenReady(timeout)
        for _ in 0..<3 { app.buttons["Next"].tapWhenReady(timeout) }
        expect(app.staticTexts["Pick a sidequest"])
        // B's row reads "Arrive … · back by …" once the route is timed.
        expect(element(app, labelContains: "back by"))
    }

    private func launchSignedIn(_ extraArguments: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES",
                                "-SQAPIMode", "mock", "-SQMockLatency", "0"]
        if let extra = ProcessInfo.processInfo.environment["SQ_UI_LAUNCH_ARGS"] {
            app.launchArguments += extra.split(separator: " ").map(String.init)
        }
        app.launchArguments += extraArguments
        app.launch()
        type(app.textFields["Email"], "jordan@gatech.edu")
        type(app.secureTextFields["Password"], "sidequest1\n")
        expect(app.buttons["Sidequests"])
        return app
    }

    private var shotsDir: String? { ProcessInfo.processInfo.environment["SQ_SHOTS_DIR"] }

    /// Writes the screen to `<SQ_SHOTS_DIR>/<name>.png` when the run asks for screenshots.
    private func shot(_ app: XCUIApplication, _ name: String) {
        guard let shotsDir else { return }
        let url = URL(fileURLWithPath: shotsDir).appendingPathComponent("\(name).png")
        XCTAssertNoThrow(try FileManager.default.createDirectory(atPath: shotsDir, withIntermediateDirectories: true))
        XCTAssertNoThrow(try app.screenshot().pngRepresentation.write(to: url))
    }

    private func type(_ field: XCUIElement, _ text: String, file: StaticString = #filePath, line: UInt = #line) {
        field.tapWhenReady(timeout, file: file, line: line)
        field.typeText(text)
    }

    private func expect(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(element.waitForExistence(timeout: timeout), "Missing: \(element)", file: file, line: line)
    }

    private func expectGone(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: timeout), .completed, "Still there: \(element)", file: file, line: line)
    }

    private func element(_ app: XCUIApplication, type: XCUIElement.ElementType = .any, labelContains text: String) -> XCUIElement {
        app.descendants(matching: type).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    private func element(_ app: XCUIApplication, type: XCUIElement.ElementType = .any, labelBeginsWith text: String) -> XCUIElement {
        app.descendants(matching: type).matching(NSPredicate(format: "label BEGINSWITH %@", text)).firstMatch
    }
}

private extension XCUIElement {
    func tapWhenReady(_ timeout: TimeInterval, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(waitForExistence(timeout: timeout), "Missing: \(self)", file: file, line: line)
        tap()
    }
}

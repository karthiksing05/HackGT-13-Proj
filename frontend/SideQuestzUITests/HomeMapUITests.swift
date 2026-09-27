import XCTest

/// Home › Sidequests on the demo backend (fully offline; the demo is at Tech Square, Friday 2:10
/// PM): each sidequest's progress strip, the Timeline | Map pill, which each sidequest remembers
/// on its own, and the map's stops.
@MainActor
final class HomeMapUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    /// Friday's strip says what's next; the pill switches Friday to its map; a stop opens its sheet;
    /// swiping to Saturday shows its timeline while Friday stays on the map; and back.
    func testTimelineAndMapPerSidequest() {
        let app = launchSignedIn()

        expect(element(app, labelBeginsWith: "Next: Skyline Park rooftop at 2:30 PM"))
        let pill = app.descendants(matching: .any)["sidequest.viewMode"]
        expect(pill)
        XCTAssertEqual(pill.value as? String, "Timeline")

        // Tap the pill's map side.
        pill.coordinate(withNormalizedOffset: CGVector(dx: 0.78, dy: 0.5)).tap()
        let map = app.descendants(matching: .any)["sidequest.map"].firstMatch
        expect(map)
        XCTAssertEqual(pill.value as? String, "Map")
        // The page scrolls over the map (it doesn't pan), bringing its stops above the tab bar.
        app.swipeUp()
        let murals = element(app, type: .button, labelBeginsWith: "Stop 2, Krog Street Tunnel murals")
        expect(element(app, type: .button, labelBeginsWith: "Stop 1, Skyline Park rooftop"))
        expect(murals)
        expect(element(app, type: .button, labelBeginsWith: "Stop 3, Group dinner, Krog Street Market"))
        expect(app.descendants(matching: .any)["You are here"].firstMatch)

        // A stop opens the same sheet as its timeline block.
        murals.tap()
        expect(element(app, labelContains: "Self-guided street art walk"))
        closeSheet(app)
        expectGone(element(app, labelContains: "Self-guided street art walk"))

        // Saturday (swiped to, over Friday's strip) is still on its timeline; the header's pill
        // follows the page.
        element(app, labelBeginsWith: "Next: Skyline Park rooftop").swipeLeft()
        let saturday = element(app, labelBeginsWith: "Starts Saturday, September 26 at 1:00 PM")
        waitUntil { saturday.isHittable }
        XCTAssertTrue(saturday.isHittable, "Saturday's page isn't showing")
        waitUntil { pill.value as? String == "Timeline" }
        XCTAssertEqual(pill.value as? String, "Timeline")
        expect(element(app, type: .button, labelBeginsWith: "Atlanta Botanical Garden"))

        // Friday kept its map.
        saturday.swipeRight()
        waitUntil { pill.value as? String == "Map" }
        XCTAssertEqual(pill.value as? String, "Map")
        expect(map)

        // Swiping the pill back switches Friday to its timeline.
        pill.swipeLeft()
        waitUntil { pill.value as? String == "Timeline" }
        XCTAssertEqual(pill.value as? String, "Timeline")
        expectGone(map)
        expect(element(app, type: .button, labelBeginsWith: "Skyline Park rooftop"))
    }

    /// `-SQRoute home/map/itin-sat` opens Saturday on its map, in view. Its last stop has no
    /// location, so it's left off the map (the others keep their numbers).
    func testMapDeepLink() {
        let app = launch(["-SQRoute", "home/map/itin-sat"])

        let pill = app.descendants(matching: .any)["sidequest.viewMode"]
        expect(pill)
        waitUntil { pill.value as? String == "Map" }
        XCTAssertEqual(pill.value as? String, "Map")
        // The carousel is on Saturday's page, and its map's stops are on screen.
        let strip = element(app, labelBeginsWith: "Starts Saturday, September 26 at 1:00 PM")
        expect(strip)
        waitUntil { strip.isHittable }
        XCTAssertTrue(strip.isHittable, "Saturday's page isn't showing")
        expect(app.descendants(matching: .any)["sidequest.map"].firstMatch)
        let garden = element(app, type: .button, labelBeginsWith: "Stop 1, Atlanta Botanical Garden")
        expect(garden)
        XCTAssertTrue(garden.isHittable, "Stop 1 isn't on screen")
        expect(element(app, type: .button, labelBeginsWith: "Stop 2, Frisbee meetup"))
        XCTAssertFalse(element(app, labelBeginsWith: "Stop 3,").exists)
    }

    // MARK: Helpers

    private func launch(_ extraArguments: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES",
                                "-SQAPIMode", "mock", "-SQMockLatency", "0"]
        app.launchArguments += extraArguments
        app.launch()
        return app
    }

    private func launchSignedIn() -> XCUIApplication {
        let app = launch()
        let email = app.textFields["Email"]
        email.tapWhenReady(timeout)
        email.typeText("jordan@gatech.edu")
        let password = app.secureTextFields["Password"]
        password.tapWhenReady(timeout)
        password.typeText("sidequest1\n")
        expect(app.buttons["Sidequests"])
        return app
    }

    /// Closes the open sheet by tapping the dimmed backdrop above it (a full-screen "Close" button,
    /// so tapping the element itself would land on the sheet).
    private func closeSheet(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        let closes = app.buttons.matching(NSPredicate(format: "label == %@", "Close"))
        guard let backdrop = closes.allElementsBoundByIndex.first(where: { $0.frame.width >= app.frame.width - 1 }) else {
            XCTFail("No sheet to close", file: file, line: line)
            return
        }
        backdrop.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.08)).tap()
    }

    private func expect(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(element.waitForExistence(timeout: timeout), "Missing: \(element)", file: file, line: line)
    }

    private func expectGone(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: timeout), .completed, "Still there: \(element)", file: file, line: line)
    }

    /// Checks `condition` until it holds or the timeout passes (a value that settles after an animation).
    private func waitUntil(_ condition: () -> Bool) {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition(), Date() < deadline { Thread.sleep(forTimeInterval: 0.2) }
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

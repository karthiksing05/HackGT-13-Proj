import XCTest

/// Create › Bring friends (mock data): pick two friends on Vibe, see them on Review (and in More
/// options), start the sidequest, and Home has it with them going. Also the `create/3/friends` link
/// and "Just me" switching to "Friends only" when you pick someone.
///
/// `TEST_RUNNER_SQ_SHOTS_DIR=<dir> xcodebuild test …` also saves vibe-friends.png, picker.png,
/// review-friends.png and home-after.png there.
@MainActor
final class CreateInviteUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    /// Login → + → Next ×2 → Bring friends → Maya + Dev → Done (2) → Review shows "With Maya and Dev"
    /// → More options › Friends reopens the picker → Start → Home: the plan, 3 going.
    func testBringTwoFriendsIntoANewSidequest() {
        let app = launchSignedIn()
        app.buttons["tab.plan"].tapWhenReady(timeout)
        app.buttons["Next"].tapWhenReady(timeout)
        app.buttons["Next"].tapWhenReady(timeout)
        expect(app.staticTexts["What are you in the mood for?"])

        let bring = app.buttons["Bring friends"]
        expect(bring)
        XCTAssertEqual(bring.value as? String, "None added")
        bring.tap()

        let maya = friendRow(app, "u-mr")
        let dev = friendRow(app, "u-dp")
        expect(maya)
        expect(element(app, labelContains: "Free until 8 PM"))
        maya.tap()
        dev.tap()
        waitUntil { maya.value as? String == "1" && dev.value as? String == "1" }
        XCTAssertEqual(maya.value as? String, "1")
        XCTAssertEqual(friendRow(app, "u-st").value as? String, "0")
        // "Done (2)" on screen; VoiceOver: "Done, 2 added".
        let done = app.buttons["invite.done"]
        XCTAssertEqual(done.label, "Done")
        XCTAssertEqual(done.value as? String, "2 added")
        shot(app, "picker")
        done.tap()
        expectGone(maya)

        waitUntil { bring.value as? String == "2 added" }
        XCTAssertEqual(bring.value as? String, "2 added")
        if shotsDir != nil {
            Thread.sleep(forTimeInterval: 1)
            shot(app, "vibe-friends")
        }

        app.buttons["Next"].tapWhenReady(timeout)
        expect(app.staticTexts["Pick a sidequest"])
        expect(element(app, labelContains: "With Maya and Dev"))
        // Wait for the route before the screenshot and Start.
        expect(element(app, labelContains: "back by"))
        shot(app, "review-friends")

        // More options › Friends › Edit opens the picker again, with both still picked.
        app.buttons["More options"].tapWhenReady(timeout)
        expect(element(app, labelContains: "Maya and Dev"))
        if shotsDir != nil {
            // The answers card scrolls inside the sheet: bring the Friends row up.
            let row = element(app, labelContains: "Time window").coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
            row.press(forDuration: 0.1, thenDragTo: row.withOffset(CGVector(dx: 0, dy: -260)))
            Thread.sleep(forTimeInterval: 1)
            shot(app, "more-friends")
        }
        app.buttons["Edit Friends"].tapWhenReady(timeout)
        expect(maya)
        waitUntil { maya.value as? String == "1" }
        XCTAssertEqual(maya.value as? String, "1")
        app.buttons["invite.done"].tap()
        expectGone(maya)

        let start = app.buttons["Start this sidequest"]
        start.tapWhenReady(timeout)
        expectGone(start)
        expect(app.buttons["Sidequests"])
        expect(element(app, labelContains: "Rooftop + murals"))
        expect(element(app, labelContains: "3 going"))
        if shotsDir != nil {
            Thread.sleep(forTimeInterval: 1.5)
            shot(app, "home-after")
        }
    }

    /// `create/3/friends` opens the picker over Vibe; its search narrows the list. With "Just me"
    /// chosen, picking a friend switches Who's coming to "Friends only" and says so; "Just me" again
    /// takes them off.
    func testPickingWhileJustMeSwitchesToFriendsOnly() {
        let app = launch(["-SQRoute", "create/3/friends"])
        let done = app.buttons["invite.done"]
        expect(done)
        expect(friendRow(app, "u-mr"))
        let search = app.textFields["Search your friends"]
        search.tapWhenReady(timeout)
        search.typeText("sa")
        expectGone(friendRow(app, "u-mr"))
        expect(friendRow(app, "u-st"))
        done.tap()
        expectGone(done)

        app.buttons["Just me"].tapWhenReady(timeout)
        waitUntil { app.buttons["Just me"].isSelected }
        app.buttons["Bring friends"].tapWhenReady(timeout)
        // The search was cleared when the picker closed.
        expect(friendRow(app, "u-mr"))
        let sam = friendRow(app, "u-st")
        sam.tapWhenReady(timeout)
        waitUntil { sam.value as? String == "1" }
        done.tapWhenReady(timeout)
        expectGone(sam)

        expect(app.staticTexts["Switched to Friends only so your friends can come."])
        XCTAssertTrue(app.buttons["Friends only"].isSelected)
        XCTAssertFalse(app.buttons["Just me"].isSelected)
        XCTAssertEqual(app.buttons["Bring friends"].value as? String, "1 added")
        if shotsDir != nil {
            Thread.sleep(forTimeInterval: 1)
            shot(app, "vibe-switched")
        }

        app.buttons["Just me"].tap()
        expect(app.staticTexts["Just you now, so Sam was taken off."])
        waitUntil { app.buttons["Bring friends"].value as? String == "None added" }
        XCTAssertEqual(app.buttons["Bring friends"].value as? String, "None added")
    }

    // MARK: Helpers

    /// A picker row (a switch for VoiceOver): "invite.<user id>".
    private func friendRow(_ app: XCUIApplication, _ userId: String) -> XCUIElement {
        app.descendants(matching: .any)["invite.\(userId)"]
    }

    private func launch(_ extraArguments: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES",
                                "-SQAPIMode", "mock", "-SQMockLatency", "0"]
        if let extra = ProcessInfo.processInfo.environment["SQ_UI_LAUNCH_ARGS"] {
            app.launchArguments += extra.split(separator: " ").map(String.init)
        }
        app.launchArguments += extraArguments
        app.launch()
        return app
    }

    private func launchSignedIn() -> XCUIApplication {
        let app = launch()
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

    /// Checks `condition` until it holds or the timeout passes (for state that settles after an animation).
    private func waitUntil(_ condition: () -> Bool) {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition(), Date() < deadline { Thread.sleep(forTimeInterval: 0.2) }
    }

    private func element(_ app: XCUIApplication, type: XCUIElement.ElementType = .any, labelContains text: String) -> XCUIElement {
        app.descendants(matching: type).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }
}

private extension XCUIElement {
    func tapWhenReady(_ timeout: TimeInterval, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(waitForExistence(timeout: timeout), "Missing: \(self)", file: file, line: line)
        tap()
    }
}

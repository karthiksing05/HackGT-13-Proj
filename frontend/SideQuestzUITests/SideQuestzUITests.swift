import XCTest

/// End-to-end flows on mock data (fully offline). Every test launches signed out, past the intro,
/// with no simulated latency, and drives the app with real taps, typing and drags.
@MainActor
final class SideQuestzUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    // MARK: Auth

    /// Create an account through all five Setup steps, land on Home, and the tabs respond.
    func testCreateAccountThroughSetup() {
        let app = launch()

        element(app, type: .button, labelContains: "Create an account").tapWhenReady(timeout)
        type(app.textFields["Name"], "Sam Rivera")
        type(app.textFields["Email"], "sam@gatech.edu")
        typeOwnPassword(app, app.secureTextFields["Create a password"], "wander2026")
        typeOwnPassword(app, app.secureTextFields["Confirm password"], "wander2026")
        type(app.textFields["Username (optional)"], "samr\n")

        app.buttons["Continue"].tapWhenReady(timeout)
        app.buttons["Continue without a calendar"].tapWhenReady(timeout)
        expect(app.staticTexts["What do you enjoy?"])
        app.buttons["Continue"].tapWhenReady(timeout)
        app.buttons["Continue"].tapWhenReady(timeout)
        app.buttons["Finish setup"].tapWhenReady(timeout)

        expectMainShellResponds(app)
    }

    /// Forgot password → code → new password → "Password updated" → sign in with it.
    func testResetPasswordThenSignIn() {
        let app = launch()

        app.buttons["Forgot password?"].tapWhenReady(timeout)
        type(app.textFields["Email"], "jordan@gatech.edu")
        app.buttons["Send code"].tapWhenReady(timeout)
        type(app.textFields["6-digit code"], "123456")
        app.buttons["Verify code"].tapWhenReady(timeout)
        type(app.secureTextFields["New password"], "wander2026")
        type(app.secureTextFields["Confirm new password"], "wander2026\n")

        expect(app.staticTexts["Password updated"])
        app.buttons["Back to sign in"].tapWhenReady(timeout)
        XCTAssertEqual(app.textFields["Email"].value as? String, "jordan@gatech.edu")
        type(app.secureTextFields["Password"], "wander2026\n")

        expectMainShellResponds(app)
    }

    // MARK: Planning

    /// Login → + → Next ×3 → "Start this sidequest" → Home shows the new plan.
    func testSignInPlanAndStartSidequest() {
        let app = launchSignedIn()

        app.buttons["tab.plan"].tapWhenReady(timeout)
        expect(app.staticTexts["Where do you start and end?"])
        app.buttons["Next"].tap()
        expect(app.staticTexts["When are you free?"])
        app.buttons["Next"].tap()
        expect(app.staticTexts["What are you in the mood for?"])
        app.buttons["Next"].tap()
        expect(app.staticTexts["Pick a sidequest"])
        expect(element(app, labelContains: "Rooftop + murals"))

        let start = app.buttons["Start this sidequest"]
        start.tap()
        expectGone(start)
        expect(app.buttons["Itineraries"])
        expect(element(app, labelContains: "Rooftop + murals"))
    }

    /// Create › Review: drag stop 1 below stop 2 → the route re-times ("Transit times updated").
    func testReorderingStopsRetimesTheRoute() {
        let app = launchSignedIn()

        app.buttons["tab.plan"].tapWhenReady(timeout)
        for _ in 0..<3 { app.buttons["Next"].tapWhenReady(timeout) }
        expect(app.staticTexts["Pick a sidequest"])

        let first = app.descendants(matching: .any)["Reorder Skyline Park rooftop"]
        let second = app.descendants(matching: .any)["Reorder Krog Street Tunnel murals"]
        expect(first)
        expect(second)
        XCTAssertEqual(first.value as? String, "Stop 1 of 3")

        let from = first.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        let to = second.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9))
        from.press(forDuration: 0.2, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.2)

        expect(element(app, labelContains: "Transit times updated"))
        XCTAssertEqual(first.value as? String, "Stop 2 of 3")
    }

    /// Home › Calendar: press and hold an empty stretch, then drag down → "Plan this window" → Create
    /// opens with that window filled in. The window is too short for a full plan, so Review flags the
    /// route late.
    func testCalendarDragToPlanPrefillsCreate() {
        let app = launchSignedIn()

        app.buttons["Calendar"].tapWhenReady(timeout)
        let math = element(app, type: .button, labelBeginsWith: "MATH 3012")
        let lecture = element(app, type: .button, labelBeginsWith: "CS 3510 lecture")
        expect(math)
        expect(lecture)

        // 10:45 AM–1:00 PM is free: hold just under MATH 3012 (0.4 s activates), then stretch the
        // window to just above the lecture.
        let x = math.frame.midX
        let from = point(app, x: x, y: math.frame.maxY + 8)
        let to = point(app, x: x, y: lecture.frame.minY - 8)
        from.press(forDuration: 0.8, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.2)

        let plan = element(app, type: .button, labelBeginsWith: "Plan this window")
        expect(plan)
        // "Plan this window, 11:00 AM–12:45 PM" → "11:00 AM".
        let window = plan.label.replacingOccurrences(of: "Plan this window, ", with: "")
        let startTime = String(window.split(separator: "–").first ?? "")
        XCTAssertFalse(startTime.isEmpty, "Unexpected label: \(plan.label)")
        plan.tap()

        expect(app.staticTexts["Where do you start and end?"])
        app.buttons["Next"].tap()
        expect(app.staticTexts["When are you free?"])
        expect(element(app, labelContains: "at \(startTime)"))

        app.buttons["Next"].tap()
        app.buttons["Next"].tap()
        expect(app.staticTexts["Pick a sidequest"])
        expect(element(app, labelContains: "min past"))
    }

    // MARK: Social

    /// Forum › "Plan together" → "Message sent" → opens the DM with the intro message → Back.
    func testForumPlanTogetherOpensDirectMessage() {
        let app = launchSignedIn()

        app.buttons["tab.forum"].tapWhenReady(timeout)
        app.buttons["Plan together"].firstMatch.tapWhenReady(timeout)
        let sent = app.buttons["Message sent"].firstMatch
        expect(sent)
        sent.tap()
        expect(element(app, labelContains: "Saw your post. Want to plan something together?"))
        app.buttons["Back"].tap()
        expect(app.buttons["Message sent"].firstMatch)
    }

    /// Groups › Krog St dinner crew › Splits › "+ Add an expense" → Pizza, $40 split 3 ways → banner.
    func testAddExpenseShowsConfirmation() {
        let app = launchSignedIn()

        app.buttons["tab.groups"].tapWhenReady(timeout)
        element(app, type: .button, labelBeginsWith: "Krog St dinner crew").tapWhenReady(timeout)
        app.buttons["Splits"].tapWhenReady(timeout)
        app.buttons["Add an expense"].tapWhenReady(timeout)

        type(app.textFields["What was it for?"], "Pizza")
        type(app.textFields["Total amount in dollars"], "40")

        // The preview adds up to the cent: $13.34 + $13.33 + $13.33.
        element(app, type: .button, labelBeginsWith: "Add $40.00 · split 3 ways").tapWhenReady(timeout)
        expect(element(app, labelContains: "Added \"Pizza\""))
    }

    // MARK: Helpers

    private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES",
                                "-SQAPIMode", "mock", "-SQMockLatency", "0"]
        app.launch()
        return app
    }

    private func launchSignedIn() -> XCUIApplication {
        let app = launch()
        type(app.textFields["Email"], "jordan@gatech.edu")
        type(app.secureTextFields["Password"], "sidequest1\n")
        expect(app.buttons["Itineraries"])
        return app
    }

    /// Home is up and a tab switch works (nothing invisible is left over the app).
    private func expectMainShellResponds(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        expect(app.buttons["Itineraries"], file: file, line: line)
        let forum = app.buttons["tab.forum"]
        XCTAssertTrue(forum.isHittable, "The tab bar doesn't take taps", file: file, line: line)
        forum.tap()
        expect(app.buttons["Plan together"].firstMatch, file: file, line: line)
    }

    private func type(_ field: XCUIElement, _ text: String, file: StaticString = #filePath, line: UInt = #line) {
        field.tapWhenReady(timeout, file: file, line: line)
        field.typeText(text)
    }

    /// Sign-up forms get iOS's "Use Strong Password?" sheet; close it and type our own, like a
    /// user who picks their own password.
    private func typeOwnPassword(_ app: XCUIApplication, _ field: XCUIElement, _ text: String) {
        field.tapWhenReady(timeout)
        if app.buttons["GenerateStrongPasswordButton"].waitForExistence(timeout: 2) {
            app.buttons["xmark"].tap()
            field.tap()
        }
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

    private func point(_ app: XCUIApplication, x: CGFloat, y: CGFloat) -> XCUICoordinate {
        app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: x, dy: y))
    }
}

private extension XCUIElement {
    func tapWhenReady(_ timeout: TimeInterval, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(waitForExistence(timeout: timeout), "Missing: \(self)", file: file, line: line)
        tap()
    }
}

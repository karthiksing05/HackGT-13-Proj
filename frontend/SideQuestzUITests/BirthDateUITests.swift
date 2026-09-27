import XCTest

/// Setup › basics › "Date of birth" on mock data: the whole field opens month / day / year wheels,
/// Done sets the date they show (the one they start on too), Cancel or the backdrop keeps the old
/// date, the age note follows, and the date stays when setup comes back to step 1.
@MainActor
final class BirthDateUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    /// Done without turning a wheel sets the date they start on (20 years back); Continue creates
    /// the account, and Back to step 1 (which then edits it with `PATCH /me`) still shows the date.
    func testStartingDateCanBeChosenAndStays() {
        let app = launch()
        let field = openBasics(app)
        XCTAssertEqual(field.label, "Date of birth")
        XCTAssertEqual(field.value as? String, "Not set")
        screenshot(app, "dob-field")

        field.tap()
        expect(app.buttons["setup.birthDate.done"])
        XCTAssertEqual(app.pickerWheels.count, 3)
        XCTAssertEqual(app.pickerWheels.element(boundBy: 0).value as? String, "September")
        XCTAssertEqual(app.pickerWheels.element(boundBy: 1).value as? String, "25")
        XCTAssertEqual(app.pickerWheels.element(boundBy: 2).value as? String, "2006")
        screenshot(app, "dob-sheet")
        app.buttons["setup.birthDate.done"].tap()

        expectValue(field, "September 25, 2006")
        expect(element(app, labelContains: "Age 20: we'll hide 21+ events"))
        app.buttons["Continue"].tapWhenReady(timeout)
        expect(app.buttons["Continue without a calendar"])

        app.buttons["Back"].tapWhenReady(timeout)
        expectValue(field, "September 25, 2006")
        app.buttons["Continue"].tapWhenReady(timeout)
        expect(app.buttons["Continue without a calendar"])
    }

    /// Turning the wheels then Done sets that date; Cancel and the backdrop keep it.
    func testWheelsSetTheDateAndCancelKeepsIt() {
        let app = launch()
        let field = openBasics(app)

        field.tap()
        setWheels(app, month: "June", day: "14", year: "2003")
        app.buttons["setup.birthDate.done"].tap()
        expectValue(field, "June 14, 2003")
        expect(element(app, labelContains: "Age 23: 21+ events can show up"))
        screenshot(app, "dob-set")

        // The wheels open on the date that's set; Cancel keeps it.
        field.tap()
        expect(app.buttons["setup.birthDate.cancel"])
        XCTAssertEqual(app.pickerWheels.element(boundBy: 2).value as? String, "2003")
        app.pickerWheels.element(boundBy: 2).adjust(toPickerWheelValue: "2010")
        app.buttons["setup.birthDate.cancel"].tap()
        expectValue(field, "June 14, 2003")

        // So does tapping the backdrop.
        field.tap()
        expect(app.buttons["setup.birthDate.done"])
        app.pickerWheels.element(boundBy: 2).adjust(toPickerWheelValue: "2012")
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.12)).tap()
        expectGone(app.buttons["setup.birthDate.done"])
        expectValue(field, "June 14, 2003")
        expect(element(app, labelContains: "Age 23: 21+ events can show up"))
    }

    /// Under 13: the note says so and Continue stays on step 1.
    func testUnder13BlocksContinue() {
        let app = launch()
        let field = openBasics(app)

        field.tap()
        setWheels(app, month: "September", day: "25", year: "2015")
        app.buttons["setup.birthDate.done"].tap()
        expectValue(field, "September 25, 2015")
        expect(element(app, labelContains: "You need to be 13 or older to use SideQuests."))

        app.buttons["Continue"].tapWhenReady(timeout)
        XCTAssertFalse(app.buttons["Continue without a calendar"].waitForExistence(timeout: 3), "Continued under 13")
        XCTAssertTrue(app.staticTexts["Step 1 of 5"].exists)
    }

    /// Setup resumed after signing in: `GET /me` has no date of birth, so the field starts empty;
    /// a date picked there is sent with `PATCH /me` and stays when coming back to step 1.
    func testResumedSetupKeepsTheDate() {
        let app = launch()
        let field = openBasics(app)
        app.buttons["Continue"].tapWhenReady(timeout)
        expect(app.buttons["Continue without a calendar"])

        // Back twice leaves setup signed out; signing in again resumes it at step 2.
        app.buttons["Back"].tapWhenReady(timeout)
        expect(field)
        app.buttons["Back"].tapWhenReady(timeout)
        type(app.secureTextFields["Password"], "wander2026\n")
        expect(app.buttons["Continue without a calendar"])

        app.buttons["Back"].tapWhenReady(timeout)
        expectValue(field, "Not set")
        field.tap()
        setWheels(app, month: "March", day: "3", year: "2001")
        app.buttons["setup.birthDate.done"].tap()
        expectValue(field, "March 3, 2001")
        app.buttons["Continue"].tapWhenReady(timeout)
        expect(app.buttons["Continue without a calendar"])
        app.buttons["Back"].tapWhenReady(timeout)
        expectValue(field, "March 3, 2001")
    }

    // MARK: Helpers

    private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES",
                                "-SQAPIMode", "mock", "-SQMockLatency", "0"]
        app.launch()
        return app
    }

    /// Login › "Create an account" › step 1 filled in, scrolled to the date of birth.
    private func openBasics(_ app: XCUIApplication) -> XCUIElement {
        element(app, type: .button, labelContains: "Create an account").tapWhenReady(timeout)
        type(app.textFields["Name"], "Sam Rivera")
        type(app.textFields["Email"], "sam@gatech.edu")
        typeOwnPassword(app, app.secureTextFields["Create a password"], "wander2026")
        typeOwnPassword(app, app.secureTextFields["Confirm password"], "wander2026")
        type(app.textFields["Username (optional)"], "samr\n")
        let field = app.buttons["setup.birthDate"]
        expect(field)
        app.swipeUp()
        return field
    }

    /// Month / day / year, the US order the sheet uses.
    private func setWheels(_ app: XCUIApplication, month: String, day: String, year: String) {
        expect(app.buttons["setup.birthDate.done"])
        app.pickerWheels.element(boundBy: 0).adjust(toPickerWheelValue: month)
        app.pickerWheels.element(boundBy: 1).adjust(toPickerWheelValue: day)
        app.pickerWheels.element(boundBy: 2).adjust(toPickerWheelValue: year)
    }

    private func expectValue(_ element: XCUIElement, _ value: String, file: StaticString = #filePath, line: UInt = #line) {
        let matches = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", value), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [matches], timeout: timeout), .completed,
                       "\(element) shows \(String(describing: element.value)), not \(value)", file: file, line: line)
    }

    /// Keeps a screenshot in the test results when the run asks for them
    /// (`TEST_RUNNER_SQ_UI_SCREENSHOTS=1 xcodebuild test …`).
    private func screenshot(_ app: XCUIApplication, _ name: String) {
        guard ProcessInfo.processInfo.environment["SQ_UI_SCREENSHOTS"] != nil else { return }
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    private func type(_ field: XCUIElement, _ text: String, file: StaticString = #filePath, line: UInt = #line) {
        field.tapWhenReady(timeout, file: file, line: line)
        field.typeText(text)
    }

    /// Sign-up forms get iOS's "Use Strong Password?" sheet; close it and type our own.
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
}

/// Home › Sidequests timeline: a block too short for its label (Saturday's 15-minute walk) is a
/// slim bar with its label in a capsule, and the capsule opens the block, also where it overhangs.
@MainActor
final class TimelineCapsuleUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    func testSlimBlockCapsuleOpensTheBlock() {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES",
                                "-SQAPIMode", "mock", "-SQMockLatency", "0"]
        app.launch()
        let email = app.textFields["Email"]
        XCTAssertTrue(email.waitForExistence(timeout: timeout))
        email.tap()
        email.typeText("jordan@gatech.edu")
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText("sidequest1\n")

        let saturday = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Saturday reset")).firstMatch
        XCTAssertTrue(saturday.waitForExistence(timeout: timeout))
        saturday.tap()
        let walk = app.buttons["Walk to the Active Oval, 2:45–3:00 PM, Transit"]
        XCTAssertTrue(walk.waitForExistence(timeout: timeout))
        app.swipeUp()
        if ProcessInfo.processInfo.environment["SQ_UI_SCREENSHOTS"] != nil {
            let attachment = XCTAttachment(screenshot: app.screenshot())
            attachment.name = "timeline-saturday"
            attachment.lifetime = .keepAlways
            add(attachment)
        }

        // The block's frame is its 18pt capsule's (the 14pt bar sits 2pt inside it, top and
        // bottom): tap the capsule's text, then its top edge, 1pt above the bar.
        let sheetTitle = app.staticTexts["Walk to the Active Oval"]
        let frame = walk.frame
        XCTAssertEqual(frame.height, 18, accuracy: 0.5)
        let origin = app.coordinate(withNormalizedOffset: .zero)
        for y in [frame.midY, frame.minY + 1] {
            origin.withOffset(CGVector(dx: frame.minX + 40, dy: y)).tap()
            XCTAssertTrue(sheetTitle.waitForExistence(timeout: timeout), "The capsule didn't open the block (y \(y))")
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.06)).tap()
            let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: sheetTitle)
            XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: timeout), .completed)
        }
    }
}

private extension XCUIElement {
    func tapWhenReady(_ timeout: TimeInterval, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(waitForExistence(timeout: timeout), "Missing: \(self)", file: file, line: line)
        tap()
    }
}

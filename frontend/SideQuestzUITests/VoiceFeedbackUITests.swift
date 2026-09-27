import XCTest

/// Voice input in the offline demo (`-SQVoiceDemo YES`): the demo's words stream in one by one
/// while "Listening…" shows, then "Transcribing…" until the whole transcript lands.
@MainActor
final class VoiceFeedbackUITests: XCTestCase {
    private let timeout: TimeInterval = 15

    /// Setup › Tell us more: the first answer box fills word by word while you talk.
    func testSetupAnswerFillsWhileYouTalk() {
        let app = launch(route: "setup/5")
        let mic = app.buttons["setup.voice.q1"]
        let answer = app.descendants(matching: .any)["setup.answer.q1"]
        mic.tapWhenReady(timeout)

        expect(element(app, labelContains: "Listening… tap the mic again to stop"))
        waitUntil { self.text(of: answer).hasPrefix("Walking") }
        XCTAssertFalse(text(of: answer).contains("Nothing too loud"), "The whole answer arrived at once: \(text(of: answer))")
        waitUntil { self.text(of: answer).contains("somewhere green with") }
        XCTAssertTrue(text(of: answer).contains("somewhere green with"), "The words didn't keep coming: \(text(of: answer))")
        screenshot(app, "voice-setup-listening")

        app.buttons["Stop voice input"].firstMatch.tap()
        let transcribing = element(app, labelContains: "Transcribing")
        XCTAssertTrue(appears(transcribing), "No “Transcribing…” after stopping")
        screenshot(app, "voice-setup-transcribing")
        expectGone(transcribing)
        XCTAssertEqual(text(of: answer),
                       "Walking somewhere green with a coffee, then trying a new food spot with one or two friends. Nothing too loud.")
        XCTAssertTrue(app.buttons["setup.voice.q1"].isHittable, "The mic is back for another go")
        screenshot(app, "voice-setup-done")
    }

    /// Create › Vibe: the words stream into the voice card's quote, then land in "Or type it".
    func testVibeQuoteStreamsThenFillsTheMood() {
        let app = launch(route: "create/3")
        let mic = app.buttons["create.voice"]
        mic.tapWhenReady(timeout)

        expect(element(app, labelContains: "Listening… tap to stop"))
        let quote = element(app, labelContains: "Something chill")
        expect(quote)
        XCTAssertFalse(quote.label.contains("couple people"), "The whole transcript arrived at once: \(quote.label)")
        waitUntil { quote.label.contains("cheap food") }
        screenshot(app, "voice-vibe-listening")

        app.buttons["Stop voice input"].firstMatch.tap()
        let transcribing = element(app, labelContains: "Transcribing")
        XCTAssertTrue(appears(transcribing), "No “Transcribing…” after stopping")
        screenshot(app, "voice-vibe-transcribing")
        expectGone(transcribing)
        let transcript = "Something chill and outside, then cheap food after. Maybe meet a couple people."
        expect(element(app, labelContains: "Tap and say what you want"))
        let mood = app.textViews["Or type it"].exists ? app.textViews["Or type it"] : app.textFields["Or type it"]
        XCTAssertEqual(mood.value as? String, transcript)
        screenshot(app, "voice-vibe-done")
    }

    // MARK: Helpers

    private func launch(route: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES", "-SQAPIMode", "mock",
                                "-SQMockLatency", "0", "-SQVoiceDemo", "YES", "-SQRoute", route]
        app.launch()
        return app
    }

    private func text(of field: XCUIElement) -> String {
        field.value as? String ?? ""
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

    private func expect(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(element.waitForExistence(timeout: timeout), "Missing: \(element)", file: file, line: line)
    }

    private func expectGone(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: timeout), .completed, "Still there: \(element)", file: file, line: line)
    }

    /// Checks every 50 ms whether `element` is there, without `waitForExistence`'s first-second
    /// pause: for a state that lasts about a second ("Transcribing…").
    private func appears(_ element: XCUIElement, within seconds: TimeInterval = 3) -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        while Date() < deadline {
            if element.exists { return true }
            Thread.sleep(forTimeInterval: 0.05)
        }
        return false
    }

    /// Checks `condition` until it holds or the timeout passes.
    private func waitUntil(_ condition: () -> Bool) {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition(), Date() < deadline { Thread.sleep(forTimeInterval: 0.2) }
    }

    private func element(_ app: XCUIApplication, labelContains text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }
}

private extension XCUIElement {
    func tapWhenReady(_ timeout: TimeInterval, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(waitForExistence(timeout: timeout), "Missing: \(self)", file: file, line: line)
        tap()
    }
}

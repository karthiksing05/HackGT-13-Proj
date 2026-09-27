import XCTest

/// People › profile (mock data): a Forum post's author opens their profile, where Add turns into
/// Requested (and can be withdrawn); Forum › Friends says what it's for and shows People for you;
/// a person found in Home search opens their profile, whose Message opens the chat.
///
/// `TEST_RUNNER_SQ_SHOTS_DIR=<dir> xcodebuild test …` also saves profile-requested.png and
/// forum-friends-live.png there.
@MainActor
final class PeopleProfileUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    /// Forum › GT Outdoors Club (the author of the sunrise hike) → their profile with the taste match
    /// and why → Add friend → Requested → withdraw → Add friend again.
    func testForumAuthorOpensProfileThenAdd() {
        let app = launchSignedIn()
        app.buttons["tab.forum"].tapWhenReady(timeout)

        element(app, type: .button, labelBeginsWith: "GT Outdoors Club").tapWhenReady(timeout)
        let name = app.staticTexts["profile.name"]
        expect(name)
        XCTAssertEqual(name.label, "GT Outdoors Club")
        expect(element(app, labelContains: "63 percent taste match"))
        expect(element(app, labelContains: "You both love the outdoors"))
        expect(element(app, labelContains: "Stone Mountain sunrise hike"))

        app.buttons["Add friend"].tapWhenReady(timeout)
        let requested = app.buttons["Requested"]
        expect(requested)
        expectGone(app.buttons["Add friend"])
        shot(app, "profile-requested")

        // Once the server has it, withdraw it (after confirming): back to Add friend.
        waitUntil(requested, "enabled == true")
        requested.tap()
        app.buttons["Withdraw request"].tapWhenReady(timeout)
        expect(app.buttons["Add friend"])
    }

    /// Forum › Friends: the line saying what the tab is for, People for you with each match; Add on
    /// a card → Requested, and the card opens that person's profile.
    func testForumFriendsShowsPeopleForYou() {
        let app = launchSignedIn()
        app.buttons["tab.forum"].tapWhenReady(timeout)
        app.buttons["Friends"].tapWhenReady(timeout)

        expect(app.staticTexts["Posts from your friends: when they're free or open a plan"])
        expect(app.staticTexts["PEOPLE FOR YOU"])
        let priya = element(app, type: .button, labelBeginsWith: "Priya K.")
        expect(priya)
        XCTAssertTrue(priya.label.contains("88 percent taste match"), "Card: \(priya.label)")
        expect(app.buttons["Accept Chris N."])
        shot(app, "forum-friends-live")

        app.buttons["Add Priya K."].tapWhenReady(timeout)
        expect(app.buttons["Requested"])
        priya.tap()
        let name = app.staticTexts["profile.name"]
        expect(name)
        XCTAssertEqual(name.label, "Priya K.")
        // The profile knows about the request just sent.
        expect(app.buttons["Requested"])
        expectGone(app.buttons["Add friend"])
    }

    /// Home › search "priya" → People › Priya K. opens her profile (not a chat) → Message opens your
    /// chat with her over the tabs.
    func testSearchPersonOpensProfile() {
        let app = launchSignedIn()
        type(app.textFields["Search"], "priya")
        element(app, type: .button, labelBeginsWith: "Priya K.").tapWhenReady(timeout)

        let name = app.staticTexts["profile.name"]
        expect(name)
        XCTAssertEqual(name.label, "Priya K.")
        expect(app.buttons["Add friend"])
        expect(element(app, labelContains: "1 FRIEND IN COMMON"))

        app.buttons["Message"].tapWhenReady(timeout)
        expectGone(name)
        expect(element(app, labelContains: "hey! free this weekend?"))
    }

    /// Home › the group dinner's sheet › Who's in › Maya → her profile over the sheet → Message: both
    /// sheets close and your chat with her opens over the tabs.
    func testEventSheetPersonOpensProfileThenMessage() {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES", "-SQAPIMode", "mock",
                                "-SQMockLatency", "0", "-SQRoute", "home/sheet/a6"]
        app.launch()
        let whosIn = app.staticTexts["WHO'S IN"]
        expect(whosIn)

        app.buttons["Maya, Going"].tapWhenReady(timeout)
        let name = app.staticTexts["profile.name"]
        expect(name)
        XCTAssertEqual(name.label, "Maya R.")
        expect(app.buttons["Friends"])

        app.buttons["Message"].tapWhenReady(timeout)
        expectGone(name)
        expectGone(whosIn)
        expect(element(app, labelContains: "you coming tonight?"))
    }

    // MARK: Helpers

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
        waitUntil(element, "exists == false", file: file, line: line)
    }

    private func waitUntil(_ element: XCUIElement, _ predicate: String, file: StaticString = #filePath, line: UInt = #line) {
        let done = XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [done], timeout: timeout), .completed, "Not \(predicate): \(element)", file: file, line: line)
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

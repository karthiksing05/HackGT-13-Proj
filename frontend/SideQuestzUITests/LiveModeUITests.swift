import XCTest

/// Launch checks for the live configuration. Nothing here signs in, so no request leaves the
/// simulator (the app only talks to the server once you're signed in or tap a button).
@MainActor
final class LiveModeUITests: XCTestCase {
    /// Live mode with a demo password, signed out: the app starts on Login (never Home) and offers
    /// "Use the demo account".
    func testLiveModeShowsDemoAccountLink() {
        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES",
                                "-SQAPIMode", "live", "-SQDemoPassword", "abc"]
        app.launch()

        let demo = app.buttons["login.demo"]
        XCTAssertTrue(demo.waitForExistence(timeout: 10), "Missing the demo link")
        XCTAssertEqual(demo.label, "Use the demo account")
        XCTAssertTrue(app.buttons["Sign in"].exists)
        XCTAssertFalse(app.buttons["Sidequests"].exists, "Landed on Home without a session")
    }
}

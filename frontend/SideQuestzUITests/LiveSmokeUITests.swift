import XCTest

/// End-to-end check against a real server, off by default. It runs only when the test runner gets
/// the demo account's password:
///
///     TEST_RUNNER_SQ_LIVE_DEMO_PASSWORD=… xcodebuild test … -only-testing:SideQuestzUITests/LiveSmokeUITests
///
/// `TEST_RUNNER_SQ_LIVE_API_URL` points it elsewhere (default https://api.sidequestz.tech; the
/// socket URL follows). It signs in with "Use the demo account", visits every tab, plans a
/// sidequest up to Review and attaches a screenshot of each screen. It never starts a sidequest,
/// so the server's demo data stays as seeded.
@MainActor
final class LiveSmokeUITests: XCTestCase {
    private let timeout: TimeInterval = 30

    func testDemoAccountAgainstLiveServer() throws {
        let env = ProcessInfo.processInfo.environment
        guard let password = env["SQ_LIVE_DEMO_PASSWORD"], !password.isEmpty else {
            throw XCTSkip("Set TEST_RUNNER_SQ_LIVE_DEMO_PASSWORD to run against the live server.")
        }
        let base = env["SQ_LIVE_API_URL"] ?? "https://api.sidequestz.tech"
        let socket = base.replacingOccurrences(of: "https://", with: "wss://")
            .replacingOccurrences(of: "http://", with: "ws://") + "/ws"

        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES", "-SQAPIMode", "live",
                                "-SQAPIBaseURL", base, "-SQWebSocketURL", socket, "-SQDemoPassword", password]
        app.launch()

        let demo = app.buttons["login.demo"]
        XCTAssertTrue(demo.waitForExistence(timeout: timeout), "Missing the demo link")
        snap(app, "0-login")
        demo.tap()
        XCTAssertTrue(app.buttons["Sidequests"].waitForExistence(timeout: timeout), "Home didn't load")
        settle(app)
        snap(app, "1-home")

        app.buttons["tab.forum"].tap()
        expect(app, labelContains: "Golden hour by the market")
        settle(app)
        snap(app, "2-forum")

        app.buttons["tab.groups"].tap()
        expect(app, labelContains: "Saturday market crew")
        settle(app)
        snap(app, "3-groups")

        app.buttons["tab.account"].tap()
        expect(app, labelContains: "Sandy Byte")
        settle(app)
        snap(app, "4-account")

        app.buttons["tab.plan"].tap()
        XCTAssertTrue(app.staticTexts["Where do you start and end?"].waitForExistence(timeout: timeout))
        settle(app)
        snap(app, "5-create-where")
        app.buttons["Next"].tap()
        XCTAssertTrue(app.staticTexts["When are you free?"].waitForExistence(timeout: timeout))
        snap(app, "6-create-when")
        app.buttons["Next"].tap()
        XCTAssertTrue(app.staticTexts["What are you in the mood for?"].waitForExistence(timeout: timeout))
        snap(app, "7-create-vibe")
        app.buttons["Next"].tap()
        XCTAssertTrue(app.staticTexts["Pick a sidequest"].waitForExistence(timeout: timeout))
        XCTAssertTrue(app.buttons["Start this sidequest"].waitForExistence(timeout: 60), "No plan options came back")
        settle(app)
        snap(app, "8-create-review")
    }

    // MARK: Helpers

    private func expect(_ app: XCUIApplication, labelContains text: String, file: StaticString = #filePath, line: UInt = #line) {
        let match = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
        XCTAssertTrue(match.waitForExistence(timeout: timeout), "Never saw “\(text)”", file: file, line: line)
    }

    /// Lets loaders and cross-fades finish before a screenshot.
    private func settle(_ app: XCUIApplication) {
        _ = app.wait(for: .runningForeground, timeout: 2)
        Thread.sleep(forTimeInterval: 1.5)
    }

    private func snap(_ app: XCUIApplication, _ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }
}

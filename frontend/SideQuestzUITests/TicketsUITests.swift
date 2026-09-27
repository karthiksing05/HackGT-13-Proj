import XCTest

/// Account › Your tickets on mock data: the Account row → the list → a ticket → copy its code →
/// its page in the in-app browser; the Event sheet's "View ticket"; "Go to sidequest"; the empty list.
///
/// `TEST_RUNNER_SQ_SHOTS_DIR=<dir> xcodebuild test …` also saves tickets.png, ticket-detail.png,
/// tickets-empty.png and event-ticket.png there.
@MainActor
final class TicketsUITests: XCTestCase {
    private let timeout: TimeInterval = 10

    /// Account › "Your tickets" (1 upcoming) → the list (Upcoming, then Past) → the garden ticket →
    /// tap the code ("Copied") → "Open ticket page" opens it in the app → Done → back to Account.
    func testAccountTicketsDetailCopyAndPage() {
        let app = launch(["-SQRoute", "account"])

        let row = app.buttons["account.tickets"]
        scrollTo(row, in: app)
        XCTAssertTrue(wait(for: row, value: "1 upcoming"), "Row value: \(String(describing: row.value))")
        row.tap()

        expect(app.buttons["tickets.back"])
        let garden = element(app, type: .button, labelBeginsWith: "Atlanta Botanical Garden")
        expect(garden)
        expect(element(app, type: .button, labelBeginsWith: "Jazz night in Decatur"))
        XCTAssertTrue(garden.label.contains("Admits 1") && garden.label.contains("Saturday reset"), "Card: \(garden.label)")
        expect(app.staticTexts["Upcoming"])
        expect(app.staticTexts["Past"])
        shot(app, "tickets")

        garden.tap()
        let title = app.descendants(matching: .any)["ticketDetail.title"]
        expect(title)
        XCTAssertEqual(title.label, "Atlanta Botanical Garden")
        expect(element(app, labelBeginsWith: "Admits 1"))
        expect(element(app, labelBeginsWith: "Total paid $19.94"))
        expect(app.images["QR code for this ticket"])
        expect(app.buttons["Directions"])
        expect(app.buttons["Go to sidequest"])
        let code = app.buttons["ticketDetail.code"]
        expect(code)
        XCTAssertTrue(code.label.hasPrefix("Confirmation code S Q Z"), "Code: \(code.label)")
        shot(app, "ticket-detail")

        code.tap()
        XCTAssertTrue(wait(for: code, value: "Copied"), "Code value: \(String(describing: code.value))")

        app.buttons["Open ticket page"].tap()
        let done = app.buttons["Done"]
        expect(done)
        done.tap()
        expectGone(done)
        expect(title)

        closeButton(app).tap()
        expectGone(title)
        app.buttons["tickets.back"].tapWhenReady(timeout)
        expectGone(app.buttons["tickets.back"])
        XCTAssertTrue(row.waitForExistence(timeout: timeout) && row.isHittable, "Back on Account")
    }

    /// Home › the garden's Event sheet shows the booked ticket; "View ticket" opens the ticket's own
    /// sheet (no "Go to sidequest": you're there already).
    func testEventSheetViewTicketOpensTheDetail() {
        let app = launch(["-SQRoute", "home/sheet/b1"])

        expect(element(app, labelBeginsWith: "Ticket booked"))
        let view = app.buttons["View ticket"]
        expect(view)
        shot(app, "event-ticket")
        view.tap()

        let title = app.descendants(matching: .any)["ticketDetail.title"]
        expect(title)
        XCTAssertEqual(title.label, "Atlanta Botanical Garden")
        expect(app.buttons["ticketDetail.code"])
        expect(app.buttons["Open ticket page"])
        XCTAssertFalse(app.buttons["Go to sidequest"].exists)
    }

    /// The list's "Go to sidequest": Home, Saturday selected, the garden's Event sheet open.
    func testGoToSidequestOpensTheStopOnHome() {
        let app = launch(["-SQRoute", "tickets/tkt_5b1f0c9a2e7d4a13"])

        let title = app.descendants(matching: .any)["ticketDetail.title"]
        expect(title)
        app.buttons["Go to sidequest"].tapWhenReady(timeout)
        expectGone(title)
        // The Event sheet over Home: the stop, its booked ticket.
        expect(app.staticTexts["Atlanta Botanical Garden"])
        expect(element(app, labelBeginsWith: "Ticket booked"))
        expect(app.buttons["View ticket"])
        closeButton(app).tap()
        expectGone(app.buttons["View ticket"])
        // Home, with the ticket's plan selected.
        let saturday = element(app, type: .button, labelBeginsWith: "Saturday reset")
        expect(saturday)
        XCTAssertTrue(saturday.isSelected, "The ticket's plan is selected on Home")
    }

    /// With no tickets the list says where they'll come from.
    func testNoTicketsYet() {
        let app = launch(["-SQRoute", "account/tickets", "-SQMockTickets", "none"])

        expect(app.buttons["tickets.back"])
        expect(element(app, labelContains: "Tickets you buy, or that Muse buys for you, show up here."))
        XCTAssertFalse(app.staticTexts["Upcoming"].exists)
        shot(app, "tickets-empty")
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

    /// Swipes the screen up until `element` can be tapped.
    private func scrollTo(_ element: XCUIElement, in app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(element.waitForExistence(timeout: timeout), "Missing: \(element)", file: file, line: line)
        for _ in 0..<6 where !element.isHittable {
            app.swipeUp()
        }
        XCTAssertTrue(element.isHittable, "Not on screen: \(element)", file: file, line: line)
    }

    /// The sheet's × (the dimmed backdrop is a full-screen "Close" button too).
    private func closeButton(_ app: XCUIApplication) -> XCUIElement {
        let closes = app.buttons.matching(NSPredicate(format: "label == %@", "Close"))
        return closes.allElementsBoundByIndex.first { $0.frame.width < 100 } ?? closes.firstMatch
    }

    private func wait(for element: XCUIElement, value: String) -> Bool {
        let match = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", value), object: element)
        return XCTWaiter.wait(for: [match], timeout: timeout) == .completed
    }

    private var shotsDir: String? { ProcessInfo.processInfo.environment["SQ_SHOTS_DIR"] }

    /// Writes the screen to `<SQ_SHOTS_DIR>/<name>.png` when the run asks for screenshots.
    private func shot(_ app: XCUIApplication, _ name: String) {
        guard let shotsDir else { return }
        // Let arrivals and the sheet's spring settle first.
        Thread.sleep(forTimeInterval: 1)
        let url = URL(fileURLWithPath: shotsDir).appendingPathComponent("\(name).png")
        XCTAssertNoThrow(try FileManager.default.createDirectory(atPath: shotsDir, withIntermediateDirectories: true))
        XCTAssertNoThrow(try app.screenshot().pngRepresentation.write(to: url))
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

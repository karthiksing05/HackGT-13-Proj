import XCTest

/// The demo account's key interactions against a real server, off by default. It runs only when
/// the test runner gets the demo account's password:
///
///     TEST_RUNNER_SQ_LIVE_DEMO_PASSWORD=… xcodebuild test … -only-testing:SideQuestzUITests/LiveFlowUITests
///
/// `TEST_RUNNER_SQ_LIVE_API_URL` points it elsewhere (default https://api.sidequestz.tech; the
/// socket URL follows). Each test launches, signs in with "Use the demo account", drives one flow
/// with real taps and typing, checks what the screen says, and attaches a screenshot at each step.
///
/// Unlike LiveSmokeUITests, this CHANGES the demo account's data: it rates the Heron Creek stop,
/// starts sidequests (and writes a note on one), joins Marin's "Golden hour by the market" and
/// messages its chat, adds expenses to "Saturday market crew" and settles up there, posts (and takes
/// down) a free-now post, accepts Theo's friend request and changes (then restores) Sandy's status. It expects a
/// freshly seeded account, so seed the server before a run and again afterwards
/// (`sidequestz-admin seed-demo`: it undoes the rating, the join and Theo's friendship; the new
/// sidequests, messages and expenses stay, and the tests allow for them). The tests don't depend
/// on each other's order.
@MainActor
final class LiveFlowUITests: XCTestCase {
    private let timeout: TimeInterval = 30
    /// The real planner can take a while to come back with options.
    private let planTimeout: TimeInterval = 60

    // MARK: Home

    /// Home › "1 past event to rate" › Past › Rate Heron Creek Greenway → 4 stars, a tag and a note →
    /// Save rating → the row shows the stars, and the to-rate card counts one fewer.
    func testRatePastEvent() throws {
        let app = try launchSignedIn()

        let card = element(app, type: .button, labelContains: "to rate. Ratings tune")
        expect(card, "Home has no past events to rate (seed the demo account first)")
        let before = leadingNumber(card.label)
        XCTAssertGreaterThanOrEqual(before, 1, "Unexpected card: \(card.label)")
        settle()
        snap(app, "rate-1-home")

        card.tap()
        let row = element(app, type: .button, labelBeginsWith: "Rate Heron Creek Greenway")
        expect(row, "Heron Creek Greenway isn't waiting for a rating (seed the demo account first)")
        settle()
        snap(app, "rate-2-past")

        row.tap()
        expect(app.staticTexts["How was Heron Creek Greenway?"])
        element(app, type: .button, labelBeginsWith: "4 stars").tapWhenReady(timeout)
        expect(app.staticTexts["Really good"])
        app.buttons["Would go again"].tapWhenReady(timeout)
        type(textInput(app, labelBeginsWith: "Anything else?"), "Quiet trail at dusk")
        settle()
        snap(app, "rate-3-sheet")

        let save = app.buttons["Save rating"]
        save.tapWhenReady(timeout)
        expectGone(save)
        let rated = element(app, type: .button, labelBeginsWith: "Heron Creek Greenway")
        expect(rated)
        expectLabel(rated, contains: "rated 4 of 5")
        expectGone(element(app, type: .button, labelBeginsWith: "Rate Heron Creek Greenway"))
        settle()
        snap(app, "rate-4-rated")

        app.buttons["Sidequests"].tap()
        if before == 1 {
            expectGone(element(app, type: .button, labelContains: "to rate. Ratings tune"))
        } else {
            let left = before - 1
            expect(element(app, type: .button, labelBeginsWith: "\(left) past event\(left == 1 ? "" : "s") to rate"))
        }
        settle()
        snap(app, "rate-5-home")
    }

    /// + › Next through Where / When / Vibe → Review lists the real planner's options → "Start this
    /// sidequest" → Home shows it.
    func testPlanAndStartSidequest() throws {
        let app = try launchSignedIn()

        let name = planAndStartSidequest(app, shots: "plan")
        let card = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ AND selected == true", "\(name), ")).firstMatch
        expectLabel(card, contains: "stop")
        // Its timeline is the one on screen, with the stops in it.
        let stops = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", ", Sidequest"))
        XCTAssertNotNil(firstOnScreen(app, stops), "The new sidequest's timeline has no stops")
        settle()
        snap(app, "plan-5-home")
    }

    /// Start a sidequest, open one of its stops (tap its timeline block) → the Event sheet → write
    /// a note → "Saved" → close, reload Home, reopen the stop → the note is still there.
    func testStopNoteIsSaved() throws {
        let app = try launchSignedIn()
        planAndStartSidequest(app, shots: nil)

        // The new sidequest is selected, so its timeline is the one on screen.
        let blocks = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", ", Sidequest"))
        let block = try XCTUnwrap(firstOnScreen(app, blocks), "No stop on the new sidequest's timeline")
        let blockLabel = block.label
        let stop = blockTitle(blockLabel)
        settle()
        snap(app, "note-1-timeline")

        scrollAboveTabBar(app, block)
        block.tap()
        expect(eventSheet(app).staticTexts[stop], "The Event sheet didn't open for “\(stop)”")
        let notes = notesField(app)
        let note = "Bring a jacket (QA \(Self.stamp()))"
        type(notes, note)
        expect(element(app, labelContains: "Saved"), "The note never said Saved")
        settle()
        snap(app, "note-2-saved")

        closeSheet(app)
        expectGone(notes)
        // Leaving Home and coming back reloads the sidequests from the server.
        app.buttons["tab.forum"].tapWhenReady(timeout)
        app.buttons["tab.home"].tapWhenReady(timeout)
        expect(app.buttons["Sidequests"])
        settle()

        let again = try XCTUnwrap(firstOnScreen(app, app.buttons.matching(NSPredicate(format: "label == %@", blockLabel))),
                                  "Lost the stop after reloading Home")
        scrollAboveTabBar(app, again)
        again.tap()
        expect(eventSheet(app).staticTexts[stop], "The Event sheet didn't reopen for “\(stop)”")
        // The sheet reads the stop from the server too (GET /events/{id}).
        settle(3)
        XCTAssertEqual(notesField(app).value as? String, note, "The note didn't come back")
        snap(app, "note-3-reopened")
    }

    // MARK: Forum

    /// Forum › Marin's "Golden hour by the market" › Request to join → "You're in · Open chat" and one
    /// spot fewer → Groups lists the plan's chat → open it → send a message → it shows as sent, and
    /// Groups shows it as the chat's last message → the plan is on Home.
    func testJoinOpenPlanAndMessageItsChat() throws {
        let app = try launchSignedIn()

        app.buttons["tab.forum"].tapWhenReady(timeout)
        expect(element(app, labelContains: "Golden hour by the market"))
        let join = app.buttons["Request to join"]
        expect(join, "Marin's plan isn't open to join (seed the demo account first)")
        let spots = element(app, labelContains: "of 6 spots left")
        expect(spots)
        let spotsBefore = try XCTUnwrap(spotsLeft(spots.label), "Can't read the spots: \(spots.label)")
        settle()
        snap(app, "join-1-forum")

        scrollAboveTabBar(app, join)
        join.tap()
        let joined = app.buttons["You're in · Open chat"]
        expect(joined, "Joining didn't say You're in")
        expect(element(app, labelContains: "\(spotsBefore - 1) of 6 spots left"), "The plan didn't count you in")
        settle()
        snap(app, "join-2-joined")

        app.buttons["tab.groups"].tapWhenReady(timeout)
        let chat = element(app, type: .button, labelBeginsWith: "Golden hour by the market")
        expect(chat, "Groups doesn't list the plan's chat")
        settle()
        snap(app, "join-3-groups")

        chat.tap()
        expect(app.buttons["Chat"])
        let message = "Count me in! (QA \(Self.stamp()))"
        type(app.textFields["Message"], message)
        app.buttons["Send message"].tapWhenReady(timeout)
        let bubble = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "You: \(message)")).firstMatch
        expect(bubble, "The message never showed in the chat")
        expectValue(of: bubble, isNot: "Sending")
        XCTAssertNotEqual(bubble.value as? String, "Not sent", "The message failed to send")
        settle()
        snap(app, "join-4-message")

        app.buttons["Back"].tapWhenReady(timeout)
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ AND label CONTAINS %@",
                                                   "Golden hour by the market", message)).firstMatch
        expect(row, "Groups doesn't show the message on the plan's chat")
        app.buttons["tab.home"].tapWhenReady(timeout)
        expect(element(app, type: .button, labelBeginsWith: "Golden hour by the market, "), "Marin's plan isn't on Home")
        settle()
        snap(app, "join-5-home")
    }

    /// Forum › "Friends only" (free now) → "YOUR POST · LIVE" → Take down → back to "Bored right now?".
    func testPostFreeNowAndTakeItDown() throws {
        let app = try launchSignedIn()

        app.buttons["tab.forum"].tapWhenReady(timeout)
        expect(element(app, labelContains: "Golden hour by the market"))
        // A post left live by an earlier run comes down first.
        if app.buttons["Take down"].waitForExistence(timeout: 3) {
            app.buttons["Take down"].tap()
        }
        expect(element(app, labelContains: "Bored right now?"))
        settle()
        snap(app, "free-1-forum")

        app.buttons["Friends only"].tapWhenReady(timeout)
        expect(app.staticTexts["YOUR POST · LIVE"], "Posting didn't go live")
        let takeDown = app.buttons["Take down"]
        expect(takeDown)
        settle()
        snap(app, "free-2-live")

        takeDown.tap()
        expectGone(app.staticTexts["YOUR POST · LIVE"])
        expect(element(app, labelContains: "Bored right now?"))
        settle()
        snap(app, "free-3-taken-down")
    }

    // MARK: Groups

    /// Groups › Saturday market crew › Splits › "+ Add an expense" → Pizza, $40 split 3 ways → the
    /// preview is $13.34 / $13.33 / $13.33 → Add → the banner, the new expense, one more expense, and
    /// the balance moves by what the others now owe you.
    func testAddExpenseToMarketCrew() throws {
        let app = try launchSignedIn()

        app.buttons["tab.groups"].tapWhenReady(timeout)
        element(app, type: .button, labelBeginsWith: "Saturday market crew").tapWhenReady(timeout)
        app.buttons["Splits"].tapWhenReady(timeout)
        let balance = element(app, labelBeginsWith: "Your balance in this group")
        expect(balance)
        let before = balance.label
        let netBefore = try XCTUnwrap(net(before), "Can't read the balance: \(before)")
        let countBefore = try XCTUnwrap(expenseCount(before), "Can't read the expense count: \(before)")
        settle()
        snap(app, "split-1-splits")

        // Named per run, since the seed keeps expenses added before.
        let pizza = "Pizza (QA \(Self.stamp()))"
        app.buttons["Add an expense"].tapWhenReady(timeout)
        type(app.textFields["What was it for?"], pizza)
        type(app.textFields["Total amount in dollars"], "40")

        // The preview adds up to the cent: $13.34 + $13.33 + $13.33 (you paid, so the extra cent is yours).
        expect(share(app, "Your share (you paid)", "$13.34"), "Your share isn't $13.34")
        expect(share(app, "Marin owes you", "$13.33"), "Marin's share isn't $13.33")
        expect(share(app, "Theo owes you", "$13.33"), "Theo's share isn't $13.33")
        expect(element(app, labelContains: "$40.00 of $40.00"))
        settle()
        snap(app, "split-2-preview")

        let add = element(app, type: .button, labelBeginsWith: "Add $40.00 · split 3 ways")
        add.tapWhenReady(timeout)
        expect(element(app, labelContains: "Added \"\(pizza)\" · $40.00 split equally 3 ways"), "No confirmation banner")
        expect(element(app, labelContains: "\(pizza), You paid · split 3 ways"), "The new expense isn't listed")

        let changed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label != %@", before), object: balance)
        XCTAssertEqual(XCTWaiter.wait(for: [changed], timeout: timeout), .completed, "The balance didn't change")
        // Marin and Theo now owe you $26.66 more.
        XCTAssertEqual(net(balance.label), netBefore + 2666, "Wrong balance: \(balance.label) (was \(before))")
        XCTAssertEqual(expenseCount(balance.label), countBefore + 1, "Wrong expense count: \(balance.label)")
        settle()
        snap(app, "split-3-added")
    }

    /// Groups › Saturday market crew › Splits: Marin pays $30 for the three of you, so you owe Marin;
    /// "Settle up" offers what you owe (Marin's share, not the group's net, since Theo may owe you),
    /// and settling says so and clears what you owe.
    func testSettleUpPaysWhatYouOwe() throws {
        let app = try launchSignedIn()

        app.buttons["tab.groups"].tapWhenReady(timeout)
        element(app, type: .button, labelBeginsWith: "Saturday market crew").tapWhenReady(timeout)
        app.buttons["Splits"].tapWhenReady(timeout)
        expect(element(app, labelBeginsWith: "Your balance in this group"))

        let round = "Lemonade (QA \(Self.stamp()))"
        app.buttons["Add an expense"].tapWhenReady(timeout)
        type(app.textFields["What was it for?"], round)
        type(app.textFields["Total amount in dollars"], "30")
        app.buttons.matching(NSPredicate(format: "label == %@", "Marin")).firstMatch.tapWhenReady(timeout) // Paid by
        expect(share(app, "You owe Marin", "$10.00"), "Your share of Marin's round isn't $10.00")
        element(app, type: .button, labelBeginsWith: "Add $30.00 · split 3 ways").tapWhenReady(timeout)
        expect(element(app, labelContains: "Added \"\(round)\""), "No confirmation banner")

        // What you owe, row by row; the button pays exactly that.
        let owe = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "You owe "))
        let marin = element(app, labelBeginsWith: "You owe Marin ")
        expect(marin, "You don't owe Marin after Marin paid")
        let owed = owe.allElementsBoundByIndex.compactMap { cents($0.label) }.reduce(0, +)
        let settleButton = element(app, type: .button, labelBeginsWith: "Settle up $")
        expect(settleButton, "No Settle up button while you owe Marin")
        XCTAssertEqual(cents(settleButton.label), owed, "Settle up offers \(settleButton.label), you owe \(owed)¢")
        settleButton.tap()
        expect(element(app, labelBeginsWith: "Settled up "), "Settling didn't confirm (the server refused the amount?)")
        XCTAssertFalse(element(app, labelContains: "The balance changed").exists, "The server called the amount stale")
        expectGone(marin)
        expectGone(settleButton)
        settle()
        snap(app, "settle-1-settled")
    }

    // MARK: Account

    /// Account › Friends › Accept Theo Park → he's in the friends list, and still is after the list
    /// reloads from the server.
    func testAcceptTheosFriendRequest() throws {
        let app = try launchSignedIn()

        app.buttons["tab.account"].tapWhenReady(timeout)
        app.buttons["Friends"].tapWhenReady(timeout)
        let accept = app.buttons["Accept Theo Park"]
        expect(accept, "No request from Theo (seed the demo account first)")
        expect(element(app, labelContains: "Met at the market"))
        settle()
        snap(app, "friends-1-request")

        accept.tap()
        expectGone(accept)
        expect(app.buttons["Message Theo Park"], "Theo isn't in the friends list")
        settle()
        snap(app, "friends-2-accepted")

        // Back on Account, the friends list reloads from the server.
        app.buttons["tab.home"].tapWhenReady(timeout)
        app.buttons["tab.account"].tapWhenReady(timeout)
        expect(app.buttons["Message Theo Park"])
        expect(app.buttons["Message Marin Okafor"])
        XCTAssertFalse(app.buttons["Accept Theo Park"].exists, "Theo's request came back")
        settle()
        snap(app, "friends-3-reloaded")
    }

    /// Account › your status: pick another one → it's selected and your photo says so → after
    /// signing in again it's still the one. Then the original status is put back.
    func testChangeStatusPersists() throws {
        var app = try launchSignedIn()

        app.buttons["tab.account"].tapWhenReady(timeout)
        let statuses = ["Open to all", "Friends only", "Busy"]
        for status in statuses { expect(app.buttons[status]) }
        let original = try XCTUnwrap(statuses.first { app.buttons[$0].isSelected }, "No status is selected")
        let target = original == "Busy" ? "Friends only" : "Busy"
        settle()
        snap(app, "status-1-before")

        app.buttons[target].tap()
        expectStatus(app, target)
        settle()
        snap(app, "status-2-changed")

        app = try launchSignedIn()
        app.buttons["tab.account"].tapWhenReady(timeout)
        expectStatus(app, target)
        settle()
        snap(app, "status-3-after-sign-in")

        app.buttons[original].tap()
        expectStatus(app, original)
    }

    // MARK: Flows

    /// + › Next ×3 → Review (the real planner) → "Start this sidequest" → back on Home. Returns the
    /// picked option's name.
    @discardableResult
    private func planAndStartSidequest(_ app: XCUIApplication, shots: String?,
                                       file: StaticString = #filePath, line: UInt = #line) -> String {
        let before = sidequestCount(app, file: file, line: line)
        app.buttons["tab.plan"].tapWhenReady(timeout, file: file, line: line)
        expect(app.staticTexts["Where do you start and end?"], file: file, line: line)
        settle()
        if let shots { snap(app, "\(shots)-1-where") }
        app.buttons["Next"].tap()
        expect(app.staticTexts["When are you free?"], file: file, line: line)
        if let shots { snap(app, "\(shots)-2-when") }
        app.buttons["Next"].tap()
        expect(app.staticTexts["What are you in the mood for?"], file: file, line: line)
        if let shots { snap(app, "\(shots)-3-vibe") }
        app.buttons["Next"].tap()
        expect(app.staticTexts["Pick a sidequest"], file: file, line: line)

        let picked = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ AND selected == true", "Option ")).firstMatch
        XCTAssertTrue(picked.waitForExistence(timeout: planTimeout), "No plan options came back", file: file, line: line)
        let options = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Option "))
        XCTAssertGreaterThanOrEqual(options.count, 1, file: file, line: line)
        let name = optionName(picked.label)
        XCTAssertFalse(name.isEmpty, "Unexpected option: \(picked.label)", file: file, line: line)
        // Start turns on once the route is timed.
        let start = app.buttons["Start this sidequest"]
        expect(start, file: file, line: line)
        expectEnabled(start, file: file, line: line)
        settle()
        if let shots { snap(app, "\(shots)-4-review") }

        start.tap()
        expectGone(start, timeout: planTimeout, file: file, line: line)
        expect(app.buttons["Sidequests"], file: file, line: line)
        // One more sidequest on Home, and it's the selected card.
        let count = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label MATCHES %@", "Sidequest [0-9]+ of \(before + 1)")).firstMatch
        expect(count, "Home doesn't list \(before + 1) sidequests", file: file, line: line)
        let card = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ AND selected == true", "\(name), ")).firstMatch
        expect(card, "Home didn't select “\(name)”", file: file, line: line)
        return name
    }

    /// How many sidequests Home lists once they've loaded: "Sidequest 2 of 5" → 5, the empty state → 0.
    private func sidequestCount(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) -> Int {
        let dots = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label MATCHES %@", "Sidequest [0-9]+ of [0-9]+")).firstMatch
        let empty = element(app, labelContains: "No active sidequests yet")
        let deadline = Date.now.addingTimeInterval(timeout)
        repeat {
            if dots.exists, let total = Int(dots.label.split(separator: " ").last ?? "") { return total }
            if empty.exists { return 0 }
            Thread.sleep(forTimeInterval: 0.5)
        } while Date.now < deadline
        XCTFail("Home's sidequests never loaded", file: file, line: line)
        return 0
    }

    /// A row of Add expense's "Check the split" ("Marin owes you" … "$13.33").
    private func share(_ app: XCUIApplication, _ who: String, _ amount: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH %@ AND label CONTAINS %@", who, amount)).firstMatch
    }

    /// "Anchor Park, 7:30–8:30 PM, Sidequest" → "Anchor Park".
    private func blockTitle(_ label: String) -> String {
        guard let kind = label.range(of: ", Sidequest", options: .backwards),
              let time = label[..<kind.lowerBound].range(of: ", ", options: .backwards) else { return label }
        return String(label[..<time.lowerBound])
    }

    /// "Option A, Scenic: Harbor loop. A → B → C. $ · 1.2 mi" → "Harbor loop".
    private func optionName(_ label: String) -> String {
        guard let colon = label.range(of: ": ") else { return "" }
        let rest = label[colon.upperBound...]
        return String(rest.components(separatedBy: ". ").first ?? "")
    }

    /// The Event sheet's notes ("Notes, Only you").
    private func notesField(_ app: XCUIApplication) -> XCUIElement {
        textInput(app, labelBeginsWith: "Notes, ")
    }

    /// The Event sheet's scroll view (the one with the notes). Home's timelines keep their blocks'
    /// titles in the tree too, so the stop's name alone doesn't say the sheet is open.
    private func eventSheet(_ app: XCUIApplication) -> XCUIElement {
        app.scrollViews.containing(NSPredicate(format: "label BEGINSWITH %@", "Notes, ")).firstMatch
    }

    /// Closes the open sheet by tapping the dimmed backdrop above it. The backdrop is a full-screen
    /// "Close" button (for VoiceOver), so tapping the element itself lands on the middle of the
    /// sheet; and while the keyboard is up, the sheet's own × is scrolled out of view.
    private func closeSheet(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        let closes = app.buttons.matching(NSPredicate(format: "label == %@", "Close"))
        guard let backdrop = closes.allElementsBoundByIndex.first(where: { $0.frame.width >= app.frame.width - 1 }) else {
            XCTFail("No sheet to close", file: file, line: line)
            return
        }
        backdrop.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.08)).tap()
    }

    private func expectStatus(_ app: XCUIApplication, _ status: String, file: StaticString = #filePath, line: UInt = #line) {
        let button = app.buttons[status]
        let settled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "selected == true AND value != %@", "Saving"), object: button)
        XCTAssertEqual(XCTWaiter.wait(for: [settled], timeout: timeout), .completed, "“\(status)” isn't selected", file: file, line: line)
        expect(element(app, labelContains: "status \(status)"), "Your photo doesn't say “\(status)”", file: file, line: line)
    }

    // MARK: Launch

    /// Launches against the server in `SQ_LIVE_API_URL` and signs in with "Use the demo account".
    /// Sign-in is rate limited per email (5 a minute), so a "Too many attempts" answer waits and
    /// tries again.
    private func launchSignedIn(file: StaticString = #filePath, line: UInt = #line) throws -> XCUIApplication {
        let env = ProcessInfo.processInfo.environment
        guard let password = env["SQ_LIVE_DEMO_PASSWORD"], !password.isEmpty else {
            throw XCTSkip("Set TEST_RUNNER_SQ_LIVE_DEMO_PASSWORD to run against a live server.")
        }
        let base = env["SQ_LIVE_API_URL"] ?? "https://api.sidequestz.tech"
        let socket = base.replacingOccurrences(of: "https://", with: "wss://")
            .replacingOccurrences(of: "http://", with: "ws://") + "/ws"
        continueAfterFailure = false

        let app = XCUIApplication()
        app.launchArguments += ["-SQSkipIntro", "YES", "-SQResetSession", "YES", "-SQAPIMode", "live",
                                "-SQAPIBaseURL", base, "-SQWebSocketURL", socket, "-SQDemoPassword", password]
        app.launch()

        let demo = app.buttons["login.demo"]
        let home = app.buttons["Sidequests"]
        let throttled = element(app, labelContains: "Too many attempts")
        for _ in 0..<4 {
            demo.tapWhenReady(timeout, file: file, line: line)
            if home.waitForExistence(timeout: timeout) { break }
            guard throttled.exists else { break }
            Thread.sleep(forTimeInterval: 20)
        }
        XCTAssertTrue(home.exists, "Home didn't load after signing in", file: file, line: line)
        return app
    }

    // MARK: Helpers

    private func type(_ field: XCUIElement, _ text: String, file: StaticString = #filePath, line: UInt = #line) {
        field.tapWhenReady(timeout, file: file, line: line)
        field.typeText(text)
    }

    private func expect(_ element: XCUIElement, _ message: String? = nil, timeout: TimeInterval? = nil,
                        file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(element.waitForExistence(timeout: timeout ?? self.timeout), message ?? "Missing: \(element)", file: file, line: line)
    }

    private func expectGone(_ element: XCUIElement, timeout: TimeInterval? = nil, file: StaticString = #filePath, line: UInt = #line) {
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: timeout ?? self.timeout), .completed, "Still there: \(element)", file: file, line: line)
    }

    private func expectEnabled(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        let enabled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [enabled], timeout: planTimeout), .completed, "Never enabled: \(element)", file: file, line: line)
    }

    private func expectLabel(_ element: XCUIElement, contains text: String, file: StaticString = #filePath, line: UInt = #line) {
        let match = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label CONTAINS %@", text), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [match], timeout: timeout), .completed, "“\(element.label)” never said “\(text)”", file: file, line: line)
    }

    private func expectValue(of element: XCUIElement, isNot value: String, file: StaticString = #filePath, line: UInt = #line) {
        let done = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value != %@", value), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [done], timeout: timeout), .completed, "Still “\(value)”: \(element)", file: file, line: line)
    }

    private func element(_ app: XCUIApplication, type: XCUIElement.ElementType = .any, labelContains text: String) -> XCUIElement {
        app.descendants(matching: type).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    private func element(_ app: XCUIApplication, type: XCUIElement.ElementType = .any, labelBeginsWith text: String) -> XCUIElement {
        app.descendants(matching: type).matching(NSPredicate(format: "label BEGINSWITH %@", text)).firstMatch
    }

    /// A text field or (multi-line) text view, not the caption above it.
    private func textInput(_ app: XCUIApplication, labelBeginsWith text: String) -> XCUIElement {
        let types = [XCUIElement.ElementType.textField, .textView].map(\.rawValue)
        return app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH %@ AND elementType IN %@", text, types)).firstMatch
    }

    /// Drags the screen up until `element` is clear of the tab bar. Tab content runs under the bar
    /// (masked, not clipped), so XCUITest calls a button there hittable and the tap lands on a tab.
    private func scrollAboveTabBar(_ app: XCUIApplication, _ element: XCUIElement) {
        let limit = app.buttons["tab.home"].frame.minY - 12
        for _ in 0..<6 where element.frame.maxY > limit {
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7))
            from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45)))
        }
        XCTAssertLessThanOrEqual(element.frame.maxY, limit, "Couldn't scroll \(element) above the tab bar")
    }

    /// The first match whose middle is on screen. The Home carousel keeps every sidequest's timeline
    /// in the tree, and asking an off-screen page's block `isHittable` fails the test.
    private func firstOnScreen(_ app: XCUIApplication, _ query: XCUIElementQuery) -> XCUIElement? {
        let deadline = Date.now.addingTimeInterval(timeout)
        repeat {
            let screen = app.frame
            let match = query.allElementsBoundByIndex.first { screen.contains(CGPoint(x: $0.frame.midX, y: $0.frame.midY)) }
            if let match { return match }
            Thread.sleep(forTimeInterval: 0.5)
        } while Date.now < deadline
        return nil
    }

    /// "3 past events to rate. …" → 3.
    private func leadingNumber(_ text: String) -> Int {
        Int(text.prefix { $0.isNumber }) ?? 0
    }

    /// The first "$1,234.56" in `text`, in cents.
    private func cents(_ text: String) -> Int? {
        guard let range = text.range(of: #"\$[0-9,]+\.[0-9]{2}"#, options: .regularExpression) else { return nil }
        return Int(text[range].filter(\.isNumber))
    }

    /// The Splits headline as a net in cents: "You owe $2.00" → -200, "You're owed $5.00" → 500.
    private func net(_ label: String) -> Int? {
        if label.contains("All settled") { return 0 }
        if let range = label.range(of: "You owe ") { return cents(String(label[range.upperBound...])).map { -$0 } }
        if let range = label.range(of: "You're owed ") { return cents(String(label[range.upperBound...])) }
        return nil
    }

    /// "…, 3 expenses" → 3.
    private func expenseCount(_ label: String) -> Int? {
        guard let range = label.range(of: #"[0-9]+ expenses?"#, options: .regularExpression) else { return nil }
        return Int(label[range].prefix { $0.isNumber })
    }

    /// "4 of 6 spots left" → 4.
    private func spotsLeft(_ label: String) -> Int? {
        guard let range = label.range(of: #"[0-9]+ of [0-9]+ spots left"#, options: .regularExpression) else { return nil }
        return Int(label[range].prefix { $0.isNumber })
    }

    /// Makes typed text unique per run ("07:26:39").
    private static func stamp() -> String {
        Date.now.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits).second(.twoDigits))
    }

    /// Lets loaders and cross-fades finish before a screenshot.
    private func settle(_ seconds: TimeInterval = 1.5) {
        Thread.sleep(forTimeInterval: seconds)
    }

    private func snap(_ app: XCUIApplication, _ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }
}

private extension XCUIElement {
    func tapWhenReady(_ timeout: TimeInterval, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(waitForExistence(timeout: timeout), "Missing: \(self)", file: file, line: line)
        tap()
    }
}

import Foundation
import Testing
@testable import SideQuestz

/// Home timeline and calendar labels that fit their blocks, Setup's date of birth, and the
/// Must-see search's failure message and skeleton time.
@MainActor
struct PolishTests {
    private let clock = AppClock.demo

    // MARK: Timeline labels

    @Test func labelStyleThresholds() {
        typealias Block = HomeTimelineBlock
        #expect(Block.labelStyle(height: 94) == .twoLines)
        #expect(Block.labelStyle(height: 50) == .twoLines)
        #expect(Block.labelStyle(height: 49.9) == .oneLine(12))
        #expect(Block.labelStyle(height: 22) == .oneLine(12))
        #expect(Block.labelStyle(height: 21.9) == .oneLine(11))
        #expect(Block.labelStyle(height: 16) == .oneLine(11))
        #expect(Block.labelStyle(height: 15.9) == .capsule)
        #expect(Block.labelStyle(height: 12) == .capsule)

        // Larger text raises every threshold in step; smaller text keeps the default ones.
        #expect(Block.labelStyle(height: 60, textScale: 1.5) == .oneLine(12))
        #expect(Block.labelStyle(height: 30, textScale: 1.5) == .oneLine(11))
        #expect(Block.labelStyle(height: 20, textScale: 1.5) == .capsule)
        #expect(Block.labelStyle(height: 75, textScale: 1.5) == .twoLines)
        #expect(Block.labelStyle(height: 15, textScale: 0.8) == .capsule)
        #expect(Block.labelStyle(height: 49, textScale: 0.8) == .oneLine(12))
    }

    /// The demo's short blocks: 25 and 20 minutes get one line, 15 minutes a capsule.
    @Test func demoBlocksGetTheirStyles() throws {
        let itineraries = MockData.itineraries()
        func style(_ id: String) throws -> HomeTimelineBlock.LabelStyle {
            let itinerary = try #require(itineraries.first { $0.items.contains { $0.id == id } })
            let item = try #require(itinerary.items.first { $0.id == id })
            return HomeTimelineBlock.labelStyle(height: HomeTimelineLayout(itinerary: itinerary, clock: clock).blockHeight(item))
        }
        #expect(try style("a1") == .twoLines)   // CS 3510 lecture, 50 min
        #expect(try style("a2") == .oneLine(12)) // Transit to Ponce City Market, 25 min (24.7pt)
        #expect(try style("a4") == .oneLine(11)) // Walk the Eastside Trail, 20 min (19.3pt)
        #expect(try style("b2") == .capsule)     // Walk to the Active Oval, 15 min (14pt)
        #expect(try style("b4") == .oneLine(12)) // Call with family, 30 min (30pt)
    }

    /// A tall block's title only takes the lines that leave its time line (and faces) whole.
    @Test func titleLinesLeaveRoomForTheTime() {
        typealias Block = HomeTimelineBlock
        #expect(Block.titleLineLimit(height: 51.3) == 1)   // 50 minutes
        #expect(Block.titleLineLimit(height: 62) == 2)     // an hour
        #expect(Block.titleLineLimit(height: 94.7) == 3)   // 90 minutes
        #expect(Block.titleLineLimit(height: 94.7, showsPeople: true) == 2)
        #expect(Block.titleLineLimit(height: 80, showsPeople: true) == 1)
        #expect(Block.titleLineLimit(height: 62, textScale: 1.3) == 1)
        #expect(Block.titleLineLimit(height: 20) == 1)
        for height in stride(from: CGFloat(50), through: 200, by: 0.5) {
            for people in [false, true] where !people || height >= 80 {
                let lines = CGFloat(Block.titleLineLimit(height: height, showsPeople: people))
                #expect(10 + lines * 17.5 + 16.2 + (people ? 28 : 0) <= height, "\(height)pt, people: \(people)")
            }
        }
    }

    /// Home › Calendar: a block shows the lines of "Title · time" that fit whole (15pt each under
    /// 4pt of padding), one at least.
    @Test func calendarBlocksShowTheLinesThatFit() {
        #expect(HomeDayPanel.labelLines(height: 20) == 1)    // 30 minutes (the shortest block)
        #expect(HomeDayPanel.labelLines(height: 28) == 1)    // an hour
        #expect(HomeDayPanel.labelLines(height: 35.5) == 2)  // 75 minutes
        #expect(HomeDayPanel.labelLines(height: 43) == 2)    // 90 minutes
        #expect(HomeDayPanel.labelLines(height: 118) == 7)   // 4 hours
        #expect(HomeDayPanel.labelLines(height: 43, textScale: 1.5) == 1)
        for height in stride(from: CGFloat(20), through: 300, by: 0.5) {
            let lines = HomeDayPanel.labelLines(height: height)
            #expect(lines == 1 || 4 + CGFloat(lines) * 15 <= height - 1, "\(height)pt")
        }
    }

    // MARK: Date of birth

    @Test func birthDateWheelsStartOnTheDateOrTwentyYearsBack() {
        let draft = SetupDraft()
        #expect(draft.birthDateStart(now: clock.now, calendar: clock.calendar) == clock.date(2006, 9, 25))
        draft.birthDate = clock.date(2003, 6, 14)
        #expect(draft.birthDateStart(now: clock.now, calendar: clock.calendar) == clock.date(2003, 6, 14))
    }

    /// Done keeps the day the wheels show, at midnight in the app's time zone: the age counts whole
    /// days (a 13th birthday today counts), and the date goes out like the contract's example.
    @Test func doneSetsTheDayAtMidnight() throws {
        let draft = SetupDraft()
        draft.setBirthDate(clock.date(2003, 6, 14, 15, 30), calendar: clock.calendar)
        #expect(draft.birthDate == clock.date(2003, 6, 14))
        #expect(draft.age(on: clock.now, calendar: clock.calendar) == 23)
        #expect(Validation.ageNote(age: draft.age(on: clock.now, calendar: clock.calendar))
                == "Age 23: 21+ events can show up in your suggestions. Never shown to others.")

        // Today is Sep 25, 2026, 2:10 PM: born Sep 25, 2013 (the wheels kept 8 PM) is 13 today.
        draft.setBirthDate(clock.date(2013, 9, 25, 20, 0), calendar: clock.calendar)
        #expect(draft.age(on: clock.now, calendar: clock.calendar) == 13)
        draft.setBirthDate(clock.date(2013, 9, 26), calendar: clock.calendar)
        #expect(draft.age(on: clock.now, calendar: clock.calendar) == 12)
        #expect(Validation.ageNote(age: 12) == "You need to be 13 or older to use SideQuests.")

        draft.setBirthDate(clock.date(2004, 5, 2, 9, 45), calendar: clock.calendar)
        let request = SignupRequest(name: "Sam", email: "sam@gatech.edu", password: "wander2026", username: nil,
                                    dateOfBirth: draft.birthDate)
        let json = String(decoding: try APICoding.encoder().encode(request), as: UTF8.self)
        #expect(json.contains(#""date_of_birth":"2004-05-02T04:00:00Z""#))
    }

    @Test func birthDateFieldText() {
        #expect(BirthDateSheet.text(clock.date(2003, 6, 14), clock: clock) == "June 14, 2003")
        #expect(BirthDateSheet.text(clock.date(2006, 9, 25), clock: clock) == "September 25, 2006")
        // In the app's time zone: 11:30 PM in Atlanta is already the next day in UTC.
        #expect(BirthDateSheet.text(clock.date(2003, 6, 14, 23, 30), clock: clock) == "June 14, 2003")
    }

    // MARK: Must-see search

    @Test func mustSeeFailureMessages() {
        #expect(CreateFlowModel.mustSeeFailureMessage(APIError.notFound) == "Search isn't available right now.")
        #expect(CreateFlowModel.mustSeeFailureMessage(APIError.network("offline")) == "You're offline or the server is unreachable.")
        #expect(CreateFlowModel.mustSeeFailureMessage(APIError.server(status: 500, message: nil)) == "Something went wrong on our end.")
        #expect(CreateFlowModel.mustSeeFailureMessage(CancellationError()) == "Something went wrong.")
    }

    /// A server without `GET /activities/search` (404 at once): the section says search isn't
    /// available, after the skeleton has shown for its minimum time.
    @Test func mustSeeSearchWithoutTheEndpoint() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [NotFoundURLProtocol.self]
        let auth = AuthStore(service: "tests.polish.mustSee")
        let api = LiveAPIClient(baseURL: try #require(URL(string: "https://api.example.invalid")), auth: auth, clock: clock,
                                session: URLSession(configuration: configuration))
        let env = AppEnvironment(mode: .live, clock: clock, api: api, auth: auth, socketURL: nil, forceVoiceDemo: true)
        let model = CreateFlowModel(draft: CreateDraft(), env: env)

        let elapsed = await ContinuousClock().measure { await model.searchMustSee() }
        guard case .failed(let message) = model.mustSeeResults else {
            Issue.record("Expected a failure, got \(model.mustSeeResults.phase)")
            return
        }
        #expect(message == "Search isn't available right now.")
        #expect(elapsed >= CreateFlowModel.mustSeeMinimumSkeleton)
        #expect(model.mustSeeResultsSearch == nil && !model.searchingMustSee)
    }

    /// Other failures keep their own sentence (and the skeleton's minimum time); results land at once.
    @Test func mustSeeSearchOtherFailuresAndSuccess() async throws {
        let env = AppEnvironment.preview()
        let api = try #require(env.api as? MockAPIClient)
        let model = CreateFlowModel(draft: CreateDraft(), env: env)
        await model.loadDefaultPlacesIfNeeded()

        api.failing = ["activities"]
        let failed = await ContinuousClock().measure { await model.searchMustSee() }
        guard case .failed(let message) = model.mustSeeResults else {
            Issue.record("Expected a failure, got \(model.mustSeeResults.phase)")
            return
        }
        #expect(message == "You're offline or the server is unreachable.")
        #expect(failed >= CreateFlowModel.mustSeeMinimumSkeleton)

        // "Try again": results show as soon as they arrive.
        api.failing = []
        let loaded = await ContinuousClock().measure { await model.searchMustSee() }
        #expect(model.mustSeeResults.value?.count == CreateFlowModel.mustSeeLimit)
        #expect(loaded < CreateFlowModel.mustSeeMinimumSkeleton)
    }
}

/// Answers every request with a 404, like a server that doesn't have the endpoint yet.
private nonisolated final class NotFoundURLProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let url = request.url,
              let response = HTTPURLResponse(url: url, statusCode: 404, httpVersion: "HTTP/1.1",
                                             headerFields: ["Content-Type": "text/plain"]) else { return }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data("404 page not found".utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

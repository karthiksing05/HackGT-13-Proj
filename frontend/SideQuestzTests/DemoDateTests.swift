import Foundation
import Testing
@testable import SideQuestz

/// A demo account (`demo_date` on its user) runs on the server's demo date: the live clock moves the
/// real time to that day in New York, keeping the time of day, and everything that holds the clock
/// follows.
@MainActor
struct DemoDateTests {
    private static let newYork = TimeZone(identifier: "America/New_York")!
    private static let sandy = User(id: "sandy", name: "Sandy Byte", email: AppEnvironment.demoEmail, avatarColor: .ink,
                                    status: .open, ageBracket: .adult, demoDate: "2026-09-24")

    /// A stand-in for the real time that a test moves by hand.
    private final class RealTime {
        var now: Date
        init(_ now: Date) { self.now = now }
    }

    private final class Counter {
        var value = 0
    }

    private var newYorkCalendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = Self.newYork
        return calendar
    }

    /// A wall-clock time in New York.
    private func ny(_ year: Int, _ month: Int, _ day: Int, _ hour: Int, _ minute: Int, _ second: Int = 0) -> Date {
        newYorkCalendar.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute, second: second))!
    }

    /// Year, month, day, hour, minute and second on a New York wall clock.
    private func wall(_ date: Date) -> [Int] {
        let c = newYorkCalendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
        return [c.year, c.month, c.day, c.hour, c.minute, c.second].map { $0 ?? -1 }
    }

    /// Live mode over the demo backend: the session code runs as in the app, and nothing goes over
    /// the network.
    private func liveEnvironment(_ clock: AppClock, _ service: String) -> AppEnvironment {
        let auth = AuthStore(service: service)
        auth.clear()
        return AppEnvironment(mode: .live, clock: clock, api: MockAPIClient(clock: clock, latencyScale: 0), auth: auth,
                              socketURL: nil, forceVoiceDemo: true)
    }

    @Test func userDecodesDemoDate() throws {
        let decoder = APICoding.decoder(timeZone: Self.newYork)
        let sandy = try decoder.decode(User.self, from: Data(#"{"id":"u1","name":"Sandy Byte","demo_date":"2026-09-27"}"#.utf8))
        #expect(sandy.demoDate == "2026-09-27")
        let login = #"{"user":{"id":"u1","name":"Sandy Byte","demo_date":"2026-09-24"},"tokens":{"access_token":"a","refresh_token":"r"}}"#
        #expect(try decoder.decode(AuthResponse.self, from: Data(login.utf8)).user.demoDate == "2026-09-24")
        // Everyone else: left out, or null.
        #expect(try decoder.decode(User.self, from: Data(#"{"id":"u2","name":"Sam"}"#.utf8)).demoDate == nil)
        #expect(try decoder.decode(User.self, from: Data(#"{"id":"u2","name":"Sam","demo_date":null}"#.utf8)).demoDate == nil)
        // Written as `demo_date`, and left out when nil (so the contract examples stay as they are).
        let encoder = APICoding.encoder()
        #expect(String(decoding: try encoder.encode(sandy), as: UTF8.self).contains(#""demo_date":"2026-09-27""#))
        #expect(!String(decoding: try encoder.encode(MockData.user()), as: UTF8.self).contains("demo_date"))
    }

    @Test func demoDateKeepsTheRealTimeOfDay() {
        let real = RealTime(ny(2026, 9, 27, 15, 42, 7))
        let clock = AppClock(timeZone: Self.newYork, realTime: { real.now })
        #expect(clock.now == real.now && clock.demoDate == nil)

        clock.demoDate = "2026-09-24"
        #expect(clock.demoDate == "2026-09-24")
        #expect(wall(clock.now) == [2026, 9, 24, 15, 42, 7])
        #expect(clock.dayKey(clock.now) == "2026-09-24")
        #expect(clock.isToday(ny(2026, 9, 24, 9, 0)) && !clock.isToday(ny(2026, 9, 27, 9, 0)))
        #expect(clock.realNow == real.now)
        // Worked out on every read: days later it's still that date, at the new time of day.
        real.now = ny(2026, 10, 1, 9, 5)
        #expect(wall(clock.now) == [2026, 9, 24, 9, 5, 0])
        // From before the demo date too, up to the last second of the day.
        real.now = ny(2026, 9, 20, 23, 59, 59)
        #expect(wall(clock.now) == [2026, 9, 24, 23, 59, 59])
        // On the demo date itself nothing moves.
        clock.demoDate = "2026-09-27"
        real.now = ny(2026, 9, 27, 18, 30)
        #expect(clock.now == real.now)
    }

    @Test func demoDateAcrossDaylightSaving() throws {
        // Clocks fall back on Nov 1, 2026: a November afternoon (EST) reads the same time on Sep 24
        // (EDT), so the move is 40 days and an hour, not a whole number of 24-hour days.
        let real = RealTime(ny(2026, 11, 3, 14, 30))
        let clock = AppClock(timeZone: Self.newYork, realTime: { real.now })
        clock.demoDate = "2026-09-24"
        #expect(wall(clock.now) == [2026, 9, 24, 14, 30, 0])
        #expect(real.now.timeIntervalSince(clock.now) == 40 * 86_400 + 3_600)
        // The second 1:30 AM of Nov 1 (EST) is still 1:30 AM.
        real.now = try #require(ISO8601DateFormatter().date(from: "2026-11-01T06:30:00Z"))
        #expect(wall(real.now) == [2026, 11, 1, 1, 30, 0])
        #expect(wall(clock.now) == [2026, 9, 24, 1, 30, 0])
        // And the other way: a demo date after the change, from a real day before it.
        clock.demoDate = "2026-11-02"
        real.now = ny(2026, 10, 30, 8, 15)
        #expect(wall(clock.now) == [2026, 11, 2, 8, 15, 0])
    }

    /// Like the server, the day is New York's whatever the phone's time zone: at 10 PM in Los
    /// Angeles it's already 1 AM the next day there.
    @Test func demoDateIsADayInNewYork() throws {
        let real = try #require(ISO8601DateFormatter().date(from: "2026-09-28T05:00:00Z"))
        let clock = AppClock(timeZone: try #require(TimeZone(identifier: "America/Los_Angeles")), realTime: { real })
        clock.demoDate = "2026-09-24"
        #expect(wall(clock.now) == [2026, 9, 24, 1, 0, 0])
        #expect(clock.dayKey(clock.now) == "2026-09-23")
    }

    @Test func clearingOrABadDateUsesTheRealTime() {
        let real = ny(2026, 9, 27, 12, 0)
        let clock = AppClock(timeZone: Self.newYork, realTime: { real })
        clock.demoDate = "2026-09-24"
        #expect(clock.dayKey(clock.now) == "2026-09-24")
        clock.demoDate = nil
        #expect(clock.now == real && clock.demoDate == nil)
        for bad in ["", "2026-9-24", "2026-02-30", "24/09/2026", "2026-09-24T10:00:00Z", " 2026-09-24", "tomorrow"] {
            clock.demoDate = bad
            #expect(clock.now == real && clock.demoDate == nil, "\(bad)")
        }
        // The mock's fixed clock stays where it is.
        let fixed = AppClock(timeZone: Self.newYork, fixedNow: real)
        fixed.demoDate = "2026-09-24"
        #expect(fixed.now == real && fixed.realNow == real)
    }

    /// The clock is a struct copied into the API client, `TimeFormat` and the models: they all see
    /// the same date, whichever copy sets it.
    @Test func everyCopyOfTheClockFollows() {
        let real = ny(2026, 10, 1, 19, 0)
        let clock = AppClock(timeZone: Self.newYork, realTime: { real })
        let copy = clock
        let format = TimeFormat(clock: clock)
        let api = LiveAPIClient(baseURL: URL(string: "https://api.sidequestz.tech")!, auth: AuthStore(service: "tests.demo-date.copies"),
                                clock: clock)
        clock.demoDate = "2026-09-24"
        #expect(copy.dayKey(copy.now) == "2026-09-24")
        #expect(api.clock.dayKey(api.clock.now) == "2026-09-24")
        #expect(format.relativeDay(ny(2026, 9, 24, 18, 0)) == "Today")
        #expect(format.relativeDay(ny(2026, 9, 25, 18, 0)) == "Fri")
        #expect(format.relativeDay(ny(2026, 10, 1, 18, 0)) == "Thu, Oct 1")
        copy.demoDate = nil
        #expect(clock.now == real && api.clock.now == real)
    }

    /// Sign-in, `GET /me` and `PATCH /me` all set `env.user`, and the clock follows it.
    @Test func sessionSetsAndClearsTheDemoDate() async {
        let real = ny(2026, 10, 1, 15, 42)
        let clock = AppClock(timeZone: Self.newYork, realTime: { real })
        let env = liveEnvironment(clock, "tests.demo-date.session")
        env.user = Self.sandy
        #expect(clock.demoDate == "2026-09-24" && clock.dayKey(env.clock.now) == "2026-09-24")
        env.user?.status = .busy
        #expect(clock.demoDate == "2026-09-24")
        // A `GET /me` without `demo_date` (the demo backend's user) puts it back on the real date.
        await env.refreshSession()
        #expect(env.user?.demoDate == nil && clock.demoDate == nil && clock.now == real)
        env.user = Self.sandy
        #expect(clock.dayKey(clock.now) == "2026-09-24")
        await env.signOut()
        #expect(env.user == nil && clock.demoDate == nil && clock.now == real)

        // Mock mode keeps its fixed Friday.
        let mock = AppEnvironment.preview()
        mock.user = Self.sandy
        #expect(mock.clock.demoDate == nil && mock.clock.dayKey(mock.clock.now) == "2026-09-25")
    }

    /// When the date arrives after screens loaded (a slow `GET /me` at launch), they reload for the
    /// new day; other profile changes and signing out don't reload anything.
    @Test func screensReloadWhenTodayMoves() async throws {
        let clock = AppClock(timeZone: Self.newYork, realTime: { [self] in ny(2026, 10, 1, 15, 42) })
        let env = liveEnvironment(clock, "tests.demo-date.reload")
        let reloads = Counter()
        env.registerReload("tests.screen") { reloads.value += 1 }
        env.user = MockData.user()
        env.user = Self.sandy
        for _ in 0..<200 where reloads.value == 0 { try await Task.sleep(nanoseconds: 5_000_000) }
        #expect(reloads.value == 1)
        env.user?.status = .busy
        env.user = nil
        try await Task.sleep(nanoseconds: 50_000_000)
        #expect(reloads.value == 1)
        env.unregisterReload("tests.screen")
    }

    @Test func createStartsOnTheDemoDate() {
        let clock = AppClock(timeZone: Self.newYork, realTime: { [self] in ny(2026, 10, 1, 15, 42) })
        let env = liveEnvironment(clock, "tests.demo-date.create")
        env.user = Self.sandy
        let model = CreateFlowModel(draft: CreateDraft(), env: env)
        #expect(model.date == clock.date(2026, 9, 24))
        #expect(wall(model.startTime) == [2026, 9, 24, 15, 45, 0])   // now, rounded up to 5 minutes
        #expect(wall(model.backBy) == [2026, 9, 24, 18, 45, 0])      // and 3 hours
        // Joining a shared plan can't lock before now, on the demo date.
        #expect(wall(model.lockRange.lowerBound) == [2026, 9, 24, 15, 42, 0])

        // Without a demo date the same moment plans on the real day.
        env.user = MockData.user()
        let plain = CreateFlowModel(draft: CreateDraft(), env: env)
        #expect(plain.date == clock.date(2026, 10, 1))
        #expect(wall(plain.startTime) == [2026, 10, 1, 15, 45, 0])
    }
}

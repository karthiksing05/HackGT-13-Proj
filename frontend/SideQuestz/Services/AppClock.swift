import Foundation
import Observation

/// "Now", time zone and calendar for the whole app.
///
/// The demo is set in Atlanta on Friday Sep 25, 2026 at 2:10 PM (the "Now" line on Home), so mock
/// mode pins the clock there. Live mode uses the real clock and the device time zone; a demo
/// account moves it to the server's demo date (`demoDate`).
struct AppClock {
    let timeZone: TimeZone
    let calendar: Calendar
    private let fixedNow: Date?
    /// The real time (`Date()`; tests pass their own).
    private let realTime: () -> Date
    /// Shared by every copy of this clock (see `demoDate`).
    private let demoDay = DemoDay()

    /// Mock mode: the fixed demo time. Live: the real time, moved to the demo date when there is
    /// one. Worked out on every read, so a demo account stays on its date whatever the real day.
    var now: Date {
        if let fixedNow { return fixedNow }
        let real = realTime()
        guard let day = demoDay.day else { return real }
        return Self.moved(real, to: day)
    }

    /// `now` without the demo date, for times the server stamps with its real clock (a message's
    /// `sent_at`). Fixed in mock mode, like `now`.
    var realNow: Date { fixedNow ?? realTime() }

    /// The demo account's date (`User.demoDate`, "2026-09-27"), or nil for the real date. The server
    /// keeps a demo account on that day at the real time of day in New York, and so does `now`.
    /// Every copy of this clock (the API client's, `TimeFormat`'s, the models') shares it, and views
    /// that read `now` redraw when it changes. Anything but a real `YYYY-MM-DD` day counts as nil.
    var demoDate: String? {
        get { demoDay.day.map { String(format: "%04d-%02d-%02d", $0.year ?? 0, $0.month ?? 0, $0.day ?? 0) } }
        nonmutating set {
            let day = newValue.flatMap { Self.parseDay($0) }
            if demoDay.day != day { demoDay.day = day }
        }
    }

    init(timeZone: TimeZone, fixedNow: Date? = nil, realTime: @escaping () -> Date = { Date() }) {
        self.timeZone = timeZone
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = timeZone
        cal.locale = Locale(identifier: "en_US_POSIX")
        self.calendar = cal
        self.fixedNow = fixedNow
        self.realTime = realTime
    }

    /// Atlanta: the offline demo's time zone, and the one the server keeps demo dates in.
    static let demoTimeZone = TimeZone(identifier: "America/New_York")!

    /// Friday, September 25, 2026 · 2:10 PM in Atlanta.
    static let demo: AppClock = {
        let clock = AppClock(timeZone: demoTimeZone)
        return AppClock(timeZone: demoTimeZone, fixedNow: clock.date(2026, 9, 25, 14, 10))
    }()

    /// The real clock in the device's time zone. Each one has its own demo date, so the app makes
    /// one and hands it to everything that needs it (`AppEnvironment.makeDefault`).
    static var live: AppClock { AppClock(timeZone: .current) }

    /// Builds a date in this clock's time zone.
    func date(_ year: Int, _ month: Int, _ day: Int, _ hour: Int = 0, _ minute: Int = 0) -> Date {
        calendar.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute))!
    }

    /// Same calendar day as `day`, at hour:minute.
    func on(_ day: Date, _ hour: Int, _ minute: Int = 0) -> Date {
        calendar.date(bySettingHour: hour, minute: minute, second: 0, of: day)!
    }

    func startOfDay(_ date: Date) -> Date { calendar.startOfDay(for: date) }

    func isToday(_ date: Date) -> Bool { calendar.isDate(date, inSameDayAs: now) }

    /// Minutes since midnight.
    func minutesIntoDay(_ date: Date) -> Int {
        let c = calendar.dateComponents([.hour, .minute], from: date)
        return (c.hour ?? 0) * 60 + (c.minute ?? 0)
    }

    func addingMinutes(_ minutes: Int, to date: Date) -> Date {
        date.addingTimeInterval(TimeInterval(minutes * 60))
    }

    /// "2026-09-25"
    func dayKey(_ date: Date) -> String {
        let c = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", c.year ?? 0, c.month ?? 0, c.day ?? 0)
    }

    // MARK: Demo date

    /// Demo dates are days in `demoTimeZone`, like the server's.
    private static let demoCalendar: Calendar = {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = demoTimeZone
        cal.locale = Locale(identifier: "en_US_POSIX")
        return cal
    }()

    /// `real` moved by whole calendar days so that its date in New York is `day`, at the same time
    /// of day. Calendar days, not 24-hour steps, so a daylight-saving change in between doesn't
    /// move the time of day.
    private static func moved(_ real: Date, to day: DateComponents) -> Date {
        let cal = demoCalendar
        guard let target = cal.date(from: day) else { return real }
        let days = cal.dateComponents([.day], from: cal.startOfDay(for: real), to: target).day ?? 0
        return cal.date(byAdding: .day, value: days, to: real) ?? real
    }

    /// "2026-09-27" → year, month and day; nil unless it's a real day written exactly that way.
    private static func parseDay(_ text: String) -> DateComponents? {
        let parts = text.split(separator: "-", omittingEmptySubsequences: false)
        guard parts.count == 3,
              zip(parts, [4, 2, 2]).allSatisfy({ part, length in part.count == length && part.allSatisfy { $0.isASCII && $0.isNumber } }),
              let year = Int(parts[0]), let month = Int(parts[1]), let day = Int(parts[2]),
              let date = demoCalendar.date(from: DateComponents(year: year, month: month, day: day))
        else { return nil }
        // Calendar rolls "2026-02-30" over into March: only a day that comes back unchanged is real.
        let check = demoCalendar.dateComponents([.year, .month, .day], from: date)
        guard check.year == year, check.month == month, check.day == day else { return nil }
        return DateComponents(year: year, month: month, day: day)
    }
}

/// Where `AppClock.demoDate` lives: a reference, so every copy of a clock sees the same date, and
/// observable, so views that read `now` redraw when it's set or cleared.
@Observable
private final class DemoDay {
    var day: DateComponents?
}

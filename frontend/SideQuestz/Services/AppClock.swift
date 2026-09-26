import Foundation

/// "Now", time zone and calendar for the whole app.
///
/// The demo is set in Atlanta on Friday Sep 25, 2026 at 2:10 PM (the "Now" line on Home), so mock
/// mode pins the clock there. Live mode uses the real clock and the device time zone.
struct AppClock {
    let timeZone: TimeZone
    let calendar: Calendar
    private let fixedNow: Date?

    var now: Date { fixedNow ?? Date() }

    init(timeZone: TimeZone, fixedNow: Date? = nil) {
        self.timeZone = timeZone
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = timeZone
        cal.locale = Locale(identifier: "en_US_POSIX")
        self.calendar = cal
        self.fixedNow = fixedNow
    }

    static let demoTimeZone = TimeZone(identifier: "America/New_York")!

    /// Friday, September 25, 2026 · 2:10 PM in Atlanta.
    static let demo: AppClock = {
        let clock = AppClock(timeZone: demoTimeZone)
        return AppClock(timeZone: demoTimeZone, fixedNow: clock.date(2026, 9, 25, 14, 10))
    }()

    static let live = AppClock(timeZone: .current)

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
}

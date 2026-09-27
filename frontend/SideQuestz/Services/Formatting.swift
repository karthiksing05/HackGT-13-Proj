import Foundation

/// Time and date formatting that matches the prototype's copy exactly.
/// All functions take the `AppClock` so they render in the itinerary's time zone.
struct TimeFormat {
    let clock: AppClock

    private func parts(_ date: Date) -> (hour12: Int, minute: Int, isPM: Bool) {
        let c = clock.calendar.dateComponents([.hour, .minute], from: date)
        let h = c.hour ?? 0
        return ((h % 12) == 0 ? 12 : h % 12, c.minute ?? 0, h >= 12)
    }

    /// "2:10 PM"
    func time(_ date: Date) -> String {
        let p = parts(date)
        return "\(p.hour12):\(String(format: "%02d", p.minute)) \(p.isPM ? "PM" : "AM")"
    }

    /// "2:30–4:00 PM" when both share AM/PM, else "11:00 AM–1:00 PM".
    func range(_ start: Date, _ end: Date) -> String {
        let a = parts(start), b = parts(end)
        let aStr = "\(a.hour12):\(String(format: "%02d", a.minute))"
        let bStr = "\(b.hour12):\(String(format: "%02d", b.minute)) \(b.isPM ? "PM" : "AM")"
        return a.isPM == b.isPM ? "\(aStr)–\(bStr)" : "\(aStr) \(a.isPM ? "PM" : "AM")–\(bStr)"
    }

    /// "2:10 PM–6:30 PM" (both suffixes, as in the Review summary).
    func fullRange(_ start: Date, _ end: Date) -> String {
        "\(time(start))–\(time(end))"
    }

    /// Compact hour range for itinerary cards: "1–8 PM", "1–7:30 PM", "11 AM–2 PM".
    func compactRange(_ start: Date, _ end: Date) -> String {
        let a = parts(start), b = parts(end)
        func short(_ p: (hour12: Int, minute: Int, isPM: Bool)) -> String {
            p.minute == 0 ? "\(p.hour12)" : "\(p.hour12):\(String(format: "%02d", p.minute))"
        }
        let suffixB = b.isPM ? "PM" : "AM"
        return a.isPM == b.isPM ? "\(short(a))–\(short(b)) \(suffixB)" : "\(short(a)) \(a.isPM ? "PM" : "AM")–\(short(b)) \(suffixB)"
    }

    /// Hour label on timelines: "1 PM", "6 AM", "12 PM".
    func hourLabel(_ hour24: Int) -> String {
        let h12 = (hour24 % 12) == 0 ? 12 : hour24 % 12
        return "\(h12) \(hour24 % 24 >= 12 ? "PM" : "AM")"
    }

    private func formatter(_ template: String) -> DateFormatter {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US")
        f.timeZone = clock.timeZone
        f.setLocalizedDateFormatFromTemplate(template)
        return f
    }

    /// "FRIDAY, SEPTEMBER 25" (Home eyebrow).
    func eyebrowDate(_ date: Date) -> String {
        formatter("EEEEMMMMd").string(from: date).uppercased()
    }

    /// "Friday, Sep 25" (calendar day panel title).
    func dayTitle(_ date: Date) -> String {
        formatter("EEEEMMMd").string(from: date)
    }

    /// "Fri, Sep 25" (Create › When, Review summary).
    func shortDate(_ date: Date) -> String {
        formatter("EEEMMMd").string(from: date)
    }

    /// "Friday, September 25" (accessibility labels).
    func longDate(_ date: Date) -> String {
        formatter("EEEEMMMMd").string(from: date)
    }

    /// "Fri" / "Sat"
    func weekdayShort(_ date: Date) -> String {
        formatter("EEE").string(from: date)
    }

    /// Day of month: "25"
    func dayNumber(_ date: Date) -> String {
        String(clock.calendar.component(.day, from: date))
    }

    /// "Today" or "Sat" / "Fri, Oct 2" for itinerary cards.
    func relativeDay(_ date: Date) -> String {
        if clock.isToday(date) { return "Today" }
        let days = clock.calendar.dateComponents([.day], from: clock.startOfDay(clock.now), to: clock.startOfDay(date)).day ?? 0
        return (0..<7).contains(days) ? weekdayShort(date) : shortDate(date)
    }

    /// "in 20 min", "in 1 hr", "in 1 hr 5 min" from `now` to `date` (a started minute counts);
    /// `spoken` spells the units out for VoiceOver ("in 1 hour 5 minutes").
    func countdown(to date: Date, from now: Date, spoken: Bool = false) -> String {
        let minutes = max(1, Int((date.timeIntervalSince(now) / 60).rounded(.up)))
        let hours = minutes / 60, rest = minutes % 60
        func unit(_ n: Int, _ short: String, _ long: String) -> String {
            spoken ? "\(n) \(long)\(n == 1 ? "" : "s")" : "\(n) \(short)"
        }
        guard hours > 0 else { return "in \(unit(minutes, "min", "minute"))" }
        return rest == 0 ? "in \(unit(hours, "hr", "hour"))" : "in \(unit(hours, "hr", "hour")) \(unit(rest, "min", "minute"))"
    }
}

/// Distances in US units: under 0.1 mi in feet (to the nearest 50 ft), under 10 mi with one
/// decimal ("1.4 mi"), else whole miles ("263 mi").
enum DistanceFormat {
    static let metersPerMile = 1609.344
    static let feetPerMeter = 3.280_84

    /// "550 ft", "1.4 mi", "263 mi"
    static func short(_ meters: Double) -> String {
        switch reading(meters) {
        case .feet(let feet): "\(feet) ft"
        case .tenths(let miles): "\(String(format: "%.1f", miles)) mi"
        case .miles(let miles): "\(grouped(miles)) mi"
        }
    }

    /// For VoiceOver: "550 feet", "1.4 miles", "1 mile", "263 miles".
    static func spoken(_ meters: Double) -> String {
        switch reading(meters) {
        case .feet(let feet):
            return "\(feet) feet"
        case .tenths(let miles):
            guard miles != miles.rounded() else { return Int(miles) == 1 ? "1 mile" : "\(Int(miles)) miles" }
            return "\(String(format: "%.1f", miles)) miles"
        case .miles(let miles):
            return "\(grouped(miles)) miles"
        }
    }

    private enum Reading {
        case feet(Int), tenths(Double), miles(Int)
    }

    private static func reading(_ meters: Double) -> Reading {
        let miles = max(0, meters) / metersPerMile
        if miles < 0.1 {
            let feet = Int((max(0, meters) * feetPerMeter / 50).rounded()) * 50
            return .feet(max(50, feet))
        }
        // 9.96 mi rounds to "10.0": from there it's whole miles.
        let tenths = (miles * 10).rounded() / 10
        return tenths < 10 ? .tenths(tenths) : .miles(Int(miles.rounded()))
    }

    /// "1,243"
    private static func grouped(_ value: Int) -> String {
        value.formatted(.number.locale(Locale(identifier: "en_US")))
    }
}

/// Money is stored as Int cents and formatted with the currency FormatStyle.
enum Money {
    /// "$9.00"
    static func format(_ cents: Int) -> String {
        (Decimal(cents) / 100).formatted(.currency(code: "USD").locale(Locale(identifier: "en_US")))
    }

    /// "$9" when whole dollars, else "$9.50" (chips like "You owe $9").
    static func compact(_ cents: Int) -> String {
        cents % 100 == 0 ? "$\(cents / 100)" : format(cents)
    }

    /// Unknown real prices stay a visible placeholder: never invent one.
    static func orPlaceholder(_ cents: Int?, placeholder: String = "$[price]") -> String {
        guard let cents else { return placeholder }
        return format(cents)
    }

    /// Parses user input like "40", "$13.50", "1,200.5" into cents (0 when invalid).
    static func parseCents(_ text: String) -> Int {
        let cleaned = text.filter { $0.isNumber || $0 == "." }
        guard let value = Decimal(string: cleaned, locale: Locale(identifier: "en_US")) else { return 0 }
        var cents = value * 100
        var rounded = Decimal()
        NSDecimalRound(&rounded, &cents, 0, .plain)
        return NSDecimalNumber(decimal: rounded).intValue
    }
}

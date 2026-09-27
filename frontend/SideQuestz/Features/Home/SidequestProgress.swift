import CoreLocation
import SwiftUI

/// Where a sidequest stands at `now`: the stop you're at or heading to, how much of each stop is
/// behind you and, when the phone knows where you are, how far that stop is. Pure, so Home works
/// it out afresh on every tick of its clock.
nonisolated struct SidequestProgress {
    nonisolated enum Phase: Equatable {
        /// Another day; the first block starts at `start`.
        case later(start: Date)
        /// Today, before the first block.
        case startsSoon(start: Date)
        /// Inside a stop, which ends at `until`.
        case atStop(ItineraryItem, until: Date)
        /// Between stops (in transit or a calendar block); `next` starts at `at`.
        case onTheWay(next: ItineraryItem, at: Date)
        /// The stops are behind you and the last block (the way back) runs until `until`.
        case headingBack(until: Date)
        /// After the last block.
        case done

        /// Same phase, same stop (by id) and time. (By id: comparing whole items needs the main
        /// actor.)
        static func == (a: Phase, b: Phase) -> Bool {
            switch (a, b) {
            case (.later(let x), .later(let y)), (.startsSoon(let x), .startsSoon(let y)), (.headingBack(let x), .headingBack(let y)):
                return x == y
            case (.atStop(let i, let x), .atStop(let j, let y)), (.onTheWay(let i, let x), .onTheWay(let j, let y)):
                return i.id == j.id && x == y
            case (.done, .done):
                return true
            default:
                return false
            }
        }
    }

    /// Within this of the stop you're at or heading to, you're there.
    static let hereRadius: CLLocationDistance = 150
    /// Farther than this from every point on the route, you're somewhere else entirely.
    static let farAwayDistance: CLLocationDistance = 50_000

    /// Sidequest and group blocks, in the order you visit them.
    let stops: [ItineraryItem]
    let phase: Phase
    /// How much of each stop is behind you (0…1), in `stops` order.
    let fills: [Double]
    /// Stops that are over.
    let completed: Int
    /// The stop you're at or heading to (the first one before the sidequest starts); nil once the
    /// stops are behind you.
    let target: ItineraryItem?
    /// Meters from you to `target`, or, when you're far away, to the nearest point on the route.
    let distance: CLLocationDistance?
    /// More than 50 km from every point on the route.
    let isFarAway: Bool

    init(itinerary: Itinerary, now: Date, calendar: Calendar, here: Coordinate?) {
        let stops = Self.stops(of: itinerary)
        let first = itinerary.items.map(\.start).min() ?? itinerary.start
        let last = itinerary.items.map(\.end).max() ?? itinerary.backBy
        let phase: Phase
        if now >= last {
            phase = .done
        } else if now < first {
            phase = calendar.isDate(first, inSameDayAs: now) ? .startsSoon(start: first) : .later(start: first)
        } else if let current = stops.first(where: { $0.start <= now && now < $0.end }) {
            phase = .atStop(current, until: current.end)
        } else if let next = stops.first(where: { $0.start > now }) {
            phase = .onTheWay(next: next, at: next.start)
        } else {
            phase = .headingBack(until: last)
        }

        let fills = stops.map { stop -> Double in
            if now >= stop.end { return 1 }
            if now <= stop.start { return 0 }
            return now.timeIntervalSince(stop.start) / stop.end.timeIntervalSince(stop.start)
        }

        let target: ItineraryItem?
        switch phase {
        case .atStop(let stop, _), .onTheWay(let stop, _): target = stop
        case .later, .startsSoon: target = stops.first
        case .headingBack, .done: target = nil
        }

        var distance: CLLocationDistance?
        var isFarAway = false
        if let here {
            let nearest = SidequestRoute(itinerary: itinerary).path.map { Self.meters(here, $0) }.min()
            if let nearest, nearest > Self.farAwayDistance {
                isFarAway = true
                distance = nearest
            } else if let place = target?.place?.coordinate {
                distance = Self.meters(here, place)
            }
        }

        self.stops = stops
        self.phase = phase
        self.fills = fills
        self.completed = fills.filter { $0 >= 1 }.count
        self.target = target
        self.distance = distance
        self.isFarAway = isFarAway
    }

    /// You're within 150 m of the stop you're at or heading to, while the sidequest is on.
    var isHere: Bool {
        guard !isFarAway, let distance else { return false }
        switch phase {
        case .atStop, .onTheWay: return distance <= Self.hereRadius
        default: return false
        }
    }

    /// 1 for the first stop: its place among the sidequest's stops.
    func number(of item: ItineraryItem) -> Int? {
        stops.firstIndex { $0.id == item.id }.map { $0 + 1 }
    }

    /// Sidequest and group blocks ("3 stops"), in start order.
    static func stops(of itinerary: Itinerary) -> [ItineraryItem] {
        itinerary.items.filter { $0.kind == .sidequest || $0.kind == .group }.sorted { $0.start < $1.start }
    }

    /// Straight-line meters between two points.
    static func meters(_ a: Coordinate, _ b: Coordinate) -> CLLocationDistance {
        CLLocation(latitude: a.lat, longitude: a.lng).distance(from: CLLocation(latitude: b.lat, longitude: b.lng))
    }
}

/// The strip's one line: what's on now or next, then where you are in relation to it, e.g.
/// "Next: Skyline Park rooftop at 2:30 PM · in 20 min" + "1.4 mi". `spoken` is the whole thing
/// for VoiceOver, units spelled out.
struct SidequestProgressLine: Equatable {
    /// What's on now or next.
    var text: String
    /// "1.4 mi", "You're here", "You're 263 mi away"; nil when there's nothing to say.
    var note: String?
    var spoken: String

    init(_ progress: SidequestProgress, format: TimeFormat, now: Date) {
        let total = progress.stops.count
        let stops = "\(total) \(total == 1 ? "stop" : "stops")"
        let completed = "\(progress.completed) of \(stops)"

        // What's on now or next, on screen and read aloud; VoiceOver ends with the count so far.
        let said: [String]
        var tally: String? = "\(completed) done"
        switch progress.phase {
        case .later(let start):
            text = "Starts \(format.relativeDay(start)) at \(format.time(start)) · \(stops)"
            said = ["Starts \(format.longDate(start)) at \(format.time(start))", stops]
            tally = nil
        case .startsSoon(let start):
            text = "Starts at \(format.time(start)) · \(format.countdown(to: start, from: now))"
            said = ["Starts at \(format.time(start))", format.countdown(to: start, from: now, spoken: true)]
        case .atStop(let stop, let until):
            text = "Now at \(stop.title) · until \(format.time(until))"
            said = ["Now at \(stop.title)", "until \(format.time(until))"]
        case .onTheWay(let next, let at):
            text = "Next: \(next.title) at \(format.time(at)) · \(format.countdown(to: at, from: now))"
            said = ["Next: \(next.title) at \(format.time(at))", format.countdown(to: at, from: now, spoken: true)]
        case .headingBack:
            text = "Heading back · \(completed) done"
            said = ["Heading back"]
        case .done:
            text = "Done · \(completed)"
            said = ["Done", completed]
            tally = nil
        }

        // Then where you are: far from the whole route (until it's over), or, on the way to a stop
        // or at it, there already or how far it is.
        let place: String?
        switch (progress.phase, progress.distance) {
        case (.headingBack, _), (.done, _), (_, nil):
            note = nil
            place = nil
        case (_, let meters?) where progress.isFarAway:
            note = "You're \(DistanceFormat.short(meters)) away"
            place = "You're \(DistanceFormat.spoken(meters)) away"
        case (.atStop, _) where progress.isHere, (.onTheWay, _) where progress.isHere:
            note = "You're here"
            place = note
        case (.atStop, let meters?):
            note = "\(DistanceFormat.short(meters)) away"
            place = "\(DistanceFormat.spoken(meters)) away"
        case (.onTheWay, let meters?):
            note = DistanceFormat.short(meters)
            place = "\(DistanceFormat.spoken(meters)) away"
        default:
            note = nil
            place = nil
        }
        spoken = (said + [place, tally].compactMap { $0 }).joined(separator: ", ")
    }

    /// The line as it reads on screen.
    var display: String {
        note.map { "\(text) · \($0)" } ?? text
    }
}

/// A sidequest's progress on Home, above its timeline or map: one capsule per stop (sage once
/// it's over, filling while you're there, grey ahead) and one line saying what's on now or next
/// and how far it is from you.
struct SidequestProgressStrip: View {
    let progress: SidequestProgress
    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let line = SidequestProgressLine(progress, format: env.format, now: env.clock.now)
        VStack(alignment: .leading, spacing: 9) {
            bar
            lineText(line)
                .sqFont(13, .medium)
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .sqNumeric()
        }
        .padding(.horizontal, 14)
        .padding(.top, 12)
        .padding(.bottom, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: progress.fills)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: line)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(line.spoken)
        .accessibilityIdentifier("sidequest.progress")
    }

    private func lineText(_ line: SidequestProgressLine) -> Text {
        guard let note = line.note else { return Text(line.text) }
        return Text(line.text) + Text(" · ") + Text(note).fontWeight(.semibold).foregroundStyle(Theme.sageInk)
    }

    /// One 6pt capsule per stop; the one you're at fills as it goes.
    private var bar: some View {
        HStack(spacing: 4) {
            if progress.fills.isEmpty {
                Capsule().fill(Theme.mutedBorder)
            }
            ForEach(Array(progress.fills.enumerated()), id: \.offset) { _, fill in
                Capsule()
                    .fill(Theme.mutedBorder)
                    .overlay {
                        Rectangle()
                            .fill(Theme.sage)
                            .scaleEffect(x: fill, y: 1, anchor: .leading)
                    }
                    .clipShape(Capsule())
            }
        }
        .frame(height: 6)
        .accessibilityHidden(true)
    }
}

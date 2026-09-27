import CoreGraphics
import Foundation

/// One thing in the demo catalog (Create › Vibe › Must-see): a place the demo's plans and swaps use,
/// or an event on the demo day.
struct MockActivity {
    let id: String
    let title: String
    let kind: PlanStopKind
    let category: String
    /// "Live music", "Skyline views"
    let label: String
    /// "Free", "$", "$$"
    let priceLabel: String
    let venue: Place
    let tags: [String]
    /// How long a visit takes (an event: how long you'd stay).
    let minutes: Int
    var start: Date? = nil
    var end: Date? = nil
    var priceCents: Int? = nil

    /// A planned stop's line: "Live music · Free".
    var stopSubtitle: String { "\(label) · \(priceLabel)" }
}

/// The demo server's `GET /activities/search`: the places from the demo's plans and swap catalog,
/// plus a few events on Friday, Sep 25. Places have no opening hours here, so they're always open.
enum MockActivities {
    private static let clock = AppClock.demo

    static let all: [MockActivity] = places + events

    static func activity(id: String) -> MockActivity? { all.first { $0.id == id } }

    /// Every stop the demo's options use (the Forum dinner isn't a place to pick) and the swap
    /// catalog's spots, once each.
    static let places: [MockActivity] = {
        let planned = (MockData.firstOptions + MockData.moreOptionBatches.flatMap { $0 })
            .flatMap(\.stops)
            .filter { !$0.title.contains("(Forum)") }
            .map { (title: $0.title, subtitle: $0.subtitle, minutes: $0.durationMinutes, coordinate: MockPlaces.stops[$0.title]?.coordinate) }
        let spots = MockAlternatives.catalog.map { (title: $0.title, subtitle: $0.subtitle, minutes: $0.minutes, coordinate: Optional($0.coordinate)) }
        var seen = Set<String>()
        return (planned + spots).filter { seen.insert($0.title).inserted }.map { spot in
            let parts = spot.subtitle.components(separatedBy: " · ")
            let price = parts.count > 1 ? parts[parts.count - 1] : "Free"
            let kind = MockAlternatives.kinds(of: PlanStop(id: "", title: spot.title, subtitle: spot.subtitle,
                                                           place: Place(name: spot.title), durationMinutes: 0)).first
            return MockActivity(id: id(for: spot.title), title: spot.title, kind: .place, category: kind?.category ?? "place",
                                label: parts.first ?? spot.subtitle, priceLabel: price,
                                venue: Place(name: spot.title, coordinate: spot.coordinate), tags: kind?.tags ?? [],
                                minutes: spot.minutes, priceCents: price == "Free" ? 0 : nil)
        }
    }()

    /// Friday's events, one that's over by the demo's 2:10 PM and one on Saturday (the search leaves
    /// those two out on Friday).
    static let events: [MockActivity] = [
        event("Rooftop trivia", category: "trivia", label: "Trivia", at: skyline, 15, 0, stay: 60, until: (16, 0),
              price: ("$", 500), tags: ["trivia", "games", "rooftop"]),
        event("Gallery talk at the High", category: "art", label: "Gallery talk",
              at: Place(name: "High Museum of Art", coordinate: Coordinate(lat: 33.7901, lng: -84.3856)), 15, 30, stay: 45,
              until: (16, 15), price: ("$$", 1800), tags: ["art", "museum", "talk"]),
        event("Food truck Friday", category: "food", label: "Food trucks", at: MockPlaces.piedmont.place, 16, 0, stay: 60,
              until: (19, 0), price: ("$", nil), tags: ["food", "trucks", "outdoors"]),
        event("Pickup soccer", category: "sports", label: "Sports",
              at: Place(name: "Historic Fourth Ward Park", coordinate: MockPlaces.stops["Historic Fourth Ward Park"]?.coordinate),
              16, 30, stay: 60, until: (17, 30), price: ("Free", 0), tags: ["soccer", "sports", "active", "outdoors"]),
        event("Sunset jazz on the Eastside Trail", category: "live_music", label: "Live music",
              at: Place(name: "BeltLine Eastside Trail", coordinate: Coordinate(lat: 33.7650, lng: -84.3615)), 17, 0, stay: 60,
              until: (18, 0), price: ("Free", 0), tags: ["jazz", "music", "outdoors"]),
        event("Late show at the Plaza Theatre", category: "film", label: "Film",
              at: Place(name: "Plaza Theatre", coordinate: Coordinate(lat: 33.7735, lng: -84.3530)), 20, 0, stay: 120,
              until: (22, 0), price: ("$", 1200), tags: ["film", "movie", "cinema"]),
        event("Morning farmers market", category: "market", label: "Farmers market",
              at: Place(name: "Grant Park", coordinate: Coordinate(lat: 33.7370, lng: -84.3700)), 9, 0, stay: 60,
              until: (13, 0), price: ("Free", 0), tags: ["market", "food", "outdoors"]),
        event("Sunrise yoga in Piedmont Park", category: "fitness", label: "Yoga", on: 26, at: MockPlaces.piedmont.place, 7, 30,
              stay: 60, until: (8, 30), price: ("Free", 0), tags: ["yoga", "active", "outdoors"]),
    ]

    private static var skyline: Place {
        Place(name: "Skyline Park rooftop", coordinate: MockPlaces.stops["Skyline Park rooftop"]?.coordinate)
    }

    private static func event(_ title: String, category: String, label: String, on day: Int = 25, at venue: Place,
                              _ hour: Int, _ minute: Int, stay: Int, until end: (Int, Int), price: (label: String, cents: Int?),
                              tags: [String]) -> MockActivity {
        MockActivity(id: id(for: title), title: title, kind: .event, category: category, label: label, priceLabel: price.label,
                     venue: venue, tags: tags, minutes: stay, start: clock.date(2026, 9, day, hour, minute),
                     end: clock.date(2026, 9, day, end.0, end.1), priceCents: price.cents)
    }

    /// Stable ids, so a pick means the same activity in every search and plan.
    static func id(for title: String) -> String {
        "act-" + title.lowercased().map { $0.isLetter || $0.isNumber ? String($0) : "-" }.joined()
    }

    // MARK: Search

    /// Like the server: `query` matches the name, the venue, the category or a tag (any case or
    /// accent); events are that day's (today without a `date`) and not over yet. An empty query
    /// suggests that day's events by start, then the places nearest `near`. Name-prefix matches come
    /// first, then events by start, then places by distance.
    static func search(_ query: String, near: Coordinate?, date: Date?, limit: Int, clock: AppClock) -> [ActivityHit] {
        let q = fold(query.trimmingCharacters(in: .whitespacesAndNewlines))
        let day = date ?? clock.now
        let from = near.map { MockRouteEngine.mapPoint(for: Place(name: "", coordinate: $0)) }
        let found = all.filter { activity in
            if let start = activity.start {
                guard clock.calendar.isDate(start, inSameDayAs: day), (activity.end ?? start) > clock.now else { return false }
            }
            return q.isEmpty || matches(activity, q)
        }
        let ranked = found.map { activity -> (activity: MockActivity, prefix: Bool, miles: Double?) in
            let miles: Double? = from.map { from -> Double in
                let to = MockRouteEngine.mapPoint(for: activity.venue)
                return (Double(hypot(from.x - to.x, from.y - to.y)) * MockRouteEngine.milesPerPoint * 10).rounded() / 10
            }
            return (activity, !q.isEmpty && fold(activity.title).hasPrefix(q), miles)
        }
        .sorted { a, b in
            if a.prefix != b.prefix { return a.prefix }
            switch (a.activity.start, b.activity.start) {
            case let (x?, y?) where x != y: return x < y
            case (.some, nil): return true
            case (nil, .some): return false
            default: break
            }
            if let x = a.miles, let y = b.miles, x != y { return x < y }
            return a.activity.title < b.activity.title
        }
        return ranked.prefix(min(max(limit, 1), 50)).map { hit($0.activity, miles: $0.miles, clock: clock) }
    }

    /// What the search returns for `activity`, with the server's one-line subtitle
    /// ("Live music · 5:00 PM · 0.9 mi", "Skyline views · Free · 1.1 mi").
    static func hit(_ activity: MockActivity, miles: Double?, clock: AppClock) -> ActivityHit {
        let when = activity.start.map { TimeFormat(clock: clock).time($0) } ?? activity.priceLabel
        let distance = miles.map { String(format: "%.1f mi", max($0, 0.1)) }
        return ActivityHit(id: activity.id, title: activity.title, kind: activity.kind, category: activity.category,
                           subtitle: [activity.label, when, distance].compactMap { $0 }.joined(separator: " · "),
                           place: activity.venue, start: activity.start, end: activity.end, priceCents: activity.priceCents,
                           distanceMi: miles.map { max($0, 0.1) })
    }

    private static func matches(_ activity: MockActivity, _ q: String) -> Bool {
        ([activity.title, activity.venue.name, activity.category, activity.label] + activity.tags).contains { fold($0).contains(q) }
    }

    private static func fold(_ text: String) -> String {
        text.lowercased().folding(options: .diacriticInsensitive, locale: nil)
    }
}

private extension MockAlternatives.Kind {
    var category: String {
        switch self {
        case .views: "views"
        case .art: "art"
        case .food: "food"
        case .park: "park"
        case .games: "games"
        case .books: "books"
        }
    }

    var tags: [String] {
        switch self {
        case .views: ["views", "skyline", "rooftop"]
        case .art: ["art", "murals", "gallery", "museum"]
        case .food: ["food", "market", "tacos", "bbq"]
        case .park: ["park", "outdoors", "walk", "trail"]
        case .games: ["games", "arcade", "mini golf"]
        case .books: ["books", "bookstore"]
        }
    }
}

/// The demo planner's side of must-see picks (`PlanRequest.mustInclude`): every option gets every
/// pick, at a time that works, and the option's own stops make way when the window is too short.
/// The real planner does this with real travel times and opening hours.
enum MockPicks {
    /// The server's cap.
    static let maxPicks = 10

    enum Resolution {
        /// The picks, in the order sent (none: plan as usual).
        case picks([MockActivity])
        /// This pick can't be in any option: the batch is empty with `must_include_unavailable`.
        case unavailable(String)
    }

    /// Duplicates are ignored and more than 10 is a 400. An id the catalog doesn't know ("a pick"),
    /// an event on another day, or one that's over makes the batch empty.
    static func resolve(_ request: PlanRequest, clock: AppClock) throws -> Resolution {
        var seen = Set<String>()
        let ids = request.mustInclude.filter { seen.insert($0).inserted }
        guard ids.count <= maxPicks else { throw APIError.validation("Pick up to 10 must-see spots.") }
        var picks: [MockActivity] = []
        for id in ids {
            guard let activity = MockActivities.activity(id: id) else { return .unavailable("a pick") }
            if let start = activity.start {
                let over = (activity.end ?? start) <= clock.now
                guard clock.calendar.isDate(start, inSameDayAs: request.date), !over else { return .unavailable(activity.title) }
            }
            picks.append(activity)
        }
        return .picks(picks)
    }

    /// `base` with every pick in it, or nil when the picks can't all fit in the window (even without
    /// the option's own stops). A stop the option already has becomes the pick; the others go where
    /// they keep the route shortest (events wait for their start). As many of the option's own stops
    /// stay as the window allows, back by `backBy` with no late start; among equals, the plan that
    /// gets back first.
    static func fit(_ base: PlanOption, picks: [MockActivity], request: PlanRequest) -> PlanOption? {
        let pickIds = Set(picks.map(\.id))
        var stops = base.stops
        var toPlace: [PlanStop] = []
        for activity in picks {
            let pick = stop(for: activity, in: base.id)
            if let index = stops.firstIndex(where: { $0.title == activity.title && $0.activityId == nil }) {
                stops[index] = pick
            } else {
                toPlace.append(pick)
            }
        }
        // Fixed starts first, in time order: they pin the timeline.
        toPlace.sort { ($0.arriveTime ?? .distantFuture) < ($1.arriveTime ?? .distantFuture) }
        let own = stops.indices.filter { !pickIds.contains(stops[$0].activityId ?? "") }
        // Every choice of own stops to leave out (the demo's options have three), the later ones
        // first, so a tie keeps the option's opening stops.
        var found: (stops: [PlanStop], kept: Int, arrival: Date)?
        for mask in 0..<(1 << own.count) {
            let dropped = Set(own.indices.filter { mask & (1 << (own.count - 1 - $0)) != 0 }.map { own[$0] })
            var candidate = stops.indices.filter { !dropped.contains($0) }.map { stops[$0] }
            for pick in toPlace {
                candidate = best((0...candidate.count).map { index in
                    var placed = candidate
                    placed.insert(pick, at: index)
                    return placed
                }, request)
            }
            let result = route(candidate, request)
            guard result.brokenAt < 0, result.minutesLate == 0 else { continue }
            let kept = own.count - dropped.count
            if let current = found, current.kept > kept || (current.kept == kept && current.arrival <= result.arrival) { continue }
            found = (candidate, kept, result.arrival)
        }
        guard let planned = found?.stops else { return nil }
        var option = base
        option.stops = planned
        if planned.map(\.title) != base.stops.map(\.title) { option.meta = meta(planned, request) }
        // A name like "Rooftop + murals" is only kept while those stops are still in it.
        if !base.stops.allSatisfy({ own in planned.contains { $0.title == own.title } }) { option.name = name(planned) }
        return option
    }

    /// A pick as a stop of `optionId`. An event keeps its start (`arriveTime`, not flexible).
    static func stop(for activity: MockActivity, in optionId: String) -> PlanStop {
        PlanStop(id: "\(optionId)-pick-\(activity.id)", title: activity.title, subtitle: activity.stopSubtitle,
                 place: activity.venue, durationMinutes: activity.minutes, arriveTime: activity.start,
                 departTime: activity.start.map { $0.addingTimeInterval(TimeInterval(activity.minutes * 60)) },
                 kind: activity.kind, flexible: activity.kind == .place, activityId: activity.id)
    }

    private static func route(_ stops: [PlanStop], _ request: PlanRequest) -> RouteResult {
        MockRouteEngine.route(stops: stops, start: request.start, end: request.end, startTime: request.startTime,
                              backBy: request.backBy, ride: request.ride)
    }

    /// No late start first, then the least time past `backBy`, then the earliest arrival.
    private static func best(_ candidates: [[PlanStop]], _ request: PlanRequest) -> [PlanStop] {
        let scored = candidates.map { stops -> (stops: [PlanStop], score: (Int, Int, Date)) in
            let result = route(stops, request)
            return (stops, (result.brokenAt >= 0 ? 1 : 0, result.minutesLate, result.arrival))
        }
        return scored.min { $0.score < $1.score }?.stops ?? candidates[0]
    }

    /// "Games, views + live music": what the stops are, in order, like the demo's own names.
    private static func name(_ stops: [PlanStop]) -> String {
        var words: [String] = []
        for stop in stops {
            let word = stop.kind == .event
                ? stop.subtitle.components(separatedBy: " · ").first?.lowercased()
                : MockAlternatives.kinds(of: stop).first?.category
            if let word, !words.contains(word) { words.append(word) }
        }
        guard let first = words.first else { return stops.first?.title ?? "" }
        let named = Array(words.prefix(3))
        let text = named.count == 1 ? first : named.dropLast().joined(separator: ", ") + " + " + named[named.count - 1]
        return text.prefix(1).uppercased() + text.dropFirst()
    }

    /// "~$ · 1.2 mi walking · 2 transit legs" for the route as planned.
    private static func meta(_ stops: [PlanStop], _ request: PlanRequest) -> String {
        let places = [request.start] + stops.map(\.place) + [request.end]
        let legs = route(stops, request).legs
        var walking = 0.0
        for (index, leg) in legs.enumerated() where leg.mode == .walk && index + 1 < places.count {
            let a = MockRouteEngine.mapPoint(for: places[index]), b = MockRouteEngine.mapPoint(for: places[index + 1])
            walking += Double(hypot(a.x - b.x, a.y - b.y)) * MockRouteEngine.milesPerPoint
        }
        let tier = stops.map { $0.subtitle.components(separatedBy: " · ").last?.filter { $0 == "$" }.count ?? 0 }.max() ?? 0
        var parts = [tier == 0 ? "Free" : "~" + String(repeating: "$", count: tier)]
        if walking >= 0.05 { parts.append(String(format: "%.1f mi walking", walking)) }
        let rides = legs.filter { $0.mode != .walk }.count
        if rides > 0 {
            let noun: (one: String, many: String) = switch request.ride {
            case .none: ("transit leg", "transit legs")
            case .drive: ("drive", "drives")
            case .cover: ("ride", "rides")
            }
            parts.append("\(rides) \(rides == 1 ? noun.one : noun.many)")
        }
        return parts.joined(separator: " · ")
    }
}

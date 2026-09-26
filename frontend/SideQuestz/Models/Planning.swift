import Foundation

/// Create › Where › "How far will you go in between?"
enum TravelRange: String, Codable, CaseIterable, Identifiable {
    case walkable, transit, anywhere
    var id: String { rawValue }
    var label: String {
        switch self {
        case .walkable: "Walkable"
        case .transit: "Transit"
        case .anywhere: "Anywhere"
        }
    }
    var sublabel: String {
        switch self {
        case .walkable: "≤ 15 min"
        case .transit: "≤ 30 min"
        case .anywhere: "Any distance"
        }
    }
}

/// Create › Where › "Can you provide a ride this time?" (asked for every sidequest).
enum RideChoice: String, Codable, CaseIterable, Identifiable {
    case drive, cover, none
    var id: String { rawValue }
    var label: String {
        switch self {
        case .drive: "I can drive"
        case .cover: "I'll cover rides"
        case .none: "No ride"
        }
    }
    var sublabel: String {
        switch self {
        case .drive: "Own car"
        case .cover: "Uber / Lyft"
        case .none: "Walk + transit"
        }
    }
    var note: String {
        switch self {
        case .drive: "We'll plan driving legs and parking. If the plan is shared, others see you can give rides."
        case .cover: "We'll plan rideshare legs and show the estimated cost split per person."
        case .none: "We'll keep it to walking, MARTA and bus. You can still join someone else's ride."
        }
    }
    /// More options › Ride row value.
    func summary(openSeats: Int) -> String {
        switch self {
        case .drive: "Driving · \(openSeats) open seats"
        case .cover: "Covering rides (Uber / Lyft)"
        case .none: "No ride · walk + transit"
        }
    }
}

enum TravelMode: String, Codable, CaseIterable, Identifiable {
    case walk, marta, rideshare, drive, uber
    var id: String { rawValue }
    var label: String {
        switch self {
        case .walk: "Walk"
        case .marta: "MARTA"
        case .rideshare: "Rideshare"
        case .drive: "Drive"
        case .uber: "Uber"
        }
    }
}

/// Everything the Create flow collects (POST /plans/generate).
struct PlanRequest: Codable, Hashable {
    var start: Place
    var end: Place
    var date: Date
    var startTime: Date
    var backBy: Date
    var range: TravelRange
    var ride: RideChoice
    var openSeats: Int?
    var moodText: String
    var tags: [String]
    /// 0 = Free, 1 = $, 2 = $$, 3 = $$$
    var budget: Int
    var who: Visibility
    var pace: Pace
    var modes: Set<TravelMode>
}

struct PlanStop: Codable, Identifiable, Hashable {
    let id: String
    var title: String
    /// "Games + views · $"
    var subtitle: String
    var place: Place
    var durationMinutes: Int
}

struct PlanOption: Codable, Identifiable, Hashable {
    let id: String
    var name: String
    /// "Best match", "Chill", "Meet people"…
    var tag: String
    /// "~$ · 1.8 mi walking · 2 transit legs"
    var meta: String
    var stops: [PlanStop]
}

/// Something similar that could take a stop's place (`POST /plans/alternatives`). Its `stop.id` can
/// go in a route's `stopOrder` like any of the option's own stops.
struct PlanAlternative: Codable, Identifiable, Hashable {
    var stop: PlanStop
    /// Why it's suggested, in a few words: "Also rooftop views · 0.2 mi away".
    var reason: String = ""

    var id: String { stop.id }
}

/// A page of plan options (generate / load more).
struct PlanBatch: Codable, Hashable {
    var options: [PlanOption]
    var cursor: String?
    /// true when there are no more options to load.
    var done: Bool
}

struct Leg: Codable, Hashable {
    var mode: TravelMode
    var minutes: Int
}

/// POST /plans/route — recalculated after a reorder, a swap or a removal.
struct RouteRequest: Codable, Hashable {
    var optionId: String
    /// Stop ids in the new order: the option's own, or alternatives the server suggested for it.
    /// A stop left out is removed.
    var stopOrder: [String]
    var start: Place
    var end: Place
    var startTime: Date
    var backBy: Date
    var ride: RideChoice
    var modes: Set<TravelMode>
}

struct RouteResult: Codable, Hashable {
    /// legs.count == stops.count + 1 (start → stop 1 … last stop → end)
    var legs: [Leg]
    var stopTimes: [DateInterval]
    var arrival: Date
    /// > 0 when you'd get back after `backBy`.
    var minutesLate: Int
}

/// POST /itineraries — save the chosen option.
struct CreateItineraryRequest: Codable, Hashable {
    var plan: PlanRequest
    /// The option as edited on Review (swapped stops in their places, removed ones gone).
    var option: PlanOption
    /// Stop ids in the final order.
    var stopOrder: [String]
    var route: RouteResult
    var visibility: Visibility
    var lockAt: Date?
    var maxGroupSize: Int?
}

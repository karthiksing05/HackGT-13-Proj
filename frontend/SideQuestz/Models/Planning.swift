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

/// How a leg is travelled. `marta` is the app's transit mode (the demo city's is MARTA); the server
/// may also say `transit`, `bus`, `train` or `subway`, which read as `marta`. Anything else reads as
/// a walk rather than failing the whole route.
enum TravelMode: String, Codable, CaseIterable, Identifiable {
    case walk, marta, rideshare, drive, uber
    var id: String { rawValue }
    var label: String {
        switch self {
        case .walk: "Walk"
        case .marta: "Transit"
        case .rideshare: "Rideshare"
        case .drive: "Drive"
        case .uber: "Uber"
        }
    }

    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        switch raw {
        case "transit", "bus", "train", "subway": self = .marta
        default: self = TravelMode(rawValue: raw) ?? .walk
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
    /// Create › Vibe › Must-see: catalog activity ids (`ActivityHit.id`) every option must include,
    /// in the order they were picked. Sent as `must_include`, and left out when there are none.
    var mustInclude: [String] = []

    enum CodingKeys: String, CodingKey {
        case start, end, date, startTime, backBy, range, ride, openSeats, moodText, tags, budget, who, pace, modes, mustInclude
    }

    /// A set has no order: `modes` goes out sorted, so the same request always reads the same.
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(start, forKey: .start)
        try c.encode(end, forKey: .end)
        try c.encode(date, forKey: .date)
        try c.encode(startTime, forKey: .startTime)
        try c.encode(backBy, forKey: .backBy)
        try c.encode(range, forKey: .range)
        try c.encode(ride, forKey: .ride)
        try c.encodeIfPresent(openSeats, forKey: .openSeats)
        try c.encode(moodText, forKey: .moodText)
        try c.encode(tags, forKey: .tags)
        try c.encode(budget, forKey: .budget)
        try c.encode(who, forKey: .who)
        try c.encode(pace, forKey: .pace)
        try c.encode(modes.sorted { $0.rawValue < $1.rawValue }, forKey: .modes)
        if !mustInclude.isEmpty { try c.encode(mustInclude, forKey: .mustInclude) }
    }
}

/// What a stop (or a catalog activity) is: an `event` has a fixed start (a show, a market opening),
/// a `place` can be visited whenever the route gets there.
enum PlanStopKind: String, Codable {
    case event, place
}

/// Something in the user's catalog that Create › Vibe › Must-see can pick (`GET /activities/search`).
/// Its `id` is the planner's activity id: picks go out in `PlanRequest.mustInclude` and come back
/// as stops' `activityId`.
struct ActivityHit: Codable, Identifiable, Hashable {
    let id: String
    var title: String
    var kind: PlanStopKind
    /// The catalog's category ("live_music"); the app shows `subtitle` instead.
    var category: String? = nil
    /// One short display line, made by the server: "Live music · 6:30 PM · 0.7 mi".
    var subtitle: String = ""
    var place: Place? = nil
    /// Events only: when it starts, and ends when known.
    var start: Date? = nil
    var end: Date? = nil
    /// nil when the price isn't known.
    var priceCents: Int? = nil
    /// From the plan's start; nil when the search had no `near`.
    var distanceMi: Double? = nil
}

struct PlanStop: Codable, Identifiable, Hashable {
    let id: String
    var title: String
    /// "Games + views · $"
    var subtitle: String
    var place: Place
    var durationMinutes: Int

    // Planner extras (optional, ignored by the demo; see API_CONTRACT.md › Planning).

    /// When the planner scheduled this visit; `RouteResult.stopTimes` has the timing of the order on screen.
    var arriveTime: Date? = nil
    var departTime: Date? = nil
    var kind: PlanStopKind? = nil
    /// true when the visit can move (a place, a drop-in); false for a fixed start.
    var flexible: Bool? = nil
    /// The catalog activity behind this stop (comes back on the saved itinerary's items).
    var activityId: String? = nil
}

struct PlanOption: Codable, Identifiable, Hashable {
    let id: String
    var name: String
    /// "Best match", "Chill", "Meet people"…
    var tag: String
    /// "~$ · 1.8 mi walking · 2 transit legs"
    var meta: String
    var stops: [PlanStop]

    // Planner extras (optional).

    /// A fixed-start stop would be reached after it starts at this pace ("Tight timing" on the card).
    var lateFlag: Bool = false
    /// Sum of the known prices; nil when the planner didn't say.
    var totalCostCents: Int? = nil
}

/// Something similar that could take a stop's place (`POST /plans/alternatives`). Its `stop.id` can
/// go in a route's `stopOrder` like any of the option's own stops.
struct PlanAlternative: Codable, Identifiable, Hashable {
    var stop: PlanStop
    /// Why it's suggested, in a few words: "Also rooftop views · 0.2 mi away".
    var reason: String = ""

    var id: String { stop.id }
}

/// Why a batch came back with no options (`PlanBatch.reason`, only with empty `options`). On the wire
/// it's one string: `no_candidates_fit_window`, `no_feasible_itinerary`, `invalid_request: <detail>`,
/// `must_include_unavailable: <title>`, `must_include_no_fit`, or anything else the server wants to
/// log (`other`).
enum PlanEmptyReason: Hashable {
    case noCandidatesFitWindow
    case noFeasibleItinerary
    case invalidRequest(String)
    /// A must-see pick can't be in any option: it isn't in the catalog, isn't on this date, is over,
    /// or isn't for this account's age. The server names the first such pick ("a pick" for an id it
    /// doesn't know).
    case mustIncludeUnavailable(String)
    /// The picks exist but can't all fit in this window with travel.
    case mustIncludeNoFit
    case other(String)

    /// The copy when the server gave no reason.
    static let defaultMessage = "No options fit this window. Try changing filters in More options."

    private static let unavailablePrefix = "must_include_unavailable"
    /// What the server calls a pick it can't name.
    private static let unnamedPick = "a pick"

    /// What Review says instead of options.
    var message: String {
        switch self {
        case .noCandidatesFitWindow: "Nothing nearby fits this window yet. Try a longer window, a wider range, or another day."
        case .noFeasibleItinerary: "We couldn't fit stops into this window. Try a wider range or a later back-by time."
        case .invalidRequest: "Check your start, end and times, then try again."
        case .mustIncludeUnavailable(let title):
            // "a pick" starts the sentence, so it gets a capital.
            "\(title.isEmpty || title == Self.unnamedPick ? "A pick" : title) isn't available in this window. Remove it or pick another time."
        case .mustIncludeNoFit: "Your must-see picks don't all fit in this window. Try a longer window or fewer picks."
        case .other: Self.defaultMessage
        }
    }

    init(rawValue raw: String) {
        let trimmed = raw.trimmingCharacters(in: .whitespaces)
        switch trimmed {
        case "no_candidates_fit_window": self = .noCandidatesFitWindow
        case "no_feasible_itinerary": self = .noFeasibleItinerary
        case "must_include_no_fit": self = .mustIncludeNoFit
        default:
            if trimmed.hasPrefix("invalid_request") {
                let detail = trimmed.dropFirst("invalid_request".count).trimmingCharacters(in: CharacterSet(charactersIn: ": "))
                self = .invalidRequest(detail)
            } else if trimmed == Self.unavailablePrefix || trimmed.hasPrefix(Self.unavailablePrefix + ":") {
                // Only the colon after the code goes: a title keeps its own punctuation.
                self = .mustIncludeUnavailable(trimmed.dropFirst(Self.unavailablePrefix.count + 1).trimmingCharacters(in: .whitespaces))
            } else {
                self = .other(trimmed)
            }
        }
    }

    var rawValue: String {
        switch self {
        case .noCandidatesFitWindow: "no_candidates_fit_window"
        case .noFeasibleItinerary: "no_feasible_itinerary"
        case .invalidRequest(let detail): detail.isEmpty ? "invalid_request" : "invalid_request: \(detail)"
        case .mustIncludeUnavailable(let title): title.isEmpty ? Self.unavailablePrefix : "\(Self.unavailablePrefix): \(title)"
        case .mustIncludeNoFit: "must_include_no_fit"
        case .other(let raw): raw
        }
    }
}

extension PlanEmptyReason: Codable {
    init(from decoder: Decoder) throws {
        self.init(rawValue: try decoder.singleValueContainer().decode(String.self))
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        try c.encode(rawValue)
    }
}

/// A page of plan options (generate / load more).
struct PlanBatch: Codable, Hashable {
    var options: [PlanOption]
    var cursor: String?
    /// true when there are no more options to load.
    var done: Bool
    /// Why `options` is empty, when the server says (Review shows `reason.message`).
    var reason: PlanEmptyReason? = nil
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

    enum CodingKeys: String, CodingKey {
        case optionId, stopOrder, start, end, startTime, backBy, ride, modes
    }

    /// `modes` goes out sorted (see `PlanRequest`).
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(optionId, forKey: .optionId)
        try c.encode(stopOrder, forKey: .stopOrder)
        try c.encode(start, forKey: .start)
        try c.encode(end, forKey: .end)
        try c.encode(startTime, forKey: .startTime)
        try c.encode(backBy, forKey: .backBy)
        try c.encode(ride, forKey: .ride)
        try c.encode(modes.sorted { $0.rawValue < $1.rawValue }, forKey: .modes)
    }
}

struct RouteResult: Codable, Hashable {
    /// legs.count == stops.count + 1 (start → stop 1 … last stop → end)
    var legs: [Leg]
    var stopTimes: [DateInterval]
    var arrival: Date
    /// > 0 when you'd get back after `backBy`.
    var minutesLate: Int
    /// Index (in the order sent) of the first stop with a fixed start that this order reaches too
    /// late, or -1 when every stop is on time. The route card marks that stop.
    var brokenAt: Int = -1
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
    /// Create › Vibe › Bring friends: the friends (user ids, in the order picked) who are on the plan
    /// from the start. The server adds them like an accepted join, with the plan's group chat; each
    /// must be a friend, 12 at most. Sent as `invite_user_ids`, and left out when there are none.
    var inviteUserIds: [String] = []

    enum CodingKeys: String, CodingKey {
        case plan, option, stopOrder, route, visibility, lockAt, maxGroupSize, inviteUserIds
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(plan, forKey: .plan)
        try c.encode(option, forKey: .option)
        try c.encode(stopOrder, forKey: .stopOrder)
        try c.encode(route, forKey: .route)
        try c.encode(visibility, forKey: .visibility)
        try c.encodeIfPresent(lockAt, forKey: .lockAt)
        try c.encodeIfPresent(maxGroupSize, forKey: .maxGroupSize)
        if !inviteUserIds.isEmpty { try c.encode(inviteUserIds, forKey: .inviteUserIds) }
    }
}

extension CreateItineraryRequest {
    /// The app sends this; it decodes only in the contract tests. `invite_user_ids` may be missing
    /// (it's left out when nobody is brought along).
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        plan = try c.decode(PlanRequest.self, forKey: .plan)
        option = try c.decode(PlanOption.self, forKey: .option)
        stopOrder = try c.decode([String].self, forKey: .stopOrder)
        route = try c.decode(RouteResult.self, forKey: .route)
        visibility = try c.decode(Visibility.self, forKey: .visibility)
        lockAt = try c.decodeIfPresent(Date.self, forKey: .lockAt)
        maxGroupSize = try c.decodeIfPresent(Int.self, forKey: .maxGroupSize)
        inviteUserIds = try c.decodeIfPresent([String].self, forKey: .inviteUserIds) ?? []
    }
}

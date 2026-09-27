import Foundation

// Lenient decoding for the live API.
//
// - Fields that have a sensible default decode with `decodeIfPresent`, so the backend can omit them.
// - Properties with acronyms (`photoURL`, `websiteURL`) map to "photo_url" / "website_url": with the
//   snake_case key strategies the CodingKey must be the camelCase form ("photoUrl").
// Initializers live in extensions so the memberwise initializers stay available.

extension User {
    enum CodingKeys: String, CodingKey {
        case id, name, username, email, photoURL = "photoUrl", avatarColor, status, ageBracket, school, setupComplete, homeBase, city
        case demoDate
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        username = try c.decodeIfPresent(String.self, forKey: .username)
        email = try c.decodeIfPresent(String.self, forKey: .email) ?? ""
        photoURL = try c.decodeIfPresent(URL.self, forKey: .photoURL)
        avatarColor = try c.decodeIfPresent(AvatarColor.self, forKey: .avatarColor) ?? .ink
        status = try c.decodeIfPresent(PresenceStatus.self, forKey: .status) ?? .open
        ageBracket = try c.decodeIfPresent(AgeBracket.self, forKey: .ageBracket) ?? .adult
        school = try c.decodeIfPresent(String.self, forKey: .school)
        setupComplete = try c.decodeIfPresent(Bool.self, forKey: .setupComplete) ?? true
        homeBase = try c.decodeIfPresent(Place.self, forKey: .homeBase)
        city = try c.decodeIfPresent(String.self, forKey: .city)
        demoDate = try c.decodeIfPresent(String.self, forKey: .demoDate)
    }
}

extension PersonRef {
    enum CodingKeys: String, CodingKey {
        case id, name, initials, colorHex, photoURL = "photoUrl", username
    }

    /// Initials and color are optional on the wire (derived from the name / ink when missing).
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        initials = try c.decodeIfPresent(String.self, forKey: .initials) ?? Initials.from(name)
        colorHex = try c.decodeIfPresent(String.self, forKey: .colorHex) ?? "#18211C"
        photoURL = try c.decodeIfPresent(URL.self, forKey: .photoURL)
        username = try c.decodeIfPresent(String.self, forKey: .username)
    }
}

extension FriendRequest {
    enum CodingKeys: String, CodingKey {
        case id, person, note, outgoing
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        person = try c.decode(PersonRef.self, forKey: .person)
        note = try c.decodeIfPresent(String.self, forKey: .note) ?? ""
        outgoing = try c.decodeIfPresent(Bool.self, forKey: .outgoing) ?? false
    }
}

extension UserSearchResult {
    enum CodingKeys: String, CodingKey {
        case person, relation, requestId
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        person = try c.decode(PersonRef.self, forKey: .person)
        relation = try c.decodeIfPresent(FriendRelation.self, forKey: .relation) ?? .none
        requestId = try c.decodeIfPresent(String.self, forKey: .requestId)
    }
}

extension Preferences {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let defaults = Preferences()
        ratings = try c.decodeIfPresent([TripType: Int].self, forKey: .ratings) ?? defaults.ratings
        company = try c.decodeIfPresent(Company.self, forKey: .company) ?? defaults.company
        pace = try c.decodeIfPresent(Pace.self, forKey: .pace) ?? defaults.pace
        spend = try c.decodeIfPresent(SpendTier.self, forKey: .spend) ?? defaults.spend
        flexibility = try c.decodeIfPresent(Flexibility.self, forKey: .flexibility) ?? defaults.flexibility
        splitStyle = try c.decodeIfPresent(SplitStyle.self, forKey: .splitStyle) ?? defaults.splitStyle
        preferFree = try c.decodeIfPresent(Bool.self, forKey: .preferFree) ?? defaults.preferFree
        answers = try c.decodeIfPresent([String: String].self, forKey: .answers) ?? defaults.answers
        instantCheckout = try c.decodeIfPresent(Bool.self, forKey: .instantCheckout) ?? defaults.instantCheckout
        instantCheckoutLimitCents = try c.decodeIfPresent(Int.self, forKey: .instantCheckoutLimitCents) ?? defaults.instantCheckoutLimitCents
    }

    enum CodingKeys: String, CodingKey {
        case ratings, company, pace, spend, flexibility, splitStyle, preferFree, answers, instantCheckout, instantCheckoutLimitCents
    }
}

extension ItineraryItem {
    enum CodingKeys: String, CodingKey {
        case id, kind, title, place, start, end, description, websiteURL = "websiteUrl", bookable, priceCents
        case people, interested, extraGoing, notes, notesScope, rating, transitMode, ticket
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decode(BlockKind.self, forKey: .kind)
        title = try c.decode(String.self, forKey: .title)
        place = try c.decodeIfPresent(Place.self, forKey: .place)
        start = try c.decode(Date.self, forKey: .start)
        end = try c.decode(Date.self, forKey: .end)
        description = try c.decodeIfPresent(String.self, forKey: .description)
        websiteURL = try c.decodeIfPresent(URL.self, forKey: .websiteURL)
        bookable = try c.decodeIfPresent(Bool.self, forKey: .bookable) ?? false
        priceCents = try c.decodeIfPresent(Int.self, forKey: .priceCents)
        people = try c.decodeIfPresent([PersonRef].self, forKey: .people) ?? []
        interested = try c.decodeIfPresent([PersonRef].self, forKey: .interested) ?? []
        extraGoing = try c.decodeIfPresent(Int.self, forKey: .extraGoing) ?? 0
        notes = try c.decodeIfPresent(String.self, forKey: .notes)
        notesScope = try c.decodeIfPresent(NotesScope.self, forKey: .notesScope)
        rating = try c.decodeIfPresent(Rating.self, forKey: .rating)
        transitMode = try c.decodeIfPresent(TravelMode.self, forKey: .transitMode)
        ticket = try c.decodeIfPresent(Ticket.self, forKey: .ticket)
    }
}

extension Itinerary {
    enum CodingKeys: String, CodingKey {
        case id, title, date, start, backBy, startPlace, endPlace, visibility, lockAt, maxGroupSize, items, goingCount, isHost
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        start = try c.decode(Date.self, forKey: .start)
        date = try c.decodeIfPresent(Date.self, forKey: .date) ?? start
        backBy = try c.decode(Date.self, forKey: .backBy)
        startPlace = try c.decode(Place.self, forKey: .startPlace)
        endPlace = try c.decode(Place.self, forKey: .endPlace)
        visibility = try c.decodeIfPresent(Visibility.self, forKey: .visibility) ?? .justMe
        lockAt = try c.decodeIfPresent(Date.self, forKey: .lockAt)
        maxGroupSize = try c.decodeIfPresent(Int.self, forKey: .maxGroupSize)
        items = try c.decodeIfPresent([ItineraryItem].self, forKey: .items) ?? []
        goingCount = try c.decodeIfPresent(Int.self, forKey: .goingCount) ?? 1
        isHost = try c.decodeIfPresent(Bool.self, forKey: .isHost) ?? true
    }
}

extension CalendarItem {
    enum CodingKeys: String, CodingKey {
        case id, kind, title, start, end, people, interested, itineraryId
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decode(BlockKind.self, forKey: .kind)
        title = try c.decode(String.self, forKey: .title)
        start = try c.decode(Date.self, forKey: .start)
        end = try c.decode(Date.self, forKey: .end)
        people = try c.decodeIfPresent([PersonRef].self, forKey: .people) ?? []
        interested = try c.decodeIfPresent([PersonRef].self, forKey: .interested) ?? []
        itineraryId = try c.decodeIfPresent(String.self, forKey: .itineraryId)
    }
}

extension ForumPost {
    enum CodingKeys: String, CodingKey {
        case id, type, author, isFriend, friendsOnly, title, text, meta, when, route, startsInMinutes, day, distanceMi
        case priceTier, tags, spotsLeft, capacity, lockLabel, going, goingCount, interestedCount, postedMinutesAgo
        case joinStatus, planTogetherSent, threadId
    }

    /// Older servers send `join_requested: true` instead of `join_status`.
    private enum LegacyKeys: String, CodingKey { case joinRequested }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        type = try c.decode(ForumPostType.self, forKey: .type)
        author = try c.decode(PersonRef.self, forKey: .author)
        isFriend = try c.decodeIfPresent(Bool.self, forKey: .isFriend) ?? false
        friendsOnly = try c.decodeIfPresent(Bool.self, forKey: .friendsOnly) ?? false
        title = try c.decodeIfPresent(String.self, forKey: .title)
        text = try c.decodeIfPresent(String.self, forKey: .text)
        meta = try c.decodeIfPresent(String.self, forKey: .meta) ?? ""
        when = try c.decodeIfPresent(String.self, forKey: .when)
        route = try c.decodeIfPresent(String.self, forKey: .route)
        startsInMinutes = try c.decodeIfPresent(Int.self, forKey: .startsInMinutes) ?? 0
        day = try c.decodeIfPresent(String.self, forKey: .day) ?? "today"
        distanceMi = try c.decodeIfPresent(Double.self, forKey: .distanceMi) ?? 0
        priceTier = try c.decodeIfPresent(Int.self, forKey: .priceTier) ?? 0
        tags = try c.decodeIfPresent([String].self, forKey: .tags) ?? []
        spotsLeft = try c.decodeIfPresent(Int.self, forKey: .spotsLeft)
        capacity = try c.decodeIfPresent(Int.self, forKey: .capacity)
        lockLabel = try c.decodeIfPresent(String.self, forKey: .lockLabel)
        going = try c.decodeIfPresent([PersonRef].self, forKey: .going) ?? []
        goingCount = try c.decodeIfPresent(Int.self, forKey: .goingCount) ?? going.count
        interestedCount = try c.decodeIfPresent(Int.self, forKey: .interestedCount) ?? 0
        postedMinutesAgo = try c.decodeIfPresent(Int.self, forKey: .postedMinutesAgo) ?? 0
        if let status = try c.decodeIfPresent(JoinStatus.self, forKey: .joinStatus) {
            joinStatus = status
        } else {
            let legacy = try decoder.container(keyedBy: LegacyKeys.self)
            joinStatus = (try legacy.decodeIfPresent(Bool.self, forKey: .joinRequested) ?? false) ? .requested : .none
        }
        planTogetherSent = try c.decodeIfPresent(Bool.self, forKey: .planTogetherSent) ?? false
        threadId = try c.decodeIfPresent(String.self, forKey: .threadId)
    }
}

// The planner's shapes: the app's keys first, then the backend's older spellings (`name`,
// `duration_min`, a bare `lat`/`lng`, `title`/`summary` on an option, `recalculated_legs`) so a
// server that's mid-migration still decodes. The planner's extra fields are optional.

extension PlanStop {
    enum CodingKeys: String, CodingKey {
        case id, title, subtitle, place, durationMinutes, arriveTime, departTime, kind, flexible, activityId
    }

    private enum LegacyKeys: String, CodingKey { case name, durationMin, lat, lng }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let legacy = try decoder.container(keyedBy: LegacyKeys.self)
        id = try c.decode(String.self, forKey: .id)
        let name = try legacy.decodeIfPresent(String.self, forKey: .name)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? name ?? ""
        subtitle = try c.decodeIfPresent(String.self, forKey: .subtitle) ?? ""
        if let known = try c.decodeIfPresent(Place.self, forKey: .place) {
            place = known
        } else {
            let lat = try legacy.decodeIfPresent(Double.self, forKey: .lat)
            let lng = try legacy.decodeIfPresent(Double.self, forKey: .lng)
            let coordinate = lat.flatMap { lat in lng.map { Coordinate(lat: lat, lng: $0) } }
            place = Place(name: name ?? title, coordinate: coordinate)
        }
        durationMinutes = try c.decodeIfPresent(Int.self, forKey: .durationMinutes)
            ?? legacy.decodeIfPresent(Int.self, forKey: .durationMin) ?? 0
        arriveTime = try c.decodeIfPresent(Date.self, forKey: .arriveTime)
        departTime = try c.decodeIfPresent(Date.self, forKey: .departTime)
        // A kind the app doesn't know is no kind, not a failed stop.
        kind = try c.decodeIfPresent(String.self, forKey: .kind).flatMap(PlanStopKind.init(rawValue:))
        flexible = try c.decodeIfPresent(Bool.self, forKey: .flexible)
        activityId = try c.decodeIfPresent(String.self, forKey: .activityId)
    }
}

extension PlanOption {
    enum CodingKeys: String, CodingKey {
        case id, name, tag, meta, stops, lateFlag, totalCostCents
    }

    private enum LegacyKeys: String, CodingKey { case title, summary }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let legacy = try decoder.container(keyedBy: LegacyKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? legacy.decodeIfPresent(String.self, forKey: .title) ?? ""
        tag = try c.decodeIfPresent(String.self, forKey: .tag) ?? ""
        meta = try c.decodeIfPresent(String.self, forKey: .meta) ?? legacy.decodeIfPresent(String.self, forKey: .summary) ?? ""
        stops = try c.decodeIfPresent([PlanStop].self, forKey: .stops) ?? []
        lateFlag = try c.decodeIfPresent(Bool.self, forKey: .lateFlag) ?? false
        totalCostCents = try c.decodeIfPresent(Int.self, forKey: .totalCostCents)
    }
}

extension PlanBatch {
    enum CodingKeys: String, CodingKey { case options, cursor, done, reason }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        options = try c.decodeIfPresent([PlanOption].self, forKey: .options) ?? []
        cursor = try c.decodeIfPresent(String.self, forKey: .cursor)
        // No cursor means nothing more to load, unless the server says otherwise.
        done = try c.decodeIfPresent(Bool.self, forKey: .done) ?? (cursor == nil)
        reason = try c.decodeIfPresent(PlanEmptyReason.self, forKey: .reason)
    }
}

extension PlanRequest {
    /// The app sends this; it decodes only in the contract tests. `must_include` may be missing
    /// (it's left out when there are no picks).
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        start = try c.decode(Place.self, forKey: .start)
        end = try c.decode(Place.self, forKey: .end)
        date = try c.decode(Date.self, forKey: .date)
        startTime = try c.decode(Date.self, forKey: .startTime)
        backBy = try c.decode(Date.self, forKey: .backBy)
        range = try c.decode(TravelRange.self, forKey: .range)
        ride = try c.decode(RideChoice.self, forKey: .ride)
        openSeats = try c.decodeIfPresent(Int.self, forKey: .openSeats)
        moodText = try c.decodeIfPresent(String.self, forKey: .moodText) ?? ""
        tags = try c.decodeIfPresent([String].self, forKey: .tags) ?? []
        budget = try c.decode(Int.self, forKey: .budget)
        who = try c.decode(Visibility.self, forKey: .who)
        pace = try c.decode(Pace.self, forKey: .pace)
        modes = try c.decodeIfPresent(Set<TravelMode>.self, forKey: .modes) ?? []
        mustInclude = try c.decodeIfPresent([String].self, forKey: .mustInclude) ?? []
    }
}

extension ActivityHit {
    enum CodingKeys: String, CodingKey {
        case id, title, kind, category, subtitle, place, start, end, priceCents, distanceMi
    }

    /// Only `id` and `title` are required. A missing or unknown `kind` reads as an event when the
    /// hit has a start, else as a place.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        category = try c.decodeIfPresent(String.self, forKey: .category)
        subtitle = try c.decodeIfPresent(String.self, forKey: .subtitle) ?? ""
        place = try c.decodeIfPresent(Place.self, forKey: .place)
        start = try c.decodeIfPresent(Date.self, forKey: .start)
        end = try c.decodeIfPresent(Date.self, forKey: .end)
        priceCents = try c.decodeIfPresent(Int.self, forKey: .priceCents)
        distanceMi = try c.decodeIfPresent(Double.self, forKey: .distanceMi)
        kind = try c.decodeIfPresent(String.self, forKey: .kind).flatMap(PlanStopKind.init(rawValue:)) ?? (start == nil ? .place : .event)
    }
}

extension PlanAlternative {
    enum CodingKeys: String, CodingKey { case stop, reason }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        stop = try c.decode(PlanStop.self, forKey: .stop)
        reason = try c.decodeIfPresent(String.self, forKey: .reason) ?? ""
    }
}

extension RouteResult {
    /// Stop times travel as `{ "start": …, "end": … }` rather than Foundation's `{ start, duration }`;
    /// the backend's older `{ "arrive_time": …, "depart_time": … }` reads the same.
    private struct Window: Codable {
        var start: Date
        var end: Date

        private enum CodingKeys: String, CodingKey { case start, end, arriveTime, departTime }

        init(start: Date, end: Date) {
            self.start = start
            self.end = end
        }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            if let start = try c.decodeIfPresent(Date.self, forKey: .start) {
                self.start = start
            } else {
                self.start = try c.decode(Date.self, forKey: .arriveTime)
            }
            if let end = try c.decodeIfPresent(Date.self, forKey: .end) {
                self.end = end
            } else {
                self.end = try c.decodeIfPresent(Date.self, forKey: .departTime) ?? start
            }
        }

        func encode(to encoder: Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(start, forKey: .start)
            try c.encode(end, forKey: .end)
        }
    }

    enum CodingKeys: String, CodingKey {
        case legs, stopTimes, arrival, minutesLate, brokenAt
    }

    private enum LegacyKeys: String, CodingKey { case recalculatedLegs }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let legacy = try decoder.container(keyedBy: LegacyKeys.self)
        legs = try c.decodeIfPresent([Leg].self, forKey: .legs) ?? legacy.decodeIfPresent([Leg].self, forKey: .recalculatedLegs) ?? []
        stopTimes = try c.decodeIfPresent([Window].self, forKey: .stopTimes)?.map { DateInterval(start: $0.start, end: max($0.start, $0.end)) } ?? []
        arrival = try c.decode(Date.self, forKey: .arrival)
        minutesLate = try c.decodeIfPresent(Int.self, forKey: .minutesLate) ?? 0
        brokenAt = try c.decodeIfPresent(Int.self, forKey: .brokenAt) ?? -1
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(legs, forKey: .legs)
        try c.encode(stopTimes.map { Window(start: $0.start, end: $0.end) }, forKey: .stopTimes)
        try c.encode(arrival, forKey: .arrival)
        try c.encode(minutesLate, forKey: .minutesLate)
        try c.encode(brokenAt, forKey: .brokenAt)
    }
}

extension Leg {
    enum CodingKeys: String, CodingKey { case mode, minutes }

    private enum LegacyKeys: String, CodingKey { case durationMin }

    /// `minutes`, or the backend's older `duration_min`.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        mode = try c.decodeIfPresent(TravelMode.self, forKey: .mode) ?? .walk
        minutes = try c.decodeIfPresent(Int.self, forKey: .minutes)
            ?? decoder.container(keyedBy: LegacyKeys.self).decodeIfPresent(Int.self, forKey: .durationMin) ?? 0
    }
}

extension ChatThread {
    enum CodingKeys: String, CodingKey {
        case id, isGroup, title, subtitle, members, faces, lastMessage, lastTime, chips, unread, albumTitle, albumSubtitle
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        isGroup = try c.decodeIfPresent(Bool.self, forKey: .isGroup) ?? false
        title = try c.decode(String.self, forKey: .title)
        subtitle = try c.decodeIfPresent(String.self, forKey: .subtitle) ?? ""
        members = try c.decodeIfPresent([PersonRef].self, forKey: .members) ?? []
        faces = try c.decodeIfPresent([PersonRef].self, forKey: .faces) ?? Array(members.prefix(2))
        lastMessage = try c.decodeIfPresent(String.self, forKey: .lastMessage) ?? ""
        lastTime = try c.decodeIfPresent(String.self, forKey: .lastTime) ?? ""
        chips = try c.decodeIfPresent([String].self, forKey: .chips) ?? []
        unread = try c.decodeIfPresent(Int.self, forKey: .unread) ?? 0
        albumTitle = try c.decodeIfPresent(String.self, forKey: .albumTitle)
        albumSubtitle = try c.decodeIfPresent(String.self, forKey: .albumSubtitle)
    }
}

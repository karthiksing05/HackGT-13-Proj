import CoreGraphics
import Foundation

/// Seed data for the offline demo, copied from the prototype's `renderVals()` (Main.dc.html).
/// Atlanta, Friday Sep 25 2026, user Jordan Lee (@jordanlee, JL). Where GUI_PLAN.md and the
/// prototype disagree on data (e.g. Chris's and Priya's colors), GUI_PLAN.md wins.
enum MockPeople {
    static let me = PersonRef(id: "u-jl", name: "Jordan Lee", initials: "JL", colorHex: "#18211C")
    static let maya = PersonRef(id: "u-mr", name: "Maya R.", initials: "MR", colorHex: "#1D4ED8")
    static let dev = PersonRef(id: "u-dp", name: "Dev P.", initials: "DP", colorHex: "#0F766E")
    static let ava = PersonRef(id: "u-ak", name: "Ava K.", initials: "AK", colorHex: "#B45309")
    static let sam = PersonRef(id: "u-st", name: "Sam T.", initials: "ST", colorHex: "#BE185D")
    static let chris = PersonRef(id: "u-cn", name: "Chris N.", initials: "CN", colorHex: "#4338CA")
    static let priya = PersonRef(id: "u-pk", name: "Priya K.", initials: "PK", colorHex: "#9D174D")
    static let gtOutdoors = PersonRef(id: "u-go", name: "GT Outdoors Club", initials: "GO", colorHex: "#4338CA")

    static let all = [me, maya, dev, ava, sam, chris, priya, gtOutdoors]

    static func byId(_ id: String) -> PersonRef? { all.first { $0.id == id } }
}

/// Places used by the Create flow's suggestions, pins and the mock route engine.
/// `mapPoint` is the place's position in the prototype's 350×220 map, which the mock route
/// engine uses so leg times match the prototype exactly ("Transit · 14 min", "Arrive 5:57 PM").
struct MockPlace: Hashable {
    /// Suggestion pill text ("Current location").
    var pillName: String
    /// Label once picked ("Tech Square (current location)").
    var label: String
    var coordinate: Coordinate
    var mapPoint: CGPoint

    var place: Place { Place(name: label, coordinate: coordinate) }
}

enum MockPlaces {
    static let techSquare = MockPlace(pillName: "Current location", label: "Tech Square (current location)", coordinate: Coordinate(lat: 33.7766, lng: -84.3890), mapPoint: CGPoint(x: 170, y: 110))
    static let home = MockPlace(pillName: "Home · North Ave Apts", label: "Home · North Ave Apts", coordinate: Coordinate(lat: 33.7710, lng: -84.3918), mapPoint: CGPoint(x: 100, y: 58))
    static let ponce = MockPlace(pillName: "Ponce City Market", label: "Ponce City Market", coordinate: Coordinate(lat: 33.7726, lng: -84.3655), mapPoint: CGPoint(x: 262, y: 64))
    static let krogMarket = MockPlace(pillName: "Krog Street Market", label: "Krog Street Market", coordinate: Coordinate(lat: 33.7571, lng: -84.3640), mapPoint: CGPoint(x: 292, y: 176))
    static let piedmont = MockPlace(pillName: "Piedmont Park", label: "Piedmont Park", coordinate: Coordinate(lat: 33.7851, lng: -84.3738), mapPoint: CGPoint(x: 250, y: 30))
    static let centennial = MockPlace(pillName: "Centennial Olympic Park", label: "Centennial Olympic Park", coordinate: Coordinate(lat: 33.7603, lng: -84.3932), mapPoint: CGPoint(x: 60, y: 186))

    /// Suggestion catalog in prototype order (the first 4 show with an empty search).
    static let suggestions = [techSquare, home, ponce, krogMarket, piedmont, centennial]

    /// Stop places from the plan options, with their prototype map positions.
    static let stops: [String: (coordinate: Coordinate, point: CGPoint)] = [
        "Skyline Park rooftop": (Coordinate(lat: 33.7727, lng: -84.3653), CGPoint(x: 262, y: 64)),
        "Atlanta Botanical Garden": (Coordinate(lat: 33.7900, lng: -84.3730), CGPoint(x: 250, y: 24)),
        "Piedmont Park loop": (Coordinate(lat: 33.7870, lng: -84.3740), CGPoint(x: 240, y: 40)),
        "Krog Street Tunnel murals": (Coordinate(lat: 33.7535, lng: -84.3630), CGPoint(x: 295, y: 170)),
        "Historic Fourth Ward Park": (Coordinate(lat: 33.7667, lng: -84.3639), CGPoint(x: 270, y: 110)),
        "Board game café": (Coordinate(lat: 33.7770, lng: -84.3880), CGPoint(x: 172, y: 104)),
        "Krog Street Market": (Coordinate(lat: 33.7571, lng: -84.3640), CGPoint(x: 292, y: 176)),
        "Taco stand on the BeltLine": (Coordinate(lat: 33.7650, lng: -84.3620), CGPoint(x: 280, y: 130)),
        "Ponce City Market food hall": (Coordinate(lat: 33.7725, lng: -84.3657), CGPoint(x: 262, y: 66)),
        "Bookstore browse": (Coordinate(lat: 33.7790, lng: -84.3850), CGPoint(x: 200, y: 90)),
        "Centennial Olympic Park": (Coordinate(lat: 33.7603, lng: -84.3932), CGPoint(x: 60, y: 186)),
        "SkyView Ferris wheel": (Coordinate(lat: 33.7590, lng: -84.3925), CGPoint(x: 70, y: 178)),
        "Open group dinner (Forum)": (Coordinate(lat: 33.7575, lng: -84.3645), CGPoint(x: 285, y: 172)),
    ]
}

enum MockData {
    static let clock = AppClock.demo
    static let walkNote = "Route options below. Times update live if you run late."
    static let photoColors = ["#DDD3F3", "#F3D9C4", "#C9DDF2", "#CFE6D6", "#F0CFDD", "#E6E0C8", "#D2D2DA", "#E4D2F4", "#C8E3E0"]

    private static func at(_ month: Int, _ day: Int, _ hour: Int, _ minute: Int = 0) -> Date {
        clock.date(2026, month, day, hour, minute)
    }

    static func user(email: String = "jordan@gatech.edu", name: String = "Jordan Lee") -> User {
        User(id: MockPeople.me.id, name: name, username: "jordanlee", email: email, photoURL: nil,
             avatarColor: .ink, status: .open, ageBracket: .adult, school: "Georgia Tech")
    }

    static let preferences = Preferences(
        ratings: [.outdoors: 5, .food: 4, .museums: 3, .nightlife: 2],
        company: .smallGroup, pace: .balanced, spend: .under15, flexibility: .bitOverOK,
        splitStyle: .equally, preferFree: true, answers: [:]
    )

    static let taste = TasteProfile(bars: [
        TasteBar(label: "Outdoors", value: 0.82),
        TasteBar(label: "Food", value: 0.70),
        TasteBar(label: "Art", value: 0.55),
        TasteBar(label: "Social", value: 0.64),
        TasteBar(label: "Nightlife", value: 0.28),
    ])

    // MARK: Itineraries

    static func itineraries() -> [Itinerary] {
        let p = MockPeople.self
        let friday = Itinerary(
            id: "itin-fri", title: "Free Friday afternoon", date: at(9, 25, 0), start: at(9, 25, 13), backBy: at(9, 25, 20),
            startPlace: MockPlaces.techSquare.place, endPlace: MockPlaces.home.place, visibility: .open,
            lockAt: at(9, 25, 17), maxGroupSize: 6,
            items: [
                ItineraryItem(id: "a1", kind: .busy, title: "CS 3510 lecture", place: Place(name: "Klaus Advanced Computing Building"),
                              start: at(9, 25, 13, 0), end: at(9, 25, 13, 50), description: "From your Google Calendar. SideQuests plans around it."),
                ItineraryItem(id: "a2", kind: .transit, title: "Transit to Ponce City Market", place: Place(name: "Tech Square → Ponce City Market"),
                              start: at(9, 25, 14, 0), end: at(9, 25, 14, 25), description: walkNote),
                ItineraryItem(id: "a3", kind: .sidequest, title: "Skyline Park rooftop", place: Place(name: "Ponce City Market, rooftop", coordinate: MockPlaces.stops["Skyline Park rooftop"]?.coordinate),
                              start: at(9, 25, 14, 30), end: at(9, 25, 16, 0), description: "Mini golf, carnival games and skyline views on the roof.",
                              websiteURL: URL(string: "https://poncecitymarket.com"),
                              ticketURL: URL(string: "https://events.sidequestz.tech/skyline-park-rooftop/tickets"), bookable: true, priceCents: nil),
                ItineraryItem(id: "a4", kind: .transit, title: "Walk the Eastside Trail", place: Place(name: "BeltLine Eastside Trail"),
                              start: at(9, 25, 16, 0), end: at(9, 25, 16, 20), description: walkNote),
                ItineraryItem(id: "a5", kind: .sidequest, title: "Krog Street Tunnel murals", place: Place(name: "Krog Street Tunnel, Cabbagetown", coordinate: MockPlaces.stops["Krog Street Tunnel murals"]?.coordinate),
                              start: at(9, 25, 16, 20), end: at(9, 25, 17, 30), description: "Self-guided street art walk. Free.",
                              websiteURL: URL(string: "https://atlantabeltline.org")),
                ItineraryItem(id: "a6", kind: .group, title: "Group dinner, Krog Street Market", place: Place(name: "Krog Street Market", coordinate: MockPlaces.krogMarket.coordinate),
                              start: at(9, 25, 18, 0), end: at(9, 25, 19, 30), description: "3 people joined your open plan. Joining locked at 5 PM.",
                              websiteURL: URL(string: "https://krogstreetmarket.com"), people: [p.me, p.maya, p.dev], interested: [p.ava]),
            ],
            goingCount: 3
        )
        let saturday = Itinerary(
            id: "itin-sat", title: "Saturday reset", date: at(9, 26, 0), start: at(9, 26, 13), backBy: at(9, 26, 19, 30),
            startPlace: MockPlaces.home.place, endPlace: MockPlaces.home.place, visibility: .justMe,
            items: [
                ItineraryItem(id: "b1", kind: .sidequest, title: "Atlanta Botanical Garden", place: Place(name: "Piedmont Park, Midtown", coordinate: MockPlaces.stops["Atlanta Botanical Garden"]?.coordinate),
                              start: at(9, 26, 13, 0), end: at(9, 26, 14, 45), description: "Gardens and the canopy walk.",
                              websiteURL: URL(string: "https://atlantabg.org"),
                              ticketURL: URL(string: "https://events.sidequestz.tech/atlanta-botanical-garden/tickets"), bookable: true, priceCents: nil),
                ItineraryItem(id: "b2", kind: .transit, title: "Walk to the Active Oval", place: Place(name: "Through Piedmont Park"),
                              start: at(9, 26, 14, 45), end: at(9, 26, 15, 0), description: walkNote),
                ItineraryItem(id: "b3", kind: .group, title: "Frisbee meetup", place: Place(name: "Piedmont Park, Active Oval", coordinate: MockPlaces.piedmont.coordinate),
                              start: at(9, 26, 15, 0), end: at(9, 26, 16, 30), description: "Open plan from the Forum. 8 going.",
                              people: [p.me, p.ava, p.chris], interested: [p.sam, p.priya], extraGoing: 5),
                ItineraryItem(id: "b4", kind: .busy, title: "Call with family", place: Place(name: "Anywhere"),
                              start: at(9, 26, 17, 0), end: at(9, 26, 17, 30), description: "From your Google Calendar."),
                ItineraryItem(id: "b5", kind: .sidequest, title: "Sunset at Jackson Street Bridge", place: Place(name: "Jackson Street Bridge, Old Fourth Ward"),
                              start: at(9, 26, 19, 0), end: at(9, 26, 19, 30), description: "Skyline view spot. Free."),
            ],
            goingCount: 1
        )
        return [friday, saturday]
    }

    // MARK: Calendar days (Sep 25 → Oct 4)

    /// Calendar-only entries that aren't itinerary items get their details from here.
    struct CalendarExtra {
        var place: String
        var note: String
    }

    static let calendarExtras: [String: CalendarExtra] = [
        "c3": CalendarExtra(place: "Stone Mountain Park", note: "GT Outdoors Club plan from the Forum. Carpool from Tech Square at 5:30 AM."),
        "c4": CalendarExtra(place: "Little Five Points", note: "Sam's plan from the Forum. 4 stops, walking."),
        "c5": CalendarExtra(place: "[venue]", note: "Maya's plan from the Forum."),
    ]

    static func calendarDays() -> [CalendarDay] {
        let p = MockPeople.self
        func item(_ id: String?, _ key: String, _ index: Int, _ title: String, _ kind: BlockKind, _ s: (Int, Int), _ e: (Int, Int), people: [PersonRef] = [], interested: [PersonRef] = []) -> CalendarItem {
            let parts = key.split(separator: "-").compactMap { Int($0) }
            let start = clock.date(parts[0], parts[1], parts[2], s.0, s.1)
            let end = clock.date(parts[0], parts[1], parts[2], e.0, e.1)
            return CalendarItem(id: id ?? "cal-\(key)-\(index)", kind: kind, title: title, start: start, end: end, people: people, interested: interested)
        }
        func day(_ key: String, _ items: [CalendarItem]) -> CalendarDay {
            let parts = key.split(separator: "-").compactMap { Int($0) }
            return CalendarDay(id: key, date: clock.date(parts[0], parts[1], parts[2]), items: items)
        }
        return [
            day("2026-09-25", [
                item(nil, "2026-09-25", 0, "MATH 3012", .busy, (9, 30), (10, 45)),
                item("a1", "2026-09-25", 1, "CS 3510 lecture", .busy, (13, 0), (13, 50)),
                item("a3", "2026-09-25", 2, "Skyline Park rooftop", .sidequest, (14, 30), (16, 0)),
                item("a5", "2026-09-25", 3, "Krog Street Tunnel murals", .sidequest, (16, 20), (17, 30)),
                item("a6", "2026-09-25", 4, "Group dinner, Krog St", .group, (18, 0), (19, 30), people: [p.me, p.maya, p.dev], interested: [p.ava]),
            ]),
            day("2026-09-26", [
                item("b1", "2026-09-26", 0, "Atlanta Botanical Garden", .sidequest, (13, 0), (14, 45)),
                item("b3", "2026-09-26", 1, "Frisbee meetup", .group, (15, 0), (16, 30), people: [p.me, p.ava, p.chris], interested: [p.sam, p.priya]),
                item("b4", "2026-09-26", 2, "Call with family", .busy, (17, 0), (17, 30)),
                item("b5", "2026-09-26", 3, "Sunset at Jackson St Bridge", .sidequest, (19, 0), (19, 30)),
            ]),
            day("2026-09-27", [item(nil, "2026-09-27", 0, "Club meeting", .busy, (18, 0), (19, 0))]),
            day("2026-09-28", [item(nil, "2026-09-28", 0, "MATH 3012", .busy, (9, 30), (10, 45))]),
            day("2026-09-29", [item(nil, "2026-09-29", 0, "CS 3510 lecture", .busy, (13, 0), (13, 50))]),
            day("2026-09-30", [
                item(nil, "2026-09-30", 0, "MATH 3012", .busy, (9, 30), (10, 45)),
                item(nil, "2026-09-30", 1, "CS 3510 midterm", .busy, (18, 0), (19, 30)),
            ]),
            day("2026-10-01", [item(nil, "2026-10-01", 0, "CS 3510 lecture", .busy, (13, 0), (13, 50))]),
            day("2026-10-02", [
                item(nil, "2026-10-02", 0, "MATH 3012", .busy, (9, 30), (10, 45)),
                item("c5", "2026-10-02", 1, "Trivia night", .group, (20, 0), (21, 30), people: [p.maya, p.dev], interested: [p.me, p.ava]),
            ]),
            day("2026-10-03", [item("c3", "2026-10-03", 0, "Stone Mountain sunrise hike", .group, (6, 0), (10, 0), people: [p.chris, p.ava], interested: [p.me, p.maya])]),
            day("2026-10-04", [item("c4", "2026-10-04", 0, "Thrift crawl, Little Five Points", .group, (12, 0), (16, 0), people: [p.sam, p.me, p.ava], interested: [p.maya])]),
        ]
    }

    /// Extra going count for calendar-only group entries (Stone Mountain: 7 going).
    static let calendarExtraGoing: [String: Int] = ["c3": 5]

    // MARK: Past events

    static func pastEvents() -> [PastEvent] {
        [
            PastEvent(id: "x1", title: "Board game café", place: "Tech Square", company: "with 3 others", date: at(9, 24, 19), kind: .group),
            PastEvent(id: "x2", title: "Eastside Trail walk", place: "BeltLine", company: "solo", date: at(9, 24, 16), kind: .sidequest),
            PastEvent(id: "x3", title: "Jazz night in Decatur", place: "Decatur Square", company: "with 2 others", date: at(9, 19, 20), kind: .group,
                      rating: Rating(stars: 5, tags: ["Great people", "Would go again"], note: nil)),
            PastEvent(id: "x4", title: "Dinner before the show", place: "Decatur", company: "with 2 others", date: at(9, 19, 18), kind: .sidequest),
            PastEvent(id: "x5", title: "Atlanta Food Walk", place: "Downtown", company: "solo", date: at(9, 12, 12), kind: .sidequest,
                      rating: Rating(stars: 4, tags: ["Good value"], note: nil)),
            PastEvent(id: "x6", title: "Chattahoochee paddle", place: "Chattahoochee River", company: "with 4 others", date: at(9, 6, 10), kind: .group,
                      rating: Rating(stars: 3, tags: ["Hard to get to"], note: nil)),
        ]
    }

    // MARK: Plan options (Create › Review)

    private static func stop(_ optionId: String, _ index: Int, _ title: String, _ subtitle: String, _ minutes: Int) -> PlanStop {
        let place = Place(name: title, coordinate: MockPlaces.stops[title]?.coordinate)
        return PlanStop(id: "\(optionId)-\(index)", title: title, subtitle: subtitle, place: place, durationMinutes: minutes)
    }

    /// The first batch (A, B, C).
    static let firstOptions: [PlanOption] = [
        PlanOption(id: "opt-a", name: "Rooftop + murals", tag: "Best match", meta: "~$ · 1.8 mi walking · 2 transit legs", stops: [
            stop("opt-a", 0, "Skyline Park rooftop", "Games + views · $", 80),
            stop("opt-a", 1, "Krog Street Tunnel murals", "Street art · Free", 60),
            stop("opt-a", 2, "Krog Street Market", "Food hall · $", 35),
        ]),
        PlanOption(id: "opt-b", name: "Park + food hall", tag: "Chill", meta: "~$ · 2.3 mi walking · 1 transit leg", stops: [
            stop("opt-b", 0, "Piedmont Park loop", "Walk · Free", 75),
            stop("opt-b", 1, "Board game café", "Games · $", 60),
            stop("opt-b", 2, "Ponce City Market food hall", "Food hall · $$", 50),
        ]),
        PlanOption(id: "opt-c", name: "Downtown loop", tag: "Meet people", meta: "~$$ · 1.1 mi walking · 2 transit legs", stops: [
            stop("opt-c", 0, "Centennial Olympic Park", "Park · Free", 60),
            stop("opt-c", 1, "SkyView Ferris wheel", "Views · $$", 50),
            stop("opt-c", 2, "Open group dinner (Forum)", "3 going · $", 75),
        ]),
    ]

    /// "Load more options" batches: D + E, then F + G.
    static let moreOptionBatches: [[PlanOption]] = [
        [
            PlanOption(id: "opt-d", name: "Books + park", tag: "Low key", meta: "~$ · 2.0 mi walking · 1 transit leg", stops: [
                stop("opt-d", 0, "Bookstore browse", "Books · Free", 50),
                stop("opt-d", 1, "Historic Fourth Ward Park", "Park · Free", 60),
                stop("opt-d", 2, "Taco stand on the BeltLine", "Tacos · $", 40),
            ]),
            // Option E shows the planner's "Tight timing" chip: a fixed-start stop it would just miss.
            PlanOption(id: "opt-e", name: "Games + views", tag: "Social", meta: "~$ · 1.4 mi walking · 2 transit legs", stops: [
                stop("opt-e", 0, "Board game café", "Games · $", 70),
                stop("opt-e", 1, "Skyline Park rooftop", "Games + views · $", 70),
                stop("opt-e", 2, "Krog Street Market", "Food hall · $", 40),
            ], lateFlag: true),
        ],
        [
            PlanOption(id: "opt-f", name: "Gardens loop", tag: "Outdoorsy", meta: "~$$ · 2.6 mi walking · 1 transit leg", stops: [
                stop("opt-f", 0, "Atlanta Botanical Garden", "Gardens · $$", 90),
                stop("opt-f", 1, "Piedmont Park loop", "Walk · Free", 45),
                stop("opt-f", 2, "Ponce City Market food hall", "Food hall · $$", 50),
            ]),
            PlanOption(id: "opt-g", name: "Downtown sights", tag: "New to you", meta: "~$$ · 0.9 mi walking · 2 transit legs", stops: [
                stop("opt-g", 0, "Centennial Olympic Park", "Park · Free", 55),
                stop("opt-g", 1, "SkyView Ferris wheel", "Views · $$", 40),
                stop("opt-g", 2, "Taco stand on the BeltLine", "Tacos · $", 45),
            ]),
        ],
    ]

    // MARK: Forum

    static func forumPosts() -> [ForumPost] {
        let p = MockPeople.self
        return [
            ForumPost(id: "p1", type: .plan, author: p.maya, isFriend: true, friendsOnly: false,
                      title: "Sunset + tacos on the BeltLine", text: nil, meta: "Hosting · 0.4 mi away",
                      when: "Today · 5:30–8 PM", route: "3 stops along the Eastside Trail",
                      startsInMinutes: 210, day: "today", distanceMi: 0.4, priceTier: 1, tags: ["Outdoors", "Food"],
                      spotsLeft: 2, capacity: 6, lockLabel: "Locks 5:00 PM", going: [p.dev, p.ava, p.chris],
                      goingCount: 4, interestedCount: 2, postedMinutesAgo: 25),
            ForumPost(id: "p2", type: .freeNow, author: p.dev, isFriend: true, friendsOnly: true,
                      title: nil, text: "Free 2–6 near Tech Square. Down for anything outside or a board game café.",
                      meta: "Free now · 0.3 mi away", when: nil, route: nil,
                      startsInMinutes: 0, day: "today", distanceMi: 0.3, priceTier: 0, tags: ["Outdoors", "Games"],
                      spotsLeft: nil, capacity: nil, lockLabel: nil, going: [], goingCount: 0, interestedCount: 0, postedMinutesAgo: 8),
            ForumPost(id: "p3", type: .plan, author: p.gtOutdoors, isFriend: false, friendsOnly: false,
                      title: "Stone Mountain sunrise hike", text: nil, meta: "Hosting · meets 0.2 mi away",
                      when: "Sat · 6–10 AM", route: "Carpool from Tech Square",
                      startsInMinutes: 900, day: "sat", distanceMi: 0.2, priceTier: 1, tags: ["Outdoors", "Active"],
                      spotsLeft: 5, capacity: 12, lockLabel: "Locks Fri 9 PM", going: [p.chris, p.ava, p.priya],
                      goingCount: 7, interestedCount: 5, postedMinutesAgo: 180),
            ForumPost(id: "p4", type: .freeNow, author: p.priya, isFriend: false, friendsOnly: false,
                      title: nil, text: "Layover at ATL until 7 PM. Anyone near the airport want to grab food?",
                      meta: "12 min ago · 8.1 mi away", when: nil, route: nil,
                      startsInMinutes: 0, day: "today", distanceMi: 8.1, priceTier: 1, tags: ["Food"],
                      spotsLeft: nil, capacity: nil, lockLabel: nil, going: [], goingCount: 0, interestedCount: 0, postedMinutesAgo: 12),
            ForumPost(id: "p5", type: .plan, author: p.sam, isFriend: true, friendsOnly: true,
                      title: "Thrift crawl in Little Five Points", text: nil, meta: "Hosting · 1.8 mi away",
                      when: "Sun · 12–4 PM", route: "4 stops · walking",
                      startsInMinutes: 2750, day: "sun", distanceMi: 1.8, priceTier: 1, tags: ["Shopping"],
                      spotsLeft: 1, capacity: 4, lockLabel: "Locks Sun 10 AM", going: [p.sam, p.ava, p.maya],
                      goingCount: 3, interestedCount: 1, postedMinutesAgo: 90),
        ]
    }

    // MARK: Threads, album, splits

    struct GroupSeed {
        var thread: ChatThread
        var messages: [(sender: PersonRef, senderName: String, text: String)]
        var photoCount: Int
        var photoBy: [String]
        var expenses: [(what: String, cents: Int, payer: PersonRef)]
    }

    static func groupSeeds() -> [GroupSeed] {
        let p = MockPeople.self
        return [
            GroupSeed(
                thread: ChatThread(id: "g1", isGroup: true, title: "Krog St dinner crew", subtitle: "3 people · Today 6 PM",
                                   members: [p.me, p.maya, p.dev], lastMessage: "Maya: photo dump in the album after pls", lastTime: "5:12 PM",
                                   albumTitle: "Krog St dinner · Sep 25", albumSubtitle: "9 photos · 3 people"),
                messages: [(p.maya, "Maya", "grabbing a table by the window"), (p.me, "You", "omw, 5 min out"),
                           (p.dev, "Dev", "got the first round of dumplings"), (p.maya, "Maya", "photo dump in the album after pls")],
                photoCount: 9, photoBy: ["Maya", "You", "Dev"],
                expenses: [("Dumplings", 4200, p.dev), ("MARTA fares", 750, p.me)]
            ),
            GroupSeed(
                thread: ChatThread(id: "g2", isGroup: true, title: "Stone Mountain sunrise", subtitle: "12 people · Sat 6 AM",
                                   members: [p.me, p.ava, p.chris, p.sam], lastMessage: "You: I got it", lastTime: "Thu",
                                   albumTitle: "Stone Mountain · Sep 26", albumSubtitle: "3 photos so far"),
                messages: [(p.gtOutdoors, "GT Outdoors", "Carpool list is pinned. Meet at Tech Square 5:30."),
                           (p.ava, "Ava", "can someone bring a speaker"), (p.me, "You", "I got it")],
                photoCount: 3, photoBy: ["Ava", "Chris", "You"],
                expenses: [("Parking", 2000, p.ava), ("Snacks", 1200, p.me)]
            ),
            GroupSeed(
                thread: ChatThread(id: "g3", isGroup: true, title: "Thrift crawl", subtitle: "4 people · Sun 12 PM",
                                   members: [p.me, p.sam, p.ava, p.maya], lastMessage: "Sam: $30ish", lastTime: "Tue",
                                   albumTitle: "Thrift crawl · Sep 20", albumSubtitle: "12 photos · 4 people"),
                messages: [(p.sam, "Sam", "bring cash for L5P"), (p.me, "You", "how much we thinking"), (p.sam, "Sam", "$30ish")],
                photoCount: 12, photoBy: ["Sam", "You", "Ava"],
                expenses: [("Coffee", 1600, p.me), ("Lunch", 4800, p.sam)]
            ),
        ]
    }

    /// Group avatars shown in the Groups list (first two faces).
    static let groupFaces: [String: [PersonRef]] = [
        "g1": [MockPeople.maya, MockPeople.dev],
        "g2": [MockPeople.gtOutdoors, MockPeople.ava],
        "g3": [MockPeople.sam, MockPeople.me],
    ]

    // MARK: Friends

    static func friends() -> [Friend] {
        let p = MockPeople.self
        return [
            Friend(person: p.maya, statusLine: "Free until 8 PM", activity: .free),
            Friend(person: p.dev, statusLine: "Free now · 0.3 mi away", activity: .free),
            Friend(person: p.sam, statusLine: "On a sidequest · Thrift crawl", activity: .onSidequest),
            Friend(person: p.ava, statusLine: "Busy until 5 PM", activity: .busy),
        ]
    }

    static func friendRequests() -> [FriendRequest] {
        [FriendRequest(id: "fr-chris", person: MockPeople.chris, note: "Met on Stone Mountain sunrise")]
    }

    /// Opening lines of each friend's DM.
    static func dmSeed(for person: PersonRef) -> [(sender: PersonRef, text: String)] {
        if person.id == MockPeople.maya.id {
            return [(person, "you coming tonight?"), (MockPeople.me, "yes!! just joined the plan"), (person, "yay see you at 6")]
        }
        return [(person, "hey! free this weekend?")]
    }

    // MARK: Checkout + transit

    static func transitOptions() -> [TransitOption] {
        [
            TransitOption(mode: .walk, minutes: 18, costCents: 0),
            TransitOption(mode: .marta, minutes: 12, costCents: 250),
            TransitOption(mode: .rideshare, minutes: 8, costCents: nil),
        ]
    }

    static let checkoutSteps = [
        CheckoutStep(text: "Found tickets on the official site", done: true),
        CheckoutStep(text: "Filled in your name and email", done: true),
        CheckoutStep(text: "Waiting for your approval", done: false),
    ]
}

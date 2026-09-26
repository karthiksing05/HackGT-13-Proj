import Foundation

// MARK: - Forum

enum ForumPostType: String, Codable {
    case plan
    case freeNow = "free_now"
}

struct ForumPost: Codable, Identifiable, Hashable {
    let id: String
    var type: ForumPostType
    var author: PersonRef
    var isFriend: Bool
    var friendsOnly: Bool
    /// Open plans: "Sunset + tacos on the BeltLine"
    var title: String?
    /// Free-now posts: the post text.
    var text: String?
    /// "Hosting · 0.4 mi away"
    var meta: String
    /// "Today · 5:30–8 PM"
    var when: String?
    /// "3 stops along the Eastside Trail"
    var route: String?
    /// Minutes from now until it starts (0 = now). Used for "Soonest".
    var startsInMinutes: Int
    /// "today", "sat", "sun" — used by the When filter.
    var day: String
    var distanceMi: Double
    /// 0 = Free … 3 = $$$
    var priceTier: Int
    var tags: [String]
    var spotsLeft: Int?
    var capacity: Int?
    /// "Locks 5:00 PM"
    var lockLabel: String?
    var going: [PersonRef]
    var goingCount: Int
    var interestedCount: Int
    var postedMinutesAgo: Int
    /// Where you stand on this open plan (requested, in, full, closed).
    var joinStatus: JoinStatus = .none
    var planTogetherSent: Bool = false
    /// The plan's group chat, once you're in (`join_status: joined`).
    var threadId: String? = nil

    /// Shortcut for the button: asked (or already in).
    var joinRequested: Bool {
        get { joinStatus == .requested || joinStatus == .joined }
        set { joinStatus = newValue ? .requested : .none }
    }

    /// "2 of 6 spots left"
    var spotsLabel: String? {
        guard let spotsLeft, let capacity else { return nil }
        return "\(spotsLeft) of \(capacity) spots left"
    }
    /// Fraction of spots taken (progress bar fill).
    var fillFraction: Double {
        guard let spotsLeft, let capacity, capacity > 0 else { return 0 }
        return Double(capacity - spotsLeft) / Double(capacity)
    }
    var peopleLine: String { "\(goingCount) going · \(interestedCount) interested" }
}

/// Your place in an open plan. `joined` = the host (or auto-accept) let you in: the plan is on Home
/// and its group chat exists.
enum JoinStatus: String, Codable, Hashable {
    case none, requested, joined, full, closed

    init(from decoder: Decoder) throws {
        self = JoinStatus(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .none
    }
}

/// `POST /forum/posts/{id}/join` (and the `join.update` event).
struct JoinResult: Codable, Hashable {
    var status: JoinStatus
    /// Set when you're in: the plan on Home and its group thread.
    var itineraryId: String?
    var threadId: String?
}

/// Where the Forum looks: a named place with a center. "Current location" gets its coordinate
/// from the device right before the feed loads.
struct ForumArea: Codable, Hashable, Identifiable {
    var name: String
    var coordinate: Coordinate?
    var isCurrentLocation = false

    var id: String { isCurrentLocation ? "current-location" : name }

    static let currentLocation = ForumArea(name: "Current location", coordinate: nil, isCurrentLocation: true)
    static let midtown = ForumArea(name: "Midtown Atlanta", coordinate: Coordinate(lat: 33.7838, lng: -84.3833))
    /// The demo city's areas (the Area sheet's rows in mock mode).
    private static let demoAreas: [ForumArea] = [
        .midtown,
        ForumArea(name: "Georgia Tech campus", coordinate: Coordinate(lat: 33.7756, lng: -84.3963)),
        ForumArea(name: "Downtown Atlanta", coordinate: Coordinate(lat: 33.7550, lng: -84.3900)),
        ForumArea(name: "Decatur", coordinate: Coordinate(lat: 33.7748, lng: -84.2963)),
    ]

    /// The area around a home base, when it has a center (a name alone can't be an area).
    static func homeBase(_ place: Place?) -> ForumArea? {
        guard let place, let coordinate = place.coordinate else { return nil }
        return ForumArea(name: place.name, coordinate: coordinate)
    }

    /// The Area sheet's starting rows: "Current location", the home base, then (in the demo) the
    /// demo city's areas; search finds any other place. A live account's city isn't known beyond
    /// its home base, so it gets no made-up neighborhoods.
    static func suggestions(homeBase: Place?, isMock: Bool) -> [ForumArea] {
        var rows: [ForumArea] = [.currentLocation]
        if let home = self.homeBase(homeBase) { rows.append(home) }
        if isMock {
            rows += demoAreas.filter { area in !rows.contains { $0.id == area.id } }
        }
        return rows
    }
}

enum ForumScope: String, Codable, CaseIterable, Identifiable {
    case everyone, friends
    var id: String { rawValue }
    var label: String { self == .everyone ? "Everyone nearby" : "Friends" }
}

enum ForumTypeFilter: String, Codable, CaseIterable, Identifiable {
    case all, plans
    case freeNow = "free_now"
    var id: String { rawValue }
    var label: String {
        switch self {
        case .all: "All"
        case .plans: "Open plans"
        case .freeNow: "Free now"
        }
    }
}

enum ForumSort: String, Codable, CaseIterable, Identifiable {
    case soonest, closest, spots, newest
    var id: String { rawValue }
    var label: String {
        switch self {
        case .soonest: "Soonest"
        case .closest: "Closest"
        case .spots: "Most spots left"
        case .newest: "Newest"
        }
    }
}

enum ForumWhen: String, Codable, CaseIterable, Identifiable {
    case any, now, today, weekend
    var id: String { rawValue }
    var label: String { rawValue.capitalized }
}

/// GET /forum/posts query (sort + filters). Defaults = no filters.
struct ForumQuery: Codable, Hashable {
    var area: ForumArea = .midtown
    var radiusMi: Int = 2
    var scope: ForumScope = .everyone
    var type: ForumTypeFilter = .all
    var when: ForumWhen = .any
    /// nil = any distance
    var maxDistanceMi: Double?
    /// Empty = any cost.
    var cost: Set<Int> = []
    var tags: Set<String> = []
    var openOnly: Bool = false
    var sort: ForumSort = .soonest

    /// Number of active filters (sort excluded) — "Filter · 2".
    var filterCount: Int {
        (when != .any ? 1 : 0) + (maxDistanceMi != nil ? 1 : 0) + (cost.isEmpty ? 0 : 1) + (tags.isEmpty ? 0 : 1) + (openOnly ? 1 : 0)
    }
    var hasFiltersOrSort: Bool { filterCount > 0 || sort != .soonest }

    static let radii = [1, 2, 5, 10]
    static let interestTags = ["Outdoors", "Food", "Art", "Music", "Active", "Games", "Shopping"]

    /// Where the feed looks first: around the home base, or Midtown (the demo city) without one.
    static func defaultArea(homeBase: Place?) -> ForumArea {
        ForumArea.homeBase(homeBase) ?? .midtown
    }
}

enum ForumPostVisibility: String, Codable {
    case friends, everyone
}

/// Your own "I'm free" post (Forum › "Bored right now?"), from `GET /forum/posts/mine`.
struct MyFreePost: Codable, Hashable, Identifiable {
    let id: String
    var visibility: ForumPostVisibility
    /// "Free until 6:30 PM near Tech Square"
    var text: String
    /// When it comes down on its own.
    var until: Date?
    /// Who can see it: everyone within `radiusMi` of `areaLabel` (or your friends).
    var areaLabel: String?
    var radiusMi: Int?
}

/// `POST /forum/posts` with `type: free_now`.
struct NewFreePost: Codable, Hashable {
    var visibility: ForumPostVisibility
    /// nil = the server picks (end of your free window).
    var until: Date?
    /// Where you are: the area the post is visible around.
    var area: ForumArea?
    var radiusMi: Int?
}

// MARK: - Threads (group chats + DMs)

struct ChatThread: Codable, Identifiable, Hashable {
    let id: String
    var isGroup: Bool
    var title: String
    /// "3 people · Today 6 PM" for groups; the friend's status line for DMs.
    var subtitle: String
    var members: [PersonRef]
    /// The two overlapping avatars shown in the Groups list (server picks them).
    var faces: [PersonRef] = []
    var lastMessage: String
    /// "5:12 PM", "Thu"
    var lastTime: String
    /// Groups list chips: "3 people", "You owe $9", "9 photos".
    var chips: [String] = []
    var unread: Int = 0
    var albumTitle: String?
    var albumSubtitle: String?
}

/// Messages come oldest first; `GET …/messages?before=<message id>` pages back in time.
struct Message: Codable, Identifiable, Hashable {
    let id: String
    var senderId: String
    var senderName: String
    var text: String
    var sentAt: Date
    /// Echo of the id the sender's device made up, so a retried send isn't posted twice and the
    /// socket echo can confirm the pending bubble.
    var clientId: String?
}

struct GroupPhoto: Codable, Identifiable, Hashable {
    let id: String
    var byName: String
    /// Who added it (you can delete your own).
    var uploaderId: String?
    var createdAt: Date?
    var url: URL?
    /// Mock placeholder tile color ("#DDD3F3").
    var placeholderHex: String?
    /// Photos added on this device in mock mode.
    var imageData: Data?
}

// MARK: - Splits (equal only)

struct Expense: Codable, Identifiable, Hashable {
    let id: String
    var what: String
    var amountCents: Int
    var payerId: String
    var splitAmong: [String]
    /// Server-computed shares, same order as `splitAmong`.
    var shares: [Int]
    /// Who added it (you can delete your own). Older servers leave it out; the app then treats
    /// the payer as the one who added it.
    var createdBy: String? = nil
}

/// + = they owe you, − = you owe them.
struct Balance: Codable, Hashable {
    var userId: String
    var netCents: Int
}

struct NewExpense: Codable, Hashable {
    var what: String
    var amountCents: Int
    var payerId: String
    var splitAmong: [String]
}

/// GET /groups/{id}/expenses + /balances bundled for the Splits tab.
struct GroupLedger: Codable, Hashable {
    var members: [PersonRef]
    var expenses: [Expense]
    var balances: [Balance]
    /// Sum of balances: > 0 you're owed, < 0 you owe.
    var netCents: Int { balances.reduce(0) { $0 + $1.netCents } }
}

// MARK: - Realtime

/// `WS /ws` events (see API_CONTRACT.md › Realtime for the JSON of each).
enum RealtimeEvent: Hashable {
    case messageNew(threadId: String, message: Message)
    /// A thread was created or changed (members, last message, unread count).
    case threadUpdated(ChatThread)
    /// Read on another device.
    case threadRead(threadId: String)
    /// Someone asked to join your open plan.
    case joinRequest(postId: String, from: PersonRef)
    /// Your request to join was answered.
    case joinUpdate(postId: String, result: JoinResult)
    case friendStatus(userId: String, statusLine: String)
    case friendRequest(FriendRequest)
    case forumUpdate
    case checkoutStatus(intentId: String, state: CheckoutState)
    case transitDelay(itineraryId: String, itemId: String, minutes: Int)
    /// A plan you're on changed (the host edited it, someone joined, it was re-timed).
    case itineraryUpdated(Itinerary)
    /// A plan you're on was deleted, or you were removed from it.
    case itineraryRemoved(id: String)
    case expenseAdded(groupId: String, expense: Expense)
    case photoAdded(groupId: String, photo: GroupPhoto)
}

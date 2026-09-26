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
    var joinRequested: Bool = false
    var planTogetherSent: Bool = false

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
    var area: String = "Midtown Atlanta"
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

    static let areas = ["Current location", "Midtown Atlanta", "Georgia Tech campus", "Downtown Atlanta", "Decatur"]
    /// Center of each area (sent as lat/lng with the feed query; drawn in the area map).
    static let areaCenters: [String: Coordinate] = [
        "Current location": Coordinate(lat: 33.7766, lng: -84.3890),
        "Midtown Atlanta": Coordinate(lat: 33.7838, lng: -84.3833),
        "Georgia Tech campus": Coordinate(lat: 33.7756, lng: -84.3963),
        "Downtown Atlanta": Coordinate(lat: 33.7550, lng: -84.3900),
        "Decatur": Coordinate(lat: 33.7748, lng: -84.2963),
    ]
    static let radii = [1, 2, 5, 10]
    static let interestTags = ["Outdoors", "Food", "Art", "Music", "Active", "Games", "Shopping"]
}

enum ForumPostVisibility: String, Codable {
    case friends, everyone
}

/// Your own "I'm free" post (Forum › "Bored right now?").
struct MyFreePost: Codable, Hashable, Identifiable {
    let id: String
    var visibility: ForumPostVisibility
    /// "Free until 6:30 PM near Tech Square"
    var text: String
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

struct Message: Codable, Identifiable, Hashable {
    let id: String
    var senderId: String
    var senderName: String
    var text: String
    var sentAt: Date
}

struct GroupPhoto: Codable, Identifiable, Hashable {
    let id: String
    var byName: String
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

enum RealtimeEvent: Hashable {
    case messageNew(threadId: String, message: Message)
    case joinRequest(postId: String, from: PersonRef)
    case friendStatus(userId: String, statusLine: String)
    case forumUpdate
    case checkoutStatus(intentId: String, state: CheckoutState)
    case transitDelay(itineraryId: String, itemId: String, minutes: Int)
}

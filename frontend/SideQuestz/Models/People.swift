import SwiftUI

struct Coordinate: Codable, Hashable {
    var lat: Double
    var lng: Double
}

/// A named location. `coordinate` is nil for places that are only a label ("Anywhere").
struct Place: Codable, Hashable {
    var name: String
    var coordinate: Coordinate?
}

/// Your availability (Account › Your status, the dot on your avatar). Sent as `open`, `friends_only`
/// or `busy`; the older `online` / `not_free` values still decode.
enum PresenceStatus: String, Codable, CaseIterable, Identifiable {
    case open
    case friendsOnly = "friends_only"
    case busy

    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        switch raw {
        case "online": self = .friendsOnly
        case "not_free": self = .busy
        default: self = PresenceStatus(rawValue: raw) ?? .busy
        }
    }

    var id: String { rawValue }

    var label: String {
        switch self {
        case .open: "Open to all"
        case .friendsOnly: "Friends only"
        case .busy: "Busy"
        }
    }

    /// Bright green, light green, grey.
    var color: Color {
        switch self {
        case .open: Theme.statusOpen
        case .friendsOnly: Theme.statusFriends
        case .busy: Theme.statusBusy
        }
    }

    var description: String {
        switch self {
        case .open: "Free for plans. Anyone nearby can find you in the Forum and invite you."
        case .friendsOnly: "Free for plans, but only your friends see you and can invite you."
        case .busy: "Not free right now. You're hidden from the Forum and friends' free lists."
        }
    }
}

enum AgeBracket: String, Codable {
    case under13 = "under_13", teen
    case under21 = "under_21", adult
}

/// The five initials colors a user can pick (Photo sheet › "Or use your initials").
enum AvatarColor: String, Codable, CaseIterable, Identifiable {
    case ink, sage, clay, forest, sand

    var id: String { rawValue }

    var name: String { rawValue.capitalized }

    var background: Color {
        switch self {
        case .ink: Theme.ink
        case .sage: Theme.sage
        case .clay: Theme.clay
        case .forest: Theme.sageInk
        case .sand: Color(hex: 0xD9CBB0)
        }
    }

    var foreground: Color {
        switch self {
        case .ink, .forest: .white
        case .sage, .clay, .sand: Theme.ink
        }
    }
}

struct User: Codable, Identifiable, Hashable {
    let id: String
    var name: String
    var username: String?
    var email: String
    var photoURL: URL?
    var avatarColor: AvatarColor
    var status: PresenceStatus
    var ageBracket: AgeBracket
    /// Shown after the handle in Account ("@jordanlee · Georgia Tech").
    var school: String?
    /// False until Profile setup is finished; signing in then resumes setup instead of Home.
    var setupComplete: Bool = true
    /// Where plans start by default (Create › Where) and where the Forum looks first. Set by the
    /// server (the demo account lives at Seaside Market Square); nil when it has none.
    var homeBase: Place? = nil
    /// The city the server plans in for this account ("saltlight"); shown after the handle when
    /// there's no school.
    var city: String? = nil
    /// A demo account's date ("2026-09-27"): the server runs the account on that day at the real
    /// time of day, and the app's clock follows (`AppClock.demoDate`). nil for everyone else.
    var demoDate: String? = nil

    /// Empty when there's no name yet (never someone else's initials).
    var initials: String { Initials.from(name, fallback: "") }

    /// `city` for display: a catalog key comes all lowercase ("saltlight" → "Saltlight"); a name
    /// with its own casing ("Atlanta, GA") stays as it is. nil when empty.
    var cityLabel: String? {
        guard let city = city?.trimmingCharacters(in: .whitespaces), !city.isEmpty else { return nil }
        return city == city.lowercased() ? city.capitalized : city
    }
}

/// A lightweight reference to another person (avatars, stacks, chat senders, split members).
struct PersonRef: Codable, Hashable, Identifiable {
    let id: String
    var name: String
    var initials: String
    /// "#RRGGBB" — each person's avatar color from the prototype data.
    var colorHex: String
    /// Their profile photo, when they've added one (avatars show it instead of initials).
    var photoURL: URL?
    var username: String?

    var color: Color { Color(hexString: colorHex) ?? Theme.ink }
    var firstName: String { name.split(separator: " ").first.map(String.init) ?? name }
}

enum Initials {
    static func from(_ name: String, fallback: String = "?") -> String {
        let letters = name.split(whereSeparator: \.isWhitespace).prefix(2).compactMap(\.first)
        let result = String(letters).uppercased()
        return result.isEmpty ? fallback : result
    }
}

// MARK: - Friends

enum FriendActivity: String, Codable {
    case free, onSidequest = "on_sidequest", busy, new

    var dotColor: Color {
        switch self {
        case .free: Theme.statusOpen
        case .onSidequest: Theme.sage
        case .busy, .new: Theme.mutedStar
        }
    }
}

struct Friend: Codable, Identifiable, Hashable {
    var id: String { person.id }
    var person: PersonRef
    /// "Free until 8 PM", "On a sidequest · Thrift crawl", "Busy until 5 PM".
    var statusLine: String
    var activity: FriendActivity
}

struct FriendRequest: Codable, Identifiable, Hashable {
    let id: String
    var person: PersonRef
    /// "Met on Stone Mountain sunrise"
    var note: String
    /// true = you sent it (it can be cancelled); false = they asked you (accept or decline).
    var outgoing: Bool = false
}

/// How a search result relates to you (Friends › search).
enum FriendRelation: String, Codable, Hashable {
    case none, friend, outgoing, incoming

    init(from decoder: Decoder) throws {
        self = FriendRelation(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .none
    }
}

/// `GET /users/search` row.
struct UserSearchResult: Codable, Hashable, Identifiable {
    var person: PersonRef
    var relation: FriendRelation = .none
    /// The pending request between you (outgoing: cancel it; incoming: accept or decline it).
    var requestId: String?

    var id: String { person.id }
}

/// Someone to add (`GET /people/suggested`): taste matches, best first, then recently active people.
struct PersonSuggestion: Codable, Hashable, Identifiable {
    var person: PersonRef
    var relation: FriendRelation = .none
    var requestId: String?
    /// Taste match as a whole-number percent, 0–100; nil for someone suggested without a match.
    var compatibility: Int? = nil

    var id: String { person.id }

    /// The row as a search result, for the shared Add / Requested buttons.
    var searchResult: UserSearchResult {
        get { UserSearchResult(person: person, relation: relation, requestId: requestId) }
        set { relation = newValue.relation; requestId = newValue.requestId }
    }
}

// MARK: - Profiles

/// How the person on a profile relates to you (`PublicProfile.relation`): the Friends words, or
/// `self` on your own profile.
enum ProfileRelation: String, Codable, Hashable {
    case you = "self"
    case none, friend, outgoing, incoming

    init(from decoder: Decoder) throws {
        self = ProfileRelation(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .none
    }

    init(_ relation: FriendRelation) {
        self = ProfileRelation(rawValue: relation.rawValue) ?? .none
    }
}

/// The friends you and someone have in common: how many, and up to three of them.
struct MutualFriends: Codable, Hashable {
    var count = 0
    var people: [PersonRef] = []
}

/// Someone as you see them (`GET /users/{id}/profile`): who they are, how you relate, why your
/// tastes match, what they're into, friends in common and their plans you can join.
struct PublicProfile: Codable, Hashable, Identifiable {
    var person: PersonRef
    var school: String? = nil
    /// The city they plan in: a catalog key ("atlanta") or a name with its own casing.
    var city: String? = nil
    var status: PresenceStatus = .open
    /// Their line in your Friends list ("Free until 8 PM"); only when you're friends.
    var statusLine: String? = nil
    var relation: ProfileRelation = .none
    /// The pending request between you (outgoing: withdraw it; incoming: accept or decline it).
    var requestId: String? = nil
    /// Taste match as a whole-number percent, 0–100; nil on your own profile or when unscored.
    var compatibility: Int? = nil
    /// Up to three reasons your tastes match ("You both love live music").
    var matchReasons: [String] = []
    /// What they like most, as labels ("Live music").
    var likes: [String] = []
    var mutualFriends = MutualFriends()
    /// Their upcoming plans you can see, as the Forum shows them (with where you stand on each).
    var openPlans: [ForumPost] = []
    var sidequestsDone = 0

    var id: String { person.id }
}

extension MutualFriends {
    enum CodingKeys: String, CodingKey {
        case count, people
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        people = try c.decodeIfPresent([PersonRef].self, forKey: .people) ?? []
        count = max(people.count, try c.decodeIfPresent(Int.self, forKey: .count) ?? 0)
    }
}

extension PublicProfile {
    enum CodingKeys: String, CodingKey {
        case person, school, city, status, statusLine, relation, requestId, compatibility, matchReasons, likes
        case mutualFriends, openPlans, sidequestsDone
    }

    /// Only `person` is required: lists default to empty, and the match is kept within 0–100.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        person = try c.decode(PersonRef.self, forKey: .person)
        school = try c.decodeIfPresent(String.self, forKey: .school)
        city = try c.decodeIfPresent(String.self, forKey: .city)
        status = try c.decodeIfPresent(PresenceStatus.self, forKey: .status) ?? .open
        statusLine = try c.decodeIfPresent(String.self, forKey: .statusLine)
        relation = try c.decodeIfPresent(ProfileRelation.self, forKey: .relation) ?? .none
        requestId = try c.decodeIfPresent(String.self, forKey: .requestId)
        compatibility = try c.decodeIfPresent(Int.self, forKey: .compatibility).map { min(100, max(0, $0)) }
        matchReasons = try c.decodeIfPresent([String].self, forKey: .matchReasons) ?? []
        likes = try c.decodeIfPresent([String].self, forKey: .likes) ?? []
        mutualFriends = try c.decodeIfPresent(MutualFriends.self, forKey: .mutualFriends) ?? MutualFriends()
        openPlans = try c.decodeIfPresent([ForumPost].self, forKey: .openPlans) ?? []
        sidequestsDone = max(0, try c.decodeIfPresent(Int.self, forKey: .sidequestsDone) ?? 0)
    }
}

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

    /// Empty when there's no name yet (never someone else's initials).
    var initials: String { Initials.from(name, fallback: "") }
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

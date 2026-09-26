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

enum PresenceStatus: String, Codable, CaseIterable, Identifiable {
    case open, online, notFree

    var id: String { rawValue }

    var label: String {
        switch self {
        case .open: "Open"
        case .online: "Online"
        case .notFree: "Not free"
        }
    }

    var color: Color {
        switch self {
        case .open: Theme.statusOpen
        case .online: Theme.statusOnline
        case .notFree: Theme.statusNotFree
        }
    }

    var description: String {
        switch self {
        case .open: "Free and down for plans. Friends see you in the Forum and can invite you."
        case .online: "Around and reachable, but not looking for plans right now."
        case .notFree: "Hidden from the Forum and from friends' free lists."
        }
    }
}

enum AgeBracket: String, Codable {
    case under13, teen, under21, adult
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

    var initials: String { Initials.from(name, fallback: "JL") }
}

/// A lightweight reference to another person (avatars, stacks, chat senders, split members).
struct PersonRef: Codable, Hashable, Identifiable {
    let id: String
    var name: String
    var initials: String
    /// "#RRGGBB" — each person's avatar color from the prototype data.
    var colorHex: String

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
    case free, onSidequest, busy, new

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
}

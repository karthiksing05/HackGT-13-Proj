import SwiftUI

// People › profile: what you can do for each relation and how a profile reads (shared with the
// Forum's People for you strip).

/// A profile's buttons, left to right.
enum PersonProfileAction: Hashable {
    case add, requested, accept, decline, friends, message

    /// What you can do for how you relate to someone (nothing on your own profile).
    static func actions(for relation: ProfileRelation) -> [PersonProfileAction] {
        switch relation {
        case .you: []
        case .none: [.add, .message]
        case .outgoing: [.requested, .message]
        case .incoming: [.accept, .decline, .message]
        case .friend: [.friends, .message]
        }
    }

    var title: String {
        switch self {
        case .add: "Add friend"
        case .requested: "Requested"
        case .accept: "Accept"
        case .decline: "Decline"
        case .friends: "Friends"
        case .message: "Message"
        }
    }
}

extension PublicProfile {
    /// "@maya · Georgia Tech": the handle, then the school or else the city; nil with neither.
    var handleLine: String? {
        let handle = person.username.map { $0.trimmingCharacters(in: .whitespaces) }.flatMap { $0.isEmpty ? nil : "@\($0)" }
        let school = school.map { $0.trimmingCharacters(in: .whitespaces) }.flatMap { $0.isEmpty ? nil : $0 }
        let parts = [handle, school ?? cityLabel].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    /// `city` as a place: a catalog key reads capitalized ("atlanta" → "Atlanta"), a name keeps its
    /// own casing; nil when empty.
    var cityLabel: String? {
        guard let city = city?.trimmingCharacters(in: .whitespaces), !city.isEmpty else { return nil }
        return city == city.lowercased() ? city.capitalized : city
    }

    /// How free they are: a friend's own line ("Free until 8 PM"), else what their status lets you
    /// know ("Open to plans"); your own reads as Account's status does ("Open to all").
    var presenceLine: String {
        if relation == .you { return status.label }
        if let line = statusLine?.trimmingCharacters(in: .whitespaces), !line.isEmpty { return line }
        switch status {
        case .open: return "Open to plans"
        case .friendsOnly: return "Plans with friends only"
        case .busy: return "Busy right now"
        }
    }

    /// "14 sidequests done", "1 sidequest done", "No sidequests yet".
    var sidequestsLine: String {
        switch sidequestsDone {
        case 0: "No sidequests yet"
        case 1: "1 sidequest done"
        default: "\(sidequestsDone) sidequests done"
        }
    }

    /// "3 FRIENDS IN COMMON", "1 FRIEND IN COMMON".
    var mutualTitle: String {
        mutualFriends.count == 1 ? "1 FRIEND IN COMMON" : "\(mutualFriends.count) FRIENDS IN COMMON"
    }

    /// Friends in common by first name: "Maya", "Maya and Dev", "Maya, Dev and Sam", "Maya, Dev,
    /// Sam and 2 more"; nil with none.
    var mutualLine: String? {
        let names = mutualFriends.people.map(\.firstName)
        let more = mutualFriends.count - names.count
        guard let last = names.last else {
            let count = mutualFriends.count
            return count > 0 ? "\(count) \(count == 1 ? "friend" : "friends") in common" : nil
        }
        if more > 0 { return "\(names.joined(separator: ", ")) and \(more) more" }
        if names.count == 1 { return last }
        return "\(names.dropLast().joined(separator: ", ")) and \(last)"
    }
}

/// "86% match" on a profile: the Forum's match pill, larger. Not tappable.
struct ProfileMatchPill: View {
    let percent: Int

    var body: some View {
        Text("\(percent)% match")
            .sqFont(15, .semibold)
            .foregroundStyle(Theme.sageInk)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 14)
            .frame(height: 30)
            .background(Theme.sageTint, in: Capsule())
            .sqNumeric()
            .accessibilityLabel("\(percent) percent taste match")
    }
}

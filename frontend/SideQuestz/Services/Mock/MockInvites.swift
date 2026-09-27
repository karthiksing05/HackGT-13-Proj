import Foundation

/// The demo server's side of Create › Bring friends (`CreateItineraryRequest.inviteUserIds`), with
/// the real server's rules and results (Backend/pkg/api/itineraries/invite.go): only your friends,
/// 12 at most, and everyone has to fit in the group. They're on the plan from the start (its stops
/// show who's going), a "just me" plan becomes friends-visible, and the plan's group chat opens with
/// "Jordan added Maya and Dev".
enum MockInvites {
    static let maxInvites = 12
    static let friendsOnly = "You can only add friends."
    static let tooMany = "You can bring up to 12 friends."
    static let noRoom = "Not everyone fits in this group. Raise the max group size or bring fewer friends."
    /// A stop lists this many of the people going; the rest are its `extraGoing` (like the server).
    private static let peopleShown = 3

    /// The friends a request brings, in the order asked, with duplicates and the host left out.
    static func invited(by request: CreateItineraryRequest, host: PersonRef, friends: [Friend]) throws -> [PersonRef] {
        var seen: Set<String> = [host.id]
        let ids = request.inviteUserIds.filter { seen.insert($0).inserted }
        guard ids.count <= maxInvites else { throw APIError.validation(tooMany) }
        let people = try ids.map { id in
            guard let friend = friends.first(where: { $0.person.id == id }) else { throw APIError.validation(friendsOnly) }
            return friend.person
        }
        if let maxGroupSize = request.maxGroupSize, people.count + 1 > maxGroupSize { throw APIError.validation(noRoom) }
        return people
    }

    /// The saved plan with them on it: they're going with you, its stops are group stops that show
    /// who's going, and a "just me" plan is friends-visible.
    static func bringing(_ people: [PersonRef], host: PersonRef, to itinerary: Itinerary) -> Itinerary {
        guard !people.isEmpty else { return itinerary }
        let going = [host] + people
        var plan = itinerary
        plan.goingCount = going.count
        if plan.visibility == .justMe { plan.visibility = .friends }
        plan.items = itinerary.items.map { item in
            guard item.kind == .sidequest || item.kind == .group else { return item }
            var stop = item
            stop.kind = .group
            stop.people = Array(going.prefix(peopleShown))
            stop.extraGoing = max(0, going.count - peopleShown)
            return stop
        }
        return plan
    }

    /// The plan's group chat as its host sees it, opened by the host's line.
    static func groupThread(for itinerary: Itinerary, host: PersonRef, people: [PersonRef], id: String, lineId: String,
                            clock: AppClock) -> (thread: ChatThread, line: Message) {
        let format = TimeFormat(clock: clock)
        let text = addedLine(host: host.firstName, friends: people.map(\.firstName))
        let line = Message(id: lineId, senderId: host.id, senderName: "You", text: text, sentAt: clock.now)
        let thread = ChatThread(id: id, isGroup: true, title: itinerary.title,
                                subtitle: "\(people.count + 1) people · \(format.relativeDay(itinerary.start)) \(format.time(itinerary.start))",
                                members: [host] + people, faces: Array(people.prefix(2)),
                                lastMessage: "You: \(text)", lastTime: format.time(clock.now),
                                albumTitle: "\(itinerary.title) · \(monthDay(itinerary.date, clock: clock))")
        return (thread, line)
    }

    /// "Jordan added Maya", "Jordan added Maya and Dev", "Jordan added Maya, Dev and Sam".
    static func addedLine(host: String, friends: [String]) -> String {
        guard let last = friends.last, friends.count > 1 else { return "\(host) added \(friends.first ?? "")" }
        return "\(host) added \(friends.dropLast().joined(separator: ", ")) and \(last)"
    }

    /// "Sep 25"
    private static func monthDay(_ date: Date, clock: AppClock) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.timeZone = clock.timeZone
        formatter.setLocalizedDateFormatFromTemplate("MMMd")
        return formatter.string(from: date)
    }
}

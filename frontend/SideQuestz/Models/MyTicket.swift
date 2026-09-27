import Foundation

/// One of your tickets (`GET /me/tickets`, Account › Your tickets): the ticket you bought, yourself
/// or through Muse, and the stop it gets you into, as that stop is now.
struct MyTicket: Codable, Identifiable, Hashable {
    var ticket: Ticket
    var itemId: String
    var itineraryId: String
    /// The plan's name; nil once you can't open the plan any more (you left it, or the host
    /// deleted it). The ticket is still yours.
    var itineraryTitle: String?
    /// The stop's title.
    var title: String
    var start: Date
    var end: Date
    /// The stop's place (an empty name when the server knows none).
    var place: Place
    var kind: BlockKind

    var id: String { ticket.id }
    /// "Admits 3"
    var admitsLabel: String { "Admits \(ticket.quantity)" }
    /// Still ahead or going on at `now` (it ends at or after it).
    func isUpcoming(at now: Date) -> Bool { end >= now }
}

extension MyTicket {
    enum CodingKeys: String, CodingKey {
        case ticket, itemId, itineraryId, itineraryTitle, title, start, end, place, kind
    }

    /// Lenient like `Decoding.swift`: a missing place is an empty one, a missing kind a sidequest.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ticket = try c.decode(Ticket.self, forKey: .ticket)
        itemId = try c.decode(String.self, forKey: .itemId)
        itineraryId = try c.decode(String.self, forKey: .itineraryId)
        itineraryTitle = try c.decodeIfPresent(String.self, forKey: .itineraryTitle)
        title = try c.decode(String.self, forKey: .title)
        start = try c.decode(Date.self, forKey: .start)
        end = try c.decode(Date.self, forKey: .end)
        place = try c.decodeIfPresent(Place.self, forKey: .place) ?? Place(name: "")
        kind = try c.decodeIfPresent(BlockKind.self, forKey: .kind) ?? .sidequest
    }

    /// The ticket on a stop you're looking at (Home › Event sheet › View ticket).
    init(ticket: Ticket, item: ItineraryItem, itineraryId: String?, itineraryTitle: String?) {
        self.ticket = ticket
        itemId = item.id
        self.itineraryId = itineraryId ?? ""
        self.itineraryTitle = itineraryTitle
        title = item.title
        start = item.start
        end = item.end
        place = item.place ?? Place(name: "")
        kind = item.kind
    }
}

/// Your tickets split the way the list shows them: upcoming (ending now or later) soonest first,
/// then past, latest first. The server sends them in this order; the app sorts again against its
/// own clock, so a ticket moves to Past as its stop ends.
struct TicketSections: Equatable {
    var upcoming: [MyTicket]
    var past: [MyTicket]

    init(_ tickets: [MyTicket], now: Date) {
        upcoming = tickets.filter { $0.isUpcoming(at: now) }
            .sorted { ($0.start, $0.itemId) < ($1.start, $1.itemId) }
        past = tickets.filter { !$0.isUpcoming(at: now) }
            .sorted { $0.start != $1.start ? $0.start > $1.start : $0.itemId < $1.itemId }
    }

    var all: [MyTicket] { upcoming + past }
    var isEmpty: Bool { upcoming.isEmpty && past.isEmpty }
}

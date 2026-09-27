import Foundation

/// Account › Your tickets in the offline demo: the tickets on the demo's plans (Saturday's Atlanta
/// Botanical Garden is booked from the start, and whatever you buy here joins it) plus three for a
/// sidequest that's over (Jazz night in Decatur, the Past row `x3`). Launch with
/// `-SQMockTickets none` to start without any (the empty state).
enum MockTickets {
    static var seeded: Bool { UserDefaults.standard.string(forKey: "SQMockTickets") != "none" }

    /// Tickets already on the demo's stops, by item id (`MockAPIClient` starts with these, so the
    /// Event sheet shows the same ticket the list does).
    static var booked: [String: Ticket] {
        guard seeded else { return [:] }
        return ["b1": Ticket(id: "tkt_5b1f0c9a2e7d4a13", quantity: 1, totalCents: 1994, confirmation: "SQZ-4M7Q2K",
                             url: URL(string: "https://events.sidequestz.tech/t/tkt_5b1f0c9a2e7d4a13"), mine: true)]
    }

    /// Friday Sep 19 in Decatur with Maya and Dev: Muse got the three of them in.
    static var past: MyTicket {
        let clock = MockData.clock
        return MyTicket(
            ticket: Ticket(id: "tkt_9e2c41d07b3a58f6", quantity: 3, totalCents: 5010, confirmation: "SQZ-8R3D1W",
                           url: URL(string: "https://events.sidequestz.tech/t/tkt_9e2c41d07b3a58f6"), mine: true),
            itemId: "x3", itineraryId: "itin-decatur", itineraryTitle: "Friday night in Decatur",
            title: "Jazz night in Decatur", start: clock.date(2026, 9, 19, 20), end: clock.date(2026, 9, 19, 22, 30),
            place: Place(name: "Decatur Square", coordinate: Coordinate(lat: 33.7748, lng: -84.2963)), kind: .group)
    }

    /// Your tickets on `itineraries` (as the API returns them, tickets folded in) plus the past
    /// one, in the server's order.
    static func list(itineraries: [Itinerary], now: Date) -> [MyTicket] {
        var tickets = itineraries.flatMap { itinerary in
            itinerary.items.compactMap { item -> MyTicket? in
                guard let ticket = item.ticket, ticket.isMine else { return nil }
                return MyTicket(ticket: ticket, item: item, itineraryId: itinerary.id, itineraryTitle: itinerary.title)
            }
        }
        if seeded { tickets.append(past) }
        return TicketSections(tickets, now: now).all
    }
}

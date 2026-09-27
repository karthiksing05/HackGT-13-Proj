import Foundation
import Testing
import UIKit
@testable import SideQuestz

/// Account › Your tickets: `GET /me/tickets` as the Go server writes it (pkg/contract/tickets.go),
/// Upcoming / Past, the QR code, the offline demo's tickets and the links into the screen.
@MainActor
struct TicketsTests {
    private let clock = AppClock.demo
    private let decoder = APICoding.decoder(timeZone: AppClock.demo.timeZone)
    private var format: TimeFormat { TimeFormat(clock: clock) }

    // MARK: Decoding

    @Test func decodesTheServersList() throws {
        let json = """
        [{"ticket":{"id":"tk_1","quantity":3,"total_cents":5010,"confirmation":"SQZ-8R3D1W",
                    "url":"https://events.sidequestz.tech/t/tkt_1","mine":true},
          "item_id":"s1","itinerary_id":"it1","itinerary_title":"Friday night out","title":"Sunset Jazz",
          "start":"2026-09-26T23:00:00Z","end":"2026-09-27T01:00:00Z",
          "place":{"name":"Pier Nine Bandstand","coordinate":{"lat":31.376524,"lng":-81.41746}},"kind":"group"},
         {"ticket":{"id":"tk_2","quantity":1,"mine":true},"item_id":"s2","itinerary_id":"it2","title":"Harbor walk",
          "start":"2026-09-20T18:00:00Z","end":"2026-09-20T19:00:00Z","place":{"name":""},"kind":"sidequest"}]
        """
        let tickets = try decoder.decode([MyTicket].self, from: Data(json.utf8))
        #expect(tickets.count == 2)
        let jazz = tickets[0]
        #expect(jazz.id == "tk_1" && jazz.itemId == "s1" && jazz.itineraryId == "it1" && jazz.itineraryTitle == "Friday night out")
        #expect(jazz.ticket.quantity == 3 && jazz.ticket.totalCents == 5010 && jazz.ticket.confirmation == "SQZ-8R3D1W" && jazz.ticket.isMine)
        #expect(jazz.ticket.url?.absoluteString == "https://events.sidequestz.tech/t/tkt_1")
        #expect(jazz.title == "Sunset Jazz" && jazz.kind == .group && jazz.admitsLabel == "Admits 3")
        #expect(jazz.place.name == "Pier Nine Bandstand" && jazz.place.coordinate?.lat == 31.376524)
        #expect(jazz.start == ISO8601DateFormatter().date(from: "2026-09-26T23:00:00Z"))
        // A plan you left has no title; a stop without a place has an empty one.
        #expect(tickets[1].itineraryTitle == nil && tickets[1].place.name.isEmpty && tickets[1].ticket.totalCents == nil)
    }

    @Test func toleratesMissingPlaceAndKind() throws {
        let json = #"{"ticket":{"id":"tk_3","quantity":2},"item_id":"s3","itinerary_id":"it3","title":"Gallery","start":"2026-09-26T18:00:00Z","end":"2026-09-26T19:00:00Z"}"#
        let ticket = try decoder.decode(MyTicket.self, from: Data(json.utf8))
        #expect(ticket.place.name.isEmpty && ticket.kind == .sidequest && ticket.itineraryTitle == nil)
        #expect(ticket.ticket.isMine, "an absent mine reads as yours")
    }

    /// The Event sheet's "View ticket" makes the same shape from the stop it shows.
    @Test func buildsFromAStop() {
        let item = ItineraryItem(id: "b1", kind: .sidequest, title: "Atlanta Botanical Garden", place: Place(name: "Piedmont Park"),
                                 start: clock.date(2026, 9, 26, 13), end: clock.date(2026, 9, 26, 14, 45))
        let ticket = MyTicket(ticket: Ticket(id: "tk", quantity: 2, mine: false), item: item, itineraryId: "itin-sat", itineraryTitle: "Saturday reset")
        #expect(ticket.id == "tk" && ticket.itemId == "b1" && ticket.itineraryId == "itin-sat" && ticket.itineraryTitle == "Saturday reset")
        #expect(ticket.title == item.title && ticket.start == item.start && ticket.end == item.end && ticket.place.name == "Piedmont Park")
        #expect(!ticket.ticket.isMine)
        #expect(MyTicket(ticket: Ticket(id: "tk"), item: item, itineraryId: nil, itineraryTitle: nil).itineraryId.isEmpty)
    }

    // MARK: Upcoming / Past

    private func ticket(_ id: String, _ day: Int, _ hour: Int, minutes: Int = 60) -> MyTicket {
        let start = clock.date(2026, 9, day, hour)
        return MyTicket(ticket: Ticket(id: "tk-\(id)"), itemId: id, itineraryId: "it", itineraryTitle: "Plan", title: id,
                        start: start, end: start.addingTimeInterval(TimeInterval(minutes * 60)), place: Place(name: "Here"), kind: .sidequest)
    }

    /// Upcoming is everything that ends at or after now (a stop that's on right now too), soonest
    /// first; Past is the rest, latest first. Ties keep a stable order by stop.
    @Test func sectionsSplitAndSortAgainstNow() {
        let now = clock.now // Fri Sep 25, 2:10 PM
        let tickets = [
            ticket("lastweek", 18, 20), ticket("tomorrow", 26, 13), ticket("morning", 25, 9), ticket("now", 25, 14),
            ticket("tonight-b", 25, 19), ticket("tonight-a", 25, 19), ticket("endsnow", 25, 13, minutes: 70),
        ]
        let sections = TicketSections(tickets, now: now)
        #expect(sections.upcoming.map(\.itemId) == ["endsnow", "now", "tonight-a", "tonight-b", "tomorrow"])
        #expect(sections.past.map(\.itemId) == ["morning", "lastweek"])
        #expect(sections.all.count == tickets.count && !sections.isEmpty)
        #expect(TicketSections([], now: now).isEmpty)

        // Later the same day, what was on moves to Past.
        let evening = clock.date(2026, 9, 25, 21)
        let later = TicketSections(tickets, now: evening)
        #expect(later.upcoming.map(\.itemId) == ["tomorrow"])
        #expect(later.past.map(\.itemId) == ["tonight-a", "tonight-b", "now", "endsnow", "morning", "lastweek"])
    }

    @Test func timeLines() {
        #expect(TicketTime.line(ticket("tomorrow", 26, 13), format: format) == "Sat, Sep 26 · 1:00–2:00 PM")
        #expect(TicketTime.line(ticket("tonight", 25, 19), format: format) == "Today · 7:00–8:00 PM")
        #expect(TicketTime.full(ticket("tomorrow", 26, 13), format: format) == "Saturday, Sep 26 · 1:00–2:00 PM")
        #expect(TicketTime.spoken(ticket("tomorrow", 26, 13), format: format) == "Saturday, September 26, 1:00–2:00 PM")
    }

    // MARK: QR code

    @Test func qrCodeIsMadeOnThePhone() throws {
        let image = try #require(TicketQRCode.image(for: "https://events.sidequestz.tech/t/tkt_5b1f0c9a2e7d4a13"))
        #expect(image.size.width == image.size.height)
        #expect(image.size.width >= 21 * 12, "at least a version-1 code at 12 px per module")
        #expect(TicketQRCode.image(for: "") == nil)

        let withPage = Ticket(id: "tk", confirmation: "SQ-4F7K2", url: URL(string: "https://api.sidequestz.tech/tickets/abc"))
        #expect(TicketQRCode.payload(for: withPage) == "https://api.sidequestz.tech/tickets/abc")
        #expect(TicketQRCode.payload(for: Ticket(id: "tk", confirmation: "SQ-4F7K2")) == "SQ-4F7K2")
        #expect(TicketQRCode.payload(for: Ticket(id: "tk")) == nil)
    }

    // MARK: The offline demo

    /// Saturday's garden (booked from the start, so its Event sheet shows the same ticket) and a past
    /// one; a purchase in the demo joins the list.
    @Test func mockTicketsFollowBookings() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let start = try await api.myTickets()
        #expect(start.map(\.itemId) == ["b1", "x3"])
        #expect(start[0].itineraryTitle == "Saturday reset" && start[0].ticket.confirmation == "SQZ-4M7Q2K" && start[0].ticket.quantity == 1)
        #expect(start[1].kind == .group && start[1].ticket.quantity == 3 && !start[1].isUpcoming(at: clock.now))
        #expect(try await api.eventDetail(id: "b1").ticket?.id == start[0].ticket.id)

        let intent = try await api.createCheckoutIntent(itemId: "a3", quantity: 2)
        _ = try await api.approveCheckout(id: intent.id)
        var tickets = start
        for _ in 0..<50 where tickets.count == start.count {
            try await Task.sleep(for: .milliseconds(20))
            tickets = try await api.myTickets()
        }
        #expect(tickets.map(\.itemId) == ["a3", "b1", "x3"], "Friday's rooftop is sooner than Saturday's garden")
        #expect(tickets[0].ticket.quantity == 2 && tickets[0].itineraryTitle == "Free Friday afternoon")
    }

    @Test func modelLoadsAndCountsUpcoming() async {
        let env = AppEnvironment.preview()
        let model = TicketsModel()
        #expect(model.upcomingCount(now: clock.now) == nil)
        await model.load(env)
        #expect(model.upcomingCount(now: clock.now) == 1)
        #expect(model.sections(now: clock.now)?.past.map(\.itemId) == ["x3"])
        #expect(model.ticket(id: "tkt_9e2c41d07b3a58f6")?.title == "Jazz night in Decatur")
    }

    /// A failed refresh keeps the list on screen.
    @Test func failedRefreshKeepsTheList() async {
        let api = MockAPIClient(latencyScale: 0)
        let env = AppEnvironment(mode: .mock, clock: .demo, api: api, auth: AuthStore(service: "tests.tickets"), socketURL: nil, forceVoiceDemo: true)
        let model = TicketsModel()
        await model.load(env)
        api.failing = ["tickets"]
        await model.load(env)
        #expect(model.tickets.value?.count == 2)

        let fresh = TicketsModel()
        await fresh.load(env)
        if case .failed(let message) = fresh.tickets {
            #expect(!message.isEmpty)
        } else {
            Issue.record("a failed first load shows the failure")
        }
    }

    // MARK: Links

    @Test func deepLinksOpenTheTickets() {
        let env = AppEnvironment.preview()
        let router = Router(phase: .auth)
        router.applyLaunch(LaunchRoute("account/tickets")!, env: env)
        #expect(router.phase == .main && router.tab == .account && router.accountSegment == .me)
        #expect(router.tickets != nil && router.tickets?.ticketId == nil)

        let direct = Router(phase: .auth)
        direct.applyLaunch(LaunchRoute("tickets/tkt_5b1f0c9a2e7d4a13")!, env: env)
        #expect(direct.tab == .account && direct.tickets?.ticketId == "tkt_5b1f0c9a2e7d4a13")

        direct.goToStop("b1", itineraryId: "itin-sat")
        #expect(direct.tab == .home && direct.homeSegment == .itineraries)
        #expect(direct.selectedItineraryId == "itin-sat" && direct.stopToOpen == "b1")
        direct.signedOut()
        #expect(direct.tickets == nil && direct.stopToOpen == nil)
    }
}

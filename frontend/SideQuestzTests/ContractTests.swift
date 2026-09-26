import Foundation
import Testing
@testable import SideQuestz

/// Every API model must survive a round trip through the live client's JSON settings
/// (snake_case keys, ISO 8601 dates). Set `SQ_DUMP_CONTRACT=<dir>` (via `TEST_RUNNER_SQ_DUMP_CONTRACT`)
/// to also write example payloads for API_CONTRACT.md.
@MainActor
struct ContractTests {
    private let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.keyEncodingStrategy = .convertToSnakeCase
        e.dateEncodingStrategy = .iso8601
        e.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return e
    }()

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    private func roundTrip<T: Codable & Equatable>(_ value: T, _ name: String) throws {
        let data = try encoder.encode(value)
        let back = try decoder.decode(T.self, from: data)
        #expect(back == value, "\(name) did not round-trip")
        if let dir = ProcessInfo.processInfo.environment["SQ_DUMP_CONTRACT"] {
            try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
            try data.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).json"))
        }
    }

    @Test func modelsRoundTripThroughSnakeCaseJSON() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let clock = AppClock.demo
        let user = MockData.user()
        try roundTrip(user, "User")
        try roundTrip(AuthResponse(user: user, tokens: AuthTokens(accessToken: "eyJ…", refreshToken: "r1", expiresAt: clock.now)), "AuthResponse")
        try roundTrip(SignupRequest(name: "Jordan Lee", email: "jordan@gatech.edu", password: "wander2026", username: "jordanlee",
                                    dateOfBirth: clock.date(2004, 5, 2)), "SignupRequest")
        try roundTrip(UserPatch(name: nil, username: nil, dateOfBirth: nil, status: .busy), "UserPatch")
        try roundTrip(MockData.preferences, "Preferences")
        try roundTrip(MockData.taste, "TasteProfile")
        try roundTrip(try await api.integrations(), "Integrations")
        try roundTrip(try await api.paymentMethods(), "PaymentMethods")
        _ = try await api.facebookConnectURL(rerequest: false)
        try roundTrip(try await api.importFacebook(), "FacebookImport")
        try roundTrip(try await api.facebookConnection(), "FacebookConnection")

        let itineraries = try await api.activeItineraries()
        try roundTrip(itineraries[0], "Itinerary")
        try roundTrip(ItineraryUpdate(title: "Rooftop evening", start: clock.date(2026, 9, 25, 15, 0), backBy: clock.date(2026, 9, 25, 19, 0),
                                      visibility: .open, stopOrder: ["a5", "a3", "a6"]), "ItineraryUpdate")
        try roundTrip(try await api.eventDetail(id: "a6"), "ItineraryItem")
        try roundTrip(try await api.transitOptions(itineraryId: "itin-fri", itemId: "a3"), "TransitOptions")
        try roundTrip(try await api.calendarDays(from: clock.now, to: clock.now), "CalendarDays")
        try roundTrip(Array(try await api.pastEvents(unratedOnly: false).prefix(2)), "PastEvents")
        try roundTrip(try await api.pastInsights(), "PastInsights")
        try roundTrip(try await api.search(query: "krog"), "SearchResults")
        try roundTrip(Rating(stars: 4, tags: ["Great people"], note: "great music, but the line was long"), "Rating")

        let request = PlanRequest(start: MockPlaces.techSquare.place, end: MockPlaces.home.place, date: clock.now,
                                  startTime: clock.date(2026, 9, 25, 14, 10), backBy: clock.date(2026, 9, 25, 18, 30),
                                  range: .transit, ride: RideChoice.none, openSeats: nil,
                                  moodText: "Something chill and outside, then cheap food after.",
                                  tags: ["Outdoors", "Food", "Meet people"], budget: 1, who: .friends, pace: .balanced, modes: [.walk, .marta])
        try roundTrip(request, "PlanRequest")
        let batch = try await api.generatePlans(request)
        try roundTrip(batch, "PlanBatch")
        let option = batch.options[0]
        let routeRequest = RouteRequest(optionId: option.id, stopOrder: option.stops.map(\.id), start: request.start, end: request.end,
                                        startTime: request.startTime, backBy: request.backBy, ride: request.ride, modes: request.modes)
        try roundTrip(routeRequest, "RouteRequest")
        let route = try await api.route(routeRequest)
        try roundTrip(route, "RouteResult")
        try roundTrip(try await api.stopAlternatives(optionId: option.id, stopId: option.stops[1].id,
                                                     stopOrder: option.stops.map(\.id)), "PlanAlternatives")
        try roundTrip(CreateItineraryRequest(plan: request, option: option, stopOrder: option.stops.map(\.id), route: route,
                                             visibility: .friends, lockAt: clock.date(2026, 9, 25, 13, 30), maxGroupSize: 6), "CreateItineraryRequest")

        try roundTrip(try await api.createCheckoutIntent(itemId: "a3", quantity: 1), "CheckoutIntent")

        let posts = try await api.forumPosts(ForumQuery())
        try roundTrip(posts.filter { $0.id == "p1" || $0.id == "p2" }, "ForumPosts")
        try roundTrip(try await api.postFreeNow(visibility: .friends), "MyFreePost")

        let threads = try await api.threads()
        try roundTrip(threads.first { $0.id == "g1" }!, "ChatThread")
        try roundTrip(Array(try await api.messages(threadId: "g1", before: nil).prefix(2)), "Messages")
        try roundTrip(Array(try await api.groupPhotos(groupId: "g1").prefix(1)), "GroupPhotos")
        try roundTrip(try await api.ledger(groupId: "g1"), "GroupLedger")
        try roundTrip(NewExpense(what: "Pizza", amountCents: 4000, payerId: MockPeople.me.id,
                                 splitAmong: [MockPeople.me.id, MockPeople.maya.id, MockPeople.dev.id]), "NewExpense")
        try roundTrip(try await api.friends(), "Friends")
        try roundTrip(try await api.friendRequests(), "FriendRequests")
        try roundTrip(try await api.searchUsers(query: "a"), "UserSearchResults")
        try roundTrip(try await api.sendFriendRequest(userId: MockPeople.sam.id), "OutgoingFriendRequest")
        try roundTrip(JoinResult(status: .joined, itineraryId: "itin-42", threadId: "g42"), "JoinResult")
        try roundTrip(NewFreePost(visibility: .everyone, until: clock.date(2026, 9, 25, 18, 30), area: ForumArea.midtown, radiusMi: 2), "NewFreePost")
        try roundTrip(ForumQuery(), "ForumQuery")
        try roundTrip(Ticket(id: "tk-1", quantity: 2, totalCents: 2400, confirmation: "SQ-4F7K2", url: URL(string: "https://tickets.example/SQ-4F7K2")), "Ticket")
        try roundTrip(Message(id: "m-9", senderId: MockPeople.me.id, senderName: "You", text: "omw", sentAt: clock.now, clientId: "c-5A1B"), "Message")
    }

    @Test func liveDecodingToleratesMissingOptionalFields() throws {
        let json = #"{"id":"u1","name":"Sam T."}"#
        let user = try decoder.decode(User.self, from: Data(json.utf8))
        #expect(user.status == .open)
        #expect(user.avatarColor == .ink)
        let item = #"{"id":"x","kind":"sidequest","title":"Board game café","start":"2026-09-25T18:00:00Z","end":"2026-09-25T19:00:00Z","website_url":"https://example.com"}"#
        let decoded = try decoder.decode(ItineraryItem.self, from: Data(item.utf8))
        #expect(decoded.bookable == false)
        #expect(decoded.people.isEmpty)
        #expect(decoded.websiteURL?.absoluteString == "https://example.com")
    }
}

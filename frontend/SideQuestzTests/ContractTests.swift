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

    /// The demo's stops carry `activity_id` (Review's stop pane loads their details by it); the
    /// `PlanBatch`, `PlanAlternatives` and `CreateItineraryRequest` examples show the required keys
    /// only (`PlanBatch.dag` shows the planner's extras).
    private static func requiredKeys(_ stop: PlanStop) -> PlanStop {
        PlanStop(id: stop.id, title: stop.title, subtitle: stop.subtitle, place: stop.place, durationMinutes: stop.durationMinutes)
    }

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
        let plainBatch = PlanBatch(options: batch.options.map { option in
            var option = option
            option.stops = option.stops.map(Self.requiredKeys)
            return option
        }, cursor: batch.cursor, done: batch.done)
        try roundTrip(plainBatch, "PlanBatch")
        let option = plainBatch.options[0]
        let routeRequest = RouteRequest(optionId: option.id, stopOrder: option.stops.map(\.id), start: request.start, end: request.end,
                                        startTime: request.startTime, backBy: request.backBy, ride: request.ride, modes: request.modes)
        try roundTrip(routeRequest, "RouteRequest")
        let route = try await api.route(routeRequest)
        try roundTrip(route, "RouteResult")
        let alternatives = try await api.stopAlternatives(optionId: option.id, stopId: option.stops[1].id, stopOrder: option.stops.map(\.id))
        try roundTrip(alternatives.map { PlanAlternative(stop: Self.requiredKeys($0.stop), reason: $0.reason) }, "PlanAlternatives")
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

    /// The DAG planner's batches and routes (planner.md §6.2) decode with their extras, fields the
    /// app doesn't model are ignored, the backend's older spellings still read, and the examples
    /// `PlanBatch.dag`, `PlanBatch.empty` and `RouteResult.dag` show the extras the app understands.
    @Test func plannerExtrasDecodeLeniently() throws {
        let clock = AppClock.demo

        // The DAG planner's shape, extras and all.
        let dag = """
        {"planner":"dag","run_id":"run_7f3k","options":[{"id":"run_7f3k-0","name":"Harbor walk + tacos","tag":"Best match",
         "meta":"~$ · 1.2 mi walking · 1 marta leg","stops":[
          {"id":"stop_act_1_0","title":"Seaside boardwalk","subtitle":"Park · Free","place":{"name":"Seaside boardwalk","coordinate":{"lat":31.37,"lng":-81.42}},
           "duration_minutes":45,"activity_id":"act_1","kind":"place","category":"park","tags":["outdoors"],
           "arrive_time":"2026-09-26T22:10:00Z","depart_time":"2026-09-26T22:55:00Z","flexible":true,"price_cents":null,"price_known":true,"order":0},
          {"id":"stop_act_2_1","title":"Harbor lights market","subtitle":"Community event · $ · 7:30 PM","place":{"name":"Harbor lights market","coordinate":{"lat":31.365,"lng":-81.43}},
           "duration_minutes":60,"activity_id":"act_2","kind":"event","arrive_time":"2026-09-26T23:30:00Z","depart_time":"2026-09-27T00:30:00Z","flexible":false,"price_cents":800}],
         "legs":[{"from_stop_id":"start","to_stop_id":"stop_act_1_0","mode":"walk","minutes":8,"distance_km":0.6},
                 {"from_stop_id":"stop_act_1_0","to_stop_id":"stop_act_2_1","mode":"marta","minutes":12,"distance_km":2.1}],
         "late_flag":true,"total_cost_cents":800,"cost_known":true,"score":0.81,"metrics":{"fit":0.9}}],
         "cursor":"dag_run_7f3k_2","done":false,"relaxed":[],"debug":{"rounds":2}}
        """
        let batch = try decoder.decode(PlanBatch.self, from: Data(dag.utf8))
        let option = try #require(batch.options.first)
        #expect(batch.cursor == "dag_run_7f3k_2" && batch.done == false && batch.reason == nil)
        #expect(option.name == "Harbor walk + tacos" && option.lateFlag && option.totalCostCents == 800)
        #expect(option.stops.map(\.kind) == [.place, .event])
        #expect(option.stops.map(\.flexible) == [true, false])
        #expect(option.stops.map(\.activityId) == ["act_1", "act_2"])
        #expect(option.stops[1].arriveTime == clock.date(2026, 9, 26, 19, 30))
        #expect(option.stops[1].departTime == clock.date(2026, 9, 26, 20, 30))
        #expect(option.stops[0].durationMinutes == 45 && option.stops[0].place.coordinate?.lat == 31.37)

        // Why a batch is empty.
        let empty = try decoder.decode(PlanBatch.self, from: Data(#"{"planner":"dag","run_id":"run_9","options":[],"done":true,"reason":"no_candidates_fit_window"}"#.utf8))
        #expect(empty.options.isEmpty && empty.done && empty.reason == .noCandidatesFitWindow)
        #expect(empty.reason?.message.hasPrefix("Nothing nearby fits this window") == true)
        let invalid = try decoder.decode(PlanBatch.self, from: Data(#"{"options":[],"reason":"invalid_request: back_by is before start_time"}"#.utf8))
        #expect(invalid.done && invalid.reason == .invalidRequest("back_by is before start_time"))
        #expect(invalid.reason?.message == "Check your start, end and times, then try again.")
        let unknown = try decoder.decode(PlanBatch.self, from: Data(#"{"options":[],"done":true,"reason":"planner_timeout"}"#.utf8))
        #expect(unknown.reason == .other("planner_timeout") && unknown.reason?.message == PlanEmptyReason.defaultMessage)
        #expect(PlanEmptyReason(rawValue: "no_feasible_itinerary").message.hasPrefix("We couldn't fit stops"))
        for reason in [PlanEmptyReason.noCandidatesFitWindow, .noFeasibleItinerary, .invalidRequest(""), .invalidRequest("bad start"), .other("x")] {
            #expect(PlanEmptyReason(rawValue: reason.rawValue) == reason)
        }

        // A re-timed route: the stop this order reaches too late.
        let route = """
        {"option_id":"run_7f3k-0","legs":[{"mode":"walk","minutes":8,"distance_km":0.6,"from_stop_id":"start","to_stop_id":"stop_act_1_0"},
         {"mode":"marta","minutes":12},{"mode":"walk","minutes":5}],
         "stop_times":[{"start":"2026-09-26T22:10:00Z","end":"2026-09-26T22:55:00Z","stop_id":"stop_act_1_0","flexible":true},
                       {"start":"2026-09-26T23:30:00Z","end":"2026-09-27T00:30:00Z","stop_id":"stop_act_2_1","flexible":false}],
         "arrival":"2026-09-27T00:55:00Z","depart":"2026-09-26T22:02:00Z","minutes_late":25,"broken_at":1,"late_flag":true,"total_duration_min":173}
        """
        let timed = try decoder.decode(RouteResult.self, from: Data(route.utf8))
        #expect(timed.brokenAt == 1 && timed.minutesLate == 25)
        #expect(timed.legs.map(\.mode) == [.walk, .marta, .walk] && timed.legs.map(\.minutes) == [8, 12, 5])
        #expect(timed.stopTimes.count == 2 && timed.stopTimes[1].start == clock.date(2026, 9, 26, 19, 30))
        #expect(timed.arrival == clock.date(2026, 9, 26, 20, 55))

        // The backend's older spellings.
        let old = """
        {"id":"o1","title":"Old shape","summary":"~$ · walk","stops":[{"id":"s1","name":"Old stop","lat":33.77,"lng":-84.39,"duration_min":40,
         "arrive_time":"2026-09-25T18:00:00Z","depart_time":"2026-09-25T18:40:00Z","kind":"place","flexible":true},{"id":"s2","name":"No kind","kind":"mystery"}]}
        """
        let legacy = try decoder.decode(PlanOption.self, from: Data(old.utf8))
        #expect(legacy.name == "Old shape" && legacy.meta == "~$ · walk" && legacy.tag == "" && !legacy.lateFlag)
        let first = try #require(legacy.stops.first)
        #expect(first.title == "Old stop" && first.subtitle == "" && first.place == Place(name: "Old stop", coordinate: Coordinate(lat: 33.77, lng: -84.39)))
        #expect(first.durationMinutes == 40 && first.kind == .place && first.flexible == true && first.arriveTime == clock.date(2026, 9, 25, 14, 0))
        #expect(legacy.stops[1].kind == nil && legacy.stops[1].place.coordinate == nil && legacy.stops[1].durationMinutes == 0)
        let oldRoute = """
        {"recalculated_legs":[{"mode":"transit","duration_min":12},{"mode":"bus","minutes":4},{"mode":"train","minutes":6},{"mode":"subway","minutes":9},{"mode":"hoverboard","minutes":1}],
         "stop_times":[{"arrive_time":"2026-09-25T18:00:00Z","depart_time":"2026-09-25T18:40:00Z"}],"arrival":"2026-09-25T19:00:00Z"}
        """
        let retimed = try decoder.decode(RouteResult.self, from: Data(oldRoute.utf8))
        #expect(retimed.legs.map(\.mode) == [.marta, .marta, .marta, .marta, .walk] && retimed.legs.map(\.minutes) == [12, 4, 6, 9, 1])
        #expect(retimed.stopTimes == [DateInterval(start: clock.date(2026, 9, 25, 14, 0), end: clock.date(2026, 9, 25, 14, 40))])
        #expect(retimed.minutesLate == 0 && retimed.brokenAt == -1)
        #expect(try decoder.decode(PlanBatch.self, from: Data(#"{"options":[]}"#.utf8)).done)

        // The examples: what the app reads of the DAG planner's extras.
        func stop(_ id: String, _ title: String, _ subtitle: String, minutes: Int, kind: PlanStopKind, at hour: Int, _ minute: Int, activity: String) -> PlanStop {
            let arrive = clock.date(2026, 9, 25, hour, minute)
            return PlanStop(id: id, title: title, subtitle: subtitle, place: Place(name: title, coordinate: MockPlaces.stops[title]?.coordinate),
                            durationMinutes: minutes, arriveTime: arrive, departTime: clock.addingMinutes(minutes, to: arrive),
                            kind: kind, flexible: kind == .place, activityId: activity)
        }
        let extras = PlanBatch(options: [
            PlanOption(id: "run_7f3k-0", name: "Rooftop + murals", tag: "Best match", meta: "~$ · 1.8 mi walking · 2 marta legs", stops: [
                stop("stop_act_rooftop_0", "Skyline Park rooftop", "Games + views · $", minutes: 80, kind: .place, at: 14, 24, activity: "act_rooftop"),
                stop("stop_act_murals_1", "Krog Street Tunnel murals", "Street art · Free", minutes: 60, kind: .place, at: 15, 58, activity: "act_murals"),
                stop("stop_act_krog_2", "Krog Street Market", "Food hall · $", minutes: 35, kind: .place, at: 17, 1, activity: "act_krog"),
            ], lateFlag: false, totalCostCents: 2400),
            PlanOption(id: "run_7f3k-1", name: "Downtown loop", tag: "Meet people", meta: "~$$ · 1.1 mi walking · 2 marta legs", stops: [
                stop("stop_act_centennial_0", "Centennial Olympic Park", "Park · Free", minutes: 60, kind: .place, at: 14, 32, activity: "act_centennial"),
                stop("stop_act_skyview_1", "SkyView Ferris wheel", "Views · $$", minutes: 50, kind: .place, at: 15, 35, activity: "act_skyview"),
                stop("stop_act_dinner_2", "Open group dinner (Forum)", "Community event · $ · 5:30 PM", minutes: 75, kind: .event, at: 17, 42, activity: "act_dinner"),
            ], lateFlag: true, totalCostCents: 3400),
        ], cursor: "dag_run_7f3k_2", done: false)
        try roundTrip(extras, "PlanBatch.dag")
        try roundTrip(PlanBatch(options: [], cursor: nil, done: true, reason: .noCandidatesFitWindow), "PlanBatch.empty")
        let late = RouteResult(
            legs: [Leg(mode: .marta, minutes: 14), Leg(mode: .marta, minutes: 14), Leg(mode: .walk, minutes: 3), Leg(mode: .marta, minutes: 21)],
            stopTimes: [DateInterval(start: clock.date(2026, 9, 25, 14, 24), end: clock.date(2026, 9, 25, 15, 44)),
                        DateInterval(start: clock.date(2026, 9, 25, 15, 58), end: clock.date(2026, 9, 25, 16, 58)),
                        DateInterval(start: clock.date(2026, 9, 25, 17, 1), end: clock.date(2026, 9, 25, 17, 36))],
            arrival: clock.date(2026, 9, 25, 17, 57), minutesLate: 25, brokenAt: 1)
        try roundTrip(late, "RouteResult.dag")
    }

    /// Must-see: `ActivityHits` is what `GET /activities/search` returns (an event with every field,
    /// a free place, and a place with neither a known price nor a distance, from a search without
    /// `near`), and `PlanRequest.mustInclude` a plan request with two picks.
    @Test func mustSeeExamples() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let clock = AppClock.demo
        let near = MockPlaces.techSquare.coordinate
        let event = try #require(try await api.searchActivities(q: "trivia", near: near, date: clock.now, limit: 1).first)
        let place = try #require(try await api.searchActivities(q: "jackson", near: near, date: clock.now, limit: 1).first)
        let unknown = try #require(try await api.searchActivities(q: "politan", near: nil, date: clock.now, limit: 1).first)
        #expect(event.kind == .event && event.start != nil && event.end != nil && event.priceCents != nil && event.distanceMi != nil
                && event.category != nil && event.place?.coordinate != nil)
        #expect(place.kind == .place && place.priceCents == 0 && place.distanceMi != nil)
        #expect(unknown.kind == .place && unknown.priceCents == nil && unknown.distanceMi == nil)
        try roundTrip([event, place, unknown], "ActivityHits")

        let request = PlanRequest(start: MockPlaces.techSquare.place, end: MockPlaces.home.place, date: clock.now,
                                  startTime: clock.date(2026, 9, 25, 14, 10), backBy: clock.date(2026, 9, 25, 18, 30),
                                  range: .transit, ride: RideChoice.none, openSeats: nil,
                                  moodText: "Something chill and outside, then cheap food after.",
                                  tags: ["Outdoors", "Food", "Meet people"], budget: 1, who: .friends, pace: .balanced, modes: [.walk, .marta],
                                  mustInclude: [event.id, place.id])
        try roundTrip(request, "PlanRequest.mustInclude")
        let batch = try await api.generatePlans(request)
        #expect(!batch.options.isEmpty && batch.options.allSatisfy { Set($0.stops.compactMap(\.activityId)).isSuperset(of: [event.id, place.id]) })
    }

    /// Review › tap a stop: `GET /activities/{id}` for a place (its hours on the plan's day, rating,
    /// description and links) and for an event (its start, end, venue and price), as the demo serves
    /// them. `pkg/contract` pins the server's own shape too.
    @Test func activityDetailExamples() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let clock = AppClock.demo
        let place = try await api.activity(id: MockActivities.id(for: "Skyline Park rooftop"), date: clock.now)
        #expect(place.kind == .place && place.hoursLine != nil && place.description != nil && place.rating != nil
                && place.ratingCount != nil && place.websiteURL != nil && place.ticketURL != nil && place.place.coordinate != nil)
        try roundTrip(place, "ActivityDetail")
        let event = try await api.activity(id: MockActivities.id(for: "Gallery talk at the High"), date: clock.now)
        #expect(event.kind == .event && event.start != nil && event.end != nil && event.priceCents == 1800 && event.priceLabel == "$18"
                && event.venueName == "High Museum of Art" && event.hoursLine == nil)
        try roundTrip(event, "ActivityDetail.event")
    }

    /// Agentic checkout, as the Go backend writes it (pkg/contract/checkout.go, pkg/agent): the plan
    /// for two paid Saltlight stops (`CheckoutPlan`), the one approval (`CreateCheckoutRun`), and the
    /// finished run (`CheckoutRun`: one stop booked, the other over what was left of the budget).
    @Test func agenticCheckoutExamples() throws {
        let clock = AppClock.demo
        let merchant = "events.sidequestz.tech"
        let jazz = URL(string: "https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets")!
        let disco = URL(string: "https://events.sidequestz.tech/barnacle-bash-silent-disco/tickets")!
        let plan = CheckoutPlan(
            itineraryId: "665f1c2e9b1e8a0012345678", available: true, agenticCheckout: true,
            items: [CheckoutPlanItem(itemId: "stop-2", title: "Sunset Jazz on Pier Nine", start: clock.date(2026, 9, 26, 18, 30),
                                     merchant: merchant, ticketURL: jazz, priceCents: 1200, quantity: 1, booked: false,
                                     intentState: nil, confirmation: nil),
                    CheckoutPlanItem(itemId: "stop-4", title: "Barnacle Bash Silent Disco", start: clock.date(2026, 9, 26, 22, 0),
                                     merchant: merchant, ticketURL: disco, priceCents: 1500, quantity: 1, booked: false,
                                     intentState: nil, confirmation: nil)],
            estimateCents: 2700, defaultBudgetCents: 5000, suggestedBudgetCents: 5000,
            paymentMethodId: "pm-4242", cardBrand: "Visa", cardLast4: "4242", activeRunId: nil)
        #expect(plan.shouldPrompt && plan.buyable.count == 2)
        try roundTrip(plan, "CheckoutPlan")
        let approval = CreateCheckoutRun(budgetCents: 5000, items: [.init(itemId: "stop-2", quantity: 2), .init(itemId: "stop-4", quantity: 2)],
                                         paymentMethodId: "pm-4242")
        try roundTrip(approval, "CreateCheckoutRun")

        func step(_ text: String, _ done: Bool = true) -> CheckoutStep { CheckoutStep(text: text, done: done) }
        var booked = CheckoutIntent(id: "ci-51", itemId: "stop-2", itemTitle: "Sunset Jazz on Pier Nine",
                                    steps: [step("Waiting for Muse to open the ticket page", false), step("Opened the ticket page"),
                                            step("Found 2 tickets · $26.92 with fees"), step("Paying with a Stripe token limited to $26.92"),
                                            step("Booked · SL-7KD4Q")],
                                    subtotalCents: 2400, feesCents: 292, totalCents: 2692, cardBrand: "Visa", cardLast4: "4242",
                                    state: .booked, quantity: 2, paymentMethodId: "pm-4242")
        booked.runId = "run-9"
        booked.merchant = merchant
        booked.checkoutURL = jazz
        booked.maxAuthorizedCents = 2692
        booked.finalCents = 2692
        booked.orderRef = "SL-7KD4Q"
        booked.confirmation = "SL-7KD4Q"
        booked.ticketURL = URL(string: "https://events.sidequestz.tech/t/9f3c2a7e5b1d4c8e0a6f2b9d4e1c7a3f")
        let reason = "$33.40 is more than the $23.08 left in your budget."
        var missed = CheckoutIntent(id: "ci-52", itemId: "stop-4", itemTitle: "Barnacle Bash Silent Disco",
                                    steps: [step("Waiting for Muse to open the ticket page", false), step("Opened the ticket page"),
                                            step("Found 2 tickets · $33.40 with fees"), step(reason)],
                                    subtotalCents: 3000, feesCents: 340, totalCents: 3340, cardBrand: "Visa", cardLast4: "4242",
                                    state: .failed, quantity: 2, paymentMethodId: "pm-4242", failureReason: reason)
        missed.runId = "run-9"
        missed.merchant = merchant
        missed.checkoutURL = disco
        missed.failureCode = "over_budget"
        let run = CheckoutRun(id: "run-9", itineraryId: plan.itineraryId, state: .done, budgetCents: 5000, spentCents: 2692,
                              currency: "usd", cardBrand: "Visa", cardLast4: "4242", agent: "muse",
                              summary: "Muse got 1 of 2: Sunset Jazz on Pier Nine (2 tickets, $26.92, SL-7KD4Q). "
                                  + "Barnacle Bash Silent Disco: over budget. Spent $26.92 of your $50.00 budget (sandbox: nothing was charged).",
                              intents: [booked, missed], createdAt: clock.date(2026, 9, 26, 17, 0), finishedAt: clock.date(2026, 9, 26, 17, 1))
        #expect(run.booked.map(\.id) == ["ci-51"] && run.notBooked.first?.outcomeLine == reason)
        try roundTrip(run, "CheckoutRun")
    }

    /// `GET /me` with a home base (the demo account, Sandy Byte) decodes it, and the example
    /// `UserHomeBase` shows the shape.
    @Test func userHomeBaseDecodes() throws {
        let json = """
        {"id":"seed-sandy","name":"Sandy Byte","username":"sandybyte","email":"demo@sidequestz.tech","avatar_color":"sage","status":"open",
         "age_bracket":"adult","school":"Saltlight Harbor College","setup_complete":true,
         "home_base":{"name":"Seaside Market Square","coordinate":{"lat":31.368,"lng":-81.425}},"city":"saltlight"}
        """
        let sandy = try decoder.decode(User.self, from: Data(json.utf8))
        #expect(sandy.homeBase == Place(name: "Seaside Market Square", coordinate: Coordinate(lat: 31.368, lng: -81.425)))
        #expect(sandy.city == "saltlight" && sandy.cityLabel == "Saltlight")
        try roundTrip(sandy, "UserHomeBase")
        // A home base can come without a center; the city keeps a name's own casing.
        let named = try decoder.decode(User.self, from: Data(#"{"id":"u2","name":"Ada","home_base":{"name":"Somewhere"},"city":"Atlanta, GA"}"#.utf8))
        #expect(named.homeBase == Place(name: "Somewhere") && named.cityLabel == "Atlanta, GA")
    }
}

/// The home base: optional on the wire and in the demo, the default start of a plan and the
/// Forum's first area when set.
@MainActor
struct HomeBaseTests {
    private let decoder = APICoding.decoder(timeZone: AppClock.demo.timeZone)

    @Test func homeBaseDecodesAndDefaultsToNil() throws {
        let bare = try decoder.decode(User.self, from: Data(#"{"id":"u1","name":"Sam T."}"#.utf8))
        #expect(bare.homeBase == nil && bare.city == nil && bare.cityLabel == nil)
        #expect(MockData.user().homeBase == nil && MockData.user().city == nil)
        let home = Place(name: "Seaside Market Square", coordinate: Coordinate(lat: 31.368, lng: -81.425))
        let json = #"{"id":"u1","name":"Sandy","home_base":{"name":"Seaside Market Square","coordinate":{"lat":31.368,"lng":-81.425}},"city":"saltlight"}"#
        let sandy = try decoder.decode(User.self, from: Data(json.utf8))
        #expect(sandy.homeBase == home && sandy.city == "saltlight")
        // Sent back as `home_base` / `city`, and left out when nil.
        let encoder = APICoding.encoder()
        let sent = String(decoding: try encoder.encode(sandy), as: UTF8.self)
        #expect(sent.contains(#""home_base":{"#) && sent.contains(#""city":"saltlight""#))
        let bareSent = String(decoding: try encoder.encode(bare), as: UTF8.self)
        #expect(!bareSent.contains("home_base") && !bareSent.contains("city"))
        #expect(User(id: "u3", name: "No base", email: "", avatarColor: .ink, status: .open, ageBracket: .adult).homeBase == nil)
    }

    @Test func createStartsAtHomeBaseWhenSet() async throws {
        // The preview account has a home base: the start is set before the phone answers, and the
        // default plan comes back to it.
        let env = AppEnvironment.preview()
        let home = try #require(env.user?.homeBase)
        let model = CreateFlowModel(draft: CreateDraft(), env: env)
        #expect(model.start == nil)
        await model.loadDefaultPlacesIfNeeded()
        #expect(model.start == home)
        #expect(model.endSameAsStart && model.end == nil && model.endPlace == home)
        #expect(model.placesLoaded && model.currentPlace == MockPlaces.techSquare.place)
        await model.loadSuggestions()
        #expect(Array(model.suggestions.prefix(2)) == [.currentLocation, .homeBase(home)])
        #expect(model.suggestions.count == 4)
        #expect(!model.suggestions.contains { $0.label == home.name })
        await model.pick(.homeBase(home))
        #expect(model.start == home)

        // The demo account has none: the start is where the phone is, and there's no pill for it.
        let demo = AppEnvironment.preview()
        demo.user = MockData.user()
        let plain = CreateFlowModel(draft: CreateDraft(), env: demo)
        await plain.loadDefaultPlacesIfNeeded()
        #expect(plain.start == MockPlaces.techSquare.place && plain.end == MockPlaces.home.place)
        await plain.loadSuggestions()
        #expect(plain.suggestions.first == .currentLocation)
        #expect(!plain.suggestions.contains { if case .homeBase = $0 { true } else { false } })
    }

    @Test func forumDefaultArea() {
        #expect(ForumQuery.defaultArea(homeBase: nil) == .midtown)
        #expect(ForumQuery().area == .midtown)
        let home = Place(name: "Seaside Market Square", coordinate: Coordinate(lat: 31.368, lng: -81.425))
        let area = ForumQuery.defaultArea(homeBase: home)
        #expect(area.name == home.name && area.coordinate == home.coordinate && !area.isCurrentLocation)
        // A home base without a center can't be an area.
        #expect(ForumQuery.defaultArea(homeBase: Place(name: "Somewhere")) == .midtown)
        #expect(ForumArea.suggestions(homeBase: home, isMock: false).map(\.name) == ["Current location", home.name])
        #expect(ForumArea.suggestions(homeBase: nil, isMock: false).map(\.name) == ["Current location"])
        #expect(ForumArea.suggestions(homeBase: nil, isMock: true).map(\.name)
                == ["Current location", "Midtown Atlanta", "Georgia Tech campus", "Downtown Atlanta", "Decatur"])
        #expect(ForumArea.suggestions(homeBase: home, isMock: true).map(\.name).prefix(3) == ["Current location", home.name, "Midtown Atlanta"])
        // A home base that is one of the demo areas isn't listed twice.
        let midtownHome = Place(name: ForumArea.midtown.name, coordinate: ForumArea.midtown.coordinate)
        #expect(ForumArea.suggestions(homeBase: midtownHome, isMock: true).count == 5)
    }
}

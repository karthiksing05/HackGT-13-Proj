import Foundation
import Testing
@testable import SideQuestz

/// Create › Vibe › Must-see: the search's hits, `must_include` on the plan request, why a batch can
/// come back empty, the flow's picks and search, and the demo backend honoring the picks.
@MainActor
struct MustSeeTests {
    private let clock = AppClock.demo
    private let decoder = APICoding.decoder(timeZone: AppClock.demo.timeZone)
    private let encoder = APICoding.encoder()

    private let jazz = MockActivities.id(for: "Sunset jazz on the Eastside Trail")
    private let bridge = MockActivities.id(for: "Jackson Street Bridge")

    /// The demo's default window: Tech Square → Home, 2:10 PM → back by 6:30 PM, no ride.
    private func request(picks: [String] = [], backBy: Date? = nil) -> PlanRequest {
        PlanRequest(start: MockPlaces.techSquare.place, end: MockPlaces.home.place, date: clock.now,
                    startTime: clock.date(2026, 9, 25, 14, 10), backBy: backBy ?? clock.date(2026, 9, 25, 18, 30),
                    range: .transit, ride: RideChoice.none, openSeats: nil, moodText: "", tags: [],
                    budget: 1, who: .friends, pace: .balanced, modes: [.walk, .marta], mustInclude: picks)
    }

    // MARK: Wire

    @Test func activityHitsDecodeLeniently() throws {
        let json = """
        [{"id":"6a31ffa8d4ca54aa5578259f","title":"Sunset Jazz on Pier Nine","kind":"event","category":"live_music",
          "subtitle":"Live music · 6:30 PM · 0.7 mi","place":{"name":"Pier Nine","coordinate":{"lat":31.37,"lng":-81.42}},
          "start":"2026-09-27T22:30:00Z","end":"2026-09-28T01:00:00Z","price_cents":1500,"distance_mi":0.7,"score":0.93},
         {"id":"p1","title":"Harbor Lights Park","kind":"place","subtitle":"Park · Free","price_cents":0},
         {"id":"p2","title":"No kind, no start"},
         {"id":"e2","title":"Mystery show","kind":"hologram","start":"2026-09-27T20:00:00.250Z"}]
        """
        let hits = try decoder.decode([ActivityHit].self, from: Data(json.utf8))
        #expect(hits.count == 4)
        let event = hits[0]
        #expect(event.kind == .event && event.category == "live_music" && event.subtitle == "Live music · 6:30 PM · 0.7 mi")
        #expect(event.place == Place(name: "Pier Nine", coordinate: Coordinate(lat: 31.37, lng: -81.42)))
        #expect(event.start == clock.date(2026, 9, 27, 18, 30) && event.end == clock.date(2026, 9, 27, 21, 0))
        #expect(event.priceCents == 1500 && event.distanceMi == 0.7)
        #expect(hits[1].kind == .place && hits[1].priceCents == 0 && hits[1].place == nil && hits[1].distanceMi == nil && hits[1].start == nil)
        // No kind: a place without a start; an unknown kind with a start reads as an event.
        #expect(hits[2].kind == .place && hits[2].subtitle == "" && hits[2].category == nil)
        #expect(hits[3].kind == .event)
        // A hit needs its id and title.
        #expect(throws: DecodingError.self) { try decoder.decode(ActivityHit.self, from: Data(#"{"id":"x"}"#.utf8)) }
        // What goes back out leaves the unknown fields out.
        let sent = String(decoding: try encoder.encode(hits[1]), as: UTF8.self)
        #expect(!sent.contains("distance_mi") && !sent.contains("start") && sent.contains(#""price_cents":0"#))
    }

    @Test func mustIncludeIsSentOnlyWithPicks() throws {
        var plan = request()
        let plain = try encoder.encode(plan)
        #expect(!String(decoding: plain, as: UTF8.self).contains("must_include"))
        // Older examples (no `must_include`) still decode, with no picks.
        #expect(try decoder.decode(PlanRequest.self, from: plain) == plan)

        plan.mustInclude = [jazz, bridge]
        let picked = try encoder.encode(plan)
        #expect(String(decoding: picked, as: UTF8.self).contains(#""must_include":["\#(jazz)","\#(bridge)"]"#))
        #expect(try decoder.decode(PlanRequest.self, from: picked) == plan)
        // Picks are part of the request: other picks mean new options.
        #expect(plan != request())
    }

    @Test func emptyBatchReasonsForPicks() throws {
        let batch = try decoder.decode(PlanBatch.self, from: Data(#"{"options":[],"done":true,"reason":"must_include_unavailable: Sunset Jazz on Pier Nine"}"#.utf8))
        #expect(batch.options.isEmpty && batch.reason == .mustIncludeUnavailable("Sunset Jazz on Pier Nine"))
        #expect(batch.reason?.message == "Sunset Jazz on Pier Nine isn't available in this window. Remove it or pick another time.")
        // The server's name for an id it doesn't know starts the sentence with a capital.
        #expect(PlanEmptyReason(rawValue: "must_include_unavailable: a pick").message
                == "A pick isn't available in this window. Remove it or pick another time.")
        #expect(PlanEmptyReason(rawValue: "must_include_unavailable") == .mustIncludeUnavailable(""))
        // A title keeps its own colon.
        #expect(PlanEmptyReason(rawValue: "must_include_unavailable: Jazz: Live at Nine") == .mustIncludeUnavailable("Jazz: Live at Nine"))
        #expect(PlanEmptyReason(rawValue: "must_include_no_fit") == .mustIncludeNoFit)
        #expect(PlanEmptyReason.mustIncludeNoFit.message
                == "Your must-see picks don't all fit in this window. Try a longer window or fewer picks.")
        #expect(PlanEmptyReason(rawValue: "must_include_unavailable_later") == .other("must_include_unavailable_later"))
        for reason in [PlanEmptyReason.mustIncludeUnavailable("Pier Nine"), .mustIncludeUnavailable(""), .mustIncludeNoFit] {
            #expect(PlanEmptyReason(rawValue: reason.rawValue) == reason)
        }
    }

    // MARK: The flow

    @Test func picksToggleAndGoIntoThePlanRequest() async throws {
        let model = CreateFlowModel(draft: CreateDraft(), env: AppEnvironment.preview())
        await model.loadDefaultPlacesIfNeeded()
        #expect(model.planRequest?.mustInclude == [])

        let jazzHit = ActivityHit(id: jazz, title: "Sunset jazz on the Eastside Trail", kind: .event)
        let bridgeHit = ActivityHit(id: bridge, title: "Jackson Street Bridge", kind: .place)
        model.toggleMustSee(jazzHit)
        model.toggleMustSee(bridgeHit)
        #expect(model.isMustSee(jazz) && model.isMustSee(bridge))
        #expect(model.planRequest?.mustInclude == [jazz, bridge])

        model.toggleMustSee(jazzHit)
        #expect(!model.isMustSee(jazz) && model.planRequest?.mustInclude == [bridge])
        model.toggleMustSee(jazzHit)
        #expect(model.planRequest?.mustInclude == [bridge, jazz])
        model.removeMustSee(bridge)
        #expect(model.mustSee.map(\.id) == [jazz])

        // The app sets no limit; the server answers more than 10 with a sentence.
        for index in 0..<12 { model.toggleMustSee(ActivityHit(id: "x\(index)", title: "Spot \(index)", kind: .place)) }
        #expect(model.planRequest?.mustInclude.count == 13)
    }

    @Test func searchSuggestsTheDayThenFollowsTheQuery() async throws {
        let model = CreateFlowModel(draft: CreateDraft(), env: AppEnvironment.preview())
        await model.loadDefaultPlacesIfNeeded()
        #expect(model.mustSeeResults.isLoading)

        // Nothing typed: Friday's events by start (not the one that's over, not Saturday's), then places.
        await model.searchMustSee()
        let suggestions = try #require(model.mustSeeResults.value)
        #expect(suggestions.count == CreateFlowModel.mustSeeLimit)
        #expect(suggestions.prefix { $0.kind == .event }.map(\.title) == [
            "Rooftop trivia", "Gallery talk at the High", "Food truck Friday", "Pickup soccer",
            "Sunset jazz on the Eastside Trail", "Late show at the Plaza Theatre",
        ])
        #expect(suggestions.suffix(2).allSatisfy { $0.kind == .place && $0.distanceMi != nil })
        #expect(model.mustSeeResultsSearch == CreateMustSeeSearch(query: "", day: "2026-09-25", near: model.start?.coordinate))

        model.mustSeeQuery = "  JAZZ "
        await model.searchMustSee()
        #expect(model.mustSeeResults.value?.map(\.title) == ["Sunset jazz on the Eastside Trail"])
        #expect(model.mustSeeResultsSearch?.query == "JAZZ" && !model.searchingMustSee)

        // A pick stays while the query changes.
        let hit = try #require(model.mustSeeResults.value?.first)
        model.toggleMustSee(hit)
        model.mustSeeQuery = "zzzz"
        await model.searchMustSee()
        #expect(model.mustSeeResults.value == [])
        #expect(model.mustSee.map(\.id) == [jazz])
    }

    @Test func searchFailureShowsAndRetries() async throws {
        let env = AppEnvironment.preview()
        let api = try #require(env.api as? MockAPIClient)
        let model = CreateFlowModel(draft: CreateDraft(), env: env)
        await model.loadDefaultPlacesIfNeeded()
        api.failing = ["activities"]
        await model.searchMustSee()
        #expect(model.mustSeeResults.phase == .failed)
        api.failing = []
        await model.searchMustSee()
        #expect(model.mustSeeResults.value?.isEmpty == false)
    }

    @Test func reviewKnowsWhichStopsArePicks() async throws {
        let model = CreateFlowModel(draft: CreateDraft(), env: AppEnvironment.preview())
        await model.loadDefaultPlacesIfNeeded()
        await model.searchMustSee()
        let hits = try #require(model.mustSeeResults.value)
        model.toggleMustSee(try #require(hits.first { $0.id == jazz }))
        await model.regenerate()
        let options = model.optionList
        #expect(!options.isEmpty && model.planPicks == [jazz])
        for option in options {
            let rows = CreateRouteRow.rows(stops: option.stops, state: CreateRouteState(order: option.stops.map(\.id)),
                                           startName: "A", endName: "B", startTime: model.startTime, backBy: model.backBy,
                                           format: TimeFormat(clock: clock), picks: model.planPicks)
            #expect(rows.filter(\.isPick).map(\.title) == ["Sunset jazz on the Eastside Trail"])
        }
    }

    // MARK: The demo backend

    @Test func demoSearchFollowsTheServerRules() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let near = MockPlaces.techSquare.coordinate

        // A name that starts with the query comes first, even before an event.
        let high = try await api.searchActivities(q: "high", near: near, date: clock.now, limit: 20)
        #expect(high.map(\.title) == ["High Museum of Art", "Gallery talk at the High"])
        #expect(high[1].kind == .event && high[1].subtitle.hasPrefix("Gallery talk · 3:30 PM · ") && high[1].subtitle.hasSuffix(" mi"))
        #expect(high[0].subtitle.hasPrefix("Museum · $$ · "))

        // Venue, category and tags match too, without regard to case or accents.
        #expect(try await api.searchActivities(q: "CAFE", near: nil, date: nil, limit: 20).map(\.title) == ["Board game café"])
        #expect(try await api.searchActivities(q: "live_music", near: nil, date: nil, limit: 20).map(\.title) == ["Sunset jazz on the Eastside Trail"])
        let soccer = try await api.searchActivities(q: "fourth ward", near: near, date: nil, limit: 20)
        #expect(soccer.first?.title == "Pickup soccer")

        // Only that day's events that aren't over; places have no hours in the demo.
        let friday = try await api.searchActivities(q: "", near: near, date: clock.now, limit: 50)
        #expect(!friday.contains { $0.title == "Morning farmers market" || $0.title == "Sunrise yoga in Piedmont Park" })
        #expect(friday.count == MockActivities.all.count - 2)
        let saturday = try await api.searchActivities(q: "yoga", near: nil, date: clock.date(2026, 9, 26), limit: 5)
        #expect(saturday.map(\.title) == ["Sunrise yoga in Piedmont Park"])
        #expect(saturday.first?.subtitle == "Yoga · 7:30 AM" && saturday.first?.distanceMi == nil)

        // Places after the events, nearest first; `limit` caps the list.
        let places = friday.drop { $0.kind == .event }
        let miles = places.compactMap(\.distanceMi)
        #expect(miles.count == places.count && miles == miles.sorted())
        #expect(try await api.searchActivities(q: "", near: near, date: clock.now, limit: 3).count == 3)
    }

    @Test func demoPlannerPutsEveryPickInEveryOption() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let plan = request(picks: [jazz, bridge])
        var options = try await api.generatePlans(plan).options
        #expect(options.count == 3)
        var batch = PlanBatch(options: [], cursor: "batch-0", done: false)
        while !batch.done, let cursor = batch.cursor {
            batch = try await api.moreOptions(cursor: cursor)
            options += batch.options
        }
        #expect(options.count > 3)
        #expect(Set(options.map { $0.stops.map(\.title) }).count == options.count, "An option came out twice")

        for option in options {
            let picked = option.stops.compactMap(\.activityId).filter { $0 == jazz || $0 == bridge }
            #expect(picked == [bridge, jazz] || picked == [jazz, bridge], "\(option.id): \(option.stops.map(\.title))")
            let route = try await api.route(RouteRequest(optionId: option.id, stopOrder: option.stops.map(\.id), start: plan.start,
                                                         end: plan.end, startTime: plan.startTime, backBy: plan.backBy,
                                                         ride: plan.ride, modes: plan.modes))
            #expect(route.brokenAt == -1 && route.minutesLate == 0, "\(option.id) doesn't fit")
            // The event is on at its start, for as long as you'd stay.
            let index = try #require(option.stops.firstIndex { $0.activityId == jazz })
            #expect(route.stopTimes[index] == DateInterval(start: clock.date(2026, 9, 25, 17, 0), end: clock.date(2026, 9, 25, 18, 0)))
            #expect(option.stops[index].kind == .event && option.stops[index].flexible == false)
        }

        // Without picks, the demo's options are as before.
        #expect(try await api.generatePlans(request()).options == MockData.firstOptions)
    }

    /// A pick the option already has becomes the pick, in its place.
    @Test func demoPlannerReusesAStopTheOptionHas() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let rooftop = MockActivities.id(for: "Skyline Park rooftop")
        let options = try await api.generatePlans(request(picks: [rooftop])).options
        let a = try #require(options.first { $0.id == "opt-a" })
        #expect(a.stops.map(\.title) == MockData.firstOptions[0].stops.map(\.title))
        #expect(a.stops[0].activityId == rooftop && a.meta == MockData.firstOptions[0].meta)
        #expect(options.allSatisfy { $0.stops.contains { $0.activityId == rooftop } })
    }

    @Test func demoPlannerSaysWhyPicksCantBeIn() async throws {
        let api = MockAPIClient(latencyScale: 0)
        func reason(_ picks: [String], backBy: Date? = nil) async throws -> PlanEmptyReason? {
            let batch = try await api.generatePlans(request(picks: picks, backBy: backBy))
            #expect(batch.options.isEmpty == (batch.reason != nil) && (batch.reason == nil || (batch.done && batch.cursor == nil)))
            return batch.reason
        }
        #expect(try await reason(["no-such-id"]) == .mustIncludeUnavailable("a pick"))
        #expect(try await reason([jazz, MockActivities.id(for: "Sunrise yoga in Piedmont Park")])
                == .mustIncludeUnavailable("Sunrise yoga in Piedmont Park"))
        #expect(try await reason([MockActivities.id(for: "Morning farmers market")]) == .mustIncludeUnavailable("Morning farmers market"))

        // The 8 PM show exists but doesn't fit a window that ends at 6:30; a later back-by fits it.
        let show = MockActivities.id(for: "Late show at the Plaza Theatre")
        #expect(try await reason([show]) == .mustIncludeNoFit)
        #expect(try await reason([show], backBy: clock.date(2026, 9, 25, 23, 0)) == nil)
        // Two events at the same time can't both happen.
        #expect(try await reason([MockActivities.id(for: "Rooftop trivia"), MockActivities.id(for: "Gallery talk at the High")]) == .mustIncludeNoFit)

        // Up to 10 picks (duplicates don't count); more is a 400 with the server's sentence.
        #expect(try await reason(Array(repeating: jazz, count: 12)) == nil)
        await #expect(throws: APIError.validation("Pick up to 10 must-see spots.")) {
            try await api.generatePlans(request(picks: (0...10).map { "x\($0)" }))
        }
    }

    @Test func routesWaitForAFixedStartAndFlagALateOne() throws {
        let event = try #require(MockActivities.activity(id: jazz))
        let stop = MockPicks.stop(for: event, in: "opt-a")
        let start = clock.date(2026, 9, 25, 14, 10), backBy = clock.date(2026, 9, 25, 18, 30)
        let alone = MockRouteEngine.route(stops: [stop], start: MockPlaces.techSquare.place, end: MockPlaces.home.place,
                                          startTime: start, backBy: backBy, ride: .none)
        #expect(alone.stopTimes == [DateInterval(start: clock.date(2026, 9, 25, 17, 0), end: clock.date(2026, 9, 25, 18, 0))])
        #expect(alone.brokenAt == -1 && alone.minutesLate == 0)

        // After option A's three stops (done at 5:36 PM) the 5 PM start is missed.
        let late = MockRouteEngine.route(stops: MockData.firstOptions[0].stops + [stop], start: MockPlaces.techSquare.place,
                                         end: MockPlaces.home.place, startTime: start, backBy: backBy, ride: .none)
        #expect(late.brokenAt == 3)
    }
}

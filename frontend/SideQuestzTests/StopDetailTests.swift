import Foundation
import Testing
@testable import SideQuestz

/// Review › tap a stop: the details pane's data (`GET /activities/{id}`), its timing lines, and the
/// model behind it (loading, the failure line, Swap and Remove once it has closed).
@MainActor
struct StopDetailTests {
    private let clock = AppClock.demo
    private let decoder = APICoding.decoder(timeZone: AppClock.demo.timeZone)
    private var format: TimeFormat { TimeFormat(clock: clock) }

    // MARK: Decoding

    /// What the Go server writes for an event with every field (`pkg/contract` pins the same shape).
    @Test func decodesTheServersDetail() throws {
        let json = """
        {"id":"9bdbbc5e9aab712a5b769f72","title":"Sunset Jazz on Pier Nine","kind":"event","category":"live_music",
         "category_label":"Live music","summary":"Brass and sunsets on the pier.",
         "description":"Brass and sunsets on the pier. Bring a blanket; the bandstand has a few benches.",
         "venue_name":"Pier Nine Bandstand","address":"9 Pier Rd, Saltlight Harbor, GA 31991",
         "place":{"name":"Pier Nine Bandstand","coordinate":{"lat":31.376524,"lng":-81.41746}},
         "start":"2026-09-26T22:30:00Z","end":"2026-09-27T01:00:00Z","price_cents":1200,"price_label":"$12–$20",
         "rating":4.6,"rating_count":1204,"url":"https://saltlight.example/jazz",
         "ticket_url":"https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets","image_url":"https://saltlight.example/jazz.jpg",
         "tags":["music","outdoor"]}
        """
        let detail = try decoder.decode(ActivityDetail.self, from: Data(json.utf8))
        #expect(detail.id == "9bdbbc5e9aab712a5b769f72" && detail.title == "Sunset Jazz on Pier Nine" && detail.kind == .event)
        #expect(detail.category == "live_music" && detail.categoryLabel == "Live music" && detail.venueName == "Pier Nine Bandstand")
        #expect(detail.place == Place(name: "Pier Nine Bandstand", coordinate: Coordinate(lat: 31.376524, lng: -81.41746)))
        #expect(detail.start == clock.date(2026, 9, 26, 18, 30) && detail.end == clock.date(2026, 9, 26, 21, 0))
        #expect(detail.priceCents == 1200 && detail.priceLabel == "$12–$20" && detail.hoursLine == nil)
        #expect(detail.ratingLine == "4.6 ★ (1,204)")
        #expect(detail.websiteURL?.absoluteString == "https://saltlight.example/jazz")
        #expect(detail.ticketURL?.host == "events.sidequestz.tech" && detail.imageURL != nil && detail.tags == ["music", "outdoor"])
        // The description starts with the summary, so it's said once.
        #expect(detail.about == "Brass and sunsets on the pier. Bring a blanket; the bandstand has a few benches.")
    }

    @Test func decodesLeniently() throws {
        // Only the id and the title are required.
        let bare = try decoder.decode(ActivityDetail.self, from: Data(#"{"id":"a1","title":"Anchor Park"}"#.utf8))
        #expect(bare.kind == .place && bare.place == Place(name: "Anchor Park") && bare.tags.isEmpty)
        #expect(bare.categoryLabel.isEmpty && bare.priceLabel.isEmpty && bare.priceCents == nil && bare.hoursLine == nil)
        #expect(bare.about == nil && bare.ratingLine == nil && bare.websiteURL == nil)

        // A start without a known kind is an event; the venue names a missing place; blank text is
        // nothing; a link the in-app browser can't open is dropped instead of failing the detail.
        let odd = """
        {"id":"a2","title":"Jazz","kind":"concert","start":"2026-09-25T23:00:00Z","summary":"  ","venue_name":"Pier",
         "hours_line":"","url":"javascript:alert(1)","ticket_url":" https://t.example/x ","image_url":"ftp://files.example/x.jpg"}
        """
        let detail = try decoder.decode(ActivityDetail.self, from: Data(odd.utf8))
        #expect(detail.kind == .event && detail.summary == nil && detail.hoursLine == nil && detail.place == Place(name: "Pier"))
        #expect(detail.websiteURL == nil && detail.ticketURL?.absoluteString == "https://t.example/x" && detail.imageURL == nil)

        // It round-trips through the live client's JSON settings.
        let encoded = try APICoding.encoder().encode(detail)
        #expect(try decoder.decode(ActivityDetail.self, from: encoded) == detail)
    }

    @Test func aboutAndRatingLines() {
        var detail = ActivityDetail(id: "a", title: "A", kind: .place, place: Place(name: "A"))
        detail.summary = "Short."
        #expect(detail.about == "Short.")
        detail.description = "Longer, and not the same."
        #expect(detail.about == "Short.\n\nLonger, and not the same.")
        detail.summary = nil
        #expect(detail.about == "Longer, and not the same.")

        detail.rating = 4.6
        #expect(detail.ratingLine == "4.6 ★")
        detail.ratingCount = 0
        #expect(detail.ratingLine == "4.6 ★")
        detail.ratingCount = 21450
        #expect(detail.ratingLine == "4.6 ★ (21,450)")
    }

    // MARK: Timing lines

    @Test func visitLine() {
        let times = DateInterval(start: clock.date(2026, 9, 25, 14, 30), end: clock.date(2026, 9, 25, 16, 0))
        #expect(CreateStopTiming.visitLine(times, format: format) == "Arrive 2:30 PM · leave 4:00 PM")
        #expect(CreateStopTiming.visitLine(nil, format: format) == nil)
    }

    @Test func fixedAndFlexibleLines() {
        func stop(kind: PlanStopKind?, flexible: Bool?, arrive: Date? = nil) -> PlanStop {
            PlanStop(id: "s", title: "Stop", subtitle: "", place: Place(name: "Stop"), durationMinutes: 60,
                     arriveTime: arrive, kind: kind, flexible: flexible, activityId: "act-s")
        }
        func line(_ stop: PlanStop, _ detail: ActivityDetail? = nil) -> String? {
            CreateStopTiming.scheduleLine(stop: stop, detail: detail, format: format)
        }
        let showStart = clock.date(2026, 9, 25, 20, 0)
        let show = ActivityDetail(id: "act-s", title: "Late show", kind: .event, place: Place(name: "Plaza"),
                                  start: showStart, end: clock.date(2026, 9, 25, 22, 0))

        // The planner's fixed start: the event's own start once the details are in, its arrival before.
        let fixed = stop(kind: .event, flexible: false, arrive: clock.date(2026, 9, 25, 19, 55))
        #expect(line(fixed, show) == "Starts at 8:00 PM — fixed time")
        #expect(line(fixed) == "Starts at 7:55 PM — fixed time")
        #expect(line(stop(kind: .event, flexible: false)) == "Fixed start time")
        // No flag: an event the route waits for is fixed (the demo's picks).
        #expect(line(stop(kind: .event, flexible: nil, arrive: showStart)) == "Starts at 8:00 PM — fixed time")
        // Visit whenever: a place while it's open, a drop-in event while it's on.
        #expect(line(stop(kind: .place, flexible: true)) == "Drop in any time while it's open")
        #expect(line(stop(kind: .event, flexible: true), show) == "Drop in any time while it's on")
        // The stop doesn't say (the demo's own stops): the details decide, and nothing shows before.
        #expect(line(stop(kind: nil, flexible: nil)) == nil)
        #expect(line(stop(kind: nil, flexible: nil), ActivityDetail(id: "p", title: "Park", kind: .place, place: Place(name: "Park")))
                == "Drop in any time while it's open")

        // An event's run stands in for opening hours.
        #expect(CreateStopTiming.runLine(show, format: format) == "Runs 8:00–10:00 PM")
        #expect(CreateStopTiming.runLine(ActivityDetail(id: "p", title: "Park", kind: .place, place: Place(name: "Park")), format: format) == nil)
        #expect(CreateStopTiming.runLine(nil, format: format) == nil)
    }

    // MARK: The model

    /// Review with the demo's options, the first option's route timed.
    private func review(_ env: AppEnvironment? = nil) async throws -> (CreateFlowModel, PlanOption) {
        let model = CreateFlowModel(draft: CreateDraft(step: 4), env: env ?? .preview())
        await model.enterReview()
        let option = try #require(model.selectedOption)
        await model.refreshRoute(option.id, minimumSeconds: 0)
        // `select` also asked for a route; let whichever is newest land.
        for _ in 0..<100 where model.routes[option.id]?.isLoading == true || model.routes[option.id]?.isStale == true {
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(model.routes[option.id]?.isStale == false)
        return (model, option)
    }

    @Test func visitTimesFollowTheRouteOnScreen() async throws {
        let (model, option) = try await review()
        let stops = model.orderedStops(option)
        #expect(model.visitTimes(of: stops[0], in: option.id) == model.timeSlot(of: stops[0].id, in: option.id))
        #expect(model.visitTimes(of: stops[0], in: option.id) != nil)
        #expect(!model.isLate(stops[0].id, in: option.id))

        // While a new order waits for its timing, the planner's times stand in (the demo's own stops
        // have none).
        model.beginDrag(stops[0].id)
        model.moveDraggedStop(by: 1)
        #expect(model.timeSlot(of: stops[0].id, in: option.id) == nil)
        #expect(model.visitTimes(of: stops[0], in: option.id) == nil)
        var planned = stops[0]
        planned.arriveTime = clock.date(2026, 9, 25, 15, 0)
        planned.departTime = clock.date(2026, 9, 25, 16, 20)
        #expect(model.visitTimes(of: planned, in: option.id)
                == DateInterval(start: clock.date(2026, 9, 25, 15, 0), end: clock.date(2026, 9, 25, 16, 20)))
        planned.departTime = nil
        #expect(model.visitTimes(of: planned, in: option.id)?.duration == TimeInterval(planned.durationMinutes * 60))
        model.endDrag()
    }

    @Test func paneShowsTheStopThenItsDetails() async throws {
        let (model, option) = try await review()
        let stop = model.orderedStops(option)[0]
        model.openStopDetail(stop.id, in: option.id)
        #expect(model.stopDetail == CreateStopDetailTarget(optionId: option.id, stop: stop))
        #expect(model.stopDetailInfo.isLoading)

        await model.loadStopDetail()
        let detail = try #require(model.stopDetailInfo.value)
        #expect(detail.id == stop.activityId && detail.title == stop.title)
        #expect(detail.about?.isEmpty == false && detail.address?.isEmpty == false && !detail.priceLabel.isEmpty)

        // Reopened, the details this flow already has show at once.
        model.stopDetail = nil
        model.openStopDetail(stop.id, in: option.id)
        #expect(model.stopDetailInfo.value == detail)
        // An unknown stop opens nothing.
        model.stopDetail = nil
        model.openStopDetail("no-such-stop", in: option.id)
        #expect(model.stopDetail == nil)
    }

    @Test func failureIsOneShortLine() async throws {
        let env = AppEnvironment.preview()
        let api = try #require(env.api as? MockAPIClient)
        let (model, option) = try await review(env)
        let stop = model.orderedStops(option)[1]

        api.failing = ["activity"]
        model.openStopDetail(stop.id, in: option.id)
        await model.loadStopDetail()
        guard case .failed(let message) = model.stopDetailInfo else {
            Issue.record("expected the failure line, got \(model.stopDetailInfo.phase)")
            return
        }
        #expect(message == "More details aren't available right now.")

        // "Try again" loads them once the server answers.
        api.failing = []
        await model.loadStopDetail()
        #expect(model.stopDetailInfo.value?.title == stop.title)

        // A server without the endpoint, or an activity the catalog doesn't know, is a 404: the same line.
        var unknown = stop
        unknown.activityId = "act-not-in-the-catalog"
        await #expect(throws: APIError.notFound) { try await api.activity(id: "act-not-in-the-catalog", date: nil) }
        model.stopDetail = CreateStopDetailTarget(optionId: option.id, stop: unknown)
        await model.loadStopDetail()
        #expect(model.stopDetailInfo.phase == .failed)
    }

    @Test func swapAndRemoveHappenOnceThePaneHasClosed() async throws {
        let (model, option) = try await review()
        let stops = model.orderedStops(option)

        model.openStopDetail(stops[1].id, in: option.id)
        let swapTarget = try #require(model.stopDetail)
        model.swapFromStopDetail()
        #expect(model.stopDetail == nil && model.swapTarget == nil)
        #expect(model.takeStopDetailAction() == .swap(swapTarget))
        #expect(model.takeStopDetailAction() == nil)

        model.openStopDetail(stops[2].id, in: option.id)
        let removeTarget = try #require(model.stopDetail)
        model.removeFromStopDetail()
        #expect(model.stopDetail == nil && model.orderedStops(option).count == 3)
        #expect(model.takeStopDetailAction() == .remove(removeTarget))

        // Closing the pane any other way asks for nothing.
        model.openStopDetail(stops[0].id, in: option.id)
        model.stopDetail = nil
        #expect(model.takeStopDetailAction() == nil)
    }

    @Test func removeIsOffOnTheLastStop() async throws {
        let (model, option) = try await review()
        let stops = model.orderedStops(option)
        model.removeStop(stops[2].id, in: option.id)
        model.removeStop(stops[1].id, in: option.id)
        #expect(model.orderedStops(option).map(\.id) == [stops[0].id] && !model.canRemoveStop(in: option.id))

        model.openStopDetail(stops[0].id, in: option.id)
        model.removeFromStopDetail()
        #expect(model.stopDetail != nil && model.takeStopDetailAction() == nil)
        #expect(model.orderedStops(option).count == 1)
    }

    // MARK: The demo backend

    /// Every stop the demo's options, swaps and must-see picks can put on Review has details.
    @Test func demoHasDetailsForEveryStopItCanShow() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let options = MockData.firstOptions + MockData.moreOptionBatches.flatMap { $0 }
        let planned = options.flatMap(\.stops)
        let swaps = options.flatMap { option in
            option.stops.flatMap { MockAlternatives.alternatives(for: $0, excluding: [], limit: 50).map(\.stop) }
        }
        let picks = MockActivities.all.map { MockPicks.stop(for: $0, in: "opt-a") }
        for stop in planned + swaps + picks {
            let id = try #require(stop.activityId, "\(stop.title) has no activity")
            let detail = try await api.activity(id: id, date: clock.now)
            #expect(detail.title == stop.title && !detail.categoryLabel.isEmpty && !detail.priceLabel.isEmpty, "\(stop.title)")
            #expect(detail.summary?.isEmpty == false && detail.address?.isEmpty == false, "\(stop.title) has no words")
        }
        // Events keep their times and no opening hours; the Forum dinner is an event without one.
        let trivia = try await api.activity(id: MockActivities.id(for: "Rooftop trivia"), date: nil)
        #expect(trivia.kind == .event && trivia.start == clock.date(2026, 9, 25, 15, 0) && trivia.hoursLine == nil)
        #expect(trivia.priceLabel == "$5" && trivia.ticketURL != nil && trivia.venueName == "Skyline Park rooftop")
        let dinner = try await api.activity(id: MockActivities.id(for: MockActivityDetails.forumDinner), date: nil)
        #expect(dinner.kind == .event && dinner.start == nil && dinner.categoryLabel == "Group dinner")
    }
}

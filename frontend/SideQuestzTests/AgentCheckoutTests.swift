import Foundation
import Testing
@testable import SideQuestz

/// Agentic checkout payloads as the Go backend writes them (pkg/contract/checkout.go).
@MainActor
struct AgentCheckoutTests {
    private let decoder = APICoding.decoder(timeZone: TimeZone(identifier: "America/New_York")!)

    @Test func checkoutPlanDecodes() throws {
        let json = """
        {"itinerary_id":"it1","available":true,"agentic_checkout":true,
         "items":[{"item_id":"s1","title":"Sunset Jazz on Pier Nine","start":"2026-09-26T19:00:00-04:00",
                   "merchant":"events.sidequestz.tech","ticket_url":"https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets",
                   "price_cents":2400,"quantity":1,"booked":false},
                  {"item_id":"s2","title":"Harbor Lights Cruise","start":"2026-09-26T21:00:00-04:00",
                   "merchant":"events.sidequestz.tech","ticket_url":"https://events.sidequestz.tech/harbor-lights-cruise/tickets",
                   "quantity":2,"booked":true,"confirmation":"SQZ-7KD4Q2"}],
         "estimate_cents":2400,"default_budget_cents":5000,"suggested_budget_cents":5000,
         "payment_method_id":"pm1","card_brand":"Visa","card_last4":"4242"}
        """
        let plan = try decoder.decode(CheckoutPlan.self, from: Data(json.utf8))
        #expect(plan.items.count == 2)
        #expect(plan.items[0].ticketURL?.host == "events.sidequestz.tech")
        #expect(plan.items[1].priceCents == nil)
        #expect(plan.buyable.map(\.itemId) == ["s1"])
        #expect(plan.shouldPrompt)
        #expect(plan.activeRunId == nil)
    }

    @Test func checkoutPlanWithoutCardDoesNotPrompt() throws {
        let json = #"{"itinerary_id":"it1","available":true,"agentic_checkout":true,"items":[{"item_id":"s1","title":"A","start":"2026-09-26T19:00:00Z","merchant":"m","ticket_url":"https://events.sidequestz.tech/a/tickets","quantity":1,"booked":false}],"estimate_cents":0,"default_budget_cents":5000,"suggested_budget_cents":5000}"#
        let plan = try decoder.decode(CheckoutPlan.self, from: Data(json.utf8))
        #expect(!plan.shouldPrompt)
    }

    @Test func checkoutRunDecodesWithIntents() throws {
        let json = """
        {"id":"r1","itinerary_id":"it1","state":"done","budget_cents":6000,"spent_cents":2642,"currency":"usd",
         "card_brand":"Visa","card_last4":"4242","agent":"muse","summary":"Got tickets for Sunset Jazz.",
         "created_at":"2026-09-26T18:00:00.123456Z","finished_at":"2026-09-26T18:01:00Z",
         "intents":[
          {"id":"c1","item_id":"s1","item_title":"Sunset Jazz","steps":[{"text":"Paid $26.42","done":true}],
           "subtotal_cents":2400,"fees_cents":242,"total_cents":2642,"card_brand":"Visa","card_last4":"4242",
           "state":"booked","quantity":1,"instant":false,"run_id":"r1","merchant":"events.sidequestz.tech",
           "checkout_url":"https://events.sidequestz.tech/sunset-jazz/tickets","max_authorized_cents":2642,
           "final_cents":2642,"order_ref":"ord_1","confirmation":"SQZ-7KD4Q2",
           "ticket_url":"https://events.sidequestz.tech/tickets/tk_1"},
          {"id":"c2","item_id":"s2","item_title":"Harbor Cruise","steps":[],"card_brand":"Visa","card_last4":"4242",
           "state":"failed","quantity":2,"instant":false,"run_id":"r1","failure_code":"over_budget"}
         ]}
        """
        let run = try decoder.decode(CheckoutRun.self, from: Data(json.utf8))
        #expect(!run.isRunning)
        #expect(run.booked.map(\.id) == ["c1"])
        #expect(run.booked[0].ticketURL?.absoluteString == "https://events.sidequestz.tech/tickets/tk_1")
        #expect(run.booked[0].checkoutURL != nil)
        #expect(run.booked[0].confirmation == "SQZ-7KD4Q2")
        #expect(run.notBooked[0].outcomeLine == "Over your budget.")
    }

    @Test func unknownRunStateReadsAsRunning() throws {
        let state = try decoder.decode([CheckoutRunState].self, from: Data(#"["paused"]"#.utf8))
        #expect(state == [.running])
    }

    @Test func itineraryItemKeepsTicketURL() throws {
        let json = #"{"id":"s1","kind":"sidequest","title":"Jazz","start":"2026-09-26T19:00:00Z","end":"2026-09-26T21:00:00Z","ticket_url":"https://events.sidequestz.tech/jazz/tickets"}"#
        let item = try decoder.decode(ItineraryItem.self, from: Data(json.utf8))
        #expect(item.ticketURL?.path == "/jazz/tickets")
    }

    @Test func createRunEncodesSnakeCase() throws {
        let body = CreateCheckoutRun(budgetCents: 6000, items: [.init(itemId: "s1", quantity: 2)], paymentMethodId: nil)
        let object = try JSONSerialization.jsonObject(with: APICoding.encoder().encode(body)) as? [String: Any]
        #expect(object?["budget_cents"] as? Int == 6000)
        #expect((object?["items"] as? [[String: Any]])?.first?["item_id"] as? String == "s1")
    }

    /// The offline demo: a saved plan with paid stops, one approval, Muse books what fits the budget.
    @Test func mockRunBooksWithinBudget() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let clock = AppClock.demo
        let request = PlanRequest(start: MockPlaces.techSquare.place, end: MockPlaces.home.place, date: clock.now,
                                  startTime: clock.date(2026, 9, 25, 14, 10), backBy: clock.date(2026, 9, 25, 18, 30),
                                  range: .transit, ride: RideChoice.none, openSeats: nil, moodText: "Live music",
                                  tags: ["Music"], budget: 2, who: .justMe, pace: .balanced, modes: [.walk, .marta])
        let batch = try await api.generatePlans(request)
        let option = try #require(batch.options.first { $0.stops.filter { $0.subtitle.contains("$") }.count >= 2 })
        let order = option.stops.map(\.id)
        let route = try await api.route(RouteRequest(optionId: option.id, stopOrder: order, start: request.start, end: request.end,
                                                     startTime: request.startTime, backBy: request.backBy, ride: request.ride, modes: request.modes))
        let itinerary = try await api.createItinerary(CreateItineraryRequest(plan: request, option: option, stopOrder: order, route: route,
                                                                             visibility: .justMe, lockAt: nil, maxGroupSize: nil))
        let plan = try await api.checkoutPlan(itineraryId: itinerary.id)
        #expect(!plan.buyable.isEmpty)
        let items = plan.buyable.map { CreateCheckoutRun.Item(itemId: $0.itemId, quantity: 1) }
        let started = try await api.startCheckoutRun(itineraryId: itinerary.id, .init(budgetCents: 2500, items: items, paymentMethodId: nil))
        #expect(started.isRunning)
        var run = started
        for _ in 0..<50 where run.isRunning {
            try await Task.sleep(for: .milliseconds(20))
            run = try await api.checkoutRun(id: started.id)
        }
        #expect(run.state == .done)
        #expect(run.spentCents <= 2500)
        #expect(run.booked.count == 1, "one $18 ticket plus fees fits $25; the rest doesn't")
        #expect(run.booked.allSatisfy { $0.confirmation != nil && $0.ticketURL != nil })
        #expect(run.summary?.isEmpty == false)
    }
}

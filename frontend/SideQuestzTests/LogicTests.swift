import Foundation
import Testing
@testable import SideQuestz

/// "The split preview always adds up to the total, to the cent."
@MainActor
struct SplitMathTests {
    @Test func sharesAlwaysAddUpToTheTotal() {
        for total in [0, 1, 2, 99, 100, 101, 1333, 4000, 4001, 9999, 123_457] {
            for count in 1...9 {
                let shares = SplitMath.equalShares(totalCents: total, count: count)
                #expect(shares.count == count)
                #expect(shares.reduce(0, +) == total)
                #expect((shares.max() ?? 0) - (shares.min() ?? 0) <= 1)
            }
        }
    }

    @Test func firstPeoplePayTheExtraCent() {
        #expect(SplitMath.equalShares(totalCents: 4000, count: 3) == [1334, 1333, 1333])
        #expect(SplitMath.extraCentCount(totalCents: 4000, count: 3) == 1)
        #expect(SplitMath.equalShares(totalCents: 4200, count: 3) == [1400, 1400, 1400])
    }

    @Test func demoGroupBalances() {
        // Krog St dinner crew: Dumplings $42 (Dev), MARTA $7.50 (you), 3 people.
        let me = MockPeople.me.id, maya = MockPeople.maya.id, dev = MockPeople.dev.id
        let members = [me, maya, dev]
        let expenses = [
            Expense(id: "1", what: "Dumplings", amountCents: 4200, payerId: dev, splitAmong: members, shares: [1400, 1400, 1400]),
            Expense(id: "2", what: "MARTA fares", amountCents: 750, payerId: me, splitAmong: members, shares: [250, 250, 250]),
        ]
        let balances = SplitMath.balances(expenses: expenses, me: me, members: members)
        #expect(balances.first { $0.userId == dev }?.netCents == -1150)   // You owe Dev $11.50
        #expect(balances.first { $0.userId == maya }?.netCents == 250)    // Maya owes you $2.50
        #expect(balances.reduce(0) { $0 + $1.netCents } == -900)          // You owe $9.00
    }

    /// Settle up pays your debts, not the net: in the Krog crew you owe Dev $11.50 while Maya owes
    /// you $2.50 ("You owe $9.00"), so the button pays $11.50. The server (and the mock) refuse any
    /// other amount as stale, which is what the net used to get.
    @Test func settleUpPaysYourDebtsNotTheNet() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let ledger = try await api.ledger(groupId: "g1")
        #expect(ledger.netCents == -900)
        #expect(ledger.owedCents == 1150)
        await #expect(throws: APIError.self) {
            try await api.settleUp(groupId: "g1", amountCents: -ledger.netCents, paymentMethodId: nil)
        }
        try await api.settleUp(groupId: "g1", amountCents: ledger.owedCents, paymentMethodId: nil)
        let after = try await api.ledger(groupId: "g1")
        #expect(after.owedCents == 0)
        #expect(after.netCents == 250)   // Maya still owes you $2.50
    }

    @Test func moneyFormatting() {
        #expect(Money.format(900) == "$9.00")
        #expect(Money.compact(900) == "$9")
        #expect(Money.compact(250) == "$2.50")
        #expect(Money.parseCents("40") == 4000)
        #expect(Money.parseCents("$13.335") == 1334)
        #expect(Money.orPlaceholder(nil) == "$[price]")
    }
}

/// "Reordering stops re-times the route and flags lateness."
@MainActor
struct RouteTimingTests {
    let clock = AppClock.demo

    private func request(order: [Int]) -> (stops: [PlanStop], start: Date, backBy: Date) {
        let option = MockData.firstOptions[0]
        return (order.map { option.stops[$0] }, clock.date(2026, 9, 25, 14, 10), clock.date(2026, 9, 25, 18, 30))
    }

    @Test func defaultRouteMatchesThePrototype() {
        let r = request(order: [0, 1, 2])
        let result = MockRouteEngine.route(stops: r.stops, start: MockPlaces.techSquare.place, end: MockPlaces.home.place,
                                           startTime: r.start, backBy: r.backBy, ride: .none)
        let format = TimeFormat(clock: clock)
        #expect(result.legs.map(\.mode) == [.marta, .marta, .walk, .marta])
        #expect(result.legs.map(\.minutes) == [14, 14, 3, 21])
        #expect(format.fullRange(result.stopTimes[0].start, result.stopTimes[0].end) == "2:24 PM–3:44 PM")
        #expect(format.fullRange(result.stopTimes[1].start, result.stopTimes[1].end) == "3:58 PM–4:58 PM")
        #expect(format.time(result.arrival) == "5:57 PM")
        #expect(result.minutesLate == 0)
    }

    @Test func reorderingRetimesAndCanRunLate() {
        let original = request(order: [0, 1, 2])
        let reordered = request(order: [2, 0, 1])
        let a = MockRouteEngine.route(stops: original.stops, start: MockPlaces.techSquare.place, end: MockPlaces.home.place,
                                      startTime: original.start, backBy: original.backBy, ride: .none)
        let b = MockRouteEngine.route(stops: reordered.stops, start: MockPlaces.techSquare.place, end: MockPlaces.home.place,
                                      startTime: reordered.start, backBy: reordered.backBy, ride: .none)
        #expect(a.stopTimes != b.stopTimes)
        #expect(b.arrival > a.arrival)
        // A tight window must be flagged late.
        let tight = MockRouteEngine.route(stops: original.stops, start: MockPlaces.techSquare.place, end: MockPlaces.home.place,
                                          startTime: original.start, backBy: clock.date(2026, 9, 25, 17, 0), ride: .none)
        #expect(tight.minutesLate == 57)
    }

    @Test func rideChoiceChangesLegModes() {
        let r = request(order: [0, 1, 2])
        let driving = MockRouteEngine.route(stops: r.stops, start: MockPlaces.techSquare.place, end: MockPlaces.home.place,
                                            startTime: r.start, backBy: r.backBy, ride: .drive)
        #expect(driving.legs.map(\.mode) == [.drive, .drive, .walk, .drive])
    }
}

@MainActor
struct FormattingTests {
    let format = TimeFormat(clock: .demo)
    let clock = AppClock.demo

    @Test func prototypeTimeFormats() {
        #expect(format.range(clock.date(2026, 9, 25, 14, 30), clock.date(2026, 9, 25, 16, 0)) == "2:30–4:00 PM")
        #expect(format.range(clock.date(2026, 9, 25, 11, 0), clock.date(2026, 9, 25, 13, 0)) == "11:00 AM–1:00 PM")
        #expect(format.compactRange(clock.date(2026, 9, 25, 13, 0), clock.date(2026, 9, 25, 20, 0)) == "1–8 PM")
        #expect(format.compactRange(clock.date(2026, 9, 26, 13, 0), clock.date(2026, 9, 26, 19, 30)) == "1–7:30 PM")
        #expect(format.eyebrowDate(clock.now) == "FRIDAY, SEPTEMBER 25")
        #expect(format.dayTitle(clock.now) == "Friday, Sep 25")
        #expect(format.shortDate(clock.now) == "Fri, Sep 25")
        #expect(format.hourLabel(13) == "1 PM")
        #expect(format.hourLabel(6) == "6 AM")
    }
}

@MainActor
struct ValidationTests {
    @Test func emailAndPasswordRules() {
        #expect(Validation.isValidEmail("jordan@gatech.edu"))
        #expect(!Validation.isValidEmail("jordan@gatech"))
        #expect(!Validation.passwordRulesPass("short1", "short1"))
        #expect(!Validation.passwordRulesPass("longenough", "longenough"))
        #expect(Validation.passwordRulesPass("longenough1", "longenough1"))
        #expect(!Validation.passwordRulesPass("longenough1", "longenough2"))
    }

    @Test func ageNotes() {
        #expect(Validation.ageNote(age: nil).hasPrefix("Only used to recommend"))
        #expect(Validation.ageNote(age: 12) == "You need to be 13 or older to use SideQuests.")
        #expect(Validation.ageNote(age: 16).hasPrefix("Age 16: we'll only suggest all-ages events"))
        #expect(Validation.ageNote(age: 19).hasPrefix("Age 19: we'll hide 21+ events"))
        #expect(Validation.ageNote(age: 22).hasPrefix("Age 22: 21+ events can show up"))
    }
}

@MainActor
struct MockAPITests {
    @Test func forumDefaultsAndFilters() async throws {
        let api = MockAPIClient(latencyScale: 0)
        // "For you" by default: best taste match first, unscored free-now posts after.
        let all = try await api.forumPosts(ForumQuery())
        #expect(all.map(\.id) == ["p1", "p3", "p5", "p2", "p4"])
        var soonest = ForumQuery()
        soonest.sort = .soonest
        #expect(try await api.forumPosts(soonest).map(\.id) == ["p2", "p4", "p1", "p3", "p5"])
        var friends = ForumQuery()
        friends.scope = .friends
        #expect(try await api.forumPosts(friends).map(\.id) == ["p1", "p5", "p2"])
        var open = ForumQuery()
        open.openOnly = true
        open.sort = .closest
        #expect(try await api.forumPosts(open).map(\.id) == ["p3", "p1", "p5"])
    }

    @Test func addingAnExpenseUpdatesTheLedger() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let before = try await api.ledger(groupId: "g1")
        #expect(before.netCents == -900)
        let members = before.members.map(\.id)
        let saved = try await api.addExpense(groupId: "g1", NewExpense(what: "Pizza", amountCents: 4000, payerId: MockPeople.me.id, splitAmong: members))
        #expect(saved.shares == [1334, 1333, 1333])
        let after = try await api.ledger(groupId: "g1")
        #expect(after.netCents == -900 + 2666)
    }

    /// Edit sidequest: only the sent fields change; a new stop order is re-timed around calendar
    /// blocks and mirrored on the calendar.
    @Test func editingASidequestRetimesItsStops() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let clock = AppClock.demo
        let original = try #require(try await api.activeItineraries().first { $0.id == "itin-fri" })
        let stops = { (itin: Itinerary) in itin.items.filter { $0.kind == .sidequest || $0.kind == .group }.map(\.id) }
        #expect(stops(original) == ["a3", "a5", "a6"])

        let renamed = try await api.updateItinerary(id: "itin-fri", ItineraryUpdate(title: "Rooftop evening"))
        #expect(renamed.title == "Rooftop evening")
        #expect(renamed.items == original.items)

        let reordered = try await api.updateItinerary(id: "itin-fri", ItineraryUpdate(stopOrder: ["a5", "a3"]))
        #expect(stops(reordered) == ["a5", "a3"])
        let times = reordered.items.filter { $0.kind != .busy }
        for (earlier, later) in zip(times, times.dropFirst()) { #expect(earlier.end <= later.start) }
        let lecture = try #require(reordered.items.first { $0.id == "a1" })
        #expect(!reordered.items.contains { $0.kind != .busy && $0.start < lecture.end && $0.end > lecture.start })
        let calendar = try await api.calendarDays(from: clock.now, to: clock.now).first?.items ?? []
        #expect(!calendar.contains { $0.id == "a6" })
        #expect(calendar.first { $0.id == "a5" }?.start == reordered.items.first { $0.id == "a5" }?.start)

        await #expect(throws: APIError.self) { try await api.updateItinerary(id: "itin-fri", ItineraryUpdate(stopOrder: [])) }
        await #expect(throws: APIError.self) { try await api.leaveItinerary(id: "itin-fri") }
        try await api.deleteItinerary(id: "itin-fri")
        #expect(try await api.activeItineraries().allSatisfy { $0.id != "itin-fri" })
    }
}

/// Facebook: the backend's redirect is read the same way every time, and suggested ratings never
/// replace a rating you picked unless you've reviewed the change.
@MainActor
struct FacebookTests {
    @Test func callbackStatuses() {
        let url = { (query: String) in URL(string: "sidequestz://integrations/facebook?\(query)")! }
        #expect(FacebookConnector.outcome(of: url("status=connected")) == .connected)
        #expect(FacebookConnector.outcome(of: url("status=denied")) == .declined)
        #expect(FacebookConnector.outcome(of: url("status=error&message=Facebook%20is%20down.")) == .failed("Facebook is down."))
        #expect(FacebookConnector.outcome(of: url("status=error&message=%20")) == .failed(nil))
        #expect(FacebookConnector.outcome(of: URL(string: "sidequestz://integrations/facebook")!) == .failed(nil))
    }

    @Test func suggestionsFillOnlyUnratedUnlessOverwriting() {
        var saved = Preferences()
        saved.ratings = [.outdoors: 5, .food: 4, .museums: 3]
        let suggestions: [TripType: Int] = [.outdoors: 5, .food: 5, .liveMusic: 5, .sports: 3, .nightlife: 9]

        let filled = saved.merging(suggestions, overwrite: false)
        #expect(filled.changed == [.liveMusic, .sports])
        #expect(filled.preferences.ratings == [.outdoors: 5, .food: 4, .museums: 3, .liveMusic: 5, .sports: 3])

        let replaced = saved.merging(suggestions, overwrite: true)
        #expect(replaced.changed == [.food, .liveMusic, .sports])
        #expect(replaced.preferences.ratings[.food] == 5)
        #expect(replaced.preferences.ratings[.nightlife] == nil)
    }

    @Test func demoConnectImportAndDisconnect() async throws {
        let api = MockAPIClient(latencyScale: 0)
        #expect(try await api.facebookConnection().connected == false)
        await #expect(throws: APIError.self) { try await api.importFacebook() }

        _ = try await api.facebookConnectURL(rerequest: false)
        let imported = try await api.importFacebook()
        #expect(imported.likedPages == 48)
        #expect(imported.friendsOnApp.map(\.relation) == [.none, .incoming])

        _ = try await api.sendFriendRequest(userId: MockPeople.priya.id)
        let connection = try await api.facebookConnection()
        #expect(connection.connected)
        #expect(connection.lastImport?.friendsOnApp.first?.relation == .outgoing)

        try await api.disconnectFacebook()
        let after = try await api.facebookConnection()
        #expect(!after.connected && after.lastImport == nil)
    }
}

/// Review › hold a stop › Swap: the demo backend suggests the same kind of place, never one that's
/// already in the plan, and its routes take alternatives and shorter orders (after a removal).
@MainActor
struct StopSwapTests {
    private let clock = AppClock.demo

    private func plan(_ api: MockAPIClient) async throws -> (PlanRequest, PlanOption) {
        let request = PlanRequest(start: MockPlaces.techSquare.place, end: MockPlaces.home.place, date: clock.now,
                                  startTime: clock.date(2026, 9, 25, 14, 10), backBy: clock.date(2026, 9, 25, 18, 30),
                                  range: .transit, ride: RideChoice.none, openSeats: nil, moodText: "", tags: [],
                                  budget: 1, who: .friends, pace: .balanced, modes: [.walk, .marta])
        let option = try #require(try await api.generatePlans(request).options.first)
        return (request, option)
    }

    @Test func alternativesAreSimilarAndNotAlreadyPlanned() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let (_, option) = try await plan(api)
        let murals = option.stops[1]
        #expect(murals.title == "Krog Street Tunnel murals")

        let found = try await api.stopAlternatives(optionId: option.id, stopId: murals.id, stopOrder: option.stops.map(\.id))
        #expect(!found.isEmpty && found.count <= 4)
        #expect(found.first?.stop.title == "Cabbagetown murals")   // the nearest art
        let planned = Set(option.stops.map(\.title))
        for alternative in found {
            #expect(MockAlternatives.kinds(of: alternative.stop).contains(.art))
            #expect(!planned.contains(alternative.stop.title))
            #expect(alternative.reason.hasPrefix("Also art to see"))
        }
        #expect(Set(found.map(\.id)).count == found.count)
    }

    @Test func routesTakeAlternativesAndRemovals() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let (request, option) = try await plan(api)
        let ids = option.stops.map(\.id)
        let swapIn = try #require(try await api.stopAlternatives(optionId: option.id, stopId: ids[1], stopOrder: ids).first)
        func route(_ order: [String]) async throws -> RouteResult {
            try await api.route(RouteRequest(optionId: option.id, stopOrder: order, start: request.start, end: request.end,
                                             startTime: request.startTime, backBy: request.backBy, ride: request.ride,
                                             modes: request.modes))
        }

        let swapped = try await route([ids[0], swapIn.id, ids[2]])
        #expect(swapped.stopTimes.count == 3 && swapped.legs.count == 4)
        #expect(swapped.stopTimes[1].duration == TimeInterval(swapIn.stop.durationMinutes * 60))

        let removed = try await route([ids[0], ids[2]])
        #expect(removed.stopTimes.count == 2 && removed.legs.count == 3)
        #expect(removed.arrival < (try await route(ids)).arrival)
    }
}

/// The loading mark: the S draws from the diamond to the circle, holds, erases the same way, and
/// the loop starts and ends with nothing drawn (no jump).
@MainActor
struct LogoLoaderTests {
    @Test func drawsThenErasesWithoutJumping() {
        let cycle = LogoLoadingView.cycle
        let at = { (fraction: Double) in LogoLoadingView.trim(at: cycle * (3 + fraction)) }
        #expect(at(0).start == 0 && at(0).end == 0)
        #expect(at(0.2).start == 0 && at(0.2).end > 0 && at(0.2).end < 1)     // drawing
        #expect(at(0.47) == (start: 0, end: 1))                                 // fully drawn
        #expect(at(0.7).start > 0 && at(0.7).start < 1 && at(0.7).end == 1)   // erasing
        #expect(at(0.97) == (start: 1, end: 1))                                 // gone
        // Drawing only grows, erasing only shrinks.
        let draws = stride(from: 0.0, through: 0.45, by: 0.05).map { at($0).end }
        #expect(draws == draws.sorted())
        let erases = stride(from: 0.5, through: 0.95, by: 0.05).map { at($0).start }
        #expect(erases == erases.sorted())
    }
}

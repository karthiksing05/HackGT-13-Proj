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
        #expect(Validation.ageNote(age: 12) == "You need to be 13 or older to use SideQuestz.")
        #expect(Validation.ageNote(age: 16).hasPrefix("Age 16: we'll only suggest all-ages events"))
        #expect(Validation.ageNote(age: 19).hasPrefix("Age 19: we'll hide 21+ events"))
        #expect(Validation.ageNote(age: 22).hasPrefix("Age 22: 21+ events can show up"))
    }
}

@MainActor
struct MockAPITests {
    @Test func forumDefaultsAndFilters() async throws {
        let api = MockAPIClient(latencyScale: 0)
        let all = try await api.forumPosts(ForumQuery())
        #expect(all.map(\.id) == ["p2", "p4", "p1", "p3", "p5"])
        var friends = ForumQuery()
        friends.scope = .friends
        #expect(try await api.forumPosts(friends).map(\.id) == ["p2", "p1", "p5"])
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
}

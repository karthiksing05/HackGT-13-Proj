import Foundation
import Testing
@testable import SideQuestz

/// Create › Bring friends: the picker's selection and search, how picks move Who's coming and the
/// group size, `invite_user_ids` on the request, and the demo server bringing the friends along.
@MainActor
struct CreateInviteTests {
    private let clock = AppClock.demo
    private let decoder = APICoding.decoder(timeZone: AppClock.demo.timeZone)
    private let encoder = APICoding.encoder()
    private let maya = MockPeople.maya, dev = MockPeople.dev, sam = MockPeople.sam, ava = MockPeople.ava

    /// Create › Vibe in the demo (Who's coming starts at "Friends only").
    private func vibe(_ env: AppEnvironment? = nil) -> CreateFlowModel {
        CreateFlowModel(draft: CreateDraft(step: 3), env: env ?? .preview())
    }

    /// People who aren't in the demo, for the caps.
    private func strangers(_ count: Int) -> [PersonRef] {
        (1...count).map { PersonRef(id: "u-x\($0)", name: "Person \($0)", initials: "P\($0)", colorHex: "#18211C") }
    }

    // MARK: Picker

    @Test func pickingKeepsTheOrderAndTogglesOff() async {
        let model = vibe()
        #expect(model.friends.isLoading)
        await model.loadFriends()
        #expect(model.friends.value?.map(\.person) == MockData.friends().map(\.person))
        model.toggleInvite(dev)
        model.toggleInvite(maya)
        #expect(model.invitees == [dev, maya])
        #expect(model.isInvited(maya.id) && !model.isInvited(sam.id))
        model.toggleInvite(dev)
        #expect(model.invitees == [maya])
    }

    @Test func searchFindsFriendsByNameOrHandle() async {
        let model = vibe()
        await model.loadFriends()
        #expect(model.friendResults.count == 4)
        model.friendQuery = "ma"
        #expect(model.friendResults.map(\.person) == [maya])
        model.friendQuery = "  @DEV "
        #expect(model.friendResults.map(\.person) == [dev])
        model.friendQuery = "zz"
        #expect(model.friendResults.isEmpty)
        // Picks stay while the search changes.
        model.toggleInvite(sam)
        model.friendQuery = "ava"
        #expect(model.friendResults.map(\.person) == [ava] && model.invitees == [sam])
    }

    @Test func friendsThatCantLoadCanBeTriedAgain() async throws {
        let env = AppEnvironment.preview()
        let api = try #require(env.api as? MockAPIClient)
        let model = vibe(env)
        api.failing = ["friends"]
        await model.loadFriends()
        guard case .failed = model.friends else {
            Issue.record("expected a failure, got \(model.friends.phase)")
            return
        }
        api.failing = []
        await model.loadFriends(force: true)
        #expect(model.friends.value?.count == 4)
        // Once they're in, reopening doesn't load them again (and a failed reload keeps them).
        api.failing = ["friends"]
        await model.loadFriends()
        await model.loadFriends(force: true)
        #expect(model.friends.value?.count == 4)
    }

    @Test func twelveFriendsAtMost() {
        let model = vibe()
        let people = strangers(13)
        for person in people { model.toggleInvite(person) }
        #expect(model.invitees == Array(people.prefix(12)) && !model.canInviteMore)
        #expect(!model.isInvited(people[12].id))
        model.toggleInvite(people[0])
        #expect(model.canInviteMore)
    }

    // MARK: Who's coming

    @Test func pickingWhileJustMeSwitchesToFriendsOnly() {
        let model = vibe()
        model.chooseWho(.justMe)
        #expect(model.who == .justMe && model.whoNote == Visibility.justMe.note)
        model.toggleInvite(maya)
        #expect(model.who == .friends)
        #expect(model.whoNote == "Switched to Friends only so your friends can come.")
        model.toggleInvite(dev)
        #expect(model.who == .friends && model.whoNotice != nil)
        // Taking everyone off doesn't switch back; the choice's own note returns.
        model.toggleInvite(maya)
        model.toggleInvite(dev)
        #expect(model.invitees.isEmpty && model.who == .friends && model.whoNote == Visibility.friends.note)
        // Open to all keeps its picks, with nothing to explain.
        model.chooseWho(.open)
        model.toggleInvite(maya)
        #expect(model.who == .open && model.whoNote == Visibility.open.note && model.invitees == [maya])
    }

    @Test func justMeTakesTheFriendsOff() {
        let model = vibe()
        model.toggleInvite(maya)
        model.toggleInvite(dev)
        model.chooseWho(.justMe)
        #expect(model.who == .justMe && model.invitees.isEmpty)
        #expect(model.whoNote == "Just you now, so Maya and Dev were taken off.")
        model.chooseWho(.friends)
        #expect(model.whoNote == Visibility.friends.note)
        model.toggleInvite(sam)
        model.chooseWho(.justMe)
        #expect(model.whoNote == "Just you now, so Sam was taken off.")
        // Friends only and Open to all keep the picks.
        model.toggleInvite(ava)
        model.chooseWho(.open)
        #expect(model.invitees == [ava] && model.whoNotice == nil)
    }

    /// A live account's preferences can land after Create opened: a solo default doesn't make a plan
    /// with friends "Just me".
    @Test func aLateSoloPreferenceKeepsTheFriends() {
        let auth = AuthStore(service: "tests.createInvite")
        let env = AppEnvironment(mode: .live, clock: clock, api: MockAPIClient(clock: clock, latencyScale: 0), auth: auth,
                                 socketURL: nil, forceVoiceDemo: true)
        var prefs = MockData.preferences
        env.preferences = prefs
        let model = vibe(env)
        #expect(model.who == .friends)
        model.toggleInvite(maya)
        prefs.company = .solo
        env.preferences = prefs
        model.applyPreferenceDefaults()
        #expect(model.who == .friends && model.invitees == [maya])
    }

    // MARK: Group size

    @Test func theGroupFitsEveryonePicked() {
        let model = vibe()
        #expect(model.maxGroupSize == 6 && model.minGroupSize == 2)
        for person in [maya, dev, sam, ava] { model.toggleInvite(person) }
        #expect(model.maxGroupSize == 6 && model.minGroupSize == 5)
        model.changeMaxGroupSize(by: -1)
        model.changeMaxGroupSize(by: -1)
        #expect(model.maxGroupSize == 5, "never below you and your friends")
        // More friends than fit: the group grows with them.
        for person in strangers(3) { model.toggleInvite(person) }
        #expect(model.invitees.count == 7 && model.maxGroupSize == 8 && model.minGroupSize == 8)
        model.toggleInvite(maya)
        model.changeMaxGroupSize(by: -1)
        #expect(model.maxGroupSize == 7)
    }

    // MARK: Lines

    @Test func namesReadLikeASentence() {
        #expect(CreateFlowModel.names([]) == "")
        #expect(CreateFlowModel.names([maya]) == "Maya")
        #expect(CreateFlowModel.names([maya, dev]) == "Maya and Dev")
        #expect(CreateFlowModel.names([maya, dev, sam]) == "Maya, Dev and Sam")
        #expect(CreateFlowModel.names([maya, dev, sam], limit: 3) == "Maya, Dev and Sam")
        #expect(CreateFlowModel.names([maya, dev, sam, ava], limit: 3) == "Maya, Dev and 2 others")
        #expect(CreateFlowModel.names([maya, dev, sam, ava, MockPeople.chris], limit: 3) == "Maya, Dev and 3 others")
        #expect(MockInvites.addedLine(host: "Jordan", friends: ["Maya"]) == "Jordan added Maya")
        #expect(MockInvites.addedLine(host: "Jordan", friends: ["Maya", "Dev", "Sam"]) == "Jordan added Maya, Dev and Sam")
    }

    // MARK: Wire

    /// A saved plan's request: one stop, walking there and back.
    private func request(inviting ids: [String] = []) -> CreateItineraryRequest {
        let plan = PlanRequest(start: MockPlaces.techSquare.place, end: MockPlaces.home.place, date: clock.now,
                               startTime: clock.date(2026, 9, 25, 14, 10), backBy: clock.date(2026, 9, 25, 18, 30),
                               range: .transit, ride: RideChoice.none, openSeats: nil, moodText: "", tags: [], budget: 1,
                               who: .friends, pace: .balanced, modes: [.walk, .marta])
        let stop = PlanStop(id: "s1", title: "Skyline Park rooftop", subtitle: "Games + views · $", place: MockPlaces.ponce.place,
                            durationMinutes: 80)
        let route = RouteResult(legs: [Leg(mode: .walk, minutes: 10), Leg(mode: .walk, minutes: 10)],
                                stopTimes: [DateInterval(start: clock.date(2026, 9, 25, 14, 20), end: clock.date(2026, 9, 25, 15, 40))],
                                arrival: clock.date(2026, 9, 25, 15, 50), minutesLate: 0)
        return CreateItineraryRequest(plan: plan, option: PlanOption(id: "opt-a", name: "Rooftop", tag: "", meta: "", stops: [stop]),
                                      stopOrder: ["s1"], route: route, visibility: .friends, lockAt: nil, maxGroupSize: 6,
                                      inviteUserIds: ids)
    }

    @Test func inviteUserIdsAreSentOnlyWithFriends() throws {
        let plain = request()
        #expect(!String(decoding: try encoder.encode(plain), as: UTF8.self).contains("invite_user_ids"))
        // Older bodies (no `invite_user_ids`) still decode, with nobody brought along.
        #expect(try decoder.decode(CreateItineraryRequest.self, from: encoder.encode(plain)) == plain)

        let inviting = request(inviting: [dev.id, maya.id])
        let object = try #require(try JSONSerialization.jsonObject(with: encoder.encode(inviting)) as? [String: Any])
        #expect(object["invite_user_ids"] as? [String] == ["u-dp", "u-mr"])
        #expect(try decoder.decode(CreateItineraryRequest.self, from: encoder.encode(inviting)) == inviting)
    }

    // MARK: The demo server

    /// Vibe › Bring friends → Review → Start: the saved plan has them going, Home lists it that
    /// way, and its group chat opens with your line. Deleting the plan takes the chat with it.
    @Test func startBringsThemAlong() async throws {
        let env = AppEnvironment.preview()
        let model = vibe(env)
        model.toggleInvite(maya)
        model.toggleInvite(dev)
        await model.enterReview()
        let started = await model.startSidequest()
        let saved = try #require(started)
        #expect(saved.goingCount == 3 && saved.visibility == .friends && saved.peopleLabel == "3 going")
        let stops = saved.items.filter { $0.kind != .transit }
        #expect(!stops.isEmpty && stops.allSatisfy { $0.kind == .group && $0.people == [MockPeople.me, maya, dev] })

        let home = try await env.api.activeItineraries()
        #expect(home.first?.id == saved.id && home.first?.goingCount == 3)
        let day = try await env.api.calendarDays(from: saved.date, to: saved.date)
        #expect(day.first?.items.contains { $0.id == stops[0].id && $0.kind == .group && $0.people.count == 3 } == true)

        let group = try #require(try await env.api.threads().first)
        #expect(group.isGroup && group.title == saved.title && group.members == [MockPeople.me, maya, dev])
        #expect(group.lastMessage == "You: Jordan added Maya and Dev" && group.chips.first == "3 people")
        let messages = try await env.api.messages(threadId: group.id, before: nil)
        #expect(messages.map(\.text) == ["Jordan added Maya and Dev"] && messages.first?.senderId == MockPeople.me.id)

        try await env.api.deleteItinerary(id: saved.id)
        #expect(try await env.api.threads().contains { $0.id == group.id } == false)
    }

    @Test func theDemoServerChecksWhoComes() async throws {
        let api = MockAPIClient(clock: clock, latencyScale: 0)
        // Chris only asked to be friends; nobody else is one yet.
        await #expect(throws: APIError.validation("You can only add friends.")) {
            try await api.createItinerary(request(inviting: [MockPeople.chris.id]))
        }
        await #expect(throws: APIError.validation(MockInvites.tooMany)) {
            try await api.createItinerary(request(inviting: strangers(13).map(\.id)))
        }
        var small = request(inviting: [maya.id, dev.id])
        small.maxGroupSize = 2
        await #expect(throws: APIError.validation(MockInvites.noRoom)) { try await api.createItinerary(small) }
        #expect(try await api.activeItineraries().allSatisfy { $0.title != "Rooftop" })

        // Duplicates and you are left out.
        let fits = try await api.createItinerary(request(inviting: [maya.id, MockPeople.me.id, maya.id]))
        #expect(fits.goingCount == 2)
        // "Just me" with a friend is posted to friends.
        var solo = request(inviting: [dev.id])
        solo.visibility = .justMe
        solo.maxGroupSize = nil
        #expect(try await api.createItinerary(solo).visibility == .friends)
        // Nobody brought along: a plain solo plan and no new chat.
        let before = try await api.threads().count
        let alone = try await api.createItinerary(request())
        #expect(alone.goingCount == 1 && alone.items.allSatisfy { $0.kind != .group })
        #expect(try await api.threads().count == before)
    }
}

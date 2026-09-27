import Foundation

/// In-memory backend for the offline demo (`-SQAPIMode mock`). Behaves like the real server: it
/// owns plan generation, route timing, split math and ratings, and keeps state while the app runs.
///
/// Debug: launch with `-SQMockFail itineraries,forum` to make those endpoint groups fail and see
/// the error states; `-SQMockLatency 0` removes the artificial delay.
final class MockAPIClient: APIClient {
    let clock: AppClock
    /// Multiplier for the artificial network delay (0 in tests).
    var latencyScale: Double
    /// Endpoint groups that should fail (see `simulate(_:)` call sites for names).
    var failing: Set<String>

    private let me = MockPeople.me
    private var user: User
    private var prefs = MockData.preferences
    private var taste = MockData.taste
    private var connected: [CalendarProvider: Bool] = [.google: true, .outlook: false]
    private var cards = [PaymentMethod(id: "pm-4242", brand: "Visa", last4: "4242", isDefault: true)]
    /// Facebook starts disconnected so the connect flow can be shown.
    private var facebook = FacebookConnection(connected: false)
    private var itins = MockData.itineraries()
    private var days = MockData.calendarDays()
    private var past = MockData.pastEvents()
    private var notes: [String: String] = [:]
    private var notesScopes: [String: NotesScope] = [:]
    private var transitChoices: [String: TravelMode] = [:]
    private var tickets: [String: Ticket] = [:]
    private var itemRatings: [String: Rating] = [:]
    private var sentRequests: [FriendRequest] = []
    /// Realtime events the demo "server" sends (checkout finishing), when an environment is attached.
    var realtime: RealtimeHub?
    private var intents: [String: CheckoutIntent] = [:]
    private var runs: [String: CheckoutRun] = [:]
    private var posts = MockData.forumPosts()
    private var myPost: MyFreePost?
    private var threadsById: [String: ChatThread] = [:]
    private var groupOrder: [String] = []
    private var messagesByThread: [String: [Message]] = [:]
    private var photosByGroup: [String: [GroupPhoto]] = [:]
    private var expensesByGroup: [String: [Expense]] = [:]
    private var friendList = MockData.friends()
    private var requests = MockData.friendRequests()
    private var batchesServed = 0
    /// The last `generatePlans` request and its must-see picks ("Load more options" keeps them).
    private var planRequest: PlanRequest?
    private var planPicks: [MockActivity] = []
    /// Stop titles of the options served with picks since then, so none comes out twice.
    private var servedStops: Set<[String]> = []
    private var options: [String: PlanOption] = [:]
    /// Stops suggested as alternatives (Review › Swap), so routes and new sidequests can use them.
    private var suggestedStops: [String: PlanStop] = [:]
    private var idCounter = 0

    /// Extra people behind the group chips ("12 people" for a 4-member split).
    private let groupPeopleCount = ["g1": 3, "g2": 12, "g3": 4]

    init(clock: AppClock = .demo, latencyScale: Double = 1, failing: Set<String> = []) {
        self.clock = clock
        self.latencyScale = latencyScale
        self.failing = failing
        self.user = MockData.user()
        seedThreads()
    }

    // MARK: - Helpers

    /// Each call takes about as long as the real one would (`milliseconds` × `latencyScale`).
    private func simulate(_ group: String, _ milliseconds: Int = 180) async throws {
        let ns = UInt64(max(0, Double(milliseconds) / 1000 * latencyScale) * 1_000_000_000)
        if ns > 0 { try? await Task.sleep(nanoseconds: ns) }
        if failing.contains(group) || failing.contains("all") {
            throw APIError.network("Simulated failure for \(group)")
        }
    }

    private func nextId(_ prefix: String) -> String {
        idCounter += 1
        return "\(prefix)-\(idCounter)"
    }

    private var format: TimeFormat { TimeFormat(clock: clock) }

    private func seedThreads() {
        var minutesAgo = 60
        for seed in MockData.groupSeeds() {
            let id = seed.thread.id
            var thread = seed.thread
            thread.faces = MockData.groupFaces[id] ?? Array(thread.members.prefix(2))
            threadsById[id] = thread
            groupOrder.append(id)
            messagesByThread[id] = seed.messages.map { m in
                minutesAgo -= 3
                return Message(id: nextId("m"), senderId: m.sender.id, senderName: m.senderName, text: m.text,
                               sentAt: clock.addingMinutes(-minutesAgo, to: clock.now))
            }
            photosByGroup[id] = (0..<seed.photoCount).map { i in
                let by = seed.photoBy[i % seed.photoBy.count]
                // Who added it, so your own photos can be deleted.
                let uploader = by == "You" ? me.id : MockPeople.all.first { $0.firstName == by }?.id
                return GroupPhoto(id: nextId("ph"), byName: by, uploaderId: uploader, url: nil,
                                  placeholderHex: MockData.photoColors[i % MockData.photoColors.count], imageData: nil)
            }
            let memberIds = seed.thread.members.map(\.id)
            expensesByGroup[id] = seed.expenses.map { e in
                Expense(id: nextId("ex"), what: e.what, amountCents: e.cents, payerId: e.payer.id, splitAmong: memberIds,
                        shares: SplitMath.equalShares(totalCents: e.cents, count: memberIds.count), createdBy: e.payer.id)
            }
        }
        for friend in friendList { seedDM(with: friend.person, subtitle: friend.statusLine) }
        for id in groupOrder { refreshGroupSummary(id) }
        // A little unread news, so the badges show in the demo.
        if let group = groupOrder.first { threadsById[group]?.unread = 2 }
        threadsById[dmId(for: MockPeople.maya)]?.unread = 1
    }

    private func dmId(for person: PersonRef) -> String { "dm-\(person.firstName.lowercased())" }

    @discardableResult
    private func seedDM(with person: PersonRef, subtitle: String) -> ChatThread {
        let id = dmId(for: person)
        if let existing = threadsById[id] { return existing }
        let seed = MockData.dmSeed(for: person)
        let thread = ChatThread(id: id, isGroup: false, title: person.name, subtitle: subtitle, members: [me, person],
                                faces: [person], lastMessage: seed.last?.text ?? "", lastTime: "Today")
        threadsById[id] = thread
        var minutesAgo = 30
        messagesByThread[id] = seed.map { m in
            minutesAgo -= 2
            return Message(id: nextId("m"), senderId: m.sender.id, senderName: m.sender.id == me.id ? "You" : m.sender.firstName,
                           text: m.text, sentAt: clock.addingMinutes(-minutesAgo, to: clock.now))
        }
        return thread
    }

    private func netCents(groupId: String) -> Int {
        guard let thread = threadsById[groupId] else { return 0 }
        return SplitMath.balances(expenses: expensesByGroup[groupId] ?? [], me: me.id, members: thread.members.map(\.id))
            .reduce(0) { $0 + $1.netCents }
    }

    /// Recomputes the Groups-list chips and album subtitle after expenses or photos change.
    private func refreshGroupSummary(_ id: String) {
        guard var thread = threadsById[id] else { return }
        let photos = photosByGroup[id]?.count ?? 0
        let people = groupPeopleCount[id] ?? thread.members.count
        let net = netCents(groupId: id)
        let balanceChip = net < 0 ? "You owe \(Money.compact(-net))" : net > 0 ? "You're owed \(Money.compact(net))" : "Settled up"
        switch id {
        case "g1": thread.chips = ["\(people) people", balanceChip, "\(photos) photos"]
        case "g2": thread.chips = ["\(people) people", "\(photos) photos"]
        default: thread.chips = ["\(people) people", balanceChip]
        }
        thread.albumSubtitle = id == "g2" ? "\(photos) photos so far" : "\(photos) photos · \(thread.members.count) people"
        threadsById[id] = thread
    }

    // MARK: - Auth

    func signup(_ request: SignupRequest) async throws -> AuthResponse {
        try await simulate("auth", 400)
        user.name = request.name.trimmingCharacters(in: .whitespaces)
        user.email = request.email.trimmingCharacters(in: .whitespaces)
        if let handle = request.username?.trimmingCharacters(in: CharacterSet(charactersIn: "@ ")), !handle.isEmpty {
            user.username = handle
        }
        if let dob = request.dateOfBirth {
            user.ageBracket = Validation.ageBracket(age: Validation.age(birthDate: dob, on: clock.now, calendar: clock.calendar))
        }
        // A brand-new account has no calendar connected and no saved card yet (Setup steps 2 and 4),
        // and hasn't finished setup (saving preferences finishes it).
        connected = [.google: false, .outlook: false]
        cards = []
        facebook = FacebookConnection(connected: false)
        user.setupComplete = false
        return AuthResponse(user: user, tokens: mockTokens())
    }

    func login(email: String, password: String) async throws -> AuthResponse {
        try await simulate("auth", 400)
        guard Validation.isValidEmail(email), !password.isEmpty else { throw APIError.invalidCredentials }
        user.email = email.trimmingCharacters(in: .whitespaces)
        return AuthResponse(user: user, tokens: mockTokens())
    }

    private func mockTokens() -> AuthTokens {
        AuthTokens(accessToken: "mock-access-\(UUID().uuidString)", refreshToken: "mock-refresh", expiresAt: nil)
    }

    func refresh(refreshToken: String) async throws -> AuthTokens { mockTokens() }
    func logout() async throws { try await simulate("auth", 100) }
    func forgotPassword(email: String) async throws { try await simulate("auth", 400) }

    func verifyResetCode(email: String, code: String) async throws -> String {
        try await simulate("auth", 400)
        guard code.count == 6, code.allSatisfy(\.isNumber) else { throw APIError.invalidCode }
        return "mock-reset-token"
    }

    func resetPassword(resetToken: String, newPassword: String) async throws { try await simulate("auth", 400) }
    func resendResetCode(email: String) async throws { try await simulate("auth", 300) }

    // MARK: - Me

    func me() async throws -> User {
        try await simulate("me", 120)
        return user
    }

    func updateMe(_ patch: UserPatch) async throws -> User {
        try await simulate("me", 150)
        if let name = patch.name { user.name = name }
        if let username = patch.username { user.username = username }
        if let status = patch.status { user.status = status }
        if let dob = patch.dateOfBirth {
            user.ageBracket = Validation.ageBracket(age: Validation.age(birthDate: dob, on: clock.now, calendar: clock.calendar))
        }
        return user
    }

    func uploadPhoto(_ jpegData: Data) async throws -> URL {
        try await simulate("me", 500)
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("sq-profile-\(UUID().uuidString).jpg")
        try jpegData.write(to: url)
        user.photoURL = url
        return url
    }

    func deletePhoto() async throws {
        try await simulate("me", 150)
        user.photoURL = nil
    }

    func setAvatarColor(_ color: AvatarColor) async throws {
        try await simulate("me", 100)
        user.avatarColor = color
    }

    func preferences() async throws -> Preferences {
        try await simulate("me", 120)
        return prefs
    }

    func savePreferences(_ preferences: Preferences) async throws {
        try await simulate("me", 300)
        prefs = preferences
        user.setupComplete = true
    }

    func tasteProfile() async throws -> TasteProfile {
        try await simulate("me", 150)
        return taste
    }

    func registerDevice(pushToken: String) async throws {}
    func unregisterDevice(pushToken: String) async throws {}

    // MARK: - Integrations + payments

    func integrations() async throws -> [Integration] {
        try await simulate("me", 120)
        return CalendarProvider.allCases.map { Integration(provider: $0, connected: connected[$0] ?? false) }
    }

    func connectIntegration(_ provider: CalendarProvider) async throws -> URL {
        try await simulate("me", 200)
        return URL(string: "https://example.com/oauth/\(provider.rawValue)?mock=1")!
    }

    func completeIntegration(_ provider: CalendarProvider) async throws {
        try await simulate("me", 500)
        connected[provider] = true
    }

    func disconnectIntegration(_ provider: CalendarProvider) async throws {
        try await simulate("me", 200)
        connected[provider] = false
    }

    func paymentMethods() async throws -> [PaymentMethod] {
        try await simulate("me", 120)
        return cards
    }

    /// The demo has no hosted card page; the app adds a demo card with `addPaymentMethod` instead.
    func paymentSetupURL() async throws -> URL {
        try await simulate("me", 200)
        return URL(string: "https://pay.sidequests.app/setup/demo")!
    }

    func addPaymentMethod(token: String) async throws -> PaymentMethod {
        try await simulate("me", 500)
        let demoCards = [("Visa", "4242"), ("Mastercard", "4444"), ("Visa", "5556")]
        let (brand, last4) = demoCards[min(cards.count, demoCards.count - 1)]
        let card = PaymentMethod(id: "pm-\(last4)-\(cards.count)", brand: brand, last4: last4, isDefault: cards.isEmpty)
        cards.append(card)
        return card
    }

    func deletePaymentMethod(id: String) async throws {
        try await simulate("me", 150)
        cards.removeAll { $0.id == id }
    }

    // MARK: - Facebook (Graph API)

    func facebookConnection() async throws -> FacebookConnection {
        try await simulate("facebook", 150)
        var current = facebook
        // Friends you've added since the import show as requested/friends, like a server would.
        if let people = facebook.lastImport?.friendsOnApp {
            current.lastImport?.friendsOnApp = people.map { searchResult(for: $0.person) }
        }
        return current
    }

    /// The demo has no Facebook page to open: asking for the dialog connects the demo account.
    func facebookConnectURL(rerequest: Bool) async throws -> URL {
        try await simulate("facebook", 200)
        facebook.connected = true
        facebook.needsReconnect = false
        facebook.name = user.name
        facebook.declinedScopes = []
        return URL(string: "https://www.facebook.com/dialog/oauth?client_id=demo")!
    }

    /// What a server might suggest from 48 liked Pages (hiking clubs, indie venues, food halls…).
    /// Four ratings differ from the demo's saved ones, so Account shows changes to review.
    func importFacebook() async throws -> FacebookImport {
        try await simulate("facebook", 900)
        guard facebook.connected else { throw APIError.server(status: 409, message: "Connect Facebook first.") }
        let result = FacebookImport(importedAt: clock.now, likedPages: 48,
                                    suggestedRatings: [.outdoors: 5, .food: 5, .liveMusic: 5, .museums: 3, .sports: 3,
                                                       .nightlife: 2, .longWalks: 4],
                                    interests: ["Hiking", "Indie rock", "Coffee", "Street food", "Board games"],
                                    homeArea: "Atlanta, Georgia",
                                    friendsOnApp: [MockPeople.priya, MockPeople.chris].map(searchResult(for:)))
        facebook.lastImport = result
        return result
    }

    func disconnectFacebook() async throws {
        try await simulate("facebook", 300)
        facebook = FacebookConnection(connected: false)
    }

    // MARK: - Calendar / places / events

    func calendarDays(from: Date, to: Date) async throws -> [CalendarDay] {
        try await simulate("calendar")
        let lo = clock.startOfDay(from), hi = clock.startOfDay(to)
        // Blocks that are stops of a plan say which one, like the real server.
        var owner: [String: String] = [:]
        for itin in itins { for item in itin.items { owner[item.id] = itin.id } }
        return days.filter { $0.date >= lo && $0.date <= hi }.map { day in
            var day = day
            day.items = day.items.map { item in
                var item = item
                item.itineraryId = owner[item.id]
                return item
            }
            return day
        }
    }

    func searchPlaces(query: String, near: Coordinate?) async throws -> [Place] {
        try await simulate("places", 120)
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return MockPlaces.suggestions
            .filter { q.isEmpty || $0.pillName.lowercased().contains(q) || $0.label.lowercased().contains(q) }
            .map(\.place)
    }

    func reverseGeocode(_ coordinate: Coordinate) async throws -> Place {
        try await simulate("places", 120)
        return Place(name: "Dropped pin", coordinate: coordinate)
    }

    func eventDetail(id: String) async throws -> ItineraryItem {
        try await simulate("events", 120)
        for itin in itins {
            if let item = itin.items.first(where: { $0.id == id }) {
                return withSavedDetails(item)
            }
        }
        for day in days {
            guard let entry = day.items.first(where: { $0.id == id }) else { continue }
            let extra = MockData.calendarExtras[id]
            return ItineraryItem(
                id: entry.id, kind: entry.kind, title: entry.title,
                place: Place(name: extra?.place ?? (entry.kind == .busy ? "From Google Calendar" : "")),
                start: entry.start, end: entry.end,
                description: extra?.note ?? "From your Google Calendar.",
                people: entry.people, interested: entry.interested,
                extraGoing: MockData.calendarExtraGoing[id] ?? 0,
                notes: notes[id], notesScope: notesScopes[id], rating: itemRatings[id], transitMode: transitChoices[id], ticket: tickets[id]
            )
        }
        throw APIError.notFound
    }

    func search(query: String) async throws -> SearchResults {
        try await simulate("search", 250)
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !q.isEmpty else { return SearchResults() }
        let sidequests = itins.filter { itin in
            itin.title.lowercased().contains(q) || itin.items.contains { $0.kind != .transit && $0.title.lowercased().contains(q) }
        }.map(withSavedDetails)
        let places = MockPlaces.suggestions
            .filter { $0.pillName.lowercased().contains(q) || $0.label.lowercased().contains(q) }
            .map(\.place)
        let found = posts.filter { ($0.title ?? $0.text ?? "").lowercased().contains(q) || $0.author.name.lowercased().contains(q) }
        return SearchResults(sidequests: sidequests, people: matchingPeople(q), places: Array(places.prefix(5)), posts: found)
    }

    // MARK: - Planning

    /// Create › Vibe › Must-see (see `MockActivities`).
    func searchActivities(q: String, near: Coordinate?, date: Date?, limit: Int) async throws -> [ActivityHit] {
        try await simulate("activities", 250)
        return MockActivities.search(q, near: near, date: date, limit: limit, clock: clock)
    }

    /// The demo's options, with every must-see pick in each (see `MockPicks`), or none with the reason.
    func generatePlans(_ request: PlanRequest) async throws -> PlanBatch {
        try await simulate("plans", 700)
        batchesServed = 0
        planRequest = request
        planPicks = []
        servedStops = []
        switch try MockPicks.resolve(request, clock: clock) {
        case .unavailable(let title):
            return PlanBatch(options: [], cursor: nil, done: true, reason: .mustIncludeUnavailable(title))
        case .picks(let picks):
            planPicks = picks
        }
        let batch = serve(MockData.firstOptions)
        guard !batch.isEmpty else { return PlanBatch(options: [], cursor: nil, done: true, reason: .mustIncludeNoFit) }
        return PlanBatch(options: batch, cursor: "batch-0", done: false)
    }

    func moreOptions(cursor: String) async throws -> PlanBatch {
        try await simulate("plans", 900)
        // A batch whose options all came out like ones already shown (the picks left no room for
        // their own stops) is skipped.
        while batchesServed < MockData.moreOptionBatches.count {
            let batch = serve(MockData.moreOptionBatches[batchesServed])
            batchesServed += 1
            let done = batchesServed >= MockData.moreOptionBatches.count
            if !batch.isEmpty || done {
                return PlanBatch(options: batch, cursor: done ? nil : "batch-\(batchesServed)", done: done)
            }
        }
        return PlanBatch(options: [], cursor: nil, done: true)
    }

    /// The options of a batch as served for the last request: with its picks in them, leaving out
    /// any the picks don't fit in or that repeat an option already served.
    private func serve(_ batch: [PlanOption]) -> [PlanOption] {
        var served = batch
        if !planPicks.isEmpty, let request = planRequest {
            served = batch.compactMap { MockPicks.fit($0, picks: planPicks, request: request) }
                .filter { servedStops.insert($0.stops.map(\.title)).inserted }
        }
        for option in served { options[option.id] = option }
        return served
    }

    func route(_ request: RouteRequest) async throws -> RouteResult {
        try await simulate("plans", 250)
        guard let option = planOption(request.optionId) else { throw APIError.notFound }
        // The option's own stops or suggested alternatives, in the order asked (left out = removed).
        let known = option.stops + suggestedStops.values
        let ordered = request.stopOrder.compactMap { id in known.first { $0.id == id } }
        return MockRouteEngine.route(stops: ordered.isEmpty ? option.stops : ordered, start: request.start, end: request.end,
                                     startTime: request.startTime, backBy: request.backBy, ride: request.ride)
    }

    /// Same kind of place, not already in the plan, nearest first (see `MockAlternatives`).
    func stopAlternatives(optionId: String, stopId: String, stopOrder: [String]) async throws -> [PlanAlternative] {
        try await simulate("plans", 600)
        guard let option = planOption(optionId) else { throw APIError.notFound }
        let known = option.stops + suggestedStops.values
        guard let stop = known.first(where: { $0.id == stopId }) else { throw APIError.notFound }
        let inPlan = Set(stopOrder.compactMap { id in known.first { $0.id == id }?.title })
        let found = MockAlternatives.alternatives(for: stop, excluding: inPlan)
        for alternative in found { suggestedStops[alternative.stop.id] = alternative.stop }
        return found
    }

    /// Review › tap a stop (see `MockActivityDetails`; the same hours on every day). An id the demo
    /// catalog doesn't have is a 404, like the server's.
    func activity(id: String, date: Date?) async throws -> ActivityDetail {
        try await simulate("activity", 300)
        guard let detail = MockActivityDetails.detail(id: id) else { throw APIError.notFound }
        return detail
    }

    private func planOption(_ id: String) -> PlanOption? {
        options[id] ?? (MockData.firstOptions + MockData.moreOptionBatches.flatMap { $0 }).first { $0.id == id }
    }

    func createItinerary(_ request: CreateItineraryRequest) async throws -> Itinerary {
        try await simulate("itineraries", 500)
        let stops = request.stopOrder.compactMap { id in request.option.stops.first { $0.id == id } }
        var items: [ItineraryItem] = []
        var cursor = request.plan.startTime
        let places = stops.map(\.place.name) + [request.plan.end.name]
        for (i, leg) in request.route.legs.enumerated() {
            let legEnd = clock.addingMinutes(leg.minutes, to: cursor)
            let destination = i < places.count ? places[i] : request.plan.end.name
            items.append(ItineraryItem(id: nextId("leg"), kind: .transit, title: "\(leg.mode.label) to \(destination)",
                                       place: Place(name: destination), start: cursor, end: legEnd, description: MockData.walkNote))
            cursor = legEnd
            if i < stops.count, i < request.route.stopTimes.count {
                let stop = stops[i], time = request.route.stopTimes[i]
                var item = ItineraryItem(id: nextId("stop"), kind: .sidequest, title: stop.title, place: stop.place,
                                         start: time.start, end: time.end, description: stop.subtitle)
                // Paid stops ("· $$") are sold on the sandbox merchant, so agentic checkout can buy them.
                if stop.subtitle.contains("$") {
                    item.ticketURL = URL(string: "https://events.sidequestz.tech/\(Self.slug(stop.title))/tickets")
                    item.bookable = true
                }
                items.append(item)
                cursor = time.end
            }
        }
        let itinerary = Itinerary(id: nextId("itin"), title: request.option.name, date: clock.startOfDay(request.plan.date),
                                  start: request.plan.startTime, backBy: request.plan.backBy, startPlace: request.plan.start,
                                  endPlace: request.plan.end, visibility: request.visibility, lockAt: request.lockAt,
                                  maxGroupSize: request.maxGroupSize, items: items, goingCount: 1)
        itins.insert(itinerary, at: 0)
        // Show the new stops on the calendar too.
        let key = clock.dayKey(request.plan.date)
        if let index = days.firstIndex(where: { $0.id == key }) {
            let entries = items.filter { $0.kind == .sidequest }.map {
                CalendarItem(id: $0.id, kind: .sidequest, title: $0.title, start: $0.start, end: $0.end)
            }
            days[index].items = (days[index].items + entries).sorted { $0.start < $1.start }
        }
        return itinerary
    }

    // MARK: - Itineraries

    func activeItineraries() async throws -> [Itinerary] {
        try await simulate("itineraries")
        return itins.map(withSavedDetails)
    }

    func itinerary(id: String) async throws -> Itinerary {
        try await simulate("itineraries", 120)
        guard let itin = itins.first(where: { $0.id == id }) else { throw APIError.notFound }
        return withSavedDetails(itin)
    }

    /// Notes, ratings, travel choices and tickets are stored per item; fold them into what we return.
    private func withSavedDetails(_ itin: Itinerary) -> Itinerary {
        var copy = itin
        copy.items = itin.items.map(withSavedDetails)
        return copy
    }

    private func withSavedDetails(_ item: ItineraryItem) -> ItineraryItem {
        var i = item
        i.notes = notes[item.id] ?? item.notes
        i.notesScope = notesScopes[item.id] ?? item.notesScope
        i.rating = itemRatings[item.id] ?? item.rating
        i.transitMode = transitChoices[item.id] ?? item.transitMode
        i.ticket = tickets[item.id] ?? item.ticket
        return i
    }

    func updateItinerary(id: String, _ update: ItineraryUpdate) async throws -> Itinerary {
        try await simulate("itineraries", 400)
        guard let index = itins.firstIndex(where: { $0.id == id }) else { throw APIError.notFound }
        var itin = itins[index]
        guard itin.isHost else { throw APIError.validation("Only the host can edit this sidequest.") }
        if let title = update.title?.trimmingCharacters(in: .whitespacesAndNewlines) {
            guard !title.isEmpty else { throw APIError.validation("Give your sidequest a name.") }
            itin.title = title
        }
        if let visibility = update.visibility { itin.visibility = visibility }

        let oldStops = Set(itin.items.filter(Self.isStop).map(\.id))
        if let date = update.date {
            // A different day: move the plan there. Its calendar blocks belonged to the old day.
            let days = clock.calendar.dateComponents([.day], from: clock.startOfDay(itin.date), to: clock.startOfDay(date)).day ?? 0
            if days != 0 {
                func shift(_ d: Date) -> Date { clock.calendar.date(byAdding: .day, value: days, to: d) ?? d }
                itin.date = clock.startOfDay(date)
                itin.start = shift(itin.start)
                itin.backBy = shift(itin.backBy)
                itin.items = itin.items.filter { $0.kind != .busy }.map { item in
                    var moved = item
                    moved.start = shift(item.start)
                    moved.end = shift(item.end)
                    return moved
                }
            }
        }
        if let start = update.start { itin.start = start }
        if let backBy = update.backBy { itin.backBy = backBy }
        if let order = update.stopOrder {
            let kept = order.filter(oldStops.contains)
            guard !kept.isEmpty else { throw APIError.validation("A sidequest needs at least one stop.") }
        }
        if update.date != nil || update.start != nil || update.backBy != nil || update.stopOrder != nil {
            itin.items = retimed(itin, stopOrder: update.stopOrder)
        }
        itins[index] = itin
        syncCalendar(for: itin, previousStops: oldStops)
        return withSavedDetails(itin)
    }

    func deleteItinerary(id: String) async throws {
        try await simulate("itineraries", 200)
        guard let itin = itins.first(where: { $0.id == id }) else { throw APIError.notFound }
        guard itin.isHost else { throw APIError.validation("You joined this sidequest, so you can leave it but not delete it.") }
        itins.removeAll { $0.id == id }
        let stops = Set(itin.items.filter(Self.isStop).map(\.id))
        for index in days.indices { days[index].items.removeAll { stops.contains($0.id) } }
    }

    func leaveItinerary(id: String) async throws {
        try await simulate("itineraries", 200)
        guard let itin = itins.first(where: { $0.id == id }) else { throw APIError.notFound }
        guard !itin.isHost else { throw APIError.validation("You host this sidequest. Delete it instead.") }
        itins.removeAll { $0.id == id }
    }

    nonisolated private static func isStop(_ item: ItineraryItem) -> Bool { item.kind == .sidequest || item.kind == .group }

    /// The mock server's re-timing after an edit: stops keep their length and run back to back from
    /// the start, each after a 15-minute walk, stepping around calendar (busy) blocks.
    private func retimed(_ itin: Itinerary, stopOrder: [String]?) -> [ItineraryItem] {
        let stops = itin.items.filter(Self.isStop)
        let ordered = (stopOrder ?? stops.map(\.id)).compactMap { id in stops.first { $0.id == id } }
        let busy = itin.items.filter { $0.kind == .busy }
        var items = busy
        var cursor = itin.start
        var from = itin.startPlace.name
        for stop in ordered {
            let length = stop.end.timeIntervalSince(stop.start)
            var legStart = cursor
            while let clash = busy.first(where: { $0.start < clock.addingMinutes(15, to: legStart).addingTimeInterval(length) && $0.end > legStart }) {
                legStart = clash.end
            }
            let arrive = clock.addingMinutes(15, to: legStart)
            let to = stop.place?.name ?? stop.title
            items.append(ItineraryItem(id: "leg-\(stop.id)", kind: .transit, title: "Walk to \(stop.title)",
                                       place: Place(name: "\(from) → \(to)"), start: legStart, end: arrive,
                                       description: MockData.walkNote))
            var moved = stop
            moved.start = arrive
            moved.end = arrive.addingTimeInterval(length)
            items.append(moved)
            cursor = moved.end
            from = to
        }
        return items.sorted { $0.start < $1.start }
    }

    /// Keeps the calendar's copies of a plan's stops in step with the plan (times and day; the
    /// calendar keeps its own short titles).
    private func syncCalendar(for itin: Itinerary, previousStops: Set<String>) {
        let stops = itin.items.filter(Self.isStop)
        let current = Set(stops.map(\.id))
        for index in days.indices {
            days[index].items.removeAll { previousStops.contains($0.id) && !current.contains($0.id) }
        }
        for stop in stops {
            guard let dayIndex = days.firstIndex(where: { $0.id == clock.dayKey(stop.start) }) else { continue }
            for index in days.indices where index != dayIndex { days[index].items.removeAll { $0.id == stop.id } }
            if let i = days[dayIndex].items.firstIndex(where: { $0.id == stop.id }) {
                days[dayIndex].items[i].start = stop.start
                days[dayIndex].items[i].end = stop.end
            } else if previousStops.contains(stop.id) {
                days[dayIndex].items.append(CalendarItem(id: stop.id, kind: stop.kind, title: stop.title, start: stop.start, end: stop.end))
            }
            days[dayIndex].items.sort { $0.start < $1.start }
        }
    }

    func updateItemNotes(itineraryId: String?, itemId: String, notes text: String, scope: NotesScope?) async throws {
        try await simulate("itineraries", 150)
        notes[itemId] = text
        if let scope { notesScopes[itemId] = scope }
    }

    func selectTransit(itineraryId: String?, itemId: String, mode: TravelMode) async throws {
        try await simulate("itineraries", 150)
        transitChoices[itemId] = mode
    }

    func transitOptions(itineraryId: String?, itemId: String) async throws -> [TransitOption] {
        try await simulate("itineraries", 150)
        return MockData.transitOptions()
    }

    // MARK: - Past + ratings

    func pastEvents(unratedOnly: Bool) async throws -> [PastEvent] {
        try await simulate("past")
        let all = past.sorted { $0.date > $1.date }
        return unratedOnly ? all.filter { $0.rating == nil } : all
    }

    /// The demo server's version of the insight job: what the sidequests you rated 4–5 stars share.
    func pastInsights() async throws -> PastInsights {
        try await simulate("past", 400)
        let liked = past.filter { ($0.rating?.stars ?? 0) >= 4 }
        guard !liked.isEmpty else {
            return PastInsights(headline: "Rate a few sidequests and we'll show what your favorites have in common.")
        }
        func mostCommon(_ values: [String]) -> (value: String, count: Int) {
            let counts = Dictionary(values.map { ($0, 1) }, uniquingKeysWith: +)
            let best = counts.max { $0.value < $1.value || ($0.value == $1.value && $0.key > $1.key) }
            return (best?.key ?? "", best?.value ?? 0)
        }
        func timeOfDay(_ date: Date) -> String {
            switch clock.calendar.component(.hour, from: date) {
            case ..<12: "Mornings"
            case 12..<17: "Afternoons"
            default: "Evenings"
            }
        }
        let n = liked.count
        let of = { (count: Int) in "\(count) of your \(n) favorite\(n == 1 ? "" : "s")" }
        let time = mostCommon(liked.map { timeOfDay($0.date) })
        let company = mostCommon(liked.map { $0.company == "solo" ? "On your own" : "With friends" })
        let area = mostCommon(liked.map(\.place))
        let best = liked.max { ($0.rating?.stars ?? 0) < ($1.rating?.stars ?? 0) }
        let tags = mostCommonTags(liked.compactMap(\.rating).flatMap(\.tags))
        let who = company.value == "On your own" ? "on your own" : "with friends"
        return PastInsights(
            headline: "You like \(time.value.lowercased().dropLast()) sidequests \(who).",
            highlights: [
                PastInsight(id: "time", title: "Favorite time", value: time.value, detail: of(time.count), symbol: "moon.stars"),
                PastInsight(id: "company", title: "Company", value: company.value, detail: of(company.count), symbol: "person.2"),
                PastInsight(id: "area", title: "Favorite area", value: area.value, detail: of(area.count), symbol: "mappin.and.ellipse"),
                PastInsight(id: "best", title: "Top rated", value: best?.title ?? "", detail: best.map { "\($0.rating?.stars ?? 0) stars" }, symbol: "star"),
            ],
            topTags: tags,
            basedOn: past.filter { $0.rating != nil }.count
        )
    }

    private func mostCommonTags(_ tags: [String]) -> [String] {
        let counts = Dictionary(tags.map { ($0, 1) }, uniquingKeysWith: +)
        return counts.sorted { $0.value > $1.value || ($0.value == $1.value && $0.key < $1.key) }.map(\.key)
    }

    func rate(itemId: String, rating: Rating) async throws {
        try await simulate("past", 250)
        if let index = past.firstIndex(where: { $0.id == itemId }) {
            past[index].rating = rating
        } else {
            itemRatings[itemId] = rating
        }
    }

    // MARK: - Checkout

    func createCheckoutIntent(itemId: String, quantity: Int, paymentMethodId: String?, instant: Bool) async throws -> CheckoutIntent {
        try await simulate("checkout", 600)
        let item = try? await eventDetail(id: itemId)
        let card = cards.first { $0.id == paymentMethodId } ?? cards.first { $0.isDefault } ?? cards.first
        var intent = CheckoutIntent(id: nextId("ci"), itemId: itemId, itemTitle: item?.title ?? "Tickets",
                                    steps: MockData.checkoutSteps, subtotalCents: item?.priceCents.map { $0 * quantity },
                                    feesCents: nil, totalCents: nil,
                                    cardBrand: card?.brand ?? "", cardLast4: card?.last4 ?? "", state: .awaitingApproval,
                                    quantity: quantity, paymentMethodId: card?.id)
        if card == nil {
            intent.state = .failed
            intent.failureReason = "Add a card in Account first."
        } else if instant, prefs.instantCheckout, let total = intent.subtotalCents, total <= prefs.instantCheckoutLimitCents {
            // Instant checkout: within the limit, the agent pays without asking.
            intent.instant = true
            intent.state = .processing
            intent.steps = intent.steps.map { step in
                step.done ? step : CheckoutStep(text: "Paying instantly (within your limit)", done: false)
            }
            intents[intent.id] = intent
            scheduleCheckoutFinish(intent.id)
            return intent
        }
        intents[intent.id] = intent
        return intent
    }

    func updateCheckoutIntent(id: String, paymentMethodId: String) async throws -> CheckoutIntent {
        try await simulate("checkout", 200)
        guard var intent = intents[id] else { throw APIError.notFound }
        guard let card = cards.first(where: { $0.id == paymentMethodId }) else { throw APIError.notFound }
        intent.paymentMethodId = card.id
        intent.cardBrand = card.brand
        intent.cardLast4 = card.last4
        intents[id] = intent
        return intent
    }

    func checkoutIntent(id: String) async throws -> CheckoutIntent {
        try await simulate("checkout", 120)
        guard let intent = intents[id] else { throw APIError.notFound }
        return intent
    }

    /// Paying takes a moment: the intent comes back `processing`, then turns `booked` (a
    /// `checkout.status` event, and `checkoutIntent(id:)` from then on).
    func approveCheckout(id: String) async throws -> CheckoutIntent {
        try await simulate("checkout", 900)
        guard var intent = intents[id] else { throw APIError.notFound }
        guard intent.state == .awaitingApproval else { throw APIError.validation("This checkout already finished.") }
        intent.state = .processing
        intents[id] = intent
        scheduleCheckoutFinish(id)
        return intent
    }

    private func scheduleCheckoutFinish(_ id: String) {
        let delay = 1.2 * latencyScale
        Task { [weak self] in
            if delay > 0 { try? await Task.sleep(for: .seconds(delay)) }
            self?.finishCheckout(id)
        }
    }

    private func finishCheckout(_ id: String) {
        guard var intent = intents[id], intent.state == .processing else { return }
        intent.state = .booked
        intent.steps = intent.steps.map { CheckoutStep(text: $0.text, done: true) }
        intents[id] = intent
        tickets[intent.itemId] = Ticket(id: nextId("tk"), quantity: intent.quantity, totalCents: intent.totalCents ?? intent.subtotalCents,
                                        confirmation: "SQ-\(String(intent.id.hashValue, radix: 36).suffix(5).uppercased())")
        realtime?.publish(.checkoutStatus(intentId: id, state: .booked))
    }

    func cancelCheckout(id: String) async throws {
        try await simulate("checkout", 120)
        intents[id]?.state = .cancelled
    }

    // MARK: - Agentic checkout

    /// The demo merchant's price per ticket when the stop has none.
    private static let demoTicketCents = 1800

    private static func slug(_ title: String) -> String {
        title.lowercased().map { $0.isLetter || $0.isNumber ? String($0) : "-" }.joined()
            .split(separator: "-").joined(separator: "-")
    }

    private func mockFees(_ subtotal: Int, quantity: Int) -> Int { subtotal * 8 / 100 + 50 * quantity }

    func checkoutPlan(itineraryId: String) async throws -> CheckoutPlan {
        try await simulate("checkout", 250)
        guard let itin = itins.first(where: { $0.id == itineraryId }).map(withSavedDetails) else { throw APIError.notFound }
        let items = itin.items.filter { $0.kind == .sidequest && $0.ticketURL != nil }.map { item in
            CheckoutPlanItem(itemId: item.id, title: item.title, start: item.start, merchant: "events.sidequestz.tech",
                             ticketURL: item.ticketURL, priceCents: item.priceCents ?? Self.demoTicketCents, quantity: 1,
                             booked: item.ticket?.isMine == true, intentState: nil, confirmation: item.ticket?.confirmation)
        }
        let estimate = items.filter { !$0.booked }.reduce(0) { $0 + ($1.priceCents ?? 0) * $1.quantity }
        let card = cards.first { $0.isDefault } ?? cards.first
        let suggested = min(max((estimate * 115 / 100 + 99) / 100 * 100, prefs.instantCheckoutLimitCents), 100_000)
        return CheckoutPlan(itineraryId: itineraryId, available: true, agenticCheckout: prefs.instantCheckout, items: items,
                            estimateCents: estimate, defaultBudgetCents: prefs.instantCheckoutLimitCents,
                            suggestedBudgetCents: suggested, paymentMethodId: card?.id, cardBrand: card?.brand,
                            cardLast4: card?.last4, activeRunId: runs.values.first { $0.itineraryId == itineraryId && $0.isRunning }?.id)
    }

    func startCheckoutRun(itineraryId: String, _ request: CreateCheckoutRun) async throws -> CheckoutRun {
        try await simulate("checkout", 500)
        let plan = try await checkoutPlan(itineraryId: itineraryId)
        if plan.activeRunId != nil { throw APIError.server(status: 409, message: "Muse is already getting tickets for this plan.") }
        guard let card = cards.first(where: { $0.id == request.paymentMethodId }) ?? cards.first(where: { $0.isDefault }) ?? cards.first else {
            throw APIError.validation("Add a card in Account first.")
        }
        let runId = nextId("run")
        var list: [CheckoutIntent] = []
        for wanted in request.items {
            guard let item = plan.items.first(where: { $0.itemId == wanted.itemId }), !item.booked else { continue }
            var intent = CheckoutIntent(id: nextId("ci"), itemId: item.itemId, itemTitle: item.title,
                                        steps: [CheckoutStep(text: "Waiting for Muse", done: false)],
                                        subtotalCents: nil, feesCents: nil, totalCents: nil,
                                        cardBrand: card.brand, cardLast4: card.last4, state: .processing,
                                        quantity: wanted.quantity, paymentMethodId: card.id)
            intent.runId = runId
            intent.merchant = item.merchant
            intent.checkoutURL = item.ticketURL
            intents[intent.id] = intent
            list.append(intent)
        }
        guard !list.isEmpty else { throw APIError.server(status: 409, message: "Everything on this plan is already booked.") }
        let run = CheckoutRun(id: runId, itineraryId: itineraryId, state: .running, budgetCents: request.budgetCents, spentCents: 0,
                              currency: "usd", cardBrand: card.brand, cardLast4: card.last4, agent: "muse", summary: nil,
                              intents: list, createdAt: clock.now, finishedAt: nil)
        runs[runId] = run
        let delay = 1.1 * latencyScale
        Task { [weak self] in
            for intent in list {
                if delay > 0 { try? await Task.sleep(for: .seconds(delay)) }
                self?.advanceRunItem(runId: runId, intentId: intent.id, step: "Opened the ticket page")
                if delay > 0 { try? await Task.sleep(for: .seconds(delay)) }
                self?.buyRunItem(runId: runId, intentId: intent.id)
            }
            self?.finishRun(runId)
        }
        return run
    }

    private func advanceRunItem(runId: String, intentId: String, step: String) {
        guard runs[runId]?.isRunning == true, var intent = intents[intentId], intent.state == .processing else { return }
        intent.steps = [CheckoutStep(text: step, done: true), CheckoutStep(text: "Checking the price", done: false)]
        intents[intentId] = intent
        realtime?.publish(.checkoutStatus(intentId: intentId, state: intent.state))
    }

    private func buyRunItem(runId: String, intentId: String) {
        guard var run = runs[runId], run.isRunning, var intent = intents[intentId], intent.state == .processing else { return }
        let subtotal = Self.demoTicketCents * intent.quantity
        let total = subtotal + mockFees(subtotal, quantity: intent.quantity)
        intent.subtotalCents = subtotal
        intent.feesCents = total - subtotal
        intent.totalCents = total
        if run.spentCents + total > run.budgetCents {
            intent.state = .failed
            intent.failureCode = "over_budget"
            intent.failureReason = "\(Money.compact(total)) is more than what's left of your budget."
            intent.steps = [CheckoutStep(text: "Opened the ticket page", done: true), CheckoutStep(text: "Over budget, skipped", done: true)]
        } else {
            intent.state = .booked
            intent.maxAuthorizedCents = total
            intent.finalCents = total
            intent.confirmation = "SQZ-\(String(abs(intentId.hashValue), radix: 36).prefix(6).uppercased())"
            intent.orderRef = "ord_\(intentId)"
            let slug = intent.checkoutURL.map { $0.deletingLastPathComponent().lastPathComponent } ?? "event"
            intent.ticketURL = URL(string: "https://events.sidequestz.tech/\(slug)/ticket/\(intentId)")
            intent.steps = [CheckoutStep(text: "Opened the ticket page", done: true),
                            CheckoutStep(text: "Paid \(Money.compact(total)) with \(intent.cardBrand) •••• \(intent.cardLast4) (sandbox)", done: true),
                            CheckoutStep(text: "Got your ticket", done: true)]
            run.spentCents += total
            tickets[intent.itemId] = Ticket(id: nextId("tk"), quantity: intent.quantity, totalCents: total,
                                            confirmation: intent.confirmation, url: intent.ticketURL)
        }
        intents[intentId] = intent
        run.intents = run.intents.map { intents[$0.id] ?? $0 }
        runs[runId] = run
        realtime?.publish(.checkoutStatus(intentId: intentId, state: intent.state))
        realtime?.publish(.checkoutRun(runId: runId, state: run.state, spentCents: run.spentCents))
    }

    private func finishRun(_ runId: String) {
        guard var run = runs[runId], run.isRunning else { return }
        run.intents = run.intents.map { intents[$0.id] ?? $0 }
        run.state = .done
        run.finishedAt = clock.now
        let booked = run.booked.map(\.itemTitle), missed = run.notBooked.map(\.itemTitle)
        var parts: [String] = []
        if !booked.isEmpty { parts.append("Got tickets for \(ListFormatter.localizedString(byJoining: booked)) (\(Money.compact(run.spentCents)) of your \(Money.compact(run.budgetCents)) budget).") }
        if !missed.isEmpty { parts.append("Couldn't get \(ListFormatter.localizedString(byJoining: missed)).") }
        run.summary = parts.joined(separator: " ")
        runs[runId] = run
        realtime?.publish(.checkoutRun(runId: runId, state: .done, spentCents: run.spentCents))
    }

    func checkoutRun(id: String) async throws -> CheckoutRun {
        try await simulate("checkout", 120)
        guard var run = runs[id] else { throw APIError.notFound }
        run.intents = run.intents.map { intents[$0.id] ?? $0 }
        return run
    }

    func cancelCheckoutRun(id: String) async throws -> CheckoutRun {
        try await simulate("checkout", 200)
        guard var run = runs[id] else { throw APIError.notFound }
        if run.isRunning {
            for intent in run.intents where intents[intent.id]?.state == .processing {
                intents[intent.id]?.state = .cancelled
                intents[intent.id]?.failureCode = "cancelled"
            }
            run.state = .cancelled
            run.finishedAt = clock.now
            run.summary = run.booked.isEmpty ? "Stopped before buying anything." : "Stopped. Tickets already bought are kept."
        }
        run.intents = run.intents.map { intents[$0.id] ?? $0 }
        runs[id] = run
        realtime?.publish(.checkoutRun(runId: id, state: run.state, spentCents: run.spentCents))
        return run
    }

    // MARK: - Forum

    func forumPosts(_ query: ForumQuery) async throws -> [ForumPost] {
        try await simulate("forum")
        let filtered = posts.enumerated().filter { _, p in
            switch query.type {
            case .all: break
            case .plans: if p.type != .plan { return false }
            case .freeNow: if p.type != .freeNow { return false }
            }
            if query.scope == .friends && !p.isFriend { return false }
            switch query.when {
            case .any: break
            case .now: if p.startsInMinutes > 60 { return false }
            case .today: if p.day != "today" { return false }
            case .weekend: if !(p.day == "sat" || p.day == "sun") { return false }
            }
            if let max = query.maxDistanceMi, p.distanceMi > max { return false }
            if !query.cost.isEmpty && !query.cost.contains(p.priceTier) { return false }
            if !query.tags.isEmpty && !p.tags.contains(where: query.tags.contains) { return false }
            if query.openOnly && !(p.type == .plan && (p.spotsLeft ?? 0) > 0) { return false }
            return true
        }
        func key(_ p: ForumPost) -> Double {
            switch query.sort {
            case .soonest: Double(p.startsInMinutes)
            case .closest: p.distanceMi
            case .spots: -Double(p.spotsLeft ?? -1)
            case .newest: Double(p.postedMinutesAgo)
            }
        }
        return filtered.sorted { (a, b) in
            let ka = key(a.element), kb = key(b.element)
            return ka != kb ? ka < kb : a.offset < b.offset
        }.map(\.element)
    }

    func myFreePost() async throws -> MyFreePost? {
        try await simulate("forum", 100)
        return myPost
    }

    func postFreeNow(_ post: NewFreePost) async throws -> MyFreePost {
        try await simulate("forum", 300)
        let until = post.until ?? clock.date(2026, 9, 25, 18, 30)
        let near = post.area?.isCurrentLocation == false ? post.area?.name : "Tech Square"
        let mine = MyFreePost(id: "mine", visibility: post.visibility, text: "Free until \(format.time(until)) near \(near ?? "Tech Square")",
                              until: until, areaLabel: post.area?.name ?? ForumArea.midtown.name, radiusMi: post.radiusMi ?? 2)
        myPost = mine
        return mine
    }

    func deleteForumPost(id: String) async throws {
        try await simulate("forum", 200)
        if id == myPost?.id { myPost = nil }
    }

    /// The demo host approves by hand, so a request stays `requested` (a full plan says so).
    func requestToJoin(postId: String) async throws -> JoinResult {
        try await simulate("forum", 300)
        guard let index = posts.firstIndex(where: { $0.id == postId }) else { throw APIError.notFound }
        if posts[index].spotsLeft == 0 { return JoinResult(status: .full) }
        posts[index].joinStatus = .requested
        return JoinResult(status: .requested)
    }

    func cancelJoinRequest(postId: String) async throws {
        try await simulate("forum", 200)
        guard let index = posts.firstIndex(where: { $0.id == postId }) else { throw APIError.notFound }
        posts[index].joinStatus = .none
    }

    func planTogether(postId: String) async throws -> ChatThread {
        try await simulate("forum", 300)
        guard let index = posts.firstIndex(where: { $0.id == postId }) else { throw APIError.notFound }
        posts[index].planTogetherSent = true
        let author = posts[index].author
        let friend = friendList.first { $0.person.id == author.id }
        let thread = seedDM(with: author, subtitle: friend?.statusLine ?? posts[index].meta)
        let text = "Saw your post. Want to plan something together?"
        if !(messagesByThread[thread.id]?.contains { $0.text == text } ?? false) {
            messagesByThread[thread.id, default: []].append(Message(id: nextId("m"), senderId: me.id, senderName: "You", text: text, sentAt: clock.now))
            threadsById[thread.id]?.lastMessage = "You: \(text)"
        }
        return threadsById[thread.id] ?? thread
    }

    // MARK: - Threads

    func threads() async throws -> [ChatThread] {
        try await simulate("threads")
        let groups = groupOrder.compactMap { threadsById[$0] }
        // Most recent first, like the server.
        let latest = { (thread: ChatThread) in self.messagesByThread[thread.id]?.last?.sentAt ?? .distantPast }
        let dms = threadsById.values.filter { !$0.isGroup }.sorted { latest($0) > latest($1) }
        return groups + dms
    }

    func thread(id: String) async throws -> ChatThread {
        try await simulate("threads", 100)
        guard let thread = threadsById[id] else { throw APIError.notFound }
        return thread
    }

    func messages(threadId: String, before: String?) async throws -> [Message] {
        try await simulate("threads", 150)
        guard threadsById[threadId] != nil else { throw APIError.notFound }
        // Pages of 30, oldest first; `before` asks for the page older than that message.
        let all = messagesByThread[threadId] ?? []
        var end = all.count
        if let before {
            guard let index = all.firstIndex(where: { $0.id == before }) else { return [] }
            end = index
        }
        return Array(all[max(0, end - 30)..<end])
    }

    func sendMessage(threadId: String, text: String, clientId: String?) async throws -> Message {
        try await simulate("threads", 150)
        guard threadsById[threadId] != nil else { throw APIError.notFound }
        // A retry with the same client id returns the message that already went out.
        if let clientId, let sent = messagesByThread[threadId]?.first(where: { $0.clientId == clientId }) { return sent }
        let message = Message(id: nextId("m"), senderId: me.id, senderName: "You", text: text, sentAt: clock.now, clientId: clientId)
        messagesByThread[threadId, default: []].append(message)
        threadsById[threadId]?.lastMessage = "You: \(text)"
        threadsById[threadId]?.lastTime = format.time(clock.now)
        return message
    }

    func markThreadRead(id: String) async throws {
        try await simulate("threads", 100)
        threadsById[id]?.unread = 0
    }

    func startDM(userId: String) async throws -> ChatThread {
        try await simulate("threads", 150)
        guard let person = MockPeople.byId(userId) ?? friendList.first(where: { $0.person.id == userId })?.person else {
            throw APIError.notFound
        }
        let status = friendList.first { $0.person.id == userId }?.statusLine ?? "From the Forum"
        return seedDM(with: person, subtitle: status)
    }

    // MARK: - Album

    func groupPhotos(groupId: String) async throws -> [GroupPhoto] {
        try await simulate("album")
        return photosByGroup[groupId] ?? []
    }

    func uploadGroupPhoto(groupId: String, jpegData: Data) async throws -> GroupPhoto {
        try await simulate("album", 500)
        let photo = GroupPhoto(id: nextId("ph"), byName: "You", uploaderId: me.id, createdAt: clock.now, url: nil,
                               placeholderHex: nil, imageData: jpegData)
        photosByGroup[groupId, default: []].insert(photo, at: 0)
        refreshGroupSummary(groupId)
        return photo
    }

    func deleteGroupPhoto(groupId: String, photoId: String) async throws {
        try await simulate("album", 200)
        photosByGroup[groupId]?.removeAll { $0.id == photoId }
        refreshGroupSummary(groupId)
    }

    // MARK: - Splits

    func ledger(groupId: String) async throws -> GroupLedger {
        try await simulate("splits")
        guard let thread = threadsById[groupId], thread.isGroup else { throw APIError.notFound }
        let expenses = expensesByGroup[groupId] ?? []
        let balances = SplitMath.balances(expenses: expenses, me: me.id, members: thread.members.map(\.id))
        return GroupLedger(members: thread.members, expenses: expenses, balances: balances)
    }

    func addExpense(groupId: String, _ expense: NewExpense) async throws -> Expense {
        try await simulate("splits", 400)
        let what = expense.what.trimmingCharacters(in: .whitespaces)
        guard !what.isEmpty else { throw APIError.validation("Add what the expense was for.") }
        guard expense.amountCents > 0 else { throw APIError.validation("Enter an amount above $0.") }
        guard !expense.splitAmong.isEmpty else { throw APIError.validation("Pick at least one person to split with.") }
        let saved = Expense(id: nextId("ex"), what: what, amountCents: expense.amountCents, payerId: expense.payerId,
                            splitAmong: expense.splitAmong,
                            shares: SplitMath.equalShares(totalCents: expense.amountCents, count: expense.splitAmong.count),
                            createdBy: me.id)
        expensesByGroup[groupId, default: []].append(saved)
        refreshGroupSummary(groupId)
        return saved
    }

    func deleteExpense(groupId: String, expenseId: String) async throws {
        try await simulate("splits", 200)
        expensesByGroup[groupId]?.removeAll { $0.id == expenseId }
        refreshGroupSummary(groupId)
    }

    func settleUp(groupId: String, amountCents: Int, paymentMethodId: String?) async throws {
        try await simulate("splits", 900)
        guard let thread = threadsById[groupId] else { throw APIError.notFound }
        let balances = SplitMath.balances(expenses: expensesByGroup[groupId] ?? [], me: me.id, members: thread.members.map(\.id))
        let owed = balances.filter { $0.netCents < 0 }.reduce(0) { $0 - $1.netCents }
        guard amountCents == owed else {
            throw APIError.server(status: 409, message: "The balance changed. Check the new amount and try again.")
        }
        guard let card = cards.first(where: { $0.id == paymentMethodId }) ?? cards.first(where: \.isDefault) ?? cards.first else {
            throw APIError.validation("Add a card in Account first.")
        }
        // Record a settling payment from you to everyone you owe.
        for balance in balances where balance.netCents < 0 {
            expensesByGroup[groupId, default: []].append(
                Expense(id: nextId("ex"), what: "Settled up with \(card.brand) •••• \(card.last4)", amountCents: -balance.netCents, payerId: me.id,
                        splitAmong: [balance.userId], shares: [-balance.netCents], createdBy: me.id))
        }
        refreshGroupSummary(groupId)
    }

    // MARK: - Friends

    func friends() async throws -> [Friend] {
        try await simulate("friends")
        return friendList
    }

    func searchUsers(query: String) async throws -> [UserSearchResult] {
        try await simulate("friends", 150)
        return matchingPeople(query)
    }

    private func matchingPeople(_ query: String) -> [UserSearchResult] {
        let q = query.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "@ "))
        guard !q.isEmpty else { return [] }
        return MockPeople.all
            .filter { $0.id != me.id && ($0.name.lowercased().contains(q) || ($0.username ?? $0.name.lowercased().replacingOccurrences(of: " ", with: "")).contains(q)) }
            .map(searchResult(for:))
    }

    /// How `person` relates to you right now (search rows, Facebook friends on SideQuests).
    private func searchResult(for person: PersonRef) -> UserSearchResult {
        if friendList.contains(where: { $0.person.id == person.id }) { return UserSearchResult(person: person, relation: .friend) }
        if let request = requests.first(where: { $0.person.id == person.id }) {
            return UserSearchResult(person: person, relation: .incoming, requestId: request.id)
        }
        if let request = sentRequests.first(where: { $0.person.id == person.id }) {
            return UserSearchResult(person: person, relation: .outgoing, requestId: request.id)
        }
        return UserSearchResult(person: person)
    }

    /// Incoming requests, plus the ones you sent (`outgoing: true`), like the contract says.
    func friendRequests() async throws -> [FriendRequest] {
        try await simulate("friends", 120)
        return requests + sentRequests
    }

    func sendFriendRequest(userId: String) async throws -> FriendRequest {
        try await simulate("friends", 200)
        guard let person = MockPeople.all.first(where: { $0.id == userId }) else { throw APIError.notFound }
        if let existing = sentRequests.first(where: { $0.person.id == userId }) { return existing }
        let request = FriendRequest(id: nextId("fr"), person: person, note: "Requested just now", outgoing: true)
        sentRequests.append(request)
        return request
    }

    func cancelFriendRequest(id: String) async throws {
        try await simulate("friends", 150)
        sentRequests.removeAll { $0.id == id }
    }

    func acceptFriendRequest(id: String) async throws {
        try await simulate("friends", 250)
        guard let index = requests.firstIndex(where: { $0.id == id }) else { throw APIError.notFound }
        let request = requests.remove(at: index)
        friendList.insert(Friend(person: request.person, statusLine: "Just added", activity: .new), at: 0)
        seedDM(with: request.person, subtitle: "Just added")
    }

    func declineFriendRequest(id: String) async throws {
        try await simulate("friends", 200)
        requests.removeAll { $0.id == id }
    }

    func removeFriend(id: String) async throws {
        try await simulate("friends", 200)
        friendList.removeAll { $0.id == id }
    }

    func createInvite() async throws -> URL {
        try await simulate("friends", 200)
        return URL(string: "https://sidequests.app/invite/\(user.username ?? "jordanlee")")!
    }

    /// Demo invites use the inviter's handle as the code ("sam" → Sam).
    func acceptInvite(code: String) async throws -> Friend {
        try await simulate("friends", 250)
        let handle = code.lowercased()
        guard let person = MockPeople.all.first(where: { $0.id != me.id && ($0.username ?? $0.firstName.lowercased()) == handle }) else {
            throw APIError.validation("That invite link didn't work. Ask for a new one.")
        }
        if let existing = friendList.first(where: { $0.person.id == person.id }) { return existing }
        let friend = Friend(person: person, statusLine: "Just added", activity: .new)
        friendList.insert(friend, at: 0)
        seedDM(with: person, subtitle: "Just added")
        return friend
    }
}

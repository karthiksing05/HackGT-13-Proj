import Foundation

/// In-memory backend for the offline demo (the default). Behaves like the real server: it owns
/// plan generation, route timing, split math and ratings, and keeps state while the app runs.
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
    private var itins = MockData.itineraries()
    private var days = MockData.calendarDays()
    private var past = MockData.pastEvents()
    private var notes: [String: String] = [:]
    private var itemRatings: [String: Rating] = [:]
    private var intents: [String: CheckoutIntent] = [:]
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
    private var options: [String: PlanOption] = [:]
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

    private func simulate(_ group: String, _ milliseconds: Int = 180) async throws {
        let ns = UInt64(Double(milliseconds) * latencyScale * 1_000_000)
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
                GroupPhoto(id: nextId("ph"), byName: seed.photoBy[i % seed.photoBy.count], url: nil,
                           placeholderHex: MockData.photoColors[i % MockData.photoColors.count], imageData: nil)
            }
            let memberIds = seed.thread.members.map(\.id)
            expensesByGroup[id] = seed.expenses.map { e in
                Expense(id: nextId("ex"), what: e.what, amountCents: e.cents, payerId: e.payer.id, splitAmong: memberIds,
                        shares: SplitMath.equalShares(totalCents: e.cents, count: memberIds.count))
            }
        }
        for friend in friendList { seedDM(with: friend.person, subtitle: friend.statusLine) }
        for id in groupOrder { refreshGroupSummary(id) }
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
        // A brand-new account has no calendar connected and no saved card yet (Setup steps 2 and 4).
        connected = [.google: false, .outlook: false]
        cards = []
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
    }

    func tasteProfile() async throws -> TasteProfile {
        try await simulate("me", 150)
        return taste
    }

    func registerDevice(pushToken: String) async throws {}

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

    func addPaymentMethod(token: String) async throws -> PaymentMethod {
        try await simulate("me", 500)
        let card = PaymentMethod(id: "pm-4242", brand: "Visa", last4: "4242", isDefault: true)
        cards = [card]
        return card
    }

    func deletePaymentMethod(id: String) async throws {
        try await simulate("me", 150)
        cards.removeAll { $0.id == id }
    }

    // MARK: - Calendar / places / events

    func calendarDays(from: Date, to: Date) async throws -> [CalendarDay] {
        try await simulate("calendar")
        let lo = clock.startOfDay(from), hi = clock.startOfDay(to)
        return days.filter { $0.date >= lo && $0.date <= hi }
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
            if var item = itin.items.first(where: { $0.id == id }) {
                item.notes = notes[id] ?? item.notes
                item.rating = itemRatings[id] ?? item.rating
                return item
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
                notes: notes[id], rating: itemRatings[id]
            )
        }
        throw APIError.notFound
    }

    // MARK: - Planning

    func generatePlans(_ request: PlanRequest) async throws -> PlanBatch {
        try await simulate("plans", 700)
        batchesServed = 0
        for option in MockData.firstOptions { options[option.id] = option }
        return PlanBatch(options: MockData.firstOptions, cursor: "batch-0", done: false)
    }

    func moreOptions(cursor: String) async throws -> PlanBatch {
        try await simulate("plans", 900)
        guard batchesServed < MockData.moreOptionBatches.count else {
            return PlanBatch(options: [], cursor: nil, done: true)
        }
        let batch = MockData.moreOptionBatches[batchesServed]
        batchesServed += 1
        for option in batch { options[option.id] = option }
        let done = batchesServed >= MockData.moreOptionBatches.count
        return PlanBatch(options: batch, cursor: done ? nil : "batch-\(batchesServed)", done: done)
    }

    func route(_ request: RouteRequest) async throws -> RouteResult {
        try await simulate("plans", 250)
        guard let option = options[request.optionId] ?? (MockData.firstOptions + MockData.moreOptionBatches.flatMap { $0 }).first(where: { $0.id == request.optionId }) else {
            throw APIError.notFound
        }
        let ordered = request.stopOrder.compactMap { id in option.stops.first { $0.id == id } }
        return MockRouteEngine.route(stops: ordered.isEmpty ? option.stops : ordered, start: request.start, end: request.end,
                                     startTime: request.startTime, backBy: request.backBy, ride: request.ride)
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
                items.append(ItineraryItem(id: nextId("stop"), kind: .sidequest, title: stop.title, place: stop.place,
                                           start: time.start, end: time.end, description: stop.subtitle))
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
        return itins.map { itin in
            var copy = itin
            copy.items = itin.items.map { item in
                var i = item
                i.notes = notes[item.id] ?? item.notes
                i.rating = itemRatings[item.id] ?? item.rating
                return i
            }
            return copy
        }
    }

    func itinerary(id: String) async throws -> Itinerary {
        try await simulate("itineraries", 120)
        guard let itin = itins.first(where: { $0.id == id }) else { throw APIError.notFound }
        return itin
    }

    func deleteItinerary(id: String) async throws {
        try await simulate("itineraries", 200)
        itins.removeAll { $0.id == id }
    }

    func updateItemNotes(itineraryId: String?, itemId: String, notes text: String) async throws {
        try await simulate("itineraries", 150)
        notes[itemId] = text
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

    func rate(itemId: String, rating: Rating) async throws {
        try await simulate("past", 250)
        if let index = past.firstIndex(where: { $0.id == itemId }) {
            past[index].rating = rating
        } else {
            itemRatings[itemId] = rating
        }
    }

    // MARK: - Checkout

    func createCheckoutIntent(itemId: String, quantity: Int) async throws -> CheckoutIntent {
        try await simulate("checkout", 600)
        let item = try? await eventDetail(id: itemId)
        let intent = CheckoutIntent(id: nextId("ci"), itemId: itemId, itemTitle: item?.title ?? "Tickets",
                                    steps: MockData.checkoutSteps, subtotalCents: item?.priceCents, feesCents: nil, totalCents: nil,
                                    cardBrand: "Visa", cardLast4: "4242", state: .awaitingApproval)
        intents[intent.id] = intent
        return intent
    }

    func checkoutIntent(id: String) async throws -> CheckoutIntent {
        try await simulate("checkout", 120)
        guard let intent = intents[id] else { throw APIError.notFound }
        return intent
    }

    func approveCheckout(id: String) async throws -> CheckoutIntent {
        try await simulate("checkout", 900)
        guard var intent = intents[id] else { throw APIError.notFound }
        intent.state = .booked
        intent.steps = intent.steps.map { CheckoutStep(text: $0.text, done: true) }
        intents[id] = intent
        return intent
    }

    func cancelCheckout(id: String) async throws {
        try await simulate("checkout", 120)
        intents[id]?.state = .cancelled
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

    func postFreeNow(visibility: ForumPostVisibility) async throws -> MyFreePost {
        try await simulate("forum", 300)
        let post = MyFreePost(id: "mine", visibility: visibility, text: "Free until 6:30 PM near Tech Square")
        myPost = post
        return post
    }

    func deleteForumPost(id: String) async throws {
        try await simulate("forum", 200)
        if id == myPost?.id { myPost = nil }
    }

    func requestToJoin(postId: String) async throws {
        try await simulate("forum", 300)
        guard let index = posts.firstIndex(where: { $0.id == postId }) else { throw APIError.notFound }
        posts[index].joinRequested = true
    }

    func cancelJoinRequest(postId: String) async throws {
        try await simulate("forum", 200)
        guard let index = posts.firstIndex(where: { $0.id == postId }) else { throw APIError.notFound }
        posts[index].joinRequested = false
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
        let dms = threadsById.values.filter { !$0.isGroup }.sorted { $0.title < $1.title }
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
        return messagesByThread[threadId] ?? []
    }

    func sendMessage(threadId: String, text: String) async throws -> Message {
        try await simulate("threads", 150)
        guard threadsById[threadId] != nil else { throw APIError.notFound }
        let message = Message(id: nextId("m"), senderId: me.id, senderName: "You", text: text, sentAt: clock.now)
        messagesByThread[threadId, default: []].append(message)
        threadsById[threadId]?.lastMessage = "You: \(text)"
        threadsById[threadId]?.lastTime = format.time(clock.now)
        return message
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
        let photo = GroupPhoto(id: nextId("ph"), byName: "You", url: nil, placeholderHex: nil, imageData: jpegData)
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
                            shares: SplitMath.equalShares(totalCents: expense.amountCents, count: expense.splitAmong.count))
        expensesByGroup[groupId, default: []].append(saved)
        refreshGroupSummary(groupId)
        return saved
    }

    func deleteExpense(groupId: String, expenseId: String) async throws {
        try await simulate("splits", 200)
        expensesByGroup[groupId]?.removeAll { $0.id == expenseId }
        refreshGroupSummary(groupId)
    }

    func settleUp(groupId: String) async throws {
        try await simulate("splits", 900)
        guard let thread = threadsById[groupId] else { throw APIError.notFound }
        // Record a settling payment from you to everyone you owe.
        let balances = SplitMath.balances(expenses: expensesByGroup[groupId] ?? [], me: me.id, members: thread.members.map(\.id))
        for balance in balances where balance.netCents < 0 {
            expensesByGroup[groupId, default: []].append(
                Expense(id: nextId("ex"), what: "Settled up with Visa", amountCents: -balance.netCents, payerId: me.id,
                        splitAmong: [balance.userId], shares: [-balance.netCents]))
        }
        refreshGroupSummary(groupId)
    }

    // MARK: - Friends

    func friends() async throws -> [Friend] {
        try await simulate("friends")
        return friendList
    }

    func searchUsers(query: String) async throws -> [PersonRef] {
        try await simulate("friends", 150)
        let q = query.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "@ "))
        guard !q.isEmpty else { return [] }
        return MockPeople.all.filter { $0.id != me.id && $0.name.lowercased().contains(q) }
    }

    func friendRequests() async throws -> [FriendRequest] {
        try await simulate("friends", 120)
        return requests
    }

    func sendFriendRequest(userId: String) async throws { try await simulate("friends", 200) }

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
        return URL(string: "https://sidequestz.app/invite/\(user.username ?? "jordanlee")")!
    }
}

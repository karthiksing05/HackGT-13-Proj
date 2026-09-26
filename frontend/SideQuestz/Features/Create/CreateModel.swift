import Foundation
import Observation

/// Which pin the Where step is editing.
enum CreatePin: Hashable {
    case start, end
}

/// A suggestion pill under the Where search field. The first one is "Current location".
enum CreateSuggestion: Identifiable, Hashable {
    case currentLocation
    case place(Place)

    var id: String {
        switch self {
        case .currentLocation: "current-location"
        case .place(let place): "place-\(place.name)"
        }
    }

    var label: String {
        switch self {
        case .currentLocation: "Current location"
        case .place(let place): place.name
        }
    }
}

/// Route state for one plan option: the stop order the user wants, and the server's timing for
/// the order it last calculated.
struct CreateRouteState: Equatable {
    /// Stop ids in display order.
    var order: [String]
    var result: RouteResult?
    /// The order `result` was calculated for.
    var resultOrder: [String] = []
    var isLoading = false
    var error: String?
    /// The request whose answer this state is waiting for; late responses for older requests are ignored.
    var generation = 0

    /// true when the times on screen don't belong to the current order (first load, mid-drag,
    /// or waiting for `POST /plans/route`).
    var isStale: Bool { result == nil || resultOrder != order }
}

/// "Recalculating transit…" / "Transit times updated" above the route card.
enum CreateTransitStatus: Equatable {
    case idle, recalculating, updated
}

/// Everything the Create flow collects and shows. All data comes from `env.api` (and
/// `env.places` / `env.location` / `env.voice`); the server owns plan generation and timing.
@Observable
final class CreateFlowModel {
    /// Quick picks on the Vibe step (fixed UI choices, in display order).
    static let quickPicks = ["Outdoors", "Food", "Art", "Music", "Chill", "Active", "Meet people", "Nerdy", "Nightlife"]
    static let budgetLabels = ["Free", "$", "$$", "$$$"]
    /// Default max group size for shared plans (More options › Group).
    static let defaultMaxGroupSize = 6
    /// Joining locks this long before you leave (1:30 PM for a 2:10 PM start).
    static let lockLeadMinutes = 40
    static let voiceDemoTranscript = "Something chill and outside, then cheap food after. Maybe meet a couple people."

    @ObservationIgnored let env: AppEnvironment

    // MARK: Flow

    /// 1 Where · 2 When · 3 Vibe · 4 Review
    private(set) var step: Int
    var moreOpen = false
    /// `create/4/more`: open More options once the first options load.
    @ObservationIgnored var openMoreAfterLoad = false

    // MARK: Where

    var start: Place?
    var end: Place?
    var endSameAsStart = false {
        didSet { if endSameAsStart { activePin = .start } }
    }
    var activePin: CreatePin = .start
    var search = ""
    private(set) var suggestions: [CreateSuggestion] = []
    /// false until the first suggestions arrive (the pills show a skeleton until then).
    private(set) var suggestionsLoaded = false
    /// A place search is in flight (a small indicator in the search field; the pills stay).
    private(set) var searchingPlaces = false
    @ObservationIgnored private var suggestionGeneration = 0
    var range: TravelRange = .transit
    var ride: RideChoice = .none
    var openSeats = 3
    private(set) var placesLoaded = false
    @ObservationIgnored private var defaultsTask: Task<Void, Never>?
    @ObservationIgnored private var currentPlace: Place?
    /// Bumped on every pin change so a slow reverse-geocode can't overwrite a newer pick.
    @ObservationIgnored private var pinGeneration = 0

    // MARK: When

    private(set) var date: Date
    private(set) var startTime: Date
    private(set) var backBy: Date
    var calendarOpen = false
    private(set) var calendarItems: Loadable<[CalendarItem]> = .loading
    /// The day `calendarItems` belongs to (reopening the same day keeps them while they refresh).
    @ObservationIgnored private var calendarDayKey: String?
    /// Reloading a day whose items are already on screen (a small indicator; the items stay).
    private(set) var calendarRefreshing = false

    // MARK: Vibe

    var moodText = ""
    /// The last voice transcript (shown in quotes on the voice card).
    private(set) var transcript: String?
    private(set) var tags: Set<String> = ["Outdoors", "Food", "Meet people"]
    /// 0 Free … 3 $$$. Starts at the money preference until the user picks one.
    var budget: Int { didSet { budgetTouched = true } }
    var who: Visibility = .friends
    @ObservationIgnored private var budgetTouched = false

    // MARK: More options

    var modes: Set<TravelMode> = [.walk, .marta]
    /// Starts at the pace preference until the user picks one.
    var pace: Pace { didSet { paceTouched = true } }
    @ObservationIgnored private var paceTouched = false

    // MARK: Review

    private(set) var options: Loadable<[PlanOption]> = .loading
    private(set) var selectedOptionId: String?
    private(set) var cursor: String?
    private(set) var noMoreOptions = false
    private(set) var loadingMore = false
    private(set) var loadMoreError: String?
    private(set) var routes: [String: CreateRouteState] = [:]
    private(set) var transitStatus: CreateTransitStatus = .idle
    /// The stop being dragged on the route card (disables page scrolling).
    private(set) var draggingStopId: String?
    @ObservationIgnored private var dragMoved = false
    /// Bumped on every drop that changed the order (drives the medium haptic).
    private(set) var dropCount = 0
    private(set) var creating = false
    private(set) var createError: String?
    @ObservationIgnored private var lastRequest: PlanRequest?
    @ObservationIgnored private var generateGeneration = 0
    /// Increases with every POST /plans/route, across regenerations.
    @ObservationIgnored private var routeRequestCounter = 0

    init(draft: CreateDraft, env: AppEnvironment) {
        self.env = env
        let clock = env.clock
        step = min(4, max(1, draft.step))

        // Calendar › "Plan this window" prefills date/start/end; otherwise start now (2:10 PM in
        // the demo) and be back 4 h 20 min later (6:30 PM).
        let day = clock.startOfDay(draft.date ?? draft.start ?? clock.now)
        let start = draft.start ?? Self.roundedUp(clock.now, toMinutes: 5, clock: clock)
        date = day
        startTime = start
        backBy = draft.end ?? clock.addingMinutes(260, to: start)

        budget = env.preferences?.spend.defaultBudget ?? 1
        pace = env.preferences?.pace ?? .balanced
    }

    private static func roundedUp(_ date: Date, toMinutes step: Int, clock: AppClock) -> Date {
        let minutes = clock.minutesIntoDay(date)
        let rounded = (minutes + step - 1) / step * step
        return clock.addingMinutes(rounded, to: clock.startOfDay(date))
    }

    // MARK: - Navigation

    var canGoNext: Bool {
        switch step {
        case 1: start != nil && (endSameAsStart || end != nil)
        default: true
        }
    }

    func go(to newStep: Int) {
        let target = min(4, max(1, newStep))
        guard target != step else { return }
        if step == 3 { env.voice.cancel() }
        step = target
        if target == 4 { Task { await enterReview() } }
    }

    /// Applies preference defaults that arrived after the flow opened (unless already changed).
    /// Setting them marks them touched, so the flags are reset right after.
    func applyPreferenceDefaults() {
        guard let prefs = env.preferences else { return }
        if !budgetTouched {
            budget = prefs.spend.defaultBudget
            budgetTouched = false
        }
        if !paceTouched {
            pace = prefs.pace
            paceTouched = false
        }
    }

    // MARK: - Where

    /// Default start/end: the first two places the API returns for an empty search.
    func loadDefaultPlacesIfNeeded() async {
        if let defaultsTask { return await defaultsTask.value }
        let task = Task {
            let places = await env.places.suggestions(for: "", near: nil, limit: 2)
            if start == nil { start = places.first }
            if start == nil { start = await currentLocation() }
            if end == nil, !endSameAsStart {
                if let second = places.dropFirst().first { end = second } else { endSameAsStart = true }
            }
            placesLoaded = true
        }
        defaultsTask = task
        await task.value
    }

    private func currentLocation() async -> Place {
        if let currentPlace { return currentPlace }
        let place = await env.location.currentLocation()
        currentPlace = place
        return place
    }

    /// The pin a pick or a map tap changes (Start while "End where I start" is on).
    var editingPin: CreatePin { endSameAsStart ? .start : activePin }

    var endPlace: Place? { endSameAsStart ? start : end }

    func selectPin(_ pin: CreatePin) {
        if pin == .end && endSameAsStart { return }
        activePin = pin
    }

    func loadSuggestions() async {
        suggestionGeneration += 1
        let generation = suggestionGeneration
        searchingPlaces = true
        defer { if generation == suggestionGeneration { searchingPlaces = false } }

        let query = search.trimmingCharacters(in: .whitespacesAndNewlines)
        let near = (editingPin == .end ? end : start)?.coordinate ?? start?.coordinate
        var results = await env.places.suggestions(for: query, near: near, limit: 5)
        let current = await currentLocation()
        // A newer search (the next keystroke) replaced this one: don't flash its stale results.
        guard generation == suggestionGeneration, !Task.isCancelled else { return }
        results.removeAll { $0.name == current.name }
        var pills: [CreateSuggestion] = []
        if query.isEmpty || "current location".localizedCaseInsensitiveContains(query)
            || current.name.localizedCaseInsensitiveContains(query) {
            pills.append(.currentLocation)
        }
        pills += results.map { .place($0) }
        suggestions = Array(pills.prefix(4))
        suggestionsLoaded = true
    }

    func pick(_ suggestion: CreateSuggestion) async {
        let place: Place
        switch suggestion {
        case .currentLocation: place = await currentLocation()
        case .place(let p): place = p
        }
        setPlace(place, for: editingPin)
        search = ""
    }

    func setPlace(_ place: Place, for pin: CreatePin) {
        pinGeneration += 1
        switch pin {
        case .start: start = place
        case .end: end = place
        }
    }

    /// Map tap: the editing pin lands on the tapped point immediately (synchronously, in the same
    /// update as the tap), then "Dropped pin" is replaced by a readable name when reverse geocoding
    /// returns. The coordinate stays the tapped one.
    func dropPin(at coordinate: Coordinate) {
        let pin = editingPin
        setPlace(Place(name: "Dropped pin", coordinate: coordinate), for: pin)
        let generation = pinGeneration
        Task {
            let named = await env.places.place(for: coordinate)
            guard generation == pinGeneration else { return }
            switch pin {
            case .start: start = named
            case .end: end = named
            }
        }
    }

    // MARK: - When

    func setDate(_ newDate: Date) {
        let clock = env.clock
        let day = clock.startOfDay(newDate)
        let startMinutes = clock.minutesIntoDay(startTime)
        let endMinutes = clock.minutesIntoDay(backBy)
        date = day
        startTime = clock.addingMinutes(startMinutes, to: day)
        backBy = normalizedBackBy(minutes: endMinutes)
    }

    func setStartTime(_ time: Date) {
        let clock = env.clock
        let endMinutes = clock.minutesIntoDay(backBy)
        startTime = clock.addingMinutes(clock.minutesIntoDay(time), to: date)
        backBy = normalizedBackBy(minutes: endMinutes)
    }

    func setBackBy(_ time: Date) {
        backBy = normalizedBackBy(minutes: env.clock.minutesIntoDay(time))
    }

    /// "Back at end by" on the chosen day, or the next morning when it's earlier than the start.
    private func normalizedBackBy(minutes: Int) -> Date {
        let clock = env.clock
        let sameDay = clock.addingMinutes(minutes, to: date)
        return sameDay > startTime ? sameDay : clock.calendar.date(byAdding: .day, value: 1, to: sameDay) ?? sameDay
    }

    /// Loads the day's calendar. Reopening the same day keeps the items on screen while they
    /// refresh (and keeps them if the refresh fails); another day starts from the skeleton.
    func loadCalendar() async {
        let day = date
        let key = env.clock.dayKey(day)
        let sameDay = key == calendarDayKey && calendarItems.value != nil
        if sameDay { calendarRefreshing = true } else { calendarItems = .loading }
        defer { if sameDay { calendarRefreshing = false } }
        let result: Loadable<[CalendarItem]> = await .run {
            let days = try await env.api.calendarDays(from: day, to: day)
            let items = days.first { $0.id == key || env.clock.calendar.isDate($0.date, inSameDayAs: day) }?.items ?? []
            return items.sorted { $0.start < $1.start }
        }
        // The date changed (or the section closed) while this was loading: a newer load owns the list.
        guard !Task.isCancelled, env.clock.dayKey(date) == key else { return }
        if sameDay, case .failed = result { return }
        calendarItems = result
        calendarDayKey = key
    }

    // MARK: - Vibe

    func toggleTag(_ tag: String) {
        if tags.contains(tag) { tags.remove(tag) } else { tags.insert(tag) }
    }

    /// Selected quick picks in display order.
    var selectedTags: [String] { Self.quickPicks.filter { tags.contains($0) } }

    /// Voice: the transcript fills the "Or type it" field.
    func applyTranscript(_ text: String) {
        transcript = text
        let current = moodText.trimmingCharacters(in: .whitespacesAndNewlines)
        moodText = current.isEmpty ? text : current + " " + text
    }

    // MARK: - Review: options

    var planRequest: PlanRequest? {
        guard let start, let end = endPlace else { return nil }
        return PlanRequest(start: start, end: end, date: date, startTime: startTime, backBy: backBy,
                           range: range, ride: ride, openSeats: ride == .drive ? openSeats : nil,
                           moodText: moodText.trimmingCharacters(in: .whitespacesAndNewlines), tags: selectedTags,
                           budget: budget, who: who, pace: pace, modes: modes)
    }

    var optionList: [PlanOption] { options.value ?? [] }

    var selectedOption: PlanOption? {
        optionList.first { $0.id == selectedOptionId } ?? optionList.first
    }

    /// Entering Review: (re)generate when the answers changed since the last batch.
    func enterReview() async {
        await loadDefaultPlacesIfNeeded()
        guard let request = planRequest else {
            options = .failed("Add where you start and end first.")
            return
        }
        if request == lastRequest, options.value != nil { return }
        await generate(request)
    }

    /// More options › "Regenerate options with these settings", and error retries.
    func regenerate() async {
        await loadDefaultPlacesIfNeeded()
        guard let request = planRequest else {
            options = .failed("Add where you start and end first.")
            return
        }
        await generate(request)
    }

    private func generate(_ request: PlanRequest) async {
        generateGeneration += 1
        let generation = generateGeneration
        lastRequest = request
        options = .loading
        selectedOptionId = nil
        routes = [:]
        cursor = nil
        noMoreOptions = false
        loadingMore = false
        loadMoreError = nil
        transitStatus = .idle
        createError = nil
        do {
            let batch = try await env.api.generatePlans(request)
            guard generation == generateGeneration else { return }
            options = .loaded(batch.options)
            cursor = batch.cursor
            noMoreOptions = batch.done || batch.cursor == nil
            for option in batch.options { routes[option.id] = CreateRouteState(order: option.stops.map(\.id)) }
            if let first = batch.options.first { select(first.id) }
            if openMoreAfterLoad {
                openMoreAfterLoad = false
                moreOpen = true
            }
        } catch {
            guard generation == generateGeneration else { return }
            lastRequest = nil
            options = .failed(Self.message(for: error))
        }
    }

    func select(_ optionId: String) {
        guard optionId != selectedOptionId else { return }
        selectedOptionId = optionId
        transitStatus = .idle
        if let state = routes[optionId], state.isStale, !state.isLoading {
            Task { await refreshRoute(optionId, minimumSeconds: 0) }
        }
    }

    /// "Load more options": append the next batch and select its first option.
    func loadMoreOptions() async {
        guard !loadingMore, !noMoreOptions, let cursor, case .loaded(let current) = options else { return }
        let generation = generateGeneration
        loadingMore = true
        loadMoreError = nil
        do {
            let batch = try await env.api.moreOptions(cursor: cursor)
            guard generation == generateGeneration else { return }
            loadingMore = false
            options = .loaded(current + batch.options)
            self.cursor = batch.cursor
            noMoreOptions = batch.done || batch.cursor == nil || batch.options.isEmpty
            for option in batch.options { routes[option.id] = CreateRouteState(order: option.stops.map(\.id)) }
            if let first = batch.options.first { select(first.id) }
        } catch {
            guard generation == generateGeneration else { return }
            loadingMore = false
            loadMoreError = Self.message(for: error)
        }
    }

    /// Stop titles in the option's current order ("A → B → C" on the option card).
    func orderedStops(_ option: PlanOption) -> [PlanStop] {
        let order = routes[option.id]?.order ?? option.stops.map(\.id)
        let byId = Dictionary(uniqueKeysWithValues: option.stops.map { ($0.id, $0) })
        let ordered = order.compactMap { byId[$0] }
        return ordered.count == option.stops.count ? ordered : option.stops
    }

    // MARK: - Review: reorder + route

    func beginDrag(_ stopId: String) {
        draggingStopId = stopId
        dragMoved = false
    }

    /// Moves the dragged stop one slot up (-1) or down (+1) while dragging.
    func moveDraggedStop(by delta: Int) {
        guard let optionId = selectedOptionId, let stopId = draggingStopId else { return }
        if moveStop(stopId, in: optionId, by: delta) { dragMoved = true }
    }

    func endDrag() {
        guard draggingStopId != nil else { return }
        draggingStopId = nil
        if dragMoved, let optionId = selectedOptionId { commitReorder(optionId) }
        dragMoved = false
    }

    /// VoiceOver adjustable action: increment moves the stop down, decrement moves it up.
    func moveStopAccessibly(_ stopId: String, by delta: Int) {
        guard let optionId = selectedOptionId, moveStop(stopId, in: optionId, by: delta) else { return }
        commitReorder(optionId)
    }

    @discardableResult
    private func moveStop(_ stopId: String, in optionId: String, by delta: Int) -> Bool {
        guard var state = routes[optionId], let from = state.order.firstIndex(of: stopId) else { return false }
        let to = from + delta
        guard state.order.indices.contains(to) else { return false }
        state.order.swapAt(from, to)
        routes[optionId] = state
        return true
    }

    private func commitReorder(_ optionId: String) {
        dropCount += 1
        transitStatus = .recalculating
        Task { await refreshRoute(optionId, minimumSeconds: 0.9) }
    }

    /// POST /plans/route for the option's current order. After a drop the "Updating transit…"
    /// state stays up for at least `minimumSeconds` so the change reads.
    func refreshRoute(_ optionId: String, minimumSeconds: Double) async {
        guard var state = routes[optionId], let option = optionList.first(where: { $0.id == optionId }),
              let start, let end = endPlace else { return }
        routeRequestCounter += 1
        state.generation = routeRequestCounter
        state.isLoading = true
        state.error = nil
        let generation = state.generation
        let order = state.order
        routes[optionId] = state

        let request = RouteRequest(optionId: option.id, stopOrder: order, start: start, end: end,
                                   startTime: startTime, backBy: backBy, ride: ride, modes: modes)
        let began = Date()
        let outcome: Result<RouteResult, any Error>
        do {
            outcome = .success(try await env.api.route(request))
        } catch {
            outcome = .failure(error)
        }
        let remaining = minimumSeconds - Date().timeIntervalSince(began)
        if remaining > 0 { try? await Task.sleep(nanoseconds: UInt64(remaining * 1_000_000_000)) }

        guard var latest = routes[optionId], latest.generation == generation else { return }
        latest.isLoading = false
        switch outcome {
        case .success(let result):
            latest.result = result
            latest.resultOrder = order
            if optionId == selectedOptionId, minimumSeconds > 0 { transitStatus = .updated }
        case .failure(let error):
            latest.error = Self.message(for: error)
            if optionId == selectedOptionId { transitStatus = .idle }
        }
        routes[optionId] = latest
    }

    // MARK: - Start

    var canStart: Bool { selectedOption != nil && !creating }

    /// "Start this sidequest" → POST /itineraries. Returns the new itinerary, or nil on failure.
    func startSidequest() async -> Itinerary? {
        guard let option = selectedOption, let plan = planRequest ?? lastRequest, !creating else { return nil }
        creating = true
        createError = nil
        defer { creating = false }

        // Make sure the timing matches the order on screen (a drop may still be recalculating).
        for _ in 0..<50 where routes[option.id]?.isLoading == true {
            try? await Task.sleep(nanoseconds: 100_000_000)
        }
        if routes[option.id]?.isStale ?? true {
            await refreshRoute(option.id, minimumSeconds: 0)
        }
        guard let state = routes[option.id], let route = state.result, !state.isStale else {
            createError = routes[option.id]?.error ?? "We couldn't time this route. Try again."
            return nil
        }
        let shared = who != .justMe
        let request = CreateItineraryRequest(
            plan: plan, option: option, stopOrder: state.order, route: route, visibility: who,
            lockAt: shared ? lockAt : nil, maxGroupSize: shared ? Self.defaultMaxGroupSize : nil
        )
        do {
            return try await env.api.createItinerary(request)
        } catch {
            createError = Self.message(for: error)
            return nil
        }
    }

    /// Shared plans lock joining a little before you leave.
    var lockAt: Date { env.clock.addingMinutes(-Self.lockLeadMinutes, to: startTime) }

    // MARK: - Helpers

    static func message(for error: any Error) -> String {
        (error as? LocalizedError)?.errorDescription ?? "Something went wrong."
    }
}

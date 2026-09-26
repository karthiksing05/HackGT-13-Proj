import Foundation
import Observation

/// Which pin the Where step is editing.
enum CreatePin: Hashable {
    case start, end
}

/// A suggestion pill under the Where search field. The first one is "Current location" when the
/// phone can say where it is.
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

/// Review › hold a stop › Swap: the stop whose similar alternatives are showing.
struct CreateSwapTarget: Identifiable, Equatable {
    let optionId: String
    let stop: PlanStop
    var id: String { "\(optionId)/\(stop.id)" }
}

/// A stop just removed on Review, and where it was, so "Undo" can put it back.
struct CreateStopRemoval: Equatable {
    let optionId: String
    let stop: PlanStop
    /// Its place in the option's stops and in the route order.
    let index: Int
    let slot: Int
}

/// "Recalculating transit…" / "Transit times updated" above the route card.
enum CreateTransitStatus: Equatable {
    case idle, recalculating, updated
}

/// Everything the Create flow collects and shows. All data comes from `env.api` (and
/// `env.places` / `env.location` / `env.voice`); the server owns plan generation and timing.
///
/// Starting answers: the demo (mock mode) opens on the prototype's picks. A real account starts
/// from its preferences (budget, pace, who's coming) and otherwise neutral choices: no quick
/// picks, a 3-hour window, just you. In both, getting around follows the ride answer ("No ride"
/// is walk + MARTA; driving or covering rides allows all three) until the user picks modes.
@Observable
final class CreateFlowModel {
    /// Quick picks on the Vibe step (fixed UI choices, in display order).
    static let quickPicks = ["Outdoors", "Food", "Art", "Music", "Chill", "Active", "Meet people", "Nerdy", "Nightlife"]
    static let budgetLabels = ["Free", "$", "$$", "$$$"]
    /// More options › Getting around.
    static let travelModes: [TravelMode] = [.walk, .marta, .rideshare]
    /// Shared plans (More options › Group): joining locks this long before you leave…
    static let defaultLockLeadMinutes = 40
    /// …and the group tops out at this many people, you included.
    static let defaultMaxGroupSize = 6
    static let groupSizes = 2...20
    /// A plan's window when nothing sets its end: 3 hours (the demo: 4 h 20 min, 2:10 → 6:30 PM).
    static let defaultWindowMinutes = 180
    static let voiceDemoTranscript = "Something chill and outside, then cheap food after. Maybe meet a couple people."

    // The prototype's answers, kept for the demo and the UI tests.
    private static let demoTags: Set<String> = ["Outdoors", "Food", "Meet people"]
    private static let demoWindowMinutes = 260

    @ObservationIgnored let env: AppEnvironment

    // MARK: Flow

    /// 1 Where · 2 When · 3 Vibe · 4 Review
    private(set) var step: Int
    var moreOpen = false
    /// `create/4/more`: open More options once the first options load.
    @ObservationIgnored var openMoreAfterLoad = false
    /// Review › hold a stop › Swap: the alternatives sheet is open for this stop.
    var swapTarget: CreateSwapTarget?
    /// `create/4/swap`: open the swap sheet for the second stop once the first options load.
    @ObservationIgnored var openSwapAfterLoad = false

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
    /// "Can you provide a ride this time?" Getting around follows it until the user picks modes.
    var ride: RideChoice = .none {
        didSet { if !modesTouched { modes = Self.defaultModes(for: ride) } }
    }
    var openSeats = 3
    /// The default pins have been worked out (both rows stop shimmering).
    private(set) var placesLoaded = false
    /// The phone's location, once it has answered (nil while asking, or when it can't say).
    private(set) var currentPlace: Place?
    /// The location request has answered, with a place or with nothing.
    private(set) var locationResolved = false
    @ObservationIgnored private var defaultsTask: Task<Void, Never>?
    /// The one location request of this flow: the default start and the "Current location" pill
    /// share it.
    @ObservationIgnored private var locationTask: Task<Place?, Never>?
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
    private(set) var tags: Set<String>
    /// 0 Free … 3 $$$. Starts at the money preference until the user picks one.
    var budget: Int { didSet { budgetTouched = true } }
    /// Starts at the company preference (live) until the user picks one.
    var who: Visibility { didSet { whoTouched = true } }
    @ObservationIgnored private var budgetTouched = false
    @ObservationIgnored private var whoTouched = false

    // MARK: More options

    /// Getting around: from the ride answer until the user changes it (`toggleMode`).
    private(set) var modes: Set<TravelMode>
    @ObservationIgnored private var modesTouched = false
    /// Starts at the pace preference until the user picks one.
    var pace: Pace { didSet { paceTouched = true } }
    @ObservationIgnored private var paceTouched = false
    /// Shared plans: joining locks this many minutes before you leave. nil until the user picks a
    /// time (then `lockAt` uses the default lead).
    private(set) var lockLeadMinutes: Int?
    /// Shared plans: the most people who can be on it, you included.
    private(set) var maxGroupSize = CreateFlowModel.defaultMaxGroupSize

    // MARK: Review

    private(set) var options: Loadable<[PlanOption]> = .loading
    private(set) var selectedOptionId: String?
    private(set) var cursor: String?
    private(set) var noMoreOptions = false
    private(set) var loadingMore = false
    private(set) var loadMoreError: String?
    private(set) var routes: [String: CreateRouteState] = [:]
    /// Options as edited on Review (swapped-in stops in their slots, removed ones gone), by id.
    private(set) var editedOptions: [String: PlanOption] = [:]
    /// Just swapped in: its row gets a brief tint.
    private(set) var swappedStopId: String?
    /// The last stop removed on Review ("Removed … · Undo").
    private(set) var lastRemoval: CreateStopRemoval?
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
        // the demo) and be back 3 hours later (the demo: 6:30 PM).
        let day = clock.startOfDay(draft.date ?? draft.start ?? clock.now)
        let start = draft.start ?? Self.roundedUp(clock.now, toMinutes: 5, clock: clock)
        date = day
        startTime = start
        backBy = draft.end ?? clock.addingMinutes(env.isMock ? Self.demoWindowMinutes : Self.defaultWindowMinutes, to: start)
        // Home › search › a place: plan a sidequest that ends there (the start stays where you are).
        end = draft.destination

        let preferences = env.preferences ?? Preferences()
        budget = preferences.spend.defaultBudget
        pace = preferences.pace
        modes = Self.defaultModes(for: .none)
        if env.isMock {
            tags = Self.demoTags
            who = .friends
        } else {
            tags = []
            who = env.preferences.map { Self.visibility(for: $0.company) } ?? .justMe
        }
    }

    private static func roundedUp(_ date: Date, toMinutes step: Int, clock: AppClock) -> Date {
        let minutes = clock.minutesIntoDay(date)
        let rounded = (minutes + step - 1) / step * step
        return clock.addingMinutes(rounded, to: clock.startOfDay(date))
    }

    /// Getting around for a ride answer: "No ride" keeps it to walking and MARTA; driving or
    /// covering rides says nothing against any mode, so all three are on.
    static func defaultModes(for ride: RideChoice) -> Set<TravelMode> {
        ride == .none ? [.walk, .marta] : Set(travelModes)
    }

    /// Who's coming, from Setup › "Who do you usually go with?": solo plans stay private, small
    /// groups go to friends, big groups are open to anyone nearby.
    private static func visibility(for company: Company) -> Visibility {
        switch company {
        case .solo: .justMe
        case .smallGroup: .friends
        case .bigGroup: .open
        }
    }

    // MARK: - Navigation

    var canGoNext: Bool {
        switch step {
        case 1: hasPlaces
        default: true
        }
    }

    /// Where is answered: a start, and an end (or "End where I start").
    var hasPlaces: Bool { start != nil && (endSameAsStart || end != nil) }

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
        // The demo keeps the prototype's "Friends only".
        if !env.isMock, !whoTouched {
            who = Self.visibility(for: prefs.company)
            whoTouched = false
        }
    }

    // MARK: - Where

    /// Default pins: start where the phone is (the demo: Tech Square) and end at the first other
    /// place the API suggests for an empty search (the demo: Home), or where you start when it
    /// suggests none. A pin that's already set (Home's search sets the end) is kept. Without a
    /// location the start stays empty and the Where step asks for one. The location and the
    /// suggestions are asked for at the same time; each pin fills in as its answer arrives.
    func loadDefaultPlacesIfNeeded() async {
        if let defaultsTask { return await defaultsTask.value }
        let task = Task {
            let suggestionsCall: Task<[Place], Never>? = end == nil && !endSameAsStart
                ? Task { await env.places.suggestions(for: "", near: nil, limit: 3) }
                : nil
            let here = await currentLocation()
            if start == nil { start = here }
            if let suggestionsCall {
                let places = await suggestionsCall.value
                // The user may have picked while this loaded.
                if end == nil, !endSameAsStart {
                    if let other = places.first(where: { $0.name != here?.name }) { end = other } else { endSameAsStart = true }
                }
            }
            placesLoaded = true
        }
        defaultsTask = task
        await task.value
    }

    /// The phone's location, asked once per flow: everyone who needs it waits on the same request.
    private func currentLocation() async -> Place? {
        let task: Task<Place?, Never>
        if let locationTask {
            task = locationTask
        } else {
            task = Task { await env.location.currentLocation() }
            locationTask = task
        }
        let place = await task.value
        if !locationResolved {
            currentPlace = place
            locationResolved = true
        }
        return place
    }

    /// The phone couldn't say where it is (no permission or no fix): nothing is assumed.
    var locationUnavailable: Bool { locationResolved && currentPlace == nil }

    /// The start row has its default (or knows there's none), so it stops shimmering.
    var startDefaultLoaded: Bool { locationResolved || placesLoaded }

    /// Where has no start to offer: no location and nothing picked yet. The step says so and puts
    /// the cursor in the search field.
    var needsStartSearch: Bool { locationUnavailable && start == nil }

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
        // Near the pin being set, else the other pin, else the phone.
        let (editing, other) = editingPin == .end ? (end, start) : (start, end)
        let near = editing?.coordinate ?? other?.coordinate ?? currentPlace?.coordinate
        var results = await env.places.suggestions(for: query, near: near, limit: 5)
        // A newer search (the next keystroke) replaced this one: don't flash its stale results.
        guard generation == suggestionGeneration, !Task.isCancelled else { return }
        // Results never wait for the location: the pill joins when it's known (the step reloads
        // the pills then).
        let current = currentPlace
        if let current { results.removeAll { $0.name == current.name } }
        var pills: [CreateSuggestion] = []
        if let current, query.isEmpty || "current location".localizedCaseInsensitiveContains(query)
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
        case .currentLocation:
            guard let current = currentPlace else { return }
            place = current
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

    /// The options as they stand on Review, with the stops you swapped or removed.
    var optionList: [PlanOption] { (options.value ?? []).map { editedOptions[$0.id] ?? $0 } }

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
        editedOptions = [:]
        swappedStopId = nil
        lastRemoval = nil
        swapTarget = nil
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
            if openSwapAfterLoad, let option = selectedOption, orderedStops(option).count > 1 {
                openSwapAfterLoad = false
                openSwap(orderedStops(option)[1].id, in: option.id)
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
        let current = editedOptions[option.id] ?? option
        let order = routes[option.id]?.order ?? current.stops.map(\.id)
        let byId = Dictionary(current.stops.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let ordered = order.compactMap { byId[$0] }
        return ordered.count == order.count ? ordered : current.stops
    }

    // MARK: - Review: swap + remove

    /// Hold a stop › "Swap for something similar".
    func openSwap(_ stopId: String, in optionId: String) {
        guard let option = optionList.first(where: { $0.id == optionId }),
              let stop = option.stops.first(where: { $0.id == stopId }) else { return }
        swapTarget = CreateSwapTarget(optionId: optionId, stop: stop)
    }

    /// When a stop happens on the current route ("2:24–3:44 PM" in the swap sheet), if known.
    func timeSlot(of stopId: String, in optionId: String) -> DateInterval? {
        guard let state = routes[optionId], !state.isStale, let result = state.result,
              let index = state.order.firstIndex(of: stopId), result.stopTimes.indices.contains(index) else { return nil }
        return result.stopTimes[index]
    }

    /// `alternative` takes the stop's slot, then the server re-times the route like after a drop.
    func swapStop(_ stopId: String, with alternative: PlanStop, in optionId: String) {
        guard var option = optionList.first(where: { $0.id == optionId }),
              let index = option.stops.firstIndex(where: { $0.id == stopId }),
              var state = routes[optionId], let slot = state.order.firstIndex(of: stopId),
              !state.order.contains(alternative.id) else { return }
        option.stops[index] = alternative
        editedOptions[optionId] = option
        state.order[slot] = alternative.id
        routes[optionId] = state
        lastRemoval = nil
        swappedStopId = alternative.id
        commitReorder(optionId)
        Task {
            try? await Task.sleep(for: .seconds(1.6))
            if swappedStopId == alternative.id { swappedStopId = nil }
        }
    }

    /// A plan keeps at least one stop.
    func canRemoveStop(in optionId: String) -> Bool { (routes[optionId]?.order.count ?? 0) > 1 }

    /// Hold a stop › "Remove stop". "Undo" can put it back until the next change.
    func removeStop(_ stopId: String, in optionId: String) {
        guard var option = optionList.first(where: { $0.id == optionId }),
              let index = option.stops.firstIndex(where: { $0.id == stopId }),
              var state = routes[optionId], state.order.count > 1,
              let slot = state.order.firstIndex(of: stopId) else { return }
        let stop = option.stops.remove(at: index)
        editedOptions[optionId] = option
        state.order.remove(at: slot)
        routes[optionId] = state
        lastRemoval = CreateStopRemoval(optionId: optionId, stop: stop, index: index, slot: slot)
        commitReorder(optionId)
    }

    func undoRemoval() {
        guard let removal = lastRemoval,
              var option = optionList.first(where: { $0.id == removal.optionId }),
              var state = routes[removal.optionId] else { return }
        lastRemoval = nil
        option.stops.insert(removal.stop, at: min(removal.index, option.stops.count))
        editedOptions[removal.optionId] = option
        state.order.insert(removal.stop.id, at: min(removal.slot, state.order.count))
        routes[removal.optionId] = state
        commitReorder(removal.optionId)
    }

    /// The Undo row timed out (or was dismissed).
    func dismissRemoval(_ removal: CreateStopRemoval) {
        if lastRemoval == removal { lastRemoval = nil }
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
            lockAt: shared ? lockAt : nil, maxGroupSize: shared ? maxGroupSize : nil
        )
        do {
            return try await env.api.createItinerary(request)
        } catch {
            createError = Self.message(for: error)
            return nil
        }
    }

    // MARK: - Group (shared plans)

    /// When joining locks: the picked lead (or 40 minutes) before you leave, within `lockRange`.
    /// The default never locks a real plan before people have had a chance to join: when 40
    /// minutes before the start has already passed, joining stays open at least 40 minutes from
    /// now, or until you leave. (The demo's clock stands still at its 2:10 PM start, so it keeps
    /// the prototype's 1:30 PM.)
    var lockAt: Date {
        let clock = env.clock
        if let lockLeadMinutes {
            return Self.clamp(clock.addingMinutes(-lockLeadMinutes, to: startTime), to: lockRange)
        }
        var lock = clock.addingMinutes(-Self.defaultLockLeadMinutes, to: startTime)
        if !env.isMock {
            lock = max(lock, min(startTime, clock.addingMinutes(Self.defaultLockLeadMinutes, to: clock.now)))
        }
        return Self.clamp(lock, to: lockRange)
    }

    /// Lock times you can pick: on the plan's day, no later than the start and (live) not in the past.
    var lockRange: ClosedRange<Date> {
        let clock = env.clock
        var earliest = clock.startOfDay(startTime)
        if !env.isMock {
            let nextMinute = Date(timeIntervalSinceReferenceDate: (clock.now.timeIntervalSinceReferenceDate / 60).rounded(.up) * 60)
            earliest = max(earliest, min(nextMinute, startTime))
        }
        return earliest...startTime
    }

    /// More options › Group › "Lock joining at". Kept as a lead, so it follows a new start time.
    func setLockAt(_ time: Date) {
        let clock = env.clock
        let onPlanDay = clock.addingMinutes(clock.minutesIntoDay(time), to: clock.startOfDay(startTime))
        let lock = Self.clamp(onPlanDay, to: lockRange)
        lockLeadMinutes = max(0, Int((startTime.timeIntervalSince(lock) / 60).rounded()))
    }

    /// More options › Group › "Max group size" − / +.
    func changeMaxGroupSize(by delta: Int) {
        maxGroupSize = min(max(maxGroupSize + delta, Self.groupSizes.lowerBound), Self.groupSizes.upperBound)
    }

    // MARK: - More options

    /// Getting around chip. At least one way to get around stays on.
    func toggleMode(_ mode: TravelMode) {
        modesTouched = true
        if modes.contains(mode) {
            guard modes.count > 1 else { return }
            modes.remove(mode)
        } else {
            modes.insert(mode)
        }
    }

    /// More options › Mood: the quick picks, else what was typed or said.
    var moodSummary: String {
        if !selectedTags.isEmpty { return selectedTags.joined(separator: ", ") }
        let typed = moodText.trimmingCharacters(in: .whitespacesAndNewlines)
        return typed.isEmpty ? "Anything" : "“\(typed)”"
    }

    private static func clamp(_ date: Date, to range: ClosedRange<Date>) -> Date {
        min(max(date, range.lowerBound), range.upperBound)
    }

    // MARK: - Helpers

    static func message(for error: any Error) -> String {
        (error as? LocalizedError)?.errorDescription ?? "Something went wrong."
    }
}

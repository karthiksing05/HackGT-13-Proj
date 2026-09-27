import SwiftUI

/// Home's data, all from `env.api`: active itineraries, the unrated past events behind the
/// "N past events to rate" card, the calendar days, the past events and what you like about them.
/// It also keeps what's live on Home: stops running late (`transit.delay`), notes an Event sheet
/// closed before the server confirmed them, and agent checkouts that are still going.
///
/// Reloads keep showing what's already on screen (no loading flash) and swap the fresh data in with
/// animation (new rows rise in, removed ones fade out); errors only replace content when nothing has
/// loaded yet.
@Observable
final class HomeStore {
    private(set) var itineraries: Loadable<[Itinerary]> = .loading
    private(set) var toRate: [PastEvent] = []
    private(set) var days: Loadable<[CalendarDay]> = .loading
    private(set) var past: Loadable<[PastEvent]> = .loading
    /// Home › Past › "What you like" (`GET /me/insights`). Optional: a failure hides the card.
    private(set) var insights: Loadable<PastInsights> = .loading

    /// The selected itinerary. Also the timeline carousel's scroll position (nil = the first one).
    var selectedItineraryId: String?
    /// The selected calendar day (`CalendarDay.id`). Also the day pager's scroll position.
    var selectedDayId: String?
    /// The press-and-hold planning window, on one day.
    var planSelection: HomePlanSelection?
    /// Demo deep link `home/edit/<id>`: asks the Sidequests view to open the editor ("" = the first).
    var editRequest: String?
    /// Sidequests showing their map instead of their timeline (Timeline | Map), by id. Timeline is
    /// the default; a sidequest that leaves Home takes its choice with it.
    private(set) var mapShown: Set<String> = []

    /// A plan just made in Create is on its way: the itineraries stay on screen, dimmed, until it
    /// arrives.
    private(set) var awaitsNewItinerary = false
    /// Per itinerary, bumped to pop its card (the plan you just made) as it scrolls into view.
    private(set) var cardHighlights: [String: HomeCardHighlight] = [:]

    /// Stops running late (`transit.delay`), by item id.
    private(set) var delays: [String: HomeDelay] = [:]
    /// Notes whose Event sheet closed before the server confirmed them, by item id.
    private(set) var pendingNotes: [String: HomePendingNotes] = [:]
    /// Notes that still didn't save after a retry: Home shows a banner to try again.
    private(set) var notesFailure: HomePendingNotes?
    private(set) var isRetryingNotes = false
    /// The latest agent checkout for each item (by item id).
    private(set) var checkouts: [String: HomeCheckoutSession] = [:]

    @ObservationIgnored private var notesTasks: [String: Task<Void, Never>] = [:]
    private var wantsDays = false
    private var wantsPast = false

    /// Number of calendar days shown (Today → today + 9, like the prototype's Sep 25 → Oct 4).
    static let dayCount = 10

    // MARK: Loading

    func loadItineraries(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.activeItineraries() }
        withMotion(Motion.arrive) {
            itineraries = Self.merge(result, into: itineraries)
            if let list = itineraries.value, let id = selectedItineraryId, !list.contains(where: { $0.id == id }) {
                selectedItineraryId = nil
            }
            if let list = itineraries.value {
                let ids = Set(list.map(\.id))
                if !mapShown.isSubset(of: ids) { mapShown.formIntersection(ids) }
            }
        }
    }

    /// Unrated past events for the Itineraries card. Optional content: failures keep the last value.
    func loadToRate(_ env: AppEnvironment) async {
        guard let events = try? await env.api.pastEvents(unratedOnly: true), events != toRate else { return }
        withMotion(Motion.arrive) { toRate = events }
    }

    func loadDays(_ env: AppEnvironment) async {
        wantsDays = true
        let clock = env.clock
        let today = clock.startOfDay(clock.now)
        let dates = (0..<Self.dayCount).compactMap { clock.calendar.date(byAdding: .day, value: $0, to: today) }
        guard let last = dates.last else { return }
        let result = await Loadable.run { try await env.api.calendarDays(from: today, to: last) }
        let filled: Loadable<[CalendarDay]>
        switch result {
        case .loaded(let apiDays):
            // One panel per day, even when the server leaves out empty days.
            filled = .loaded(dates.map { date in
                let match = apiDays.first { clock.calendar.isDate($0.date, inSameDayAs: date) }
                let items = (match?.items ?? []).sorted { $0.start < $1.start }
                return CalendarDay(id: match?.id ?? clock.dayKey(date), date: date, items: items)
            })
        case .failed(let message):
            filled = .failed(message)
        case .loading:
            filled = .loading
        }
        withMotion(Motion.arrive) {
            days = Self.merge(filled, into: days)
            if let list = days.value, let id = selectedDayId, !list.contains(where: { $0.id == id }) {
                selectedDayId = nil
            }
        }
    }

    /// Past events and the "What you like" card, in parallel.
    func loadPast(_ env: AppEnvironment) async {
        wantsPast = true
        async let events: Void = loadPastEvents(env)
        async let liked: Void = loadInsights(env)
        _ = await (events, liked)
    }

    private func loadPastEvents(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.pastEvents(unratedOnly: false) }
        withMotion(Motion.arrive) { past = Self.merge(result, into: past) }
    }

    /// Optional content: a failed refresh keeps the last insights; a failed first load hides the card.
    func loadInsights(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.pastInsights() }
        withMotion(Motion.gentle) { insights = Self.merge(result, into: insights) }
    }

    /// First visit to Calendar.
    func loadDaysIfNeeded(_ env: AppEnvironment) async {
        guard !wantsDays else { return }
        await loadDays(env)
    }

    /// First visit to Past.
    func loadPastIfNeeded(_ env: AppEnvironment) async {
        guard !wantsPast else { return }
        await loadPast(env)
    }

    /// Everything Home has shown so far (after Create closes, a rating, a booking…).
    func refresh(_ env: AppEnvironment) async {
        async let itineraries: Void = loadItineraries(env)
        async let toRate: Void = loadToRate(env)
        async let days: Void = refreshDaysIfShown(env)
        async let past: Void = refreshPastIfShown(env)
        _ = await (itineraries, toRate, days, past)
    }

    /// The plans and the calendar (if it's been shown): after someone joins or a plan changes.
    func refreshPlans(_ env: AppEnvironment) async {
        async let itineraries: Void = loadItineraries(env)
        async let days: Void = refreshDaysIfShown(env)
        _ = await (itineraries, days)
    }

    /// Right after Create: everything reloads while the itineraries stay on screen, dimmed; then the
    /// new plan is selected and its card pops, once Create's cover (closing since `coverClosedAt`,
    /// about 0.6 s) is out of the way.
    func showNewItinerary(_ id: String, env: AppEnvironment, coverClosedAt: Date) async {
        withMotion { awaitsNewItinerary = true }
        await refresh(env)
        withMotion { awaitsNewItinerary = false }
        guard itineraries.value?.contains(where: { $0.id == id }) == true else { return }
        withMotion(Motion.gentle) { selectedItineraryId = id }
        let token = (cardHighlights[id]?.token ?? 0) + 1
        let delay = max(0.2, 0.6 - Date.now.timeIntervalSince(coverClosedAt))
        cardHighlights[id] = HomeCardHighlight(token: token, delay: delay)
    }

    /// Home search › a sidequest: select it and pop its card (added if the list doesn't have it yet).
    func focus(_ itinerary: Itinerary) {
        if itineraries.value?.contains(where: { $0.id == itinerary.id }) == false { upsert(itinerary) }
        withMotion(Motion.gentle) { selectedItineraryId = itinerary.id }
        let token = (cardHighlights[itinerary.id]?.token ?? 0) + 1
        cardHighlights[itinerary.id] = HomeCardHighlight(token: token, delay: 0.25)
    }

    // MARK: Timeline or map

    /// This sidequest shows its map (Home › Sidequests › Timeline | Map).
    func showsMap(_ id: String) -> Bool {
        mapShown.contains(id)
    }

    /// Switches one sidequest between its timeline and its map; the others keep theirs.
    func setShowsMap(_ shows: Bool, for id: String) {
        guard shows != mapShown.contains(id) else { return }
        if shows {
            mapShown.insert(id)
        } else {
            mapShown.remove(id)
        }
    }

    // MARK: Editing sidequests

    /// The server's copy after an edit (re-timed if the window or stops changed) replaces the old one
    /// in place, with animation.
    func apply(_ itinerary: Itinerary) {
        guard case .loaded(var list) = itineraries, let index = list.firstIndex(where: { $0.id == itinerary.id }) else { return }
        list[index] = itinerary
        withMotion(Motion.arrive) { itineraries = .loaded(list) }
    }

    /// Deleted or left: the card and timeline animate away and the neighbor is selected. Returns what
    /// was removed (and where) so a failed request can put it back.
    @discardableResult
    func remove(_ id: String) -> (itinerary: Itinerary, index: Int)? {
        guard case .loaded(var list) = itineraries, let index = list.firstIndex(where: { $0.id == id }) else { return nil }
        let removed = list.remove(at: index)
        withMotion(Motion.arrive) {
            itineraries = .loaded(list)
            if selectedItineraryId == id {
                selectedItineraryId = list.isEmpty ? nil : list[min(index, list.count - 1)].id
            }
            mapShown.remove(id)
        }
        return (removed, index)
    }

    /// Undo `remove` after the server said no.
    func restore(_ itinerary: Itinerary, at index: Int) {
        guard case .loaded(var list) = itineraries, !list.contains(where: { $0.id == itinerary.id }) else { return }
        list.insert(itinerary, at: min(index, list.count))
        withMotion(Motion.arrive) {
            itineraries = .loaded(list)
            selectedItineraryId = itinerary.id
        }
    }

    /// Keeps the timeline's copy of an item in step with what the Event sheet saved (notes, travel
    /// choice, ticket), so reopening the block shows it at once.
    func updateItem(_ itemId: String, _ change: (inout ItineraryItem) -> Void) {
        guard case .loaded(var list) = itineraries else { return }
        var found = false
        for i in list.indices {
            guard let j = list[i].items.firstIndex(where: { $0.id == itemId }) else { continue }
            change(&list[i].items[j])
            found = true
        }
        if found { itineraries = .loaded(list) }
    }

    /// The server saved `rating` for a past event: show it right away (the row's Rate pill turns
    /// into stars, the "to rate" count rolls down) while a refresh confirms it.
    func applySavedRating(_ rating: Rating, to eventId: String) {
        withMotion(Motion.arrive) {
            if case .loaded(var list) = past, let index = list.firstIndex(where: { $0.id == eventId }) {
                list[index].rating = rating
                past = .loaded(list)
            }
            toRate.removeAll { $0.id == eventId }
        }
    }

    /// Retry after a failed first load: back to the loading state, then load again.
    func retryItineraries(_ env: AppEnvironment) async {
        withMotion { itineraries = .loading }
        await loadItineraries(env)
    }

    func retryDays(_ env: AppEnvironment) async {
        withMotion { days = .loading }
        await loadDays(env)
    }

    func retryPast(_ env: AppEnvironment) async {
        withMotion {
            past = .loading
            if insights.value == nil { insights = .loading }
        }
        await loadPast(env)
    }

    func refreshDaysIfShown(_ env: AppEnvironment) async {
        if wantsDays { await loadDays(env) }
    }

    private func refreshPastIfShown(_ env: AppEnvironment) async {
        if wantsPast { await loadPast(env) }
    }

    private static func merge<T>(_ new: Loadable<T>, into old: Loadable<T>) -> Loadable<T> {
        if case .failed = new, old.value != nil { return old }
        return new
    }

    // MARK: Live updates (realtime)

    /// `itinerary.updated`: the server's copy replaces the old one in place, or arrives as a new
    /// card (a plan you just joined, or one made on another device).
    func upsert(_ itinerary: Itinerary) {
        guard case .loaded(var list) = itineraries else { return }
        if let index = list.firstIndex(where: { $0.id == itinerary.id }) {
            guard list[index] != itinerary else { return }
            list[index] = itinerary
        } else {
            list.insert(itinerary, at: list.firstIndex { $0.start > itinerary.start } ?? list.count)
        }
        // A delay on a stop that's no longer on the plan goes with it.
        let stops = Set(itinerary.items.map(\.id))
        withMotion(Motion.arrive) {
            itineraries = .loaded(list)
            delays = delays.filter { $0.value.itineraryId != itinerary.id || stops.contains($0.key) }
        }
    }

    /// `itinerary.removed`: deleted by its host, or you were taken off it. It animates away.
    func removeRemotely(_ id: String) {
        remove(id)
        guard delays.values.contains(where: { $0.itineraryId == id }) else { return }
        withMotion { delays = delays.filter { $0.value.itineraryId != id } }
    }

    /// `transit.delay`: a stop running late (0 or less means it's back on time).
    func setDelay(itineraryId: String, itemId: String, minutes: Int) {
        withMotion(Motion.arrive) {
            delays[itemId] = minutes > 0 ? HomeDelay(itineraryId: itineraryId, minutes: minutes) : nil
        }
    }

    func delay(for itemId: String) -> Int? {
        delays[itemId]?.minutes
    }

    /// How late each of an itinerary's stops is running, by item id (for its timeline).
    func delays(in itineraryId: String) -> [String: Int] {
        delays.reduce(into: [:]) { result, entry in
            if entry.value.itineraryId == itineraryId { result[entry.key] = entry.value.minutes }
        }
    }

    /// You host a plan others can join, so a join request changes its counts.
    var hostsSharedPlan: Bool {
        itineraries.value?.contains { $0.isHost && $0.visibility != .justMe } ?? false
    }

    // MARK: Notes saved after their sheet closed

    /// An Event sheet closed with notes the server hasn't confirmed: save them from here, once more
    /// after a moment if that fails, then show a banner on Home to try again. Until they're saved
    /// they stay here, and reopening the block shows them.
    func saveNotesLater(_ notes: HomePendingNotes, env: AppEnvironment) {
        pendingNotes[notes.itemId] = notes
        notesTasks[notes.itemId]?.cancel()
        notesTasks[notes.itemId] = Task { [weak self] in
            for attempt in 0..<2 {
                if attempt > 0 { try? await Task.sleep(for: .seconds(2)) }
                guard !Task.isCancelled else { return }
                do {
                    try await env.api.updateItemNotes(itineraryId: notes.itineraryId, itemId: notes.itemId,
                                                      notes: notes.text, scope: notes.scope)
                    self?.notesSaved(notes)
                    return
                } catch {
                    continue
                }
            }
            guard !Task.isCancelled else { return }
            self?.notesDidFail(notes)
        }
    }

    /// The banner's "Try again".
    func retryNotes(_ env: AppEnvironment) {
        guard let failure = notesFailure, !isRetryingNotes else { return }
        withMotion(Motion.quick) { isRetryingNotes = true }
        Task {
            if (try? await env.api.updateItemNotes(itineraryId: failure.itineraryId, itemId: failure.itemId,
                                                   notes: failure.text, scope: failure.scope)) != nil {
                notesSaved(failure)
            }
            withMotion(Motion.quick) { isRetryingNotes = false }
        }
    }

    /// Hides the banner. The notes stay pending: reopening the block shows them and saves them.
    func dismissNotesFailure() {
        withMotion { notesFailure = nil }
    }

    /// An Event sheet opened this block: it takes over its unsaved notes (and saving them).
    func adoptPendingNotes(_ itemId: String) {
        notesTasks[itemId]?.cancel()
        notesTasks[itemId] = nil
        pendingNotes[itemId] = nil
        if notesFailure?.itemId == itemId { withMotion { notesFailure = nil } }
    }

    /// The server confirmed notes for a block (from its Event sheet or from here).
    func notesConfirmed(itemId: String, text: String, scope: NotesScope?) {
        updateItem(itemId) { item in
            item.notes = text
            if let scope { item.notesScope = scope }
        }
    }

    private func notesSaved(_ notes: HomePendingNotes) {
        notesTasks[notes.itemId] = nil
        if pendingNotes[notes.itemId] == notes { pendingNotes[notes.itemId] = nil }
        notesConfirmed(itemId: notes.itemId, text: notes.text, scope: notes.scope)
        if notesFailure?.itemId == notes.itemId { withMotion { notesFailure = nil } }
    }

    private func notesDidFail(_ notes: HomePendingNotes) {
        notesTasks[notes.itemId] = nil
        guard pendingNotes[notes.itemId] == notes else { return }
        withMotion(Motion.arrive) { notesFailure = notes }
    }

    // MARK: Checkout

    /// The checkout that's still going for this item (being made, waiting for approval, paying).
    func checkout(for itemId: String) -> HomeCheckoutSession? {
        guard let session = checkouts[itemId], session.isOngoing else { return nil }
        return session
    }

    /// The last checkout for this item, whatever state it's in.
    func latestCheckout(for itemId: String) -> HomeCheckoutSession? {
        checkouts[itemId]
    }

    /// "Get tickets": the checkout still going for this item, or a new one.
    func startCheckout(for item: ItineraryItem, env: AppEnvironment) -> HomeCheckoutSession {
        if let session = checkout(for: item.id) { return session }
        let session = HomeCheckoutSession(item: item, env: env)
        let itemId = item.id
        session.onBooked = { [weak self] fresh in self?.bookingLanded(itemId: itemId, fresh: fresh) }
        checkouts[itemId] = session
        return session
    }

    /// Booked: the timeline's copy of the item gets its ticket right away.
    private func bookingLanded(itemId: String, fresh: ItineraryItem?) {
        guard let ticket = fresh?.ticket else { return }
        updateItem(itemId) { $0.ticket = ticket }
    }

    // MARK: Lookups

    /// The itinerary (and item) a block id belongs to, if it's on an active itinerary.
    func itinerary(containing itemId: String) -> (itinerary: Itinerary, item: ItineraryItem)? {
        for itinerary in itineraries.value ?? [] {
            if let item = itinerary.items.first(where: { $0.id == itemId }) { return (itinerary, item) }
        }
        return nil
    }
}

/// Which block the Event sheet shows. `seed` (when opened from a timeline) renders immediately
/// while the full details load.
struct HomeEventRoute: Identifiable, Equatable {
    let id: String
    /// The plan the block belongs to (its notes and travel go to `/itineraries/{id}/items/…`); nil
    /// for calendar-only entries (`/events/{id}`).
    var itineraryId: String?
    var seed: ItineraryItem?
    /// Opens Agent checkout on top once the details are in (demo route `home/checkout/<id>`).
    var opensCheckout = false
    /// Others are on the plan, so its notes can be shared with them.
    var planIsShared = false
}

/// A stop running late, from `transit.delay`.
struct HomeDelay: Equatable {
    var itineraryId: String
    var minutes: Int
}

/// Notes an Event sheet closed before the server confirmed them.
struct HomePendingNotes: Equatable {
    let itemId: String
    var itineraryId: String?
    /// The block's title, for the banner.
    var title: String
    var text: String
    var scope: NotesScope?
}

/// A page for the in-app browser (a website, a ticket).
struct HomeBrowserLink: Identifiable {
    let url: URL
    var id: String { url.absoluteString }
}

/// A press-and-hold planning window on one calendar day, in minutes since midnight (15-minute steps).
struct HomePlanSelection: Equatable {
    /// New for every press and hold, so a new window animates in instead of the old one moving.
    var id = UUID()
    var dayId: String
    var start: Int
    var end: Int
    /// A finger is drawing or resizing it; the "Plan this window" bar waits for the release.
    var isAdjusting: Bool

    static let earliest = 6 * 60
    static let latest = 23 * 60
    /// A new window is an hour long; resizing stops at half an hour.
    static let defaultLength = 60
    static let minimumLength = 30

    /// A one-hour window starting at `minute`, kept inside 6 AM–11 PM.
    static func hour(at minute: Int, dayId: String) -> HomePlanSelection {
        let start = min(max(minute, earliest), latest - defaultLength)
        return HomePlanSelection(dayId: dayId, start: start, end: start + defaultLength, isAdjusting: true)
    }
}

/// Pops an itinerary card once per `token`, `delay` seconds after it's set.
struct HomeCardHighlight: Equatable {
    var token: Int
    var delay: Double
}

/// A rating the server just saved from the Rate sheet, shown once the sheet is out of the way.
struct HomeSavedRating: Equatable {
    let eventId: String
    let rating: Rating
}

extension View {
    /// Puts text in the prototype's CSS line box (`line-height: multiplier`) so stacked text lands
    /// where the prototype puts it. SF's own line height is ≈1.193× the point size.
    func homeLine(_ size: CGFloat, _ multiplier: CGFloat = 1.35) -> some View {
        let extra = (multiplier - 1.193) * size
        return lineSpacing(max(0, extra)).padding(.vertical, extra / 2)
    }
}

/// Overlapping avatars as the prototype draws them: each later face sits on top of the previous
/// one, 2pt white borders, −6pt overlap (timeline blocks 22pt, calendar blocks 20pt).
struct HomeAvatarStack: View {
    let people: [PersonRef]
    var size: CGFloat = 22
    var fontSize: CGFloat = 9

    var body: some View {
        HStack(spacing: -6) {
            ForEach(Array(people.enumerated()), id: \.offset) { index, person in
                Avatar(person: person, size: size, fontSize: fontSize, ring: .white, ringWidth: 2)
                    .zIndex(Double(index))
            }
        }
        .accessibilityElement()
        .accessibilityLabel(people.map(\.firstName).joined(separator: ", "))
    }
}

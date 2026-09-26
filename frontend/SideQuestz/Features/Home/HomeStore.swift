import SwiftUI

/// Home's data, all from `env.api`: active itineraries, the unrated past events behind the
/// "N past events to rate" card, the calendar days and the past events.
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

    /// The selected itinerary. Also the timeline carousel's scroll position (nil = the first one).
    var selectedItineraryId: String?
    /// The selected calendar day (`CalendarDay.id`). Also the day pager's scroll position.
    var selectedDayId: String?
    /// The press-and-hold planning window, on one day.
    var planSelection: HomePlanSelection?

    /// A plan just made in Create is on its way: the itineraries stay on screen, dimmed, until it
    /// arrives.
    private(set) var awaitsNewItinerary = false
    /// Per itinerary, bumped to pop its card (the plan you just made) as it scrolls into view.
    private(set) var cardHighlights: [String: HomeCardHighlight] = [:]

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

    func loadPast(_ env: AppEnvironment) async {
        wantsPast = true
        let result = await Loadable.run { try await env.api.pastEvents(unratedOnly: false) }
        withMotion(Motion.arrive) { past = Self.merge(result, into: past) }
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
        withMotion { past = .loading }
        await loadPast(env)
    }

    private func refreshDaysIfShown(_ env: AppEnvironment) async {
        if wantsDays { await loadDays(env) }
    }

    private func refreshPastIfShown(_ env: AppEnvironment) async {
        if wantsPast { await loadPast(env) }
    }

    private static func merge<T>(_ new: Loadable<T>, into old: Loadable<T>) -> Loadable<T> {
        if case .failed = new, old.value != nil { return old }
        return new
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
    var itineraryId: String?
    var seed: ItineraryItem?
    /// Opens Agent checkout on top once the details are in (demo route `home/checkout/<id>`).
    var opensCheckout = false
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

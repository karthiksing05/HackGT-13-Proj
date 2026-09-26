import SwiftUI

/// Home's data, all from `env.api`: active itineraries, the unrated past events behind the
/// "N past events to rate" card, the calendar days and the past events.
///
/// Reloads keep showing what's already on screen (no loading flash); errors only replace content
/// when nothing has loaded yet.
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
    /// The drag-to-plan window, on one day.
    var planSelection: HomePlanSelection?

    private var wantsDays = false
    private var wantsPast = false

    /// Number of calendar days shown (Today → today + 9, like the prototype's Sep 25 → Oct 4).
    static let dayCount = 10

    // MARK: Loading

    func loadItineraries(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.activeItineraries() }
        itineraries = Self.merge(result, into: itineraries)
        if let list = itineraries.value, let id = selectedItineraryId, !list.contains(where: { $0.id == id }) {
            selectedItineraryId = nil
        }
    }

    /// Unrated past events for the Itineraries card. Optional content: failures keep the last value.
    func loadToRate(_ env: AppEnvironment) async {
        if let events = try? await env.api.pastEvents(unratedOnly: true) { toRate = events }
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
        days = Self.merge(filled, into: days)
        if let list = days.value, let id = selectedDayId, !list.contains(where: { $0.id == id }) {
            selectedDayId = nil
        }
    }

    func loadPast(_ env: AppEnvironment) async {
        wantsPast = true
        let result = await Loadable.run { try await env.api.pastEvents(unratedOnly: false) }
        past = Self.merge(result, into: past)
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

    /// Retry after a failed first load: back to the loading state, then load again.
    func retryItineraries(_ env: AppEnvironment) async {
        itineraries = .loading
        await loadItineraries(env)
    }

    func retryDays(_ env: AppEnvironment) async {
        days = .loading
        await loadDays(env)
    }

    func retryPast(_ env: AppEnvironment) async {
        past = .loading
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

/// A drag-to-plan window on one calendar day, in minutes since midnight (snapped to 15).
struct HomePlanSelection: Equatable {
    var dayId: String
    /// Where the drag started.
    var anchor: Int
    /// Where the finger is (or ended).
    var current: Int
    var isDragging: Bool

    var lower: Int { min(anchor, current) }
    var upper: Int { max(anchor, current) }

    static let earliest = 6 * 60
    static let latest = 23 * 60

    /// Released: under 30 minutes becomes an hour (a tap selects one hour), clamped to 6 AM–11 PM.
    func finished() -> HomePlanSelection {
        var lo = lower, hi = upper
        if hi - lo < 30 { hi = min(Self.latest, lo + 60) }
        if hi - lo < 30 { lo = max(Self.earliest, hi - 60) }
        return HomePlanSelection(dayId: dayId, anchor: lo, current: hi, isDragging: false)
    }
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

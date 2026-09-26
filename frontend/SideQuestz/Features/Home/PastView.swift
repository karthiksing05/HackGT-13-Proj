import SwiftUI

/// Home › Past (GUI_PLAN.md §7.5c): "What you like" (what your best-rated sidequests have in
/// common, from the server), then the events you went to, grouped by day. Tap one to rate it.
///
/// Motion: skeletons while the first load runs, then the insight tiles and the rows arrive one
/// after another; when a rating is saved the row's "Rate" pill shrinks away and its stars fill in
/// with a bounce, and the insights refresh in place.
struct HomePastView: View {
    let store: HomeStore
    let rate: (PastEvent) -> Void
    let retry: () -> Void
    @Environment(AppEnvironment.self) private var env
    /// Rows arrive one after another only when they replace the skeleton.
    @State private var arrival = HomeArrivalWindow()
    @State private var insightsArrival = HomeArrivalWindow()

    init(store: HomeStore, rate: @escaping (PastEvent) -> Void, retry: @escaping () -> Void) {
        self.store = store
        self.rate = rate
        self.retry = retry
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Optional: left out when it can't load.
            if store.insights.phase != .failed {
                HomeInsightsCard(state: store.insights, arrives: insightsArrival.isOpen)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 14)
                    .transition(.opacity)
            }
            HomeLoadable(state: store.past, retry: retry) {
                HomePastSkeleton()
            } content: { events in
                if events.isEmpty {
                    EmptyStateView(message: "Nothing here yet. Events you go to show up here to rate.")
                } else {
                    let arrives = arrival.isOpen
                    ForEach(groups(events), id: \.key) { group in
                        section(group, arrives: arrives)
                    }
                }
            }
            Color.clear.frame(height: 24)
        }
        .onAppear {
            arrival.begin(loading: store.past.isLoading)
            insightsArrival.begin(loading: store.insights.isLoading)
        }
        .onChange(of: store.past.phase) { _, phase in arrival.update(phase) }
        .onChange(of: store.insights.phase) { _, phase in insightsArrival.update(phase) }
    }

    private struct DayGroup {
        let key: String
        let title: String
        var events: [PastEvent]
        /// Where the group starts in the whole list (for the arrival stagger).
        let firstIndex: Int
    }

    /// Consecutive events on the same day, newest first as the server sends them.
    private func groups(_ events: [PastEvent]) -> [DayGroup] {
        var result: [DayGroup] = []
        for (index, event) in events.enumerated() {
            let key = env.clock.dayKey(event.date)
            if result.last?.key == key {
                result[result.count - 1].events.append(event)
            } else {
                result.append(DayGroup(key: key, title: env.format.dayTitle(event.date).uppercased(), events: [event], firstIndex: index))
            }
        }
        return result
    }

    private func section(_ group: DayGroup, arrives: Bool) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // "THURSDAY, SEP 24" (the prototype's 16pt top margin collapses with the intro's 4pt).
            Text(group.title)
                .sqFont(13, .semibold, relativeTo: .footnote)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .homeLine(13)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 16)
                .padding(.bottom, 8)
                .accessibilityAddTraits(.isHeader)
                .homeArrival(group.firstIndex, enabled: arrives)
            VStack(spacing: 0) {
                ForEach(Array(group.events.enumerated()), id: \.element.id) { offset, event in
                    HomePastRow(event: event) { rate(event) }
                        .homeArrival(group.firstIndex + offset + 1, enabled: arrives)
                    RowDivider(color: Theme.cream)
                }
            }
            .sqGroupedCard()
            .padding(.horizontal, Metrics.side)
        }
    }
}

/// One past event: dot, title, "Tech Square · with 3 others", the tag line once rated, and the
/// "Rate" pill or the stars. A rating that lands while the row is on screen fills its stars in.
private struct HomePastRow: View {
    let event: PastEvent
    let action: () -> Void
    /// The row started out unrated, so stars that appear were just earned: they fill in.
    @State private var startedUnrated: Bool

    init(event: PastEvent, action: @escaping () -> Void) {
        self.event = event
        self.action = action
        _startedUnrated = State(initialValue: event.rating == nil)
    }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Circle()
                    .fill(event.kind == .group ? Theme.clay : Theme.sage)
                    .frame(width: 10, height: 10)
                VStack(alignment: .leading, spacing: 0) {
                    Text(event.title)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .homeLine(15)
                    Text(event.subtitle)
                        .sqFont(12, relativeTo: .caption)
                        .foregroundStyle(Theme.text3)
                        .homeLine(12)
                    if let rating = event.rating, !rating.tagLine.isEmpty {
                        Text(rating.tagLine)
                            .sqFont(12, relativeTo: .caption)
                            .foregroundStyle(Theme.text2)
                            .homeLine(12)
                            .padding(.top, 2)
                            .sqTransition(.rise)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                // The pill and the stars swap in place (no extra container, so the resting layout is
                // the prototype's); a copy of the pill shrinks away over the stars as they fill in.
                if let rating = event.rating {
                    HomeRatedStars(rating: rating.stars, fillsIn: startedUnrated)
                        .overlay(alignment: .trailing) {
                            if startedUnrated { HomeLeavingPill { ratePill } }
                        }
                        .transition(.identity)
                } else {
                    ratePill
                        .transition(.identity)
                }
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(.isButton)
    }

    private var ratePill: some View {
        Text("Rate")
            .sqFont(13, .semibold)
            .foregroundStyle(Theme.ink)
            .padding(.horizontal, 12)
            .frame(height: 30)
            .background(Theme.sage, in: Capsule())
    }

    private var accessibilityText: String {
        guard let rating = event.rating else { return "Rate \(event.title), \(event.subtitle)" }
        return "\(event.title), \(event.subtitle), rated \(rating.stars) of 5. Edit rating"
    }
}

/// The "Rate" pill leaving: shrinks toward the trailing edge and fades as the stars take its place.
private struct HomeLeavingPill<Pill: View>: View {
    @ViewBuilder var pill: Pill
    @State private var gone = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        pill
            .scaleEffect(gone && !reduceMotion ? 0.55 : 1, anchor: .trailing)
            .opacity(gone ? 0 : 1)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
            .onAppear {
                withAnimation(reduceMotion ? Motion.reduced : Motion.standard) { gone = true }
            }
    }
}

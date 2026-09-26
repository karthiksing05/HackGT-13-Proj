import SwiftUI

/// Home tab (GUI_PLAN.md §7.5–7.6): date eyebrow + "Your SideQuests" + avatar, then
/// Itineraries | Calendar | Past (cross-fading). Tapping a block opens the Event sheet; a past event
/// opens the Rate sheet. Pull down to reload. Demo routes: `home`, `home/calendar`, `home/past`,
/// `home/sheet/<blockId>`, `home/rate/<pastId>`, `home/checkout/<blockId>`.
struct HomeView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var store = HomeStore()
    @State private var pageWidth: CGFloat = Metrics.designWidth
    @State private var ratingsSaved = 0

    // Sheets use the `isPresented` form of `sqSheet`, with the block / event read through a
    // binding inside the sheet, so the content is always the current one.
    @State private var eventRoute: HomeEventRoute?
    @State private var showsEvent = false
    @State private var reloadAfterEvent = false
    @State private var rateEvent: PastEvent?
    @State private var showsRate = false
    /// Saved in the Rate sheet; its Past row changes once the sheet is out of the way.
    @State private var savedRating: HomeSavedRating?

    private static let segments: [(value: Router.HomeSegment, label: String)] = [
        (.itineraries, "Itineraries"), (.calendar, "Calendar"), (.past, "Past"),
    ]

    var body: some View {
        @Bindable var router = router
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                TabScreenHeader(eyebrow: env.format.eyebrowDate(env.clock.now), title: "Your SideQuests") {
                    Button {
                        router.select(.account)
                    } label: {
                        CurrentUserAvatar(size: 44)
                    }
                    .buttonStyle(.sqPressable)
                    .accessibilityLabel("Open profile")
                }
                SQSegmentedControl(selection: $router.homeSegment, options: Self.segments, accessibilityLabel: "Home view")
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 12)
                segmentContent
            }
            .sqSheet(isPresented: $showsRate, onDismiss: rateSheetClosed) {
                HomeRateSheetHost(event: $rateEvent, close: { showsRate = false }, saved: ratingSaved)
            }
        }
        .scrollIndicators(.hidden)
        .sqPullToRefresh()
        // Snapped to the half point: the width feeds the carousel's card widths, and a measurement
        // that creeps by a rounding error must not trigger another layout pass (an endless loop).
        .onGeometryChange(for: CGFloat.self, of: { ($0.size.width * 2).rounded() / 2 }, action: { pageWidth = $0 })
        // Like the prototype, the page ends at the tab bar (nothing shows through it).
        .mask { Rectangle().ignoresSafeArea(edges: .top) }
        .background(Theme.cream.ignoresSafeArea())
        .sqSheet(isPresented: $showsEvent, style: SQSheetStyle(height: .fixed(660)), onDismiss: eventSheetClosed) {
            HomeEventSheetHost(route: $eventRoute, close: { showsEvent = false }, didChange: { reloadAfterEvent = true })
        }
        .sensoryFeedback(.success, trigger: ratingsSaved)
        .task { await start() }
        .onChange(of: router.homeSegment, initial: true) { _, segment in
            Task { await loadIfNeeded(segment) }
        }
        .onChange(of: router.createDraft == nil) { _, closed in
            // Create closed: a new itinerary (and its calendar entries) may exist now. A plan that
            // was just made is handled by `showItinerary`, which reloads everything too.
            if closed, router.selectedItineraryId == nil { Task { await store.refresh(env) } }
        }
        .onChange(of: router.selectedItineraryId) { _, id in
            if let id { Task { await showItinerary(id) } }
        }
        .onChange(of: router.tab) { old, new in
            // Back on Home (e.g. after rating from Account): show fresh data.
            if new == .home, old != .home { Task { await store.refresh(env) } }
        }
    }

    /// The three segments cross-fade. Each registers its pull-to-refresh reload while it's shown;
    /// a pull reloads everything Home has loaded (hidden segments stay fresh for when you switch).
    private var segmentContent: some View {
        ZStack(alignment: .top) {
            switch router.homeSegment {
            case .itineraries:
                HomeItinerariesView(store: store, pageWidth: pageWidth, openBlock: { item, itinerary in
                    openEvent(HomeEventRoute(id: item.id, itineraryId: itinerary.id, seed: item))
                }, retry: {
                    Task { await store.retryItineraries(env) }
                })
                .sqReloadable("home.itineraries") { await store.refresh(env) }
                .transition(.opacity)
            case .calendar:
                HomeCalendarView(store: store, pageWidth: pageWidth, openItem: { item in
                    openBlock(id: item.id)
                }, retry: {
                    Task { await store.retryDays(env) }
                })
                .sqReloadable("home.calendar") { await store.refresh(env) }
                .transition(.opacity)
            case .past:
                HomePastView(store: store, rate: openRating, retry: {
                    Task { await store.retryPast(env) }
                })
                .sqReloadable("home.past") { await store.refresh(env) }
                .transition(.opacity)
            }
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: router.homeSegment)
    }

    // MARK: Sheets

    private func openEvent(_ route: HomeEventRoute) {
        eventRoute = route
        showsEvent = true
    }

    /// Any block id: itinerary items open with their timeline copy while details load.
    private func openBlock(id: String, checkout: Bool = false) {
        let match = store.itinerary(containing: id)
        openEvent(HomeEventRoute(id: id, itineraryId: match?.itinerary.id, seed: match?.item, opensCheckout: checkout))
    }

    private func openRating(_ event: PastEvent) {
        rateEvent = event
        showsRate = true
    }

    private func eventSheetClosed() {
        eventRoute = nil
        guard reloadAfterEvent else { return }
        reloadAfterEvent = false
        Task { await store.refresh(env) }
    }

    /// The server saved the rating: success haptic now, the row changes when the sheet is gone.
    private func ratingSaved(_ saved: HomeSavedRating) {
        ratingsSaved += 1
        savedRating = saved
    }

    private func rateSheetClosed() {
        rateEvent = nil
        guard let saved = savedRating else { return }
        savedRating = nil
        store.applySavedRating(saved.rating, to: saved.eventId)
        Task { await store.refresh(env) }
    }

    // MARK: Loading

    private func start() async {
        async let itineraries: Void = store.loadItineraries(env)
        async let toRate: Void = store.loadToRate(env)
        _ = await (itineraries, toRate)
        if let id = router.selectedItineraryId { await showItinerary(id) }
        await handleLaunch()
    }

    private func loadIfNeeded(_ segment: Router.HomeSegment) async {
        switch segment {
        case .itineraries: break
        case .calendar: await store.loadDaysIfNeeded(env)
        case .past: await store.loadPastIfNeeded(env)
        }
    }

    /// Right after Create: reload (the itineraries stay on screen, dimmed), then select the new
    /// itinerary, scroll its card and timeline into view and pop the card. Create's cover is still
    /// sliding away when this starts, so the pop waits for at least that long.
    private func showItinerary(_ id: String) async {
        let coverClosing = Date.now
        withMotion { router.homeSegment = .itineraries }
        await store.showNewItinerary(id, env: env, coverClosedAt: coverClosing)
        if router.selectedItineraryId == id { router.selectedItineraryId = nil }
    }

    // MARK: Demo deep links

    private func handleLaunch() async {
        guard let parts = router.consumeLaunch("home") else { return }
        let id = parts.dropFirst().first
        switch parts.first {
        case "sheet", "checkout":
            guard let id else { break }
            if let match = store.itinerary(containing: id) { store.selectedItineraryId = match.itinerary.id }
            openBlock(id: id, checkout: parts.first == "checkout")
        case "rate":
            await store.loadPast(env)
            router.homeSegment = .past
            if let id, let event = store.past.value?.first(where: { $0.id == id }) { openRating(event) }
        default:
            break
        }
    }
}

/// The Event sheet for the current route (read through the binding, so it's never stale).
private struct HomeEventSheetHost: View {
    @Binding var route: HomeEventRoute?
    let close: () -> Void
    let didChange: () -> Void

    var body: some View {
        if let route {
            HomeEventSheet(route: route, close: close, didChange: didChange)
                .id(route.id)
        }
    }
}

/// The Rate sheet for the current past event.
private struct HomeRateSheetHost: View {
    @Binding var event: PastEvent?
    let close: () -> Void
    let saved: (HomeSavedRating) -> Void

    var body: some View {
        if let event {
            HomeRateSheet(event: event, close: close, saved: { saved(HomeSavedRating(eventId: event.id, rating: $0)) })
                .id(event.id)
        }
    }
}

#Preview {
    HomeView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

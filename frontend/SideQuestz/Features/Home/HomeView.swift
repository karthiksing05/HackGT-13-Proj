import SwiftUI

/// Home tab (GUI_PLAN.md §7.5–7.6): a search bar, "Your SideQuests" (two-tone) + your avatar, then
/// Sidequests | Calendar | Past (cross-fading). Typing a search swaps the page for results
/// (sidequests, people, places, Forum posts); Cancel brings Home back. Tapping a block opens the
/// Event sheet; a past event opens the Rate sheet. Pull down to reload. Changes from the server
/// apply as they arrive: plans updated or removed, stops running late, join requests, bookings.
/// Demo routes: `home`, `home/calendar`, `home/past`, `home/sheet/<blockId>`, `home/rate/<pastId>`,
/// `home/checkout/<blockId>`, `home/edit/<id>`, `home/search/<query>`.
struct HomeView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var store = HomeStore()
    @State private var search = HomeSearchModel()
    @State private var searchText = ""
    @FocusState private var searchFocused: Bool
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
        (.itineraries, "Sidequests"), (.calendar, "Calendar"), (.past, "Past"),
    ]
    private static let topId = "home.top"

    /// What's being searched for (surrounding spaces don't count).
    private var query: String { searchText.trimmingCharacters(in: .whitespacesAndNewlines) }
    /// Results take Home's place once there's something to search for.
    private var showsResults: Bool { !query.isEmpty }
    /// The search bar is in use: "Cancel" shows next to it.
    private var searchEngaged: Bool { searchFocused || !searchText.isEmpty }

    var body: some View {
        @Bindable var router = router
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    header
                        .id(Self.topId)
                    ZStack(alignment: .top) {
                        if showsResults {
                            HomeSearchResultsView(model: search,
                                                  retry: { Task { await search.search(query, env: env) } },
                                                  openSidequest: { openSidequest($0, proxy: proxy) },
                                                  openPerson: openPerson,
                                                  openPlace: openPlace,
                                                  openPost: openPost)
                                .sqReloadable("home.search") { await search.reload(env: env) }
                                .transition(.opacity)
                        } else {
                            homeContent(segment: $router.homeSegment)
                                .transition(.opacity)
                        }
                    }
                    .animation(reduceMotion ? Motion.reduced : Motion.standard, value: showsResults)
                }
                .sqSheet(isPresented: $showsRate, onDismiss: rateSheetClosed) {
                    HomeRateSheetHost(event: $rateEvent, close: { showsRate = false }, saved: ratingSaved)
                }
            }
            .scrollIndicators(.hidden)
            .scrollDismissesKeyboard(.immediately)
            .sqPullToRefresh()
            // Snapped to the half point: the width feeds the carousel's card widths, and a measurement
            // that creeps by a rounding error must not trigger another layout pass (an endless loop).
            .onGeometryChange(for: CGFloat.self, of: { ($0.size.width * 2).rounded() / 2 }, action: { pageWidth = $0 })
            // Like the prototype, the page ends at the tab bar (nothing shows through it).
            .mask { Rectangle().ignoresSafeArea(edges: .top) }
            .background(Theme.cream.ignoresSafeArea())
        }
        .sqSheet(isPresented: $showsEvent, style: SQSheetStyle(height: .fixed(660)), onDismiss: eventSheetClosed) {
            HomeEventSheetHost(route: $eventRoute, store: store, close: { showsEvent = false }, didChange: { reloadAfterEvent = true })
        }
        .sensoryFeedback(.success, trigger: ratingsSaved)
        .task { await start() }
        .task { await listenForUpdates() }
        // Typing searches after a short pause (each keystroke restarts the wait).
        .task(id: query) { await runSearch() }
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

    /// The search bar where the date used to be, with "Cancel" while it's in use, then the title
    /// and your avatar (they make way for results).
    private var header: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                HomeSearchBar(text: $searchText, focused: $searchFocused)
                if searchEngaged {
                    Button("Cancel", action: endSearch)
                        .buttonStyle(.sqLink(size: 16, weight: .regular))
                        .transition(.move(edge: .trailing).combined(with: .opacity))
                }
            }
            // The bar's height, whatever the button's 44pt touch target (nothing below moves).
            .frame(height: 42)
            .animation(reduceMotion ? Motion.reduced : Motion.quick, value: searchEngaged)
            if !showsResults {
                HStack(alignment: .bottom) {
                    Text.brand(prefix: "Your ")
                        .largeTitleStyle()
                        .accessibilityAddTraits(.isHeader)
                        .padding(.bottom, 0.67)
                    Spacer(minLength: 8)
                    Button {
                        router.select(.account)
                    } label: {
                        CurrentUserAvatar(size: 44)
                    }
                    .buttonStyle(.sqPressable)
                    .accessibilityLabel("Open profile")
                }
                .padding(.top, 10)
                .transition(.opacity)
            }
        }
        .padding(.horizontal, Metrics.side)
        .designTopPadding(56)
        .padding(.bottom, 6)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: showsResults)
    }

    private func homeContent(segment: Binding<Router.HomeSegment>) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            SQSegmentedControl(selection: segment, options: Self.segments, accessibilityLabel: "Home view")
                .padding(.horizontal, Metrics.side)
                .padding(.top, 12)
            if let failure = store.notesFailure {
                HomeNotesFailureBanner(title: failure.title, isRetrying: store.isRetryingNotes,
                                       retry: { store.retryNotes(env) }, dismiss: store.dismissNotesFailure)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 12)
                    .sqTransition(.banner)
            }
            segmentContent
        }
    }

    /// The three segments cross-fade. Each registers its pull-to-refresh reload while it's shown;
    /// a pull reloads everything Home has loaded (hidden segments stay fresh for when you switch).
    private var segmentContent: some View {
        ZStack(alignment: .top) {
            switch router.homeSegment {
            case .itineraries:
                HomeItinerariesView(store: store, pageWidth: pageWidth, openBlock: openTimelineBlock, retry: {
                    Task { await store.retryItineraries(env) }
                })
                .sqReloadable("home.itineraries") { await store.refresh(env) }
                .transition(.opacity)
            case .calendar:
                HomeCalendarView(store: store, pageWidth: pageWidth, openItem: openCalendarItem, retry: {
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

    // MARK: Search

    private func runSearch() async {
        let text = query
        guard !text.isEmpty else {
            search.reset()
            return
        }
        try? await Task.sleep(for: .milliseconds(300))
        guard !Task.isCancelled else { return }
        await search.search(text, env: env)
    }

    /// A sidequest in the results: search closes and it's selected on the Sidequests segment, its
    /// card popping as it comes into view.
    private func openSidequest(_ itinerary: Itinerary, proxy: ScrollViewProxy) {
        endSearch()
        withMotion { router.homeSegment = .itineraries }
        withMotion(Motion.gentle) { proxy.scrollTo(Self.topId, anchor: .top) }
        Task {
            // Once the segment is back on screen, so the card is there to pop.
            try? await Task.sleep(for: .milliseconds(150))
            store.focus(itinerary)
        }
    }

    /// A person: your chat with them opens (search stays, for coming back).
    private func openPerson(_ person: PersonRef) {
        searchFocused = false
        search.openChat(with: person, env: env) { thread in
            router.openThread(thread.id, isGroup: false)
        }
    }

    /// A place: plan a sidequest that ends there (search closes).
    private func openPlace(_ place: Place) {
        endSearch()
        router.openCreate(CreateDraft(destination: place))
    }

    /// A Forum post: it's in the Forum (search stays, for coming back).
    private func openPost(_ post: ForumPost) {
        searchFocused = false
        router.select(.forum)
    }

    private func endSearch() {
        searchFocused = false
        searchText = ""
        search.reset()
    }

    // MARK: Sheets

    private func openEvent(_ route: HomeEventRoute) {
        eventRoute = route
        showsEvent = true
    }

    /// A timeline block: its copy shows at once while the details load.
    private func openTimelineBlock(_ item: ItineraryItem, in itinerary: Itinerary) {
        openEvent(HomeEventRoute(id: item.id, itineraryId: itinerary.id, seed: item, planIsShared: itinerary.goingCount > 1))
    }

    /// A calendar block says which plan it's on (`itinerary_id`), so its notes and travel go there;
    /// calendar-only entries have none and save to `/events/{id}`.
    private func openCalendarItem(_ item: CalendarItem) {
        let itinerary = item.itineraryId.flatMap { id in store.itineraries.value?.first { $0.id == id } }
        openEvent(HomeEventRoute(id: item.id, itineraryId: item.itineraryId, seed: itinerary?.items.first { $0.id == item.id },
                                 planIsShared: (itinerary?.goingCount ?? 1) > 1))
    }

    /// Demo deep links by block id: the plan it's on, if it's on a loaded one.
    private func openBlock(id: String, checkout: Bool = false) {
        let match = store.itinerary(containing: id)
        openEvent(HomeEventRoute(id: id, itineraryId: match?.itinerary.id, seed: match?.item, opensCheckout: checkout,
                                 planIsShared: (match?.itinerary.goingCount ?? 1) > 1))
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
        endSearch()
        withMotion { router.homeSegment = .itineraries }
        await store.showNewItinerary(id, env: env, coverClosedAt: coverClosing)
        if router.selectedItineraryId == id { router.selectedItineraryId = nil }
    }

    /// Changes from the server (`WS /ws`), applied as they arrive.
    private func listenForUpdates() async {
        for await event in env.realtime.subscribe() {
            switch event {
            case .itineraryUpdated(let itinerary):
                store.upsert(itinerary)
                Task { await store.refreshDaysIfShown(env) }
            case .itineraryRemoved(let id):
                store.removeRemotely(id)
                Task { await store.refreshDaysIfShown(env) }
            case .transitDelay(let itineraryId, let itemId, let minutes):
                store.setDelay(itineraryId: itineraryId, itemId: itemId, minutes: minutes)
            case .joinRequest:
                // Joins are accepted as they come for now, so a plan you host has new counts.
                if store.hostsSharedPlan { Task { await store.refreshPlans(env) } }
            case .joinUpdate(_, let result):
                // You're in: the plan is on Home now.
                if result.status == .joined { Task { await store.refreshPlans(env) } }
            case .checkoutStatus(_, let state):
                // A booking landed (here or on another device): items carry their tickets.
                if state == .booked { Task { await store.loadItineraries(env) } }
            default:
                break
            }
        }
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
        case "edit":
            store.editRequest = id ?? ""
        case "rate":
            await store.loadPast(env)
            router.homeSegment = .past
            if let id, let event = store.past.value?.first(where: { $0.id == id }) { openRating(event) }
        case "search":
            searchText = parts.dropFirst().joined(separator: "/")
        default:
            break
        }
    }
}

/// Notes that still couldn't be saved after their sheet closed (and a retry): says so and offers
/// to try again. Dismissing keeps the notes; reopening the block shows them and saves them.
private struct HomeNotesFailureBanner: View {
    let title: String
    let isRetrying: Bool
    let retry: () -> Void
    let dismiss: () -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "exclamationmark.circle")
                .font(.system(size: 15, weight: .semibold))
                .accessibilityHidden(true)
            Text("Couldn't save your notes for \(title).")
                .sqFont(14)
                .homeLine(14)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, 10)
            Button(action: retry) {
                ZStack {
                    Text("Try again")
                        .opacity(isRetrying ? 0 : 1)
                    if isRetrying {
                        LoadingDots(color: Theme.dangerText, dotSize: 5)
                            .transition(.opacity)
                    }
                }
                .sqFont(13, .semibold)
                .frame(minHeight: Metrics.minTouch)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(isRetrying)
            .animation(reduceMotion ? Motion.reduced : Motion.quick, value: isRetrying)
            .accessibilityLabel("Try saving your notes again")
            .accessibilityValue(isRetrying ? "Saving" : "")
            Button(action: dismiss) {
                Image(systemName: "xmark")
                    .font(.system(size: 11, weight: .bold))
                    .frame(width: 32, height: Metrics.minTouch)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Dismiss")
        }
        .foregroundStyle(Theme.dangerText)
        .padding(.leading, 14)
        .padding(.trailing, 4)
        .background(Theme.dangerBg, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .contain)
    }
}

/// The Event sheet for the current route (read through the binding, so it's never stale).
private struct HomeEventSheetHost: View {
    @Binding var route: HomeEventRoute?
    let store: HomeStore
    let close: () -> Void
    let didChange: () -> Void

    var body: some View {
        if let route {
            HomeEventSheet(route: route, store: store, close: close, didChange: didChange)
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

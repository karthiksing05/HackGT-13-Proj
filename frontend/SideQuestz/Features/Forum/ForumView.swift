import SwiftUI

/// Forum tab (GUI_PLAN.md §7.8): area pill, Everyone nearby | Friends, "Bored right now?",
/// All / Open plans / Free now + Sort & filter, and the feed. Everything comes from `env.api`
/// (`GET /forum/posts` does the filtering and sorting around the area's center; "Current location"
/// is resolved on the device before the feed loads).
///
/// "I'm free" posts go up around the current area and radius, and the live card describes who
/// sees it from the post itself. Joining an open plan shows where you stand (requested, in, full,
/// closed); `join.update` from the socket moves a card along, `forum.update` refreshes the feed and
/// your post.
///
/// Loading: shimmering post cards on the first load, then the posts arrive one after another.
/// A new scope, type, area, sort or filter keeps the current list on screen (dimmed, with a loading
/// pill) and then animates the results into place: new posts rise in, dropped ones fade out,
/// reordered ones slide, and the results count rolls. Pull to refresh reloads the feed and your
/// post without leaving the screen.
///
/// Launch routes: `forum`, `forum/area` (Area sheet), `forum/filter` (Sort & filter sheet).
struct ForumView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var query = ForumQuery()
    /// Edited by the Sort & filter sheet, applied to `query` when the sheet closes.
    @State private var pendingQuery = ForumQuery()
    @State private var posts: Loadable<[ForumPost]> = .loading
    /// The query behind the posts on screen (the results line describes these, not the next ones).
    @State private var shownQuery = ForumQuery()
    /// Results for a new query are loading while the current list stays on screen.
    @State private var refreshing = false
    /// Posts from a load that replaced the skeleton: only these arrive one after another.
    @State private var arrivingIds: Set<String> = []
    @State private var myPost: Loadable<MyFreePost?> = .loading
    /// The "Bored right now?" button whose post is being sent.
    @State private var posting: ForumPostVisibility?
    @State private var takingDown = false
    /// You posted from this screen: the live card confirms it with a check.
    @State private var justPosted = false
    @State private var statusError: String?
    @State private var busyPostIds: Set<String> = []
    @State private var postErrors: [String: String] = [:]
    /// DM thread for each free-now post after "Plan together" (post id → thread id).
    @State private var dmThreadIds: [String: String] = [:]
    /// Group chat of each plan you got into (post id → thread id), from the join result.
    @State private var joinThreadIds: [String: String] = [:]
    /// Why the feed isn't showing "Current location" (the device didn't give a location).
    @State private var areaNote: String?
    @State private var showArea = false
    @State private var showFilter = false

    private var statusBusy: Bool { posting != nil || takingDown }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                TabScreenHeader(title: "Forum")
                areaPill
                    .padding(.horizontal, Metrics.side)
                    .padding(.bottom, 6)
                if let areaNote {
                    Text(areaNote)
                        .socialText(12)
                        .foregroundStyle(Theme.dangerText)
                        .padding(.horizontal, Metrics.side)
                        .sqTransition(.rise)
                }
                SQSegmentedControl(selection: $query.scope,
                                   options: ForumScope.allCases.map { (value: $0, label: $0.label) },
                                   accessibilityLabel: "Who you see")
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 10)
                statusCard
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 12)
                typeRow
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 14)
                    .padding(.bottom, 6)
                resultsLine
                    .padding(.horizontal, Metrics.side)
                    .padding(.bottom, 10)
                feed
                    .padding(.horizontal, Metrics.side)
                    .padding(.bottom, 24)
            }
        }
        .scrollIndicators(.hidden)
        .sqPullToRefresh()
        .background(Theme.cream.ignoresSafeArea())
        .task(id: query) { await loadPosts() }
        .task { await loadMyPost() }
        // A free post comes down on its own at `until`: check back then.
        .task(id: myPost.value??.until) { await refreshWhenPostEnds() }
        .task { await applyLaunchRoute() }
        .task { await listenForUpdates() }
        .sqReloadable("forum") { await reload() }
        .sqSheet(isPresented: $showArea, style: SQSheetStyle(height: .fitted(max: 760))) {
            ForumAreaSheet(area: $query.area, radiusMi: $query.radiusMi) { showArea = false }
        }
        .sqSheet(isPresented: $showFilter) {
            ForumFilterSheet(pending: $pendingQuery, initialCount: refreshing ? nil : posts.value?.count) { showFilter = false }
        }
        .onChange(of: showFilter) { _, shown in
            if !shown, pendingQuery != query { query = pendingQuery }
        }
        // Keep the sheet's starting point current (scope, type, area) before it opens.
        .onChange(of: query) { _, newQuery in
            if !showFilter { pendingQuery = newQuery }
        }
        // The note about a location that couldn't be found goes once you pick an area again.
        .onChange(of: showArea) { _, shown in
            if shown, areaNote != nil { withMotion(Motion.quick) { areaNote = nil } }
        }
        .animation(Motion.standard, value: areaNote)
    }

    // MARK: Header

    private var areaPill: some View {
        Button {
            showArea = true
        } label: {
            HStack(spacing: 6) {
                SocialGlyph(kind: .pin, size: 16, lineWidth: 2).foregroundStyle(Theme.sageInk)
                Text("\(query.area.name) · \(query.radiusMi) mi")
                    .sqFont(14, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                    .sqNumeric()
                SocialGlyph(kind: .chevronDown, size: 14, lineWidth: 2.2).foregroundStyle(Theme.ink)
            }
            .padding(.horizontal, 12)
            .frame(height: 34)
            .background(.white, in: Capsule())
            .contentShape(Rectangle().inset(by: -5))
            .animation(Motion.standard, value: query.area)
            .animation(Motion.standard, value: query.radiusMi)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Forum area: \(query.area.name), \(query.radiusMi) mile\(query.radiusMi == 1 ? "" : "s")")
        .accessibilityHint("Changes where posts come from")
    }

    // MARK: "Bored right now?"

    private var statusCard: some View {
        ZStack(alignment: .top) {
            if case .loaded(let post?) = myPost {
                livePostCard(post)
                    .transition(.opacity)
            } else {
                boredCard
                    .transition(.opacity)
            }
        }
    }

    private var boredCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            VStack(alignment: .leading, spacing: 0) {
                Text("Bored right now?").socialText(15, .semibold).foregroundStyle(Theme.ink)
                Text("Post that you're free. Choose who sees it.").socialText(13).foregroundStyle(Theme.text3)
            }
            .accessibilityElement(children: .combine)
            HStack(spacing: 8) {
                postButton("Friends only", visibility: .friends,
                           style: .sq(fill: .white, foreground: Theme.sageInk, border: Theme.sage, borderWidth: 1.5,
                                      height: 38, radius: 12, fontSize: 14),
                           textColor: Theme.sageInk)
                postButton("Everyone nearby", visibility: .everyone,
                           style: .sq(fill: Theme.sage, foreground: Theme.ink, height: 38, radius: 12, fontSize: 14),
                           textColor: Theme.ink)
            }
            // Until we know whether you already have a live post.
            .disabled(myPost.isLoading)
            .opacity(myPost.isLoading ? 0.6 : 1)
            .animation(Motion.standard, value: myPost.isLoading)
            statusFootnote
        }
        .padding(Metrics.cardPadding)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    /// "Friends only" / "Everyone nearby": loading dots while the post goes up; the other button dims.
    private func postButton(_ title: String, visibility: ForumPostVisibility, style: SQButtonStyle, textColor: Color) -> some View {
        let busy = posting == visibility
        return Button { postFree(visibility) } label: {
            SocialBusyLabel(isBusy: busy, color: textColor) { Text(title) }
        }
        .buttonStyle(style)
        .disabled(statusBusy)
        .opacity(posting != nil && !busy ? 0.5 : 1)
        .animation(Motion.quick, value: posting)
        .accessibilityLabel(title)
        .accessibilityValue(busy ? "Loading" : "")
    }

    private func livePostCard(_ post: MyFreePost) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                HStack(spacing: 6) {
                    // Just posted from here: a check draws in as the card turns live.
                    if justPosted {
                        AnimatedCheck(lineWidth: 3, delay: 0.3)
                            .frame(width: 13, height: 13)
                    }
                    Text("YOUR POST · LIVE")
                        .socialText(13, .semibold)
                }
                .foregroundStyle(Theme.transitText)
                Spacer(minLength: 8)
                Button { takeDown(post) } label: {
                    SocialBusyLabel(isBusy: takingDown, color: Theme.transitText, dotSize: 5) { Text("Take down") }
                }
                .buttonStyle(.sqPill(fill: .clear, foreground: Theme.transitText, border: Theme.transitText,
                                     height: 32, fontSize: 13, horizontalPadding: 12))
                .frame(height: 32)
                .disabled(statusBusy)
                .accessibilityLabel("Take down")
                .accessibilityValue(takingDown ? "Loading" : "")
            }
            Text(post.text).socialText(15, .semibold).foregroundStyle(Theme.ink)
            Text(visibilityLine(post)).socialText(13).foregroundStyle(Theme.text2)
            statusFootnote
        }
        .padding(Metrics.cardPadding)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.transitBg, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    /// Posting / take-down failures, or a failed check for your live post (with a retry).
    @ViewBuilder private var statusFootnote: some View {
        if let statusError {
            Text(statusError)
                .socialText(12)
                .foregroundStyle(Theme.dangerText)
                .sqTransition(.rise)
        } else if case .failed(let message) = myPost {
            HStack(spacing: 6) {
                Text(message).socialText(12).foregroundStyle(Theme.dangerText)
                Button("Try again") { Task { await loadMyPost() } }
                    .buttonStyle(.sqLink(size: 12))
                    .frame(height: 20)
            }
            .sqTransition(.rise)
        }
    }

    /// Who sees the post, from the post itself (where and how far it was posted), not the filter.
    private func visibilityLine(_ post: MyFreePost) -> String {
        guard post.visibility == .everyone else { return "Visible to your friends only" }
        let place = post.areaLabel.map { $0 == ForumArea.currentLocation.name ? "you" : $0 }
        switch (post.radiusMi, place) {
        case let (radius?, place?): return "Visible to everyone within \(radius) mi of \(place)"
        case let (nil, place?): return "Visible to everyone near \(place)"
        case let (radius?, nil): return "Visible to everyone within \(radius) mi"
        case (nil, nil): return "Visible to everyone nearby"
        }
    }

    // MARK: Type chips + filter

    private var typeRow: some View {
        HStack(spacing: 6) {
            ForEach(ForumTypeFilter.allCases) { type in
                SQChip(label: type.label, isOn: query.type == type, style: .socialDark) { query.type = type }
            }
            Spacer(minLength: 0)
            filterButton
        }
    }

    private var filterButton: some View {
        let count = query.filterCount
        let active = count > 0
        return Button {
            pendingQuery = query
            showFilter = true
        } label: {
            HStack(spacing: 5) {
                SocialGlyph(kind: .sliders, size: 15, lineWidth: 2)
                Text(active ? "Filter · \(count)" : "Filter").lineLimit(1).sqNumeric()
            }
            .sqFont(13, .semibold)
            .foregroundStyle(active ? Theme.sageInk : Theme.ink)
            .padding(.horizontal, 11)  // 10 + the 1pt CSS border
            .frame(height: 32)
            .background(active ? Theme.sageTint : .white, in: Capsule())
            .overlay(Capsule().strokeBorder(active ? Theme.sage : Theme.lineStrong, lineWidth: 1))
            .contentShape(Rectangle().inset(by: -6))
            .animation(Motion.quick, value: count)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Sort and filter")
        .accessibilityValue(active ? "\(count) filter\(count == 1 ? "" : "s") on" : "")
    }

    private var resultsLine: some View {
        HStack(spacing: 8) {
            if let count = posts.value?.count {
                Text("\(count) \(count == 1 ? "result" : "results") · sorted by \(shownQuery.sort.label.lowercased())")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
                    .sqNumeric()
                    .transition(.opacity)
            } else if posts.isLoading {
                SkeletonBlock(width: 150, height: 10, color: Theme.skeletonOnCream)
                    .sqShimmer()
                    .accessibilityHidden(true)
                    .transition(.opacity)
            }
            Spacer(minLength: 0)
            if query.hasFiltersOrSort {
                Button {
                    setQuery(query.clearingFilters())
                } label: {
                    Text("Clear filters")
                        .sqFont(12, .semibold)
                        .foregroundStyle(Theme.sageInk)
                        .padding(.horizontal, 8)
                        .frame(height: 24)
                        .contentShape(Rectangle().inset(by: -10))
                }
                .buttonStyle(.plain)
                .sqTransition(.pop)
            }
        }
        .frame(minHeight: 24)
        .animation(Motion.quick, value: query.hasFiltersOrSort)
    }

    // MARK: Feed

    private var feed: some View {
        ZStack(alignment: .top) {
            switch posts {
            case .loading:
                ForumFeedSkeleton()
                    .sqSlowLoading(lines: ["Finding posts near \(query.area.isCurrentLocation ? "you" : query.area.name)…"])
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message) { Task { await retryPosts() } }
                    .transition(.opacity)
            case .loaded(let list):
                postList(list)
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: posts.phase)
        .sqRefreshing(refreshing)
    }

    private func postList(_ list: [ForumPost]) -> some View {
        VStack(spacing: 12) {
            if list.isEmpty {
                Text("Nothing here yet. Try a wider area.")
                    .socialText(15)
                    .foregroundStyle(Theme.text3)
                    .multilineTextAlignment(.center)
                    .padding(20)
                    .frame(maxWidth: .infinity)
                    .transition(.opacity)
            }
            ForEach(Array(list.enumerated()), id: \.element.id) { index, post in
                ForumPostCard(post: post, isBusy: busyPostIds.contains(post.id), error: postErrors[post.id]) {
                    act(on: post)
                }
                .socialArrival(index, staggered: arrivingIds.contains(post.id))
            }
        }
    }

    // MARK: Loading

    /// Runs for every query. The first load shows the skeleton; later ones keep the list on screen.
    private func loadPosts() async {
        // "Current location" needs the device's coordinate first (the Area sheet normally has it).
        if query.area.isCurrentLocation, query.area.coordinate == nil {
            await resolveCurrentLocation()
            return
        }
        let request = query
        if posts.value == nil {
            if !posts.isLoading { withMotion { posts = .loading } }
        } else {
            refreshing = true
        }
        let result = await Loadable.run { try await env.api.forumPosts(request) }
        guard !Task.isCancelled, request == query else { return }
        show(result, for: request)
    }

    /// Fills in "Current location" (the new area reloads the feed), or goes back to the last area
    /// with a short note when the device can't say where it is.
    private func resolveCurrentLocation() async {
        if let place = await env.location.currentLocation(), let coordinate = place.coordinate {
            guard !Task.isCancelled else { return }
            query.area = ForumArea(name: ForumArea.currentLocation.name, coordinate: coordinate, isCurrentLocation: true)
        } else {
            guard !Task.isCancelled else { return }
            let fallback = shownQuery.area.coordinate == nil ? ForumArea.midtown : shownQuery.area
            query.area = fallback
            withMotion { areaNote = "Couldn't get your location. Showing \(fallback.name)." }
        }
    }

    /// "Try again" after a failed feed.
    private func retryPosts() async {
        let request = query
        withMotion { posts = .loading }
        let result = await Loadable.run { try await env.api.forumPosts(request) }
        guard request == query else { return }
        show(result, for: request)
    }

    /// Swaps in a result. Coming from the skeleton, the posts arrive one after another; otherwise
    /// they animate from the old list (inserted, removed and reordered in place).
    private func show(_ result: Loadable<[ForumPost]>, for request: ForumQuery) {
        let fromSkeleton = posts.value == nil
        arrivingIds = fromSkeleton ? Set(result.value?.map(\.id) ?? []) : []
        withMotion(fromSkeleton ? Motion.standard : Motion.gentle) {
            posts = result
            shownQuery = request
            refreshing = false
        }
    }

    private func loadMyPost() async {
        if myPost.value == nil, !myPost.isLoading { withMotion { myPost = .loading } }
        let result = await Loadable.run { try await env.api.myFreePost() }
        guard !statusBusy else { return }
        withMotion {
            myPost = result
            if case .loaded(nil) = result { justPosted = false }
        }
    }

    /// Quietly re-checks your post (a take-down elsewhere, `forum.update`); keeps it if the call fails.
    private func refreshMyPost() async {
        // (`try?` would flatten "no post" into a failure, so keep the Loadable.)
        let result = await Loadable.run { try await env.api.myFreePost() }
        guard case .loaded(let mine) = result, !statusBusy else { return }
        withMotion {
            myPost = .loaded(mine)
            if mine == nil { justPosted = false }
        }
    }

    /// Sleeps until the live post's `until`, then asks the server whether it's still up.
    private func refreshWhenPostEnds() async {
        guard case .loaded(let post?) = myPost, let until = post.until else { return }
        let seconds = until.timeIntervalSince(env.clock.now)
        guard seconds > 0 else { return }
        try? await Task.sleep(for: .seconds(seconds + 1))
        guard !Task.isCancelled else { return }
        await refreshMyPost()
    }

    /// Pull to refresh: the feed and your post, together. Keeps what's on screen if a call fails.
    private func reload() async {
        let request = query
        let feedCall = Task { try await env.api.forumPosts(request) }
        let postCall = Task { try await env.api.myFreePost() }
        if case .success(let fresh) = await feedCall.result, request == query {
            show(.loaded(fresh), for: request)
        }
        if case .success(let mine) = await postCall.result, !statusBusy {
            withMotion {
                myPost = .loaded(mine)
                if mine == nil { justPosted = false }
            }
        }
    }

    /// `forum/area` and `forum/filter` open their sheets.
    private func applyLaunchRoute() async {
        guard let parts = router.consumeLaunch("forum"), let sheet = parts.first else { return }
        try? await Task.sleep(for: .milliseconds(350))
        switch sheet {
        case "area":
            showArea = true
        case "filter":
            pendingQuery = query
            showFilter = true
        default:
            break
        }
    }

    /// `forum.update` refreshes the feed and your post in place; `join.update` moves a card along
    /// (and, once you're in, reloads everything so the plan shows on Home and its chat in Groups).
    private func listenForUpdates() async {
        for await event in env.realtime.subscribe() {
            switch event {
            case .forumUpdate:
                Task { await refreshFeed() }
                Task { await refreshMyPost() }
            case .joinUpdate(let postId, let result):
                applyJoin(result, to: postId)
            default:
                break
            }
        }
    }

    private func refreshFeed() async {
        let request = query
        if let fresh = try? await env.api.forumPosts(request), request == query {
            show(.loaded(fresh), for: request)
        }
    }

    // MARK: Actions

    private func setQuery(_ newQuery: ForumQuery) {
        query = newQuery
        pendingQuery = newQuery
    }

    private func postFree(_ visibility: ForumPostVisibility) {
        guard !statusBusy else { return }
        posting = visibility
        withMotion(Motion.quick) { statusError = nil }
        // Around the area and radius you picked (the one on screen while "Current location" resolves).
        let area = query.area.coordinate != nil ? query.area : shownQuery.area
        let request = NewFreePost(visibility: visibility, until: nil, area: area, radiusMi: query.radiusMi)
        Task {
            do {
                let post = try await env.api.postFreeNow(request)
                withMotion(Motion.arrive) {
                    myPost = .loaded(post)
                    justPosted = true
                    posting = nil
                }
            } catch {
                withMotion {
                    statusError = error.socialMessage
                    posting = nil
                }
            }
        }
    }

    private func takeDown(_ post: MyFreePost) {
        guard !statusBusy else { return }
        takingDown = true
        withMotion(Motion.quick) { statusError = nil }
        Task {
            do {
                try await env.api.deleteForumPost(id: post.id)
                withMotion(Motion.arrive) {
                    myPost = .loaded(nil)
                    justPosted = false
                    takingDown = false
                }
            } catch {
                withMotion {
                    statusError = error.socialMessage
                    takingDown = false
                }
            }
        }
    }

    private func act(on post: ForumPost) {
        switch post.type {
        case .plan:
            switch post.joinStatus {
            case .none:
                run(post) {
                    let result = try await env.api.requestToJoin(postId: post.id)
                    applyJoin(result, to: post.id)
                }
            case .requested:
                run(post) {
                    try await env.api.cancelJoinRequest(postId: post.id)
                    update(post.id) { $0.joinStatus = .none }
                }
            case .joined:
                openGroupChat(for: post.id)
            case .full, .closed:
                break
            }
        case .freeNow:
            if post.planTogetherSent {
                run(post) {
                    let threadId: String
                    if let known = dmThreadIds[post.id] {
                        threadId = known
                    } else {
                        threadId = try await env.api.startDM(userId: post.author.id).id
                        dmThreadIds[post.id] = threadId
                    }
                    router.openThread(threadId, isGroup: false)
                }
            } else {
                run(post) {
                    let thread = try await env.api.planTogether(postId: post.id)
                    dmThreadIds[post.id] = thread.id
                    update(post.id) { $0.planTogetherSent = true }
                }
            }
        }
    }

    /// Where you stand after asking (or after the host answered): the card's button follows. Once
    /// you're in, everything reloads so the plan shows on Home and its chat in Groups.
    private func applyJoin(_ result: JoinResult, to postId: String) {
        if let threadId = result.threadId { joinThreadIds[postId] = threadId }
        let wasJoined = posts.value?.first { $0.id == postId }?.joinStatus == .joined
        // A slow "requested" answer never undoes "you're in" that already came over the socket.
        guard !(wasJoined && result.status == .requested) else { return }
        update(postId) { $0.joinStatus = result.status }
        if result.status == .joined, !wasJoined {
            Task { await env.reloadAll() }
        }
    }

    /// "You're in · Open chat": the plan's group chat (from the join, or the post's `thread_id`), or
    /// the Groups tab when the server didn't say which chat it is.
    private func openGroupChat(for postId: String) {
        if let threadId = joinThreadIds[postId] ?? posts.value?.first(where: { $0.id == postId })?.threadId {
            router.openThread(threadId, isGroup: true)
        } else {
            router.select(.groups)
        }
    }

    /// Runs a card action with loading dots in its button; failures rise in under the button.
    private func run(_ post: ForumPost, _ work: @escaping () async throws -> Void) {
        guard !busyPostIds.contains(post.id) else { return }
        busyPostIds.insert(post.id)
        if postErrors[post.id] != nil { withMotion(Motion.quick) { postErrors[post.id] = nil } }
        Task {
            do {
                try await work()
            } catch {
                withMotion { postErrors[post.id] = error.socialMessage }
            }
            withMotion(Motion.arrive) { _ = busyPostIds.remove(post.id) }
        }
    }

    /// A confirmed change to one card (label and colors morph).
    private func update(_ postId: String, _ change: (inout ForumPost) -> Void) {
        guard var list = posts.value, let index = list.firstIndex(where: { $0.id == postId }) else { return }
        change(&list[index])
        withMotion(Motion.arrive) { posts = .loaded(list) }
    }
}

/// First-load placeholder for the feed: three post-shaped cards, shimmering. Reads "Loading".
private struct ForumFeedSkeleton: View {
    var body: some View {
        VStack(spacing: 12) {
            ForEach(0..<3, id: \.self) { index in
                ForumPostSkeletonCard(seed: index)
            }
        }
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

#Preview {
    ForumView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

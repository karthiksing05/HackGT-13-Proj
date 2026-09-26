import SwiftUI

/// Forum tab (GUI_PLAN.md §7.8): area pill, Everyone nearby | Friends, "Bored right now?",
/// All / Open plans / Free now + Sort & filter, and the feed. Everything comes from `env.api`
/// (`GET /forum/posts` does the filtering and sorting).
///
/// Launch routes: `forum`, `forum/area` (Area sheet), `forum/filter` (Sort & filter sheet).
struct ForumView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var query = ForumQuery()
    /// Edited by the Sort & filter sheet, applied to `query` when the sheet closes.
    @State private var pendingQuery = ForumQuery()
    @State private var posts: Loadable<[ForumPost]> = .loading
    @State private var myPost: Loadable<MyFreePost?> = .loading
    @State private var statusBusy = false
    @State private var statusError: String?
    @State private var busyPostIds: Set<String> = []
    @State private var postErrors: [String: String] = [:]
    /// DM thread for each free-now post after "Plan together" (post id → thread id).
    @State private var dmThreadIds: [String: String] = [:]
    @State private var showArea = false
    @State private var showFilter = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                TabScreenHeader(title: "Forum")
                areaPill
                    .padding(.horizontal, Metrics.side)
                    .padding(.bottom, 6)
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
        .background(Theme.cream.ignoresSafeArea())
        .task(id: query) { await loadPosts() }
        .task { await loadMyPost() }
        .task { await applyLaunchRoute() }
        .task { await listenForUpdates() }
        .sqSheet(isPresented: $showArea) {
            ForumAreaSheet(area: $query.area, radiusMi: $query.radiusMi) { showArea = false }
        }
        .sqSheet(isPresented: $showFilter) {
            ForumFilterSheet(pending: $pendingQuery, initialCount: posts.value?.count) { showFilter = false }
        }
        .onChange(of: showFilter) { _, shown in
            if !shown, pendingQuery != query { query = pendingQuery }
        }
        // Keep the sheet's starting point current (scope, type, area) before it opens.
        .onChange(of: query) { _, newQuery in
            if !showFilter { pendingQuery = newQuery }
        }
    }

    // MARK: Header

    private var areaPill: some View {
        Button {
            showArea = true
        } label: {
            HStack(spacing: 6) {
                SocialGlyph(kind: .pin, size: 16, lineWidth: 2).foregroundStyle(Theme.sageInk)
                Text("\(query.area) · \(query.radiusMi) mi")
                    .sqFont(14, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                SocialGlyph(kind: .chevronDown, size: 14, lineWidth: 2.2).foregroundStyle(Theme.ink)
            }
            .padding(.horizontal, 12)
            .frame(height: 34)
            .background(.white, in: Capsule())
            .contentShape(Rectangle().inset(by: -5))
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Forum area: \(query.area), \(query.radiusMi) mile\(query.radiusMi == 1 ? "" : "s")")
        .accessibilityHint("Changes where posts come from")
    }

    // MARK: "Bored right now?"

    @ViewBuilder private var statusCard: some View {
        if case .loaded(let post?) = myPost {
            livePostCard(post)
        } else {
            boredCard
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
                Button("Friends only") { postFree(.friends) }
                    .buttonStyle(.sq(fill: .white, foreground: Theme.sageInk, border: Theme.sage, borderWidth: 1.5,
                                     height: 38, radius: 12, fontSize: 14))
                Button("Everyone nearby") { postFree(.everyone) }
                    .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 38, radius: 12, fontSize: 14))
            }
            .disabled(statusBusy || myPost.isLoading)
            .opacity(statusBusy ? 0.6 : 1)
            statusFootnote
        }
        .padding(Metrics.cardPadding)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    private func livePostCard(_ post: MyFreePost) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("YOUR POST · LIVE")
                    .socialText(13, .semibold)
                    .foregroundStyle(Theme.transitText)
                Spacer(minLength: 8)
                Button("Take down") { takeDown(post) }
                    .buttonStyle(.sqPill(fill: .clear, foreground: Theme.transitText, border: Theme.transitText,
                                         height: 32, fontSize: 13, horizontalPadding: 12))
                    .frame(height: 32)
                    .disabled(statusBusy)
                    .opacity(statusBusy ? 0.6 : 1)
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
            Text(statusError).socialText(12).foregroundStyle(Theme.dangerText)
        } else if case .failed(let message) = myPost {
            HStack(spacing: 6) {
                Text(message).socialText(12).foregroundStyle(Theme.dangerText)
                Button("Try again") { Task { await loadMyPost() } }
                    .buttonStyle(.sqLink(size: 12))
                    .frame(height: 20)
            }
        }
    }

    private func visibilityLine(_ post: MyFreePost) -> String {
        switch post.visibility {
        case .friends: "Visible to your friends only"
        case .everyone: "Visible to everyone within \(query.radiusMi) mi of \(query.area)"
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
                Text(active ? "Filter · \(count)" : "Filter").lineLimit(1)
            }
            .sqFont(13, .semibold)
            .foregroundStyle(active ? Theme.sageInk : Theme.ink)
            .padding(.horizontal, 11)  // 10 + the 1pt CSS border
            .frame(height: 32)
            .background(active ? Theme.sageTint : .white, in: Capsule())
            .overlay(Capsule().strokeBorder(active ? Theme.sage : Theme.lineStrong, lineWidth: 1))
            .contentShape(Rectangle().inset(by: -6))
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Sort and filter")
        .accessibilityValue(active ? "\(count) filter\(count == 1 ? "" : "s") on" : "")
    }

    private var resultsLine: some View {
        HStack(spacing: 8) {
            if let count = posts.value?.count {
                Text("\(count) \(count == 1 ? "result" : "results") · sorted by \(query.sort.label.lowercased())")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
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
            }
        }
        .frame(minHeight: 24)
    }

    // MARK: Feed

    @ViewBuilder private var feed: some View {
        switch posts {
        case .loading:
            LoadingStateView()
        case .failed(let message):
            ErrorStateView(message: message) { Task { await loadPosts(showLoading: true) } }
        case .loaded(let list) where list.isEmpty:
            Text("Nothing here yet. Try a wider area.")
                .socialText(15)
                .foregroundStyle(Theme.text3)
                .multilineTextAlignment(.center)
                .padding(20)
                .frame(maxWidth: .infinity)
        case .loaded(let list):
            VStack(spacing: 12) {
                ForEach(list) { post in
                    ForumPostCard(post: post, isBusy: busyPostIds.contains(post.id), error: postErrors[post.id]) {
                        act(on: post)
                    }
                }
            }
        }
    }

    // MARK: Loading

    private func loadPosts(showLoading: Bool = false) async {
        if showLoading || posts.value == nil { posts = .loading }
        let request = query
        let result = await Loadable.run { try await env.api.forumPosts(request) }
        guard !Task.isCancelled, request == query else { return }
        withAnimation(animation) { posts = result }
    }

    private func loadMyPost() async {
        myPost = .loading
        myPost = await Loadable.run { try await env.api.myFreePost() }
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

    /// `forum.update` from the socket refreshes the feed in place.
    private func listenForUpdates() async {
        for await event in env.realtime.subscribe() {
            guard case .forumUpdate = event else { continue }
            let request = query
            if let fresh = try? await env.api.forumPosts(request), request == query {
                posts = .loaded(fresh)
            }
        }
    }

    // MARK: Actions

    private var animation: Animation? { reduceMotion ? nil : .easeInOut(duration: 0.2) }

    private func setQuery(_ newQuery: ForumQuery) {
        query = newQuery
        pendingQuery = newQuery
    }

    private func postFree(_ visibility: ForumPostVisibility) {
        statusBusy = true
        statusError = nil
        Task {
            do {
                let post = try await env.api.postFreeNow(visibility: visibility)
                withAnimation(animation) { myPost = .loaded(post) }
            } catch {
                statusError = error.socialMessage
            }
            statusBusy = false
        }
    }

    private func takeDown(_ post: MyFreePost) {
        statusBusy = true
        statusError = nil
        Task {
            do {
                try await env.api.deleteForumPost(id: post.id)
                withAnimation(animation) { myPost = .loaded(nil) }
            } catch {
                statusError = error.socialMessage
            }
            statusBusy = false
        }
    }

    private func act(on post: ForumPost) {
        switch post.type {
        case .plan:
            if post.joinRequested {
                run(post) { try await env.api.cancelJoinRequest(postId: post.id); update(post.id) { $0.joinRequested = false } }
            } else {
                run(post) { try await env.api.requestToJoin(postId: post.id); update(post.id) { $0.joinRequested = true } }
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
                    router.openThread(threadId)
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

    /// Runs a card action with the button disabled; failures show under the card's button.
    private func run(_ post: ForumPost, _ work: @escaping () async throws -> Void) {
        guard !busyPostIds.contains(post.id) else { return }
        busyPostIds.insert(post.id)
        postErrors[post.id] = nil
        Task {
            do {
                try await work()
            } catch {
                postErrors[post.id] = error.socialMessage
            }
            busyPostIds.remove(post.id)
        }
    }

    private func update(_ postId: String, _ change: (inout ForumPost) -> Void) {
        guard var list = posts.value, let index = list.firstIndex(where: { $0.id == postId }) else { return }
        change(&list[index])
        withAnimation(animation) { posts = .loaded(list) }
    }
}

#Preview {
    ForumView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

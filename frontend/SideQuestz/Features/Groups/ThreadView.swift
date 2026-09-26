import SwiftUI

/// A group chat or DM, shown by `MainShell` over the tabs when `router.openThread` is set
/// (GUI_PLAN.md §7.9). Groups get Chat | Album | Splits; DMs are chat only. Back closes it.
///
/// Launch routes: `thread/<id>/chat|album|splits` (starts on `route.tab`), `thread/g1/splits/expense`
/// (also opens Add expense; `-SQExpenseDemo YES` prefills it), `thread/dm-maya`.
struct ThreadView: View {
    let route: ThreadRoute

    var body: some View {
        ThreadScreen(route: route)
            .id(route.threadId)
    }
}

private struct ThreadScreen: View {
    let route: ThreadRoute

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var thread: Loadable<ChatThread> = .loading
    @State private var tab: ThreadTab
    /// Tabs opened so far stay alive, so switching back keeps the draft, scroll and loaded data.
    @State private var openedTabs: [ThreadTab]
    @State private var draft = ""
    /// `thread/…/splits/expense`: Splits opens Add expense once its ledger loads.
    @State private var openExpenseOnLoad = false

    init(route: ThreadRoute) {
        self.route = route
        _tab = State(initialValue: route.tab)
        _openedTabs = State(initialValue: [route.tab])
    }

    /// Before the thread loads we go by the id, like the router does for launch routes.
    private var isGroup: Bool { thread.value?.isGroup ?? !route.threadId.hasPrefix("dm-") }
    private var visibleTab: ThreadTab { isGroup ? tab : .chat }

    var body: some View {
        VStack(spacing: 0) {
            header
            Rectangle().fill(Theme.line).frame(height: 1)
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        }
        .background(Theme.cream.ignoresSafeArea())
        .task {
            if let parts = router.consumeLaunch("thread"), parts.dropFirst().contains("expense") {
                tab = .splits
                openedTabs = [.splits]
                openExpenseOnLoad = true
            }
            await loadThread()
        }
        .onChange(of: tab) { _, newTab in
            if !openedTabs.contains(newTab) { openedTabs.append(newTab) }
        }
    }

    // MARK: Header

    private var header: some View {
        VStack(spacing: 8) {
            HStack(spacing: 0) {
                backButton
                    .frame(width: 80, alignment: .leading)
                VStack(spacing: 0) {
                    Text(thread.value?.title ?? "")
                        .socialText(16, .semibold)
                        .foregroundStyle(Theme.ink)
                    Text(thread.value?.subtitle ?? "")
                        .socialText(12)
                        .foregroundStyle(Theme.text3)
                }
                .lineLimit(1)
                .frame(maxWidth: .infinity)
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
                Color.clear.frame(width: 80, height: 1)
            }
            .frame(height: 44)

            if isGroup {
                SQSegmentedControl(selection: $tab,
                                   options: ThreadTab.allCases.map { (value: $0, label: $0.label) },
                                   height: 32,
                                   accessibilityLabel: "Group sections")
                    .padding(.horizontal, 12)
                    .padding(.bottom, 4)
            }
        }
        .padding(.horizontal, 8)
        .designTopPadding(50, minimum: 0)
        .padding(.bottom, 8)
        .background(Color.white.ignoresSafeArea(edges: .top))
    }

    private var backButton: some View {
        Button {
            router.closeThread()
        } label: {
            HStack(spacing: 2) {
                SocialGlyph(kind: .chevronLeft, size: 20, lineWidth: 2.4)
                Text("Back").sqFont(17).lineLimit(1).minimumScaleFactor(0.6)
            }
            .foregroundStyle(Theme.sageInk)
            .padding(.horizontal, 4)
            .frame(height: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Back")
    }

    // MARK: Content

    @ViewBuilder private var content: some View {
        switch thread {
        case .loading:
            LoadingStateView()
        case .failed(let message):
            ErrorStateView(message: message) { Task { await loadThread() } }
        case .loaded(let loaded):
            ZStack {
                ForEach(isGroup ? openedTabs : [.chat]) { item in
                    let active = item == visibleTab
                    tabContent(item, thread: loaded, active: active)
                        .opacity(active ? 1 : 0)
                        .allowsHitTesting(active)
                        .accessibilityHidden(!active)
                        .zIndex(active ? 1 : 0)
                }
            }
        }
    }

    @ViewBuilder
    private func tabContent(_ item: ThreadTab, thread: ChatThread, active: Bool) -> some View {
        switch item {
        case .chat:
            ThreadChatView(thread: thread, draft: $draft, isActive: active)
        case .album:
            GroupAlbumView(thread: thread) { await refreshThread() }
        case .splits:
            GroupSplitsView(thread: thread, openExpenseOnLoad: $openExpenseOnLoad)
        }
    }

    // MARK: Loading

    private func loadThread() async {
        thread = .loading
        thread = await Loadable.run { try await env.api.thread(id: route.threadId) }
    }

    /// Picks up server-side changes (album subtitle after uploads) without a loader.
    private func refreshThread() async {
        if let fresh = try? await env.api.thread(id: route.threadId) { thread = .loaded(fresh) }
    }
}

#Preview("Group") {
    ThreadView(route: ThreadRoute(threadId: "g1", tab: .chat))
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

#Preview("DM") {
    ThreadView(route: ThreadRoute(threadId: "dm-maya"))
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

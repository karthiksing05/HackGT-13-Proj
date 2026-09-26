import SwiftUI

/// Groups tab (GUI_PLAN.md §7.9): one card of group rows (faces, name, time, last message, chips).
/// A row opens the thread (chat / album / splits) over the tabs.
///
/// Loading: shimmering rows shaped like the real ones, then the groups arrive one after another.
/// Later reloads (a new message, coming back from a thread, pull to refresh) keep the list and
/// animate what changed: the last message slides up, times and chips roll, rows move.
struct GroupsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var groups: Loadable<[ChatThread]> = .loading
    /// Groups from the load that replaced the skeleton: only these arrive one after another.
    @State private var arrivingIds: Set<String> = []

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                TabScreenHeader(eyebrow: "CHATS, ALBUMS, SPLITS", title: "Groups")
                content
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 12)
                Text("A group is created when someone joins your plan. Each one has a chat, a shared photo album and a running tab of who owes whom.")
                    .socialText(13)
                    .foregroundStyle(Theme.text3)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 14)
                    .padding(.bottom, 24)
            }
        }
        .scrollIndicators(.hidden)
        .sqPullToRefresh()
        .background(Theme.cream.ignoresSafeArea())
        .task { await load() }
        .task { await listenForMessages() }
        .sqReloadable("groups") { await load() }
        // Coming back from a thread: last message, balance and photo chips may have changed.
        .onChange(of: router.openThread == nil) { _, closed in
            if closed { Task { await load() } }
        }
    }

    private var content: some View {
        ZStack(alignment: .top) {
            switch groups {
            case .loading:
                GroupsSkeleton()
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message) { Task { await load(showLoading: true) } }
                    .transition(.opacity)
            case .loaded(let list) where list.isEmpty:
                EmptyStateView(message: "No groups yet.")
                    .transition(.opacity)
            case .loaded(let list):
                groupCard(list)
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: groups.phase)
    }

    private func groupCard(_ list: [ChatThread]) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(list.enumerated()), id: \.element.id) { index, thread in
                VStack(spacing: 0) {
                    Button {
                        router.openThread(thread.id)
                    } label: {
                        GroupRow(thread: thread)
                    }
                    .buttonStyle(.sqPressable)
                    .accessibilityHint("Opens the chat, album and splits")
                    RowDivider()
                }
                .socialArrival(index, staggered: arrivingIds.contains(thread.id))
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    // MARK: Loading

    /// Reloads quietly when there's already a list (no skeleton flash), unless asked; a failed quiet
    /// reload keeps the list on screen.
    private func load(showLoading: Bool = false) async {
        if showLoading || groups.value == nil, !groups.isLoading { withMotion { groups = .loading } }
        let result = await Loadable.run { try await env.api.threads().filter(\.isGroup) }
        if case .failed = result, groups.value != nil, !showLoading { return }
        let fromSkeleton = groups.value == nil
        arrivingIds = fromSkeleton ? Set(result.value?.map(\.id) ?? []) : []
        withMotion(fromSkeleton ? Motion.standard : Motion.gentle) { groups = result }
    }

    private func listenForMessages() async {
        for await event in env.realtime.subscribe() {
            if case .messageNew = event { await load() }
        }
    }
}

/// One group row: 2-avatar overlap, name + time, last message, chips.
private struct GroupRow: View {
    let thread: ChatThread
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var faces: [PersonRef] {
        thread.faces.isEmpty ? Array(thread.members.prefix(2)) : Array(thread.faces.prefix(2))
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            ZStack(alignment: .topLeading) {
                if let first = faces.first {
                    Avatar(person: first, size: 32, fontSize: 12)
                }
                if faces.count > 1 {
                    // 32pt with a 2pt white border (CSS border-box): white disc + 28pt avatar.
                    Circle()
                        .fill(.white)
                        .frame(width: 32, height: 32)
                        .overlay { Avatar(person: faces[1], size: 28, fontSize: 12) }
                        .offset(x: 16, y: 12)
                }
            }
            .frame(width: 48, height: 44, alignment: .topLeading)
            .accessibilityHidden(true)

            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .top, spacing: 8) {
                    Text(thread.title)
                        .socialText(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    Text(thread.lastTime)
                        .socialText(12)
                        .foregroundStyle(Theme.text3)
                        .fixedSize()
                        .sqNumeric()
                }
                // A new last message slides up into place.
                ZStack(alignment: .leading) {
                    Text(thread.lastMessage)
                        .socialText(14)
                        .foregroundStyle(Theme.text2)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .id(thread.lastMessage)
                        .transition(reduceMotion ? .opacity : .push(from: .bottom))
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .clipped()
                if !thread.chips.isEmpty {
                    // One row like the prototype; wraps if a server chip ("You're owed $17.66") runs long.
                    FlowLayout(spacing: 6) {
                        ForEach(Array(thread.chips.enumerated()), id: \.offset) { _, chip in
                            SocialTag(text: chip).sqNumeric()
                        }
                    }
                    .padding(.top, 4)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

/// First-load placeholder: the white card of group rows with the two-face stack, name and time,
/// last message and chips, shimmering. Reads "Loading".
private struct GroupsSkeleton: View {
    private let rows: [(name: CGFloat, message: CGFloat, chips: [CGFloat])] = [
        (150, 230, [66, 82, 70]), (170, 110, [74, 64]), (104, 120, [66, 80]),
    ]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(rows.indices, id: \.self) { index in
                row(rows[index])
                RowDivider()
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }

    private func row(_ shape: (name: CGFloat, message: CGFloat, chips: [CGFloat])) -> some View {
        HStack(alignment: .top, spacing: 12) {
            ZStack(alignment: .topLeading) {
                Circle().fill(Theme.skeleton).frame(width: 32, height: 32)
                Circle()
                    .fill(Theme.skeleton)
                    .frame(width: 28, height: 28)
                    .padding(2)
                    .background(.white, in: Circle())
                    .offset(x: 16, y: 12)
            }
            .frame(width: 48, height: 44, alignment: .topLeading)
            VStack(alignment: .leading, spacing: 9) {
                HStack(spacing: 8) {
                    SkeletonBlock(width: shape.name, height: 14)
                    Spacer(minLength: 0)
                    SkeletonBlock(width: 46, height: 10)
                }
                .padding(.top, 3)
                SkeletonBlock(width: shape.message, height: 12)
                HStack(spacing: 6) {
                    ForEach(shape.chips.indices, id: \.self) { chip in
                        SkeletonBlock(width: shape.chips[chip], height: 22, radius: 8)
                    }
                }
                .padding(.top, 3)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

#Preview {
    GroupsView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

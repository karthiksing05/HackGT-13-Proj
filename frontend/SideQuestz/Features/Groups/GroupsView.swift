import SwiftUI

/// Groups tab (GUI_PLAN.md §7.9): the group chats (faces, name, time, last message, chips), then
/// your direct messages under "MESSAGES". Both come from `GET /threads` (most recent first, with
/// unread counts); a row with unread messages shows a count badge. A row opens the thread over the
/// tabs (Chat | Album | Splits for groups, the chat for DMs).
///
/// Loading: shimmering rows shaped like the real ones, then the rows arrive one after another.
/// Later reloads (a new message, coming back from a thread, pull to refresh) keep the lists and
/// animate what changed: the last message slides up, times, chips and badges roll, rows move.
/// Realtime: `thread.updated` updates a row in place (a thread with news moves to the top of its
/// list, a new one arrives there), `thread.read` clears a badge, `message.new` reloads quietly.
struct GroupsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var threads: Loadable<[ChatThread]> = .loading
    /// Rows from the load that replaced the skeleton: only these arrive one after another.
    @State private var arrivingIds: Set<String> = []
    /// Bumped by every load; only the newest one's result is shown.
    @State private var loadGeneration = 0

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                TabScreenHeader(title: "Groups")
                content
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 12)
                    .padding(.bottom, 24)
            }
        }
        .scrollIndicators(.hidden)
        .sqPullToRefresh()
        .background(Theme.cream.ignoresSafeArea())
        .task { await load() }
        .task { await listenForUpdates() }
        .sqReloadable("groups") { await load() }
        // Coming back from a thread: last message, unread, balance and photo chips may have changed.
        .onChange(of: router.openThread == nil) { _, closed in
            if closed { Task { await load() } }
        }
    }

    @ViewBuilder private var content: some View {
        ZStack(alignment: .top) {
            switch threads {
            case .loading:
                VStack(alignment: .leading, spacing: 0) {
                    GroupsSkeleton()
                    MessagesSkeleton()
                }
                .sqSlowLoading(lines: ["Loading your groups…"])
                .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message) { Task { await load(showLoading: true) } }
                    .transition(.opacity)
            case .loaded(let list):
                lists(list)
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: threads.phase)
    }

    private func lists(_ list: [ChatThread]) -> some View {
        let groups = list.filter(\.isGroup)
        let directs = list.filter { !$0.isGroup }
        return VStack(alignment: .leading, spacing: 0) {
            if groups.isEmpty {
                EmptyStateView(message: "No groups yet.", minHeight: directs.isEmpty ? 120 : 64)
                    .transition(.opacity)
            } else {
                card(groups, indexOffset: 0) { GroupRow(thread: $0) }
                    .transition(.opacity)
            }
            if !directs.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    SocialSectionLabel(text: "MESSAGES")
                    // DMs arrive right after the groups.
                    card(directs, indexOffset: groups.count) { DirectMessageRow(thread: $0, meId: env.user?.id) }
                }
                .padding(.top, 20)
                .sqTransition(.rise)
            }
        }
        .animation(Motion.standard, value: directs.isEmpty)
    }

    /// One white card of thread rows; a row opens its thread.
    private func card<Row: View>(_ list: [ChatThread], indexOffset: Int,
                                 @ViewBuilder row: @escaping (ChatThread) -> Row) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(list.enumerated()), id: \.element.id) { index, thread in
                VStack(spacing: 0) {
                    Button {
                        open(thread)
                    } label: {
                        row(thread)
                    }
                    .buttonStyle(.sqPressable)
                    .accessibilityHint(thread.isGroup ? "Opens the chat, album and splits" : "Opens the conversation")
                    if index < list.count - 1 || thread.isGroup { RowDivider() }
                }
                .socialArrival(indexOffset + index, staggered: arrivingIds.contains(thread.id))
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    // MARK: Actions

    /// Opens the thread (the header knows right away whether it's a group) and clears its badge
    /// here; the thread marks itself read on the server once it's on screen.
    private func open(_ thread: ChatThread) {
        router.openThread(thread.id, isGroup: thread.isGroup)
        guard thread.unread > 0 else { return }
        update(thread.id) { $0.unread = 0 }
    }

    // MARK: Loading

    /// Reloads quietly when there's already a list (no skeleton flash), unless asked; a failed quiet
    /// reload keeps the lists on screen.
    private func load(showLoading: Bool = false) async {
        loadGeneration += 1
        let generation = loadGeneration
        if showLoading || threads.value == nil, !threads.isLoading { withMotion { threads = .loading } }
        var result = await Loadable.run { try await env.api.threads() }
        guard generation == loadGeneration else { return }
        if case .failed = result, threads.value != nil, !showLoading { return }
        // The thread that's open right now is being read.
        if var list = result.value, let open = router.openThread?.threadId,
           let index = list.firstIndex(where: { $0.id == open }) {
            list[index].unread = 0
            result = .loaded(list)
        }
        let fromSkeleton = threads.value == nil
        arrivingIds = fromSkeleton ? Set(result.value?.map(\.id) ?? []) : []
        withMotion(fromSkeleton ? Motion.standard : Motion.gentle) { threads = result }
    }

    private func listenForUpdates() async {
        for await event in env.realtime.subscribe() {
            switch event {
            case .threadUpdated(let thread):
                apply(thread)
            case .threadRead(let threadId):
                update(threadId) { $0.unread = 0 }
            case .messageNew:
                Task { await load() }
            default:
                break
            }
        }
    }

    /// A thread the server says changed: updated in place, or moved to the top of its list when it
    /// has news (a new one arrives there).
    private func apply(_ thread: ChatThread) {
        guard var list = threads.value else { return }
        var fresh = thread
        if router.openThread?.threadId == thread.id { fresh.unread = 0 }
        if let index = list.firstIndex(where: { $0.id == thread.id }) {
            let old = list[index]
            let hasNews = old.lastMessage != fresh.lastMessage || old.lastTime != fresh.lastTime
            list.remove(at: index)
            list.insert(fresh, at: hasNews ? 0 : index)
        } else {
            list.insert(fresh, at: 0)
        }
        withMotion(Motion.arrive) { threads = .loaded(list) }
    }

    private func update(_ threadId: String, _ change: (inout ChatThread) -> Void) {
        guard var list = threads.value, let index = list.firstIndex(where: { $0.id == threadId }) else { return }
        change(&list[index])
        withMotion(Motion.quick) { threads = .loaded(list) }
    }
}

/// One group row: 2-avatar overlap, name + time, last message (+ unread badge), chips.
private struct GroupRow: View {
    let thread: ChatThread

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
                ThreadRowHeadline(title: thread.title, time: thread.lastTime, unread: thread.unread)
                ThreadRowPreview(text: thread.lastMessage, unread: thread.unread)
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

/// One direct message: the friend's avatar, their name + time, the last message (+ unread badge).
private struct DirectMessageRow: View {
    let thread: ChatThread
    let meId: String?

    private var person: PersonRef? {
        thread.faces.first ?? thread.members.first { $0.id != meId } ?? thread.members.first
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Group {
                if let person {
                    Avatar(person: person, size: 40, fontSize: 14)
                } else {
                    Circle().fill(Theme.skeleton).frame(width: 40, height: 40)
                }
            }
            // Same text column as the group rows above.
            .frame(width: 48, alignment: .leading)
            .accessibilityHidden(true)

            VStack(alignment: .leading, spacing: 3) {
                ThreadRowHeadline(title: thread.title, time: thread.lastTime, unread: thread.unread)
                ThreadRowPreview(text: thread.lastMessage, unread: thread.unread)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

/// Name + time; the time turns `sageInk` while there's something unread.
private struct ThreadRowHeadline: View {
    let title: String
    let time: String
    let unread: Int

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(title)
                .socialText(15, .semibold)
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
            Spacer(minLength: 0)
            Text(time)
                .socialText(12, unread > 0 ? .semibold : .regular)
                .foregroundStyle(unread > 0 ? Theme.sageInk : Theme.text3)
                .fixedSize()
                .sqNumeric()
        }
    }
}

/// The last message (a new one slides up into place) and, when there's something unread, the
/// count badge (pops in, rolls, fades away once read).
private struct ThreadRowPreview: View {
    let text: String
    let unread: Int
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(alignment: .center, spacing: 8) {
            ZStack(alignment: .leading) {
                Text(text)
                    .socialText(14)
                    .foregroundStyle(unread > 0 ? Theme.ink : Theme.text2)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .id(text)
                    .transition(reduceMotion ? .opacity : .push(from: .bottom))
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .clipped()
            if unread > 0 {
                SocialUnreadBadge(count: unread)
                    .sqTransition(.pop)
            }
        }
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

/// First-load placeholder for "MESSAGES": the label and two DM rows (avatar, name, last message).
private struct MessagesSkeleton: View {
    private let rows: [(name: CGFloat, message: CGFloat)] = [(96, 170), (80, 136)]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            SkeletonBlock(width: 84, height: 11, color: Theme.skeletonOnCream)
                .padding(.vertical, 3.5)
            VStack(spacing: 0) {
                ForEach(rows.indices, id: \.self) { index in
                    HStack(alignment: .top, spacing: 12) {
                        Circle().fill(Theme.skeleton).frame(width: 40, height: 40)
                            .frame(width: 48, alignment: .leading)
                        VStack(alignment: .leading, spacing: 9) {
                            HStack(spacing: 8) {
                                SkeletonBlock(width: rows[index].name, height: 14)
                                Spacer(minLength: 0)
                                SkeletonBlock(width: 40, height: 10)
                            }
                            .padding(.top, 3)
                            SkeletonBlock(width: rows[index].message, height: 12)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .padding(14)
                    if index < rows.count - 1 { RowDivider() }
                }
            }
            .background(.white)
            .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        }
        .padding(.top, 20)
        .sqShimmer()
        .accessibilityHidden(true)
    }
}

#Preview {
    GroupsView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

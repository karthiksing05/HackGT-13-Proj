import SwiftUI

/// Groups tab (GUI_PLAN.md §7.9): one card of group rows (faces, name, time, last message, chips).
/// A row opens the thread (chat / album / splits) over the tabs.
struct GroupsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var groups: Loadable<[ChatThread]> = .loading

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                TabScreenHeader(eyebrow: "CHATS, ALBUMS, SPLITS", title: "Groups")
                LoadableView(state: groups, retry: { Task { await load(showLoading: true) } }) { list in
                    if list.isEmpty {
                        EmptyStateView(message: "No groups yet.")
                    } else {
                        groupCard(list)
                    }
                }
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
        .background(Theme.cream.ignoresSafeArea())
        .task { await load() }
        .task { await listenForMessages() }
        // Coming back from a thread: last message, balance and photo chips may have changed.
        .onChange(of: router.openThread == nil) { _, closed in
            if closed { Task { await load() } }
        }
    }

    private func groupCard(_ list: [ChatThread]) -> some View {
        VStack(spacing: 0) {
            ForEach(list) { thread in
                Button {
                    router.openThread(thread.id)
                } label: {
                    GroupRow(thread: thread)
                }
                .buttonStyle(.sqPressable)
                .accessibilityHint("Opens the chat, album and splits")
                RowDivider()
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    // MARK: Loading

    /// Reloads quietly when there's already a list (no loader flash), unless asked.
    private func load(showLoading: Bool = false) async {
        if showLoading || groups.value == nil { groups = .loading }
        let result = await Loadable.run { try await env.api.threads().filter(\.isGroup) }
        if case .failed = result, groups.value != nil, !showLoading { return }
        groups = result
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
                }
                Text(thread.lastMessage)
                    .socialText(14)
                    .foregroundStyle(Theme.text2)
                    .lineLimit(1)
                    .truncationMode(.tail)
                if !thread.chips.isEmpty {
                    // One row like the prototype; wraps if a server chip ("You're owed $17.66") runs long.
                    FlowLayout(spacing: 6) {
                        ForEach(thread.chips, id: \.self) { SocialTag(text: $0) }
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

#Preview {
    GroupsView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

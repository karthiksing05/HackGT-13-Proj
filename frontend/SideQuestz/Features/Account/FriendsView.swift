import SwiftUI
import UIKit

/// Account › Friends: find people + invite, friend requests, your friends (tap to message).
struct FriendsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var friends: Loadable<[Friend]> = .loading
    @State private var requests: Loadable<[FriendRequest]> = .loading
    @State private var query = ""
    /// nil while the search box is empty.
    @State private var results: Loadable<[PersonRef]>?
    @State private var requested: Set<String> = []
    @State private var busyId: String?
    @State private var inviting = false
    @State private var invite: AccountInviteLink?
    @State private var actionError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            searchRow
                .padding(.top, 14)
                .padding(.horizontal, Metrics.side)
            if let actionError {
                AuthErrorText(message: actionError)
                    .padding(.top, 8)
                    .padding(.horizontal, Metrics.side)
            }
            if let results {
                eyebrow("RESULTS")
                searchResults(results)
            }
            requestsSection
            eyebrow(friendsTitle)
            friendsCard
                .padding(.bottom, 28)
        }
        .task(id: router.tab == .account) {
            guard router.tab == .account else { return }
            await reload()
        }
        .task(id: query) { await search() }
        .sheet(item: $invite) { link in
            AccountShareSheet(items: [link.url])
                .presentationDetents([.medium, .large])
                .ignoresSafeArea()
        }
    }

    // MARK: Search + invite

    private var searchRow: some View {
        HStack(spacing: 8) {
            SearchField(text: $query, placeholder: "Name or @handle", height: 40, accessibilityLabel: "Find friends")
                .submitLabel(.search)
            Button(action: createInvite) {
                ZStack {
                    Text("Invite").opacity(inviting ? 0 : 1)
                    if inviting { ProgressView().controlSize(.small).tint(Theme.ink) }
                }
                .sqFont(14, .semibold)
                .foregroundStyle(Theme.ink)
                .padding(.horizontal, 14)
                .frame(height: 40)
                .background(Theme.sage, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .frame(minHeight: Metrics.minTouch)
                .contentShape(Rectangle())
            }
            .buttonStyle(.sqPressable)
            .authHitHeight(40)
            .disabled(inviting)
            .accessibilityLabel("Invite")
            .accessibilityHint("Share a link to add you on SideQuestz")
        }
    }

    @ViewBuilder private func searchResults(_ state: Loadable<[PersonRef]>) -> some View {
        SetupCard {
            LoadableView(state: state, minHeight: 88, retry: { Task { await search() } }) { people in
                if people.isEmpty {
                    EmptyStateView(message: "No one found. Try their name or @handle.", minHeight: 72)
                } else {
                    ForEach(Array(people.enumerated()), id: \.element.id) { index, person in
                        if index > 0 { RowDivider() }
                        resultRow(person)
                    }
                }
            }
        }
        .padding(.horizontal, Metrics.side)
    }

    private func resultRow(_ person: PersonRef) -> some View {
        let isFriend = friends.value?.contains { $0.person.id == person.id } ?? false
        return HStack(spacing: 12) {
            Avatar(person: person, size: 40, fontSize: 14)
                .accessibilityHidden(true)
            Text(person.name)
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
            if isFriend {
                messageButton(person)
            } else if requested.contains(person.id) {
                Text("Requested")
                    .sqFont(14, .semibold)
                    .foregroundStyle(Theme.success)
                    .padding(.horizontal, 12)
                    .frame(height: 34)
                    .background(Theme.successBg, in: Capsule())
            } else {
                Button {
                    sendRequest(to: person)
                } label: {
                    ZStack {
                        Text("Add").opacity(busyId == person.id ? 0 : 1)
                        if busyId == person.id { ProgressView().controlSize(.small).tint(Theme.ink) }
                    }
                }
                .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, horizontalPadding: 12))
                .authHitHeight(34)
                .disabled(busyId != nil)
                .accessibilityLabel("Add \(person.name)")
            }
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }

    // MARK: Requests

    @ViewBuilder private var requestsSection: some View {
        switch requests {
        case .loaded(let list) where !list.isEmpty:
            eyebrow("REQUESTS")
            SetupCard {
                ForEach(Array(list.enumerated()), id: \.element.id) { index, request in
                    if index > 0 { RowDivider() }
                    requestRow(request)
                }
            }
            .padding(.horizontal, Metrics.side)
        case .failed(let message):
            eyebrow("REQUESTS")
            SetupCard {
                ErrorStateView(message: message, minHeight: 96) { Task { await reload() } }
            }
            .padding(.horizontal, Metrics.side)
        default:
            EmptyView()
        }
    }

    private func requestRow(_ request: FriendRequest) -> some View {
        HStack(spacing: 12) {
            Avatar(person: request.person, size: 40, fontSize: 14)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text(request.person.name)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Text(request.note)
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            Button {
                accept(request)
            } label: {
                ZStack {
                    Text("Accept").opacity(busyId == request.id ? 0 : 1)
                    if busyId == request.id { ProgressView().controlSize(.small).tint(Theme.ink) }
                }
            }
            .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, horizontalPadding: 12))
            .authHitHeight(34)
            .disabled(busyId != nil)
            .accessibilityLabel("Accept \(request.person.name)")
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }

    // MARK: Friends

    private var friendsTitle: String {
        guard let count = friends.value?.count else { return "FRIENDS" }
        return count == 1 ? "1 FRIEND" : "\(count) FRIENDS"
    }

    private var friendsCard: some View {
        SetupCard {
            LoadableView(state: friends, minHeight: 120, retry: { Task { await reload() } }) { list in
                if list.isEmpty {
                    EmptyStateView(message: "No friends yet. Tap Invite to send someone your link.", minHeight: 96)
                } else {
                    ForEach(Array(list.enumerated()), id: \.element.id) { index, friend in
                        if index > 0 { RowDivider() }
                        friendRow(friend)
                    }
                }
            }
        }
        .padding(.horizontal, Metrics.side)
    }

    private func friendRow(_ friend: Friend) -> some View {
        HStack(spacing: 12) {
            Avatar(initials: friend.person.initials, fill: friend.person.color, size: 40, fontSize: 14,
                   statusColor: friend.activity.dotColor, statusSize: 12, statusBorder: .white, statusBorderWidth: 2,
                   statusOffset: 1)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text(friend.person.name)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Text(friend.statusLine)
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            messageButton(friend.person)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }

    /// 40pt `sageTint` circle with a sageInk bubble → DM thread.
    private func messageButton(_ person: PersonRef) -> some View {
        Button {
            message(person)
        } label: {
            ZStack {
                if busyId == person.id {
                    ProgressView().controlSize(.small).tint(Theme.sageInk)
                } else {
                    Image(systemName: "bubble.left")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(Theme.sageInk)
                }
            }
            .frame(width: 40, height: 40)
            .background(Theme.sageTint, in: Circle())
            .frame(width: Metrics.minTouch, height: Metrics.minTouch)
            .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .padding(-2)
        .disabled(busyId != nil)
        .accessibilityLabel("Message \(person.name)")
    }

    private func eyebrow(_ text: String) -> some View {
        SetupEyebrow(text: text)
            .padding(.top, 20)
            .padding(.bottom, 8)
            .padding(.horizontal, Metrics.side)
    }

    // MARK: Actions

    private func reload() async {
        async let friendsResult = Loadable.run { try await env.api.friends() }
        async let requestsResult = Loadable.run { try await env.api.friendRequests() }
        let (f, r) = await (friendsResult, requestsResult)
        // Keep what's on screen if a background refresh fails.
        if f.value != nil || friends.value == nil { friends = f }
        if r.value != nil || requests.value == nil { requests = r }
    }

    private func search() async {
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else {
            results = nil
            return
        }
        do {
            try await Task.sleep(nanoseconds: 300_000_000) // debounce typing
        } catch {
            return
        }
        if results == nil { results = .loading }
        let found = await Loadable.run { try await env.api.searchUsers(query: trimmed) }
        guard !Task.isCancelled else { return }
        results = found.value.map { .loaded($0.filter { $0.id != env.user?.id }) } ?? found
    }

    private func accept(_ request: FriendRequest) {
        busyId = request.id
        actionError = nil
        Task {
            defer { busyId = nil }
            do {
                try await env.api.acceptFriendRequest(id: request.id)
                await reload()
            } catch {
                actionError = authMessage(for: error, fallback: "Couldn't accept the request. Try again.")
            }
        }
    }

    private func sendRequest(to person: PersonRef) {
        busyId = person.id
        actionError = nil
        Task {
            defer { busyId = nil }
            do {
                try await env.api.sendFriendRequest(userId: person.id)
                requested.insert(person.id)
            } catch {
                actionError = authMessage(for: error, fallback: "Couldn't send the request. Try again.")
            }
        }
    }

    private func message(_ person: PersonRef) {
        busyId = person.id
        actionError = nil
        Task {
            defer { busyId = nil }
            do {
                let thread = try await env.api.startDM(userId: person.id)
                router.openThread(thread.id)
            } catch {
                actionError = authMessage(for: error, fallback: "Couldn't open the chat. Try again.")
            }
        }
    }

    private func createInvite() {
        inviting = true
        actionError = nil
        Task {
            defer { inviting = false }
            do {
                invite = AccountInviteLink(url: try await env.api.createInvite())
            } catch {
                actionError = authMessage(for: error, fallback: "Couldn't create an invite link. Try again.")
            }
        }
    }
}

/// An invite URL to hand to the share sheet.
struct AccountInviteLink: Identifiable {
    let url: URL
    var id: String { url.absoluteString }
}

/// System share sheet (`UIActivityViewController`).
struct AccountShareSheet: UIViewControllerRepresentable {
    let items: [Any]

    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: items, applicationActivities: nil)
    }

    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}

import Observation
import SwiftUI
import UIKit

/// Account › Friends data. Owned by `AccountView` so it survives Me ⇄ Friends switches.
@Observable
final class AccountFriendsModel {
    var friends: Loadable<[Friend]> = .loading
    var requests: Loadable<[FriendRequest]> = .loading
    /// People you sent a request to (shown as "Requested").
    var requested: Set<String> = []
    /// Requests accepted on this screen that the server hasn't confirmed yet: they already show in
    /// the friends list ("Adding…") and no longer under Requests.
    var accepting: [FriendRequest] = []

    /// Loads (or quietly reloads) friends and requests; keeps what's on screen if a refresh fails.
    func reload(_ env: AppEnvironment) async {
        async let friendsResult = Loadable.run { try await env.api.friends() }
        async let requestsResult = Loadable.run { try await env.api.friendRequests() }
        let (f, r) = await (friendsResult, requestsResult)
        withMotion(Motion.arrive) {
            if f.value != nil || friends.value == nil { friends = f }
            if var list = r.value {
                // Still being accepted here: keep it out of Requests.
                list.removeAll { request in accepting.contains { $0.id == request.id } }
                requests = .loaded(list)
            } else if requests.value == nil {
                requests = r
            }
            // Confirmed friends replace their "Adding…" rows.
            if let ids = friends.value.map({ Set($0.map(\.person.id)) }) {
                accepting.removeAll { ids.contains($0.person.id) }
            }
        }
    }
}

/// Account › Friends: find people + invite, friend requests, your friends (tap to message).
///
/// First load: skeleton rows, then the friends arrive one after another. Accepting a request is
/// optimistic (the request slides out, the person rises into the list as "Adding…" until the
/// server confirms, and goes back if it fails). Search results update in place, animated.
struct FriendsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    let model: AccountFriendsModel

    @State private var query = ""
    /// nil while the search box is empty.
    @State private var results: Loadable<[PersonRef]>?
    /// A newer search is running while the old results stay on screen.
    @State private var searching = false
    /// Friend requests waiting for the server (shown as a faint "Requested").
    @State private var sending: Set<String> = []
    /// The person whose DM is opening.
    @State private var messaging: String?
    @State private var inviting = false
    @State private var invite: AccountInviteLink?
    @State private var actionError: String?
    /// Rows arrive one after another when the list first loads while this view exists.
    @State private var animateRows: Bool

    init(model: AccountFriendsModel) {
        self.model = model
        _animateRows = State(initialValue: model.friends.value == nil)
    }

    /// Skeletons only shimmer while Account is the visible tab.
    private var onScreen: Bool { router.tab == .account }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            searchRow
                .padding(.top, 14)
                .padding(.horizontal, Metrics.side)
            if let actionError {
                AuthErrorText(message: actionError)
                    .padding(.top, 8)
                    .padding(.horizontal, Metrics.side)
                    .sqTransition(.rise)
            }
            if let results {
                VStack(alignment: .leading, spacing: 0) {
                    eyebrow("RESULTS")
                    searchResults(results)
                }
                .sqTransition(.rise)
            }
            requestsSection
            eyebrow(friendsTitle)
                .sqNumeric()
            friendsCard
                .padding(.bottom, 28)
        }
        .task(id: router.tab == .account) {
            guard router.tab == .account else { return }
            await model.reload(env)
        }
        .sqReloadable("account.friends") { await model.reload(env) }
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
                    if inviting {
                        LoadingDots(color: Theme.ink, dotSize: 5)
                            .sqTransition(.pop)
                    }
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
            .accessibilityValue(inviting ? "In progress" : "")
            .accessibilityHint("Share a link to add you on SideQuests")
        }
    }

    @ViewBuilder private func searchResults(_ state: Loadable<[PersonRef]>) -> some View {
        SetupCard {
            AuthLoadable(state: state, minHeight: 88, shimmers: onScreen, retry: { Task { await search() } }) {
                AccountPeopleSkeleton(count: 2, trailing: .pill)
            } content: { people in
                VStack(spacing: 0) {
                    if people.isEmpty {
                        EmptyStateView(message: "No one found. Try their name or @handle.", minHeight: 72)
                            .transition(.opacity)
                    } else {
                        ForEach(Array(people.enumerated()), id: \.element.id) { index, person in
                            if index > 0 { RowDivider() }
                            resultRow(person)
                                .sqTransition(.rise)
                        }
                    }
                }
            }
        }
        // A newer search keeps these on screen, dimmed, until its results swap in.
        .sqRefreshing(searching)
        .padding(.horizontal, Metrics.side)
    }

    private func resultRow(_ person: PersonRef) -> some View {
        let isFriend = model.friends.value?.contains { $0.person.id == person.id } ?? false
        return HStack(spacing: 12) {
            Avatar(person: person, size: 40, fontSize: 14)
                .accessibilityHidden(true)
            Text(person.name)
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
            ZStack(alignment: .trailing) {
                if isFriend {
                    messageButton(person)
                        .transition(.opacity)
                } else if model.requested.contains(person.id) {
                    requestedChip(pending: sending.contains(person.id))
                        .sqTransition(.pop)
                } else {
                    Button {
                        sendRequest(to: person)
                    } label: {
                        Text("Add")
                    }
                    .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, horizontalPadding: 12))
                    .authHitHeight(34)
                    .accessibilityLabel("Add \(person.name)")
                    .transition(.opacity)
                }
            }
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }

    /// "✓ Requested" — faint while the request is on its way, then the check draws in.
    private func requestedChip(pending: Bool) -> some View {
        HStack(spacing: 5) {
            if !pending {
                AnimatedCheck(lineWidth: 2.8, delay: 0.1)
                    .frame(width: 12, height: 12)
                    .transition(.opacity)
            }
            Text("Requested")
        }
        .sqFont(14, .semibold)
        .foregroundStyle(Theme.success)
        .padding(.horizontal, 12)
        .frame(height: 34)
        .background(Theme.successBg, in: Capsule())
        .opacity(pending ? 0.6 : 1)
        .accessibilityElement(children: .combine)
        .accessibilityValue(pending ? "Sending" : "")
    }

    // MARK: Requests

    @ViewBuilder private var requestsSection: some View {
        switch model.requests {
        case .loaded(let list) where !list.isEmpty:
            VStack(alignment: .leading, spacing: 0) {
                eyebrow("REQUESTS")
                SetupCard {
                    ForEach(Array(list.enumerated()), id: \.element.id) { index, request in
                        if index > 0 { RowDivider() }
                        requestRow(request)
                            .sqTransition(.rise)
                    }
                }
                .padding(.horizontal, Metrics.side)
            }
            .transition(.opacity)
        case .failed(let message):
            VStack(alignment: .leading, spacing: 0) {
                eyebrow("REQUESTS")
                SetupCard {
                    ErrorStateView(message: message, minHeight: 96) { Task { await model.reload(env) } }
                }
                .padding(.horizontal, Metrics.side)
            }
            .transition(.opacity)
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
                Text("Accept")
            }
            .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, horizontalPadding: 12))
            .authHitHeight(34)
            .accessibilityLabel("Accept \(request.person.name)")
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }

    // MARK: Friends

    /// Confirmed friends plus the ones being accepted right now (on top, like the server adds them).
    private var friendRows: [AccountFriendRow]? {
        guard let friends = model.friends.value else { return nil }
        let pending = model.accepting.filter { request in !friends.contains { $0.person.id == request.person.id } }
        return pending.map(AccountFriendRow.adding) + friends.map(AccountFriendRow.friend)
    }

    private var friendsTitle: String {
        guard let count = friendRows?.count else { return "FRIENDS" }
        return count == 1 ? "1 FRIEND" : "\(count) FRIENDS"
    }

    private var friendsCard: some View {
        SetupCard {
            AuthLoadable(state: model.friends, shimmers: onScreen, retry: { Task { await model.reload(env) } }) {
                AccountPeopleSkeleton(count: 4, trailing: .circle)
            } content: { _ in
                VStack(spacing: 0) {
                    let rows = friendRows ?? []
                    if rows.isEmpty {
                        EmptyStateView(message: "No friends yet. Tap Invite to send someone your link.", minHeight: 96)
                            .transition(.opacity)
                    } else {
                        ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                            if index > 0 { RowDivider() }
                            friendRow(row)
                                .authArrive(index, animated: animateRows)
                                .sqTransition(.rise)
                        }
                    }
                }
                .onAppear { animateRows = false }
            }
        }
        .padding(.horizontal, Metrics.side)
    }

    private func friendRow(_ row: AccountFriendRow) -> some View {
        let adding = row.isAdding
        return HStack(spacing: 12) {
            Avatar(initials: row.person.initials, fill: row.person.color, size: 40, fontSize: 14,
                   statusColor: row.dotColor, statusSize: 12, statusBorder: .white, statusBorderWidth: 2,
                   statusOffset: 1)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text(row.person.name)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Text(row.statusLine)
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            messageButton(row.person)
                .disabled(adding)
                .opacity(adding ? 0.45 : 1)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }

    /// 40pt `sageTint` circle with a sageInk bubble → DM thread (dots while it opens).
    private func messageButton(_ person: PersonRef) -> some View {
        let opening = messaging == person.id
        return Button {
            message(person)
        } label: {
            ZStack {
                if opening {
                    LoadingDots(color: Theme.sageInk, dotSize: 5)
                        .sqTransition(.pop)
                } else {
                    Image(systemName: "bubble.left")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(Theme.sageInk)
                        .transition(.opacity)
                }
            }
            .frame(width: 40, height: 40)
            .background(Theme.sageTint, in: Circle())
            .frame(width: Metrics.minTouch, height: Metrics.minTouch)
            .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .padding(-2)
        .disabled(messaging != nil)
        .authMotion(Motion.quick, value: opening)
        .accessibilityLabel("Message \(person.name)")
        .accessibilityValue(opening ? "Opening" : "")
    }

    private func eyebrow(_ text: String) -> some View {
        SetupEyebrow(text: text)
            .padding(.top, 20)
            .padding(.bottom, 8)
            .padding(.horizontal, Metrics.side)
    }

    // MARK: Actions

    private func search() async {
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else {
            withMotion {
                results = nil
                searching = false
            }
            return
        }
        do {
            try await Task.sleep(nanoseconds: 300_000_000) // debounce typing
        } catch {
            return
        }
        withMotion {
            if results == nil { results = .loading } else { searching = true }
        }
        let found = await Loadable.run { try await env.api.searchUsers(query: trimmed) }
        guard !Task.isCancelled else { return }
        // Rows that still match stay put, new ones rise in, the rest fade out.
        withMotion(Motion.arrive) {
            results = found.value.map { .loaded($0.filter { $0.id != env.user?.id }) } ?? found
            searching = false
        }
    }

    /// Optimistic: the request leaves Requests and the person joins your friends as "Adding…" right
    /// away; the server's list replaces it, or it all goes back with a note if accepting fails.
    private func accept(_ request: FriendRequest) {
        guard case .loaded(let list) = model.requests, let index = list.firstIndex(of: request) else { return }
        withMotion(Motion.arrive) {
            var remaining = list
            remaining.remove(at: index)
            model.requests = .loaded(remaining)
            model.accepting.insert(request, at: 0)
            actionError = nil
        }
        Task {
            do {
                try await env.api.acceptFriendRequest(id: request.id)
                await model.reload(env)
            } catch {
                withMotion(Motion.arrive) {
                    model.accepting.removeAll { $0.id == request.id }
                    if case .loaded(var current) = model.requests, !current.contains(request) {
                        current.insert(request, at: min(index, current.count))
                        model.requests = .loaded(current)
                    }
                    actionError = authMessage(for: error, fallback: "Couldn't accept the request. Try again.")
                }
            }
        }
    }

    /// Optimistic: "Requested" shows at once (faint until the server confirms, then its check draws).
    private func sendRequest(to person: PersonRef) {
        withMotion(Motion.quick) {
            model.requested.insert(person.id)
            sending.insert(person.id)
            actionError = nil
        }
        Task {
            do {
                try await env.api.sendFriendRequest(userId: person.id)
                withMotion(Motion.quick) { _ = sending.remove(person.id) }
            } catch {
                withMotion {
                    model.requested.remove(person.id)
                    sending.remove(person.id)
                    actionError = authMessage(for: error, fallback: "Couldn't send the request. Try again.")
                }
            }
        }
    }

    private func message(_ person: PersonRef) {
        withMotion(Motion.quick) {
            messaging = person.id
            actionError = nil
        }
        Task {
            do {
                let thread = try await env.api.startDM(userId: person.id)
                router.openThread(thread.id)
                messaging = nil
            } catch {
                withMotion {
                    messaging = nil
                    actionError = authMessage(for: error, fallback: "Couldn't open the chat. Try again.")
                }
            }
        }
    }

    private func createInvite() {
        withMotion(Motion.quick) {
            inviting = true
            actionError = nil
        }
        Task {
            do {
                let url = try await env.api.createInvite()
                withMotion(Motion.quick) { inviting = false }
                invite = AccountInviteLink(url: url)
            } catch {
                withMotion {
                    inviting = false
                    actionError = authMessage(for: error, fallback: "Couldn't create an invite link. Try again.")
                }
            }
        }
    }
}

/// A row of the friends list: a confirmed friend, or one being accepted right now.
private enum AccountFriendRow: Identifiable {
    case friend(Friend)
    case adding(FriendRequest)

    var id: String { person.id }

    var person: PersonRef {
        switch self {
        case .friend(let friend): friend.person
        case .adding(let request): request.person
        }
    }

    var statusLine: String {
        switch self {
        case .friend(let friend): friend.statusLine
        case .adding: "Adding…"
        }
    }

    var dotColor: Color? {
        switch self {
        case .friend(let friend): friend.activity.dotColor
        case .adding: nil
        }
    }

    var isAdding: Bool {
        if case .adding = self { return true }
        return false
    }
}

/// Rows shaped like the friends list (40pt avatar, name + status lines, a 40pt button) or the
/// search results (avatar, name, a pill).
private struct AccountPeopleSkeleton: View {
    enum Trailing { case circle, pill }

    let count: Int
    let trailing: Trailing
    private let widths: [(CGFloat, CGFloat)] = [(96, 132), (78, 176), (104, 118), (70, 150)]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<count, id: \.self) { index in
                if index > 0 { RowDivider() }
                HStack(spacing: 12) {
                    Circle().fill(Theme.skeleton).frame(width: 40, height: 40)
                    VStack(alignment: .leading, spacing: 8) {
                        SkeletonBlock(width: widths[index % widths.count].0, height: 13)
                        if trailing == .circle {
                            SkeletonBlock(width: widths[index % widths.count].1, height: 10)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    switch trailing {
                    case .circle:
                        Circle().fill(Theme.skeleton).frame(width: 40, height: 40)
                    case .pill:
                        SkeletonBlock(width: 58, height: 34, radius: 17)
                    }
                }
                .padding(.vertical, 12)
                .padding(.horizontal, 14)
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

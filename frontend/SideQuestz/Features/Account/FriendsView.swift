import Observation
import SwiftUI
import UIKit

/// Account › Friends data. Owned by `AccountView` so it survives Me ⇄ Friends switches, and kept
/// current by realtime events (`friend.request`, `friend.status`) while the app runs.
@Observable
final class AccountFriendsModel {
    var friends: Loadable<[Friend]> = .loading
    /// Requests people sent you (accept or decline).
    var requests: Loadable<[FriendRequest]> = .loading
    /// Requests you sent that are still open ("Requested", tap to withdraw): the ones the server
    /// lists as `outgoing`, plus ones sent from this screen that no list has shown yet.
    var sent: [FriendRequest] = []
    /// Sent from this screen and not in a server list yet. Once listed, the server's list decides
    /// (a request the other person declined then drops off).
    var sentUnlisted: Set<String> = []
    /// Requests accepted here that the server hasn't confirmed yet: they already show in the
    /// friends list ("Adding…") and no longer under Requests.
    var accepting: [FriendRequest] = []
    /// Requests declined or withdrawn here, until the server confirms (a reload meanwhile must not
    /// bring them back).
    var dropping: Set<String> = []
    /// Friends being removed here (person ids), for the same reason.
    var removing: Set<String> = []
    /// People for you, taste-matched by the server. nil until first asked for; then kept for the
    /// session (this model lives as long as Account) and refreshed only by pull-to-refresh.
    var suggestions: Loadable<[PersonSuggestion]>?

    /// Loads People for you the first time, or again when `force` (pull-to-refresh). A failed
    /// refresh keeps the list on screen.
    func loadSuggestions(_ env: AppEnvironment, force: Bool = false) async {
        guard force || suggestions == nil || suggestions?.phase == .failed else { return }
        if suggestions == nil { suggestions = .loading }
        let result = await Loadable.run { try await env.api.suggestedPeople() }
        withMotion(Motion.arrive) {
            if result.value != nil || suggestions?.value == nil { suggestions = result }
        }
    }

    /// Loads (or quietly reloads) friends and requests; keeps what's on screen if a refresh fails.
    func reload(_ env: AppEnvironment) async {
        async let friendsResult = Loadable.run { try await env.api.friends() }
        async let requestsResult = Loadable.run { try await env.api.friendRequests() }
        let (f, r) = await (friendsResult, requestsResult)
        withMotion(Motion.arrive) {
            if var list = f.value {
                list.removeAll { removing.contains($0.id) }
                friends = .loaded(list)
            } else if friends.value == nil {
                friends = f
            }
            if let all = r.value {
                let open = all.filter { !dropping.contains($0.id) }
                // Still being accepted here: keep it out of Requests.
                requests = .loaded(open.filter { request in
                    !request.outgoing && !accepting.contains { $0.id == request.id }
                })
                let listed = open.filter(\.outgoing)
                sentUnlisted.subtract(listed.map(\.id))
                sent = listed + sent.filter { request in
                    sentUnlisted.contains(request.id) && !listed.contains { $0.id == request.id }
                }
            } else if requests.value == nil {
                requests = r
            }
            // Confirmed friends replace their "Adding…" rows, and requests they accepted are done.
            if let ids = friends.value.map({ Set($0.map(\.person.id)) }) {
                accepting.removeAll { ids.contains($0.person.id) }
                sent.removeAll { ids.contains($0.person.id) }
            }
        }
    }

    // MARK: Realtime

    /// Applies `friend.request` and `friend.status` events for as long as the caller's task runs.
    func listen(_ env: AppEnvironment) async {
        for await event in env.realtime.subscribe() {
            switch event {
            case .friendRequest(let request):
                receive(request)
            case .friendStatus(let userId, let statusLine):
                if isFriend(userId) {
                    updateStatus(of: userId, to: statusLine)
                } else if sentRequest(to: userId) != nil || incomingRequest(from: userId) != nil {
                    // A new friend's first status line: they accepted (maybe on another device).
                    await reload(env)
                }
            default:
                break
            }
        }
    }

    /// A new request rises into Requests (or, sent from another device, joins yours).
    private func receive(_ request: FriendRequest) {
        guard !isFriend(request.person.id), !dropping.contains(request.id) else { return }
        withMotion(Motion.arrive) {
            if request.outgoing {
                // Sent from another of your devices.
                if !sent.contains(where: { $0.id == request.id }) {
                    sent.append(request)
                    sentUnlisted.insert(request.id)
                }
            } else if case .loaded(var list) = requests, !list.contains(where: { $0.id == request.id }) {
                // Still loading: the list on its way includes it.
                list.insert(request, at: 0)
                requests = .loaded(list)
            }
        }
    }

    /// The friend's status line changes in place.
    private func updateStatus(of userId: String, to statusLine: String) {
        guard case .loaded(var list) = friends, let index = list.firstIndex(where: { $0.id == userId }),
              list[index].statusLine != statusLine else { return }
        withMotion {
            list[index].statusLine = statusLine
            friends = .loaded(list)
        }
    }

    // MARK: Lookups

    func isFriend(_ personId: String) -> Bool {
        friends.value?.contains { $0.id == personId } == true || accepting.contains { $0.person.id == personId }
    }

    func incomingRequest(from personId: String) -> FriendRequest? {
        requests.value?.first { $0.person.id == personId }
    }

    func sentRequest(to personId: String) -> FriendRequest? {
        sent.first { $0.person.id == personId }
    }
}

/// Account › Friends: find people + invite, friend requests (yours and theirs), your friends.
///
/// First load: skeleton rows, then the friends arrive one after another. Everything you do here is
/// optimistic (accepting, declining, adding, withdrawing a request, removing a friend): the rows
/// change at once, stay faint while the server confirms, and go back with a short note if it
/// doesn't. Search results say how each person relates to you and update in place.
struct FriendsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    let model: AccountFriendsModel

    @State private var query = ""
    /// nil while the search box is empty.
    @State private var results: Loadable<[UserSearchResult]>?
    /// A newer search is running while the old results stay on screen.
    @State private var searching = false
    /// Friend requests on their way to the server (person ids): a faint "Requested" meanwhile.
    @State private var sending: Set<String> = []
    /// The person whose DM is opening.
    @State private var messaging: String?
    @State private var inviting = false
    @State private var invite: AccountInviteLink?
    @State private var actionError: String?
    @State private var confirming: AccountFriendsConfirm?
    /// Rows arrive one after another when the list first loads while this view exists.
    @State private var animateRows: Bool
    /// Whose profile is open (any row's avatar and name).
    @State private var profileRoute: PersonProfileRoute?

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
            suggestionsSection
            requestsSection
            eyebrow(friendsTitle)
                .sqNumeric()
            friendsCard
                .padding(.bottom, 28)
        }
        .task(id: router.tab == .account) {
            guard router.tab == .account else { return }
            async let suggestions: Void = model.loadSuggestions(env)
            await model.reload(env)
            await suggestions
        }
        .sqReloadable("account.friends") {
            async let suggestions: Void = model.loadSuggestions(env, force: true)
            await model.reload(env)
            await suggestions
        }
        .task(id: query) { await search() }
        .sheet(item: $invite) { link in
            AccountShareSheet(items: [link.url])
                .presentationDetents([.medium, .large])
                .ignoresSafeArea()
        }
        .personProfileSheet($profileRoute, changed: { Task { await model.reload(env) } })
        .confirmationDialog(confirming?.title ?? "", isPresented: confirmShown, titleVisibility: .visible,
                            presenting: confirming) { item in
            Button(item.actionLabel, role: .destructive) { perform(item) }
        } message: { item in
            if let message = item.message { Text(message) }
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

    @ViewBuilder private func searchResults(_ state: Loadable<[UserSearchResult]>) -> some View {
        SetupCard {
            AuthLoadable(state: state, minHeight: 88, shimmers: onScreen, retry: { Task { await search() } }) {
                AccountPeopleSkeleton(count: 2, trailing: .pill)
            } content: { people in
                VStack(spacing: 0) {
                    if people.isEmpty {
                        EmptyStateView(message: "No one found. Try their name or @handle.", minHeight: 72)
                            .transition(.opacity)
                    } else {
                        ForEach(Array(people.enumerated()), id: \.element.id) { index, result in
                            if index > 0 { RowDivider() }
                            resultRow(result)
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

    // MARK: People for you

    /// Taste matches who aren't friends yet, best first, each with its match percent, then people
    /// active lately (no percent). Without any taste match, a note asks for ratings; with no one at
    /// all, an empty state points to search and Invite.
    @ViewBuilder private var suggestionsSection: some View {
        if let state = model.suggestions {
            let people = (state.value ?? []).filter { !model.isFriend($0.person.id) && $0.person.id != env.user?.id }
            VStack(alignment: .leading, spacing: 0) {
                eyebrow("PEOPLE FOR YOU")
                SetupCard {
                    AuthLoadable(state: state, minHeight: 88, shimmers: onScreen,
                                 retry: { Task { await model.loadSuggestions(env, force: true) } }) {
                        AccountPeopleSkeleton(count: 3, trailing: .pill)
                    } content: { _ in
                        VStack(spacing: 0) {
                            if people.isEmpty {
                                EmptyStateView(message: "No one to suggest yet. Search by name or tap Invite.",
                                               minHeight: 72)
                                    .transition(.opacity)
                            } else {
                                if people.allSatisfy({ $0.compatibility == nil }) {
                                    Text("Rate a few places to see who shares your taste.")
                                        .sqFont(12)
                                        .foregroundStyle(Theme.text3)
                                        .authLineHeight(1.35, size: 12)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                        .padding(.top, 12)
                                        .padding(.horizontal, 14)
                                        .transition(.opacity)
                                }
                                ForEach(Array(people.enumerated()), id: \.element.id) { index, suggestion in
                                    if index > 0 { RowDivider() }
                                    resultRow(suggestion.searchResult, match: suggestion.compatibility)
                                        .sqTransition(.rise)
                                }
                            }
                        }
                    }
                }
                .padding(.horizontal, Metrics.side)
            }
            .sqTransition(.rise)
        }
    }

    /// Name (and @handle) plus what you can do: message a friend, withdraw your request, answer
    /// theirs, or add them. `match` (People for you) adds the taste-match percent.
    private func resultRow(_ result: UserSearchResult, match: Int? = nil) -> some View {
        let person = result.person
        let (relation, requestId) = relation(of: result)
        return HStack(spacing: 12) {
            profileButton(person) {
                Avatar(person: person, size: 40, fontSize: 14)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 0) {
                    HStack(spacing: 6) {
                        Text(person.name)
                            .sqFont(15, .semibold)
                            .foregroundStyle(Theme.ink)
                            .authLineHeight(1.35, size: 15)
                            .lineLimit(1)
                        if let match {
                            MatchPill(percent: match)
                        }
                    }
                    if let handle = person.username, !handle.isEmpty {
                        Text("@\(handle)")
                            .sqFont(12)
                            .foregroundStyle(Theme.text3)
                            .authLineHeight(1.35, size: 12)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            // One slot: the states cross-fade in place.
            ZStack(alignment: .trailing) {
                switch relation {
                case .friend:
                    let adding = model.accepting.contains { $0.person.id == person.id }
                    messageButton(person)
                        .disabled(adding)
                        .opacity(adding ? 0.45 : 1)
                        .transition(.opacity)
                case .outgoing:
                    requestedButton(person: person, requestId: requestId)
                        .sqTransition(.pop)
                case .incoming:
                    if let requestId {
                        answerButtons(requestId: requestId, person: person)
                            .transition(.opacity)
                    }
                case .none:
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

    /// What this screen knows now wins over what the search returned (it may be older).
    private func relation(of result: UserSearchResult) -> (FriendRelation, String?) {
        let id = result.person.id
        if model.isFriend(id) { return (.friend, nil) }
        if let request = model.incomingRequest(from: id) { return (.incoming, request.id) }
        if let request = model.sentRequest(to: id) { return (.outgoing, request.id) }
        if sending.contains(id) { return (.outgoing, nil) }
        return (result.relation, result.requestId)
    }

    /// "✓ Requested": faint while the request is on its way, then the check draws in. Tap to
    /// withdraw it (after confirming).
    private func requestedButton(person: PersonRef, requestId: String?) -> some View {
        let pending = requestId == nil
        return Button {
            if let requestId { confirming = .withdraw(requestId: requestId, person: person) }
        } label: {
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
            .frame(minHeight: Metrics.minTouch)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .authHitHeight(34)
        .disabled(pending)
        .authMotion(Motion.quick, value: pending)
        .accessibilityLabel("Requested")
        .accessibilityValue(pending ? "Sending" : "")
        .accessibilityHint(pending ? "" : "Double-tap to withdraw your request to \(person.name)")
    }

    /// Decline (a small ×) and Accept, for a request someone sent you.
    private func answerButtons(requestId: String, person: PersonRef) -> some View {
        HStack(spacing: 8) {
            Button {
                decline(requestId: requestId, person: person)
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.text2)
                    .frame(width: 34, height: 34)
                    .background(Theme.cream, in: Circle())
                    .frame(width: Metrics.minTouch, height: Metrics.minTouch)
                    .contentShape(Circle())
            }
            .buttonStyle(.sqPressable)
            .padding(.horizontal, -5)
            .accessibilityLabel("Decline \(person.name)")
            Button {
                accept(requestId: requestId, person: person)
            } label: {
                Text("Accept")
            }
            .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, horizontalPadding: 12))
            .authHitHeight(34)
            .accessibilityLabel("Accept \(person.name)")
        }
    }

    // MARK: Requests

    /// Theirs first (they need an answer), then yours.
    private var requestRows: [AccountRequestRow] {
        (model.requests.value ?? []).map(AccountRequestRow.incoming) + model.sent.map(AccountRequestRow.sent)
    }

    @ViewBuilder private var requestsSection: some View {
        let rows = requestRows
        let failure: String? = if case .failed(let message) = model.requests { message } else { nil }
        if !rows.isEmpty || failure != nil {
            VStack(alignment: .leading, spacing: 0) {
                eyebrow("REQUESTS")
                SetupCard {
                    if let failure {
                        ErrorStateView(message: failure, minHeight: 96) { Task { await model.reload(env) } }
                            .transition(.opacity)
                        if !rows.isEmpty { RowDivider() }
                    }
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                        if index > 0 { RowDivider() }
                        requestRow(row)
                            .sqTransition(.rise)
                    }
                }
                .padding(.horizontal, Metrics.side)
            }
            .transition(.opacity)
        }
    }

    private func requestRow(_ row: AccountRequestRow) -> some View {
        let request = row.request
        return HStack(spacing: 12) {
            profileButton(request.person) {
                Avatar(person: request.person, size: 40, fontSize: 14)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 0) {
                    Text(request.person.name)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .authLineHeight(1.35, size: 15)
                    if !request.note.isEmpty {
                        Text(request.note)
                            .sqFont(12)
                            .foregroundStyle(Theme.text3)
                            .authLineHeight(1.35, size: 12)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            switch row {
            case .incoming:
                answerButtons(requestId: request.id, person: request.person)
            case .sent:
                requestedButton(person: request.person, requestId: request.id)
            }
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

    /// Photo (or initials) with the activity dot, name, live status line (tap for their profile),
    /// message button. Long-press for Message / Remove friend.
    private func friendRow(_ row: AccountFriendRow) -> some View {
        let adding = row.isAdding
        let person = row.person
        return HStack(spacing: 12) {
            profileButton(person) {
                Avatar(initials: person.initials, fill: person.color, size: 40, fontSize: 14, imageURL: person.photoURL,
                       statusColor: row.dotColor, statusSize: 12, statusBorder: .white, statusBorderWidth: 2,
                       statusOffset: 1)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 0) {
                    Text(person.name)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .authLineHeight(1.35, size: 15)
                    Text(row.statusLine)
                        .sqFont(12)
                        .foregroundStyle(Theme.text3)
                        .authLineHeight(1.35, size: 12)
                        .contentTransition(.opacity)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .disabled(adding)
            messageButton(person)
                .disabled(adding)
                .opacity(adding ? 0.45 : 1)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .background(.white)
        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .contextMenu {
            if !adding {
                Button { message(person) } label: { Label("Message", systemImage: "bubble.left") }
                Button(role: .destructive) { confirming = .remove(person) } label: {
                    Label("Remove friend", systemImage: "person.badge.minus")
                }
            }
        }
        .accessibilityAction(named: "Remove friend") {
            if !adding { confirming = .remove(person) }
        }
    }

    /// A row's avatar and name, as one button that opens the person's profile (the row's own
    /// buttons stay separate).
    private func profileButton<Label: View>(_ person: PersonRef, @ViewBuilder label: () -> Label) -> some View {
        Button {
            profileRoute = PersonProfileRoute(person)
        } label: {
            HStack(spacing: 12) { label() }
                .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .combine)
        .accessibilityHint("Opens their profile")
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

    private var confirmShown: Binding<Bool> {
        Binding(get: { confirming != nil }, set: { if !$0 { confirming = nil } })
    }

    // MARK: Search

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
            results = found.value.map { .loaded($0.filter { $0.person.id != env.user?.id }) } ?? found
            searching = false
        }
    }

    /// Keeps a search row in step with something done elsewhere on this screen.
    private func setRelation(_ personId: String, _ relation: FriendRelation, requestId: String?) {
        guard case .loaded(var list) = results, let index = list.firstIndex(where: { $0.person.id == personId }) else { return }
        list[index].relation = relation
        list[index].requestId = requestId
        results = .loaded(list)
    }

    // MARK: Actions

    private func perform(_ item: AccountFriendsConfirm) {
        switch item {
        case .remove(let person): removeFriend(person)
        case .withdraw(let requestId, let person): withdraw(requestId: requestId, person: person)
        }
    }

    /// Optimistic: the request leaves Requests and the person joins your friends as "Adding…" right
    /// away; the server's list replaces it, or it all goes back with a note if accepting fails.
    private func accept(requestId: String, person: PersonRef) {
        let list = model.requests.value ?? []
        let index = list.firstIndex { $0.id == requestId }
        let request = index.map { list[$0] } ?? FriendRequest(id: requestId, person: person, note: "")
        withMotion(Motion.arrive) {
            if let index {
                var remaining = list
                remaining.remove(at: index)
                model.requests = .loaded(remaining)
            }
            model.accepting.insert(request, at: 0)
            setRelation(person.id, .friend, requestId: nil)
            actionError = nil
        }
        Task {
            do {
                try await env.api.acceptFriendRequest(id: requestId)
                await model.reload(env)
            } catch {
                withMotion(Motion.arrive) {
                    model.accepting.removeAll { $0.id == requestId }
                    restore(request, at: index)
                    setRelation(person.id, .incoming, requestId: requestId)
                    actionError = authMessage(for: error, fallback: "Couldn't accept the request. Try again.")
                }
            }
        }
    }

    /// Optimistic: the request slides out; it comes back with a note if the server says no.
    private func decline(requestId: String, person: PersonRef) {
        let list = model.requests.value ?? []
        let index = list.firstIndex { $0.id == requestId }
        let request = index.map { list[$0] }
        withMotion(Motion.arrive) {
            if let index {
                var remaining = list
                remaining.remove(at: index)
                model.requests = .loaded(remaining)
            }
            model.dropping.insert(requestId)
            setRelation(person.id, .none, requestId: nil)
            actionError = nil
        }
        Task {
            do {
                try await env.api.declineFriendRequest(id: requestId)
                model.dropping.remove(requestId)
            } catch {
                withMotion(Motion.arrive) {
                    model.dropping.remove(requestId)
                    if let request { restore(request, at: index) }
                    setRelation(person.id, .incoming, requestId: requestId)
                    actionError = authMessage(for: error, fallback: "Couldn't decline the request. Try again.")
                }
            }
        }
    }

    private func restore(_ request: FriendRequest, at index: Int?) {
        guard let index, case .loaded(var current) = model.requests, !current.contains(where: { $0.id == request.id }) else { return }
        current.insert(request, at: min(index, current.count))
        model.requests = .loaded(current)
    }

    /// Optimistic: "Requested" shows at once (faint until the server returns the request, then its
    /// check draws in); the request also joins yours under Requests, where it can be withdrawn.
    private func sendRequest(to person: PersonRef) {
        withMotion(Motion.quick) {
            _ = sending.insert(person.id)
            actionError = nil
        }
        Task {
            do {
                let request = try await env.api.sendFriendRequest(userId: person.id)
                withMotion(Motion.arrive) {
                    sending.remove(person.id)
                    if !model.sent.contains(where: { $0.id == request.id }) {
                        model.sent.append(request)
                        model.sentUnlisted.insert(request.id)
                    }
                    setRelation(person.id, .outgoing, requestId: request.id)
                }
            } catch {
                withMotion {
                    sending.remove(person.id)
                    actionError = authMessage(for: error, fallback: "Couldn't send the request. Try again.")
                }
            }
        }
    }

    /// Optimistic: your request disappears (and "Add" comes back in search); it returns with a note
    /// if the server says no.
    private func withdraw(requestId: String, person: PersonRef) {
        let index = model.sent.firstIndex { $0.id == requestId }
        let request = index.map { model.sent[$0] }
        withMotion(Motion.arrive) {
            if let index { model.sent.remove(at: index) }
            model.dropping.insert(requestId)
            setRelation(person.id, .none, requestId: nil)
            actionError = nil
        }
        Task {
            do {
                try await env.api.cancelFriendRequest(id: requestId)
                model.dropping.remove(requestId)
                model.sentUnlisted.remove(requestId)
            } catch {
                withMotion(Motion.arrive) {
                    model.dropping.remove(requestId)
                    if let request, let index, !model.sent.contains(where: { $0.id == requestId }) {
                        model.sent.insert(request, at: min(index, model.sent.count))
                    }
                    setRelation(person.id, .outgoing, requestId: requestId)
                    actionError = authMessage(for: error, fallback: "Couldn't withdraw the request. Try again.")
                }
            }
        }
    }

    /// Optimistic: the row fades out of your friends; it comes back with a note if the server says no.
    private func removeFriend(_ person: PersonRef) {
        guard case .loaded(var list) = model.friends, let index = list.firstIndex(where: { $0.id == person.id }) else { return }
        let friend = list.remove(at: index)
        withMotion(Motion.arrive) {
            model.friends = .loaded(list)
            model.removing.insert(person.id)
            setRelation(person.id, .none, requestId: nil)
            actionError = nil
        }
        Task {
            do {
                try await env.api.removeFriend(id: person.id)
                model.removing.remove(person.id)
            } catch {
                withMotion(Motion.arrive) {
                    model.removing.remove(person.id)
                    if case .loaded(var current) = model.friends, !current.contains(where: { $0.id == person.id }) {
                        current.insert(friend, at: min(index, current.count))
                        model.friends = .loaded(current)
                    }
                    setRelation(person.id, .friend, requestId: nil)
                    actionError = authMessage(for: error, fallback: "Couldn't remove \(person.firstName). Try again.")
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
                router.openThread(thread.id, isGroup: false)
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

/// A destructive Friends action waiting for confirmation.
private enum AccountFriendsConfirm {
    case remove(PersonRef)
    case withdraw(requestId: String, person: PersonRef)

    var title: String {
        switch self {
        case .remove(let person): "Remove \(person.name) from your friends?"
        case .withdraw(_, let person): "Withdraw your friend request to \(person.name)?"
        }
    }

    var actionLabel: String {
        switch self {
        case .remove: "Remove friend"
        case .withdraw: "Withdraw request"
        }
    }

    var message: String? {
        switch self {
        case .remove: "You can add them again anytime."
        case .withdraw: nil
        }
    }
}

/// A row of the Requests card: someone asking you, or your own request.
private enum AccountRequestRow: Identifiable {
    case incoming(FriendRequest)
    case sent(FriendRequest)

    var request: FriendRequest {
        switch self {
        case .incoming(let request), .sent(let request): request
        }
    }

    var id: String { request.id }
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

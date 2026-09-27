import SwiftUI

/// A profile to open: whose, and what the opener already has (their avatar and name show at once).
struct PersonProfileRoute: Identifiable, Equatable {
    let personId: String
    var seed: PersonRef?

    var id: String { personId }

    init(_ person: PersonRef) {
        personId = person.id
        seed = person
    }

    init(personId: String) {
        self.personId = personId
    }
}

extension View {
    /// Presents someone's profile while `route` is set (tap a person's avatar or name). `leave` runs
    /// when the profile opens a chat, which shows over the tabs, so a sheet under this one can close
    /// too; `changed` runs after you changed how you relate to them or where you stand on one of
    /// their plans, so the screen underneath can catch up.
    func personProfileSheet(_ route: Binding<PersonProfileRoute?>, leave: (() -> Void)? = nil,
                            changed: (() -> Void)? = nil) -> some View {
        sqSheet(item: route, style: SQSheetStyle(height: .fitted(max: 760))) { current in
            PersonProfileSheet(route: current, close: { route.wrappedValue = nil }, leave: leave, changed: changed)
        }
    }
}

/// People › profile: big avatar (with their presence), name, "@handle · school", how free they are
/// and how many sidequests they've done; your taste match with the reasons under it; what you can
/// do (Add friend, Requested, Accept / Decline, Friends ✓ with Remove friend, and Message); what
/// they're into; friends in common; their open plans, joinable from here. Your own profile shows
/// what others see, without the buttons.
///
/// Loading: the avatar and name the opener had show at once and the rest is a skeleton (the S
/// takes over past the slow-load threshold); a failed load says so, with "Try again". Friend
/// actions are optimistic like Account › Friends: the buttons change at once, the new one stays
/// faint until the server confirms (then the profile quietly reloads), and it all goes back with a
/// short note if the server says no. Message and a joined plan's chat close the sheet (and the one
/// under it) and open the chat over the tabs.
struct PersonProfileSheet: View {
    let route: PersonProfileRoute
    let close: () -> Void
    var leave: (() -> Void)? = nil
    var changed: (() -> Void)? = nil

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var profile: Loadable<PublicProfile> = .loading
    /// A friend action on its way: the button it leads to shows faint, and the others wait.
    @State private var pending: PersonProfileAction?
    @State private var messaging = false
    @State private var actionError: String?
    @State private var confirming: PersonProfileConfirm?
    @State private var busyPlanIds: Set<String> = []
    @State private var planErrors: [String: String] = [:]
    /// Group chats of plans joined here (post id → thread id).
    @State private var joinThreadIds: [String: String] = [:]
    /// A plan joined here: Muse offers its tickets once this sheet is out of the way.
    @State private var joinedItineraryId: String?

    private var person: PersonRef? { profile.value?.person ?? route.seed }

    var body: some View {
        SheetScaffold(spacing: 16) {
            identity
            ZStack(alignment: .top) {
                switch profile {
                case .loading:
                    PersonProfileSkeleton()
                        .sqSlowLoading(lines: [loadingLine])
                        .transition(.opacity)
                case .failed(let message):
                    ErrorStateView(message: message, minHeight: 180) { Task { await load() } }
                        .transition(.opacity)
                case .loaded(let loaded):
                    details(loaded)
                        .transition(.opacity)
                }
            }
            .animation(reduceMotion ? Motion.reduced : Motion.standard, value: profile.phase)
        }
        .task { await load() }
        .task { await listen() }
        .onDisappear(perform: offerTicketsAfterJoin)
        .confirmationDialog(confirming?.title ?? "", isPresented: confirmShown, titleVisibility: .visible,
                            presenting: confirming) { item in
            Button(item.actionLabel, role: .destructive) { perform(item) }
        } message: { item in
            if let message = item.message { Text(message) }
        }
    }

    private var loadingLine: String {
        person.map { "Getting \($0.firstName)'s profile…" } ?? "Getting their profile…"
    }

    // MARK: Identity

    /// Avatar (with the presence dot once known) and name, the × in the corner. Without anything
    /// from the opener (a deep link), both shimmer until the profile arrives, and a failed load
    /// leaves a plain silhouette.
    private var identity: some View {
        VStack(spacing: 10) {
            if let person {
                Avatar(initials: person.initials, fill: person.color, size: 88, fontSize: 30, imageURL: person.photoURL,
                       statusColor: profile.value?.status.color, statusSize: 20, statusBorder: .white, statusBorderWidth: 3,
                       statusOffset: -4)
                    .accessibilityHidden(true)
                Text(person.name)
                    .sqFont(24, .bold, relativeTo: .title)
                    .foregroundStyle(Theme.ink)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 36)
                    .accessibilityAddTraits(.isHeader)
                    .accessibilityIdentifier("profile.name")
            } else if profile.isLoading {
                VStack(spacing: 12) {
                    Circle().fill(Theme.skeleton).frame(width: 88, height: 88)
                    SkeletonBlock(width: 160, height: 20, radius: 8)
                }
                .sqShimmer()
                .accessibilityHidden(true)
            } else {
                Circle()
                    .fill(Theme.skeleton)
                    .frame(width: 88, height: 88)
                    .overlay {
                        Image(systemName: "person.fill")
                            .font(.system(size: 38))
                            .foregroundStyle(Theme.mutedStar)
                    }
                    .accessibilityHidden(true)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 4)
        .overlay(alignment: .topTrailing) {
            CloseCircleButton(action: close)
                .padding(-6)
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: profile.value?.status)
    }

    // MARK: Details

    private func details(_ p: PublicProfile) -> some View {
        VStack(spacing: 16) {
            VStack(spacing: 4) {
                if let handle = p.handleLine {
                    Text(handle)
                        .sqFont(15)
                        .foregroundStyle(Theme.text3)
                }
                Text("\(p.presenceLine) · \(p.sidequestsLine)")
                    .sqFont(14)
                    .foregroundStyle(Theme.text2)
                    .contentTransition(.opacity)
                    .sqNumeric()
            }
            .multilineTextAlignment(.center)
            .accessibilityElement(children: .combine)
            matchBlock(p)
            if p.relation == .you {
                Text("This is how others see your profile.")
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
            } else {
                actions(p)
            }
            if let actionError {
                Text(actionError)
                    .sqFont(13)
                    .foregroundStyle(Theme.dangerText)
                    .multilineTextAlignment(.center)
                    .sqTransition(.rise)
            }
            if !p.likes.isEmpty {
                section("INTO") {
                    FlowLayout(spacing: 6, lineSpacing: 6) {
                        ForEach(p.likes, id: \.self) { like in
                            TagLabel(text: like, fill: .white, foreground: Theme.ink, fontSize: 13, horizontalPadding: 10,
                                     verticalPadding: 5, radius: 10)
                        }
                    }
                    .accessibilityElement(children: .combine)
                }
            }
            if p.mutualFriends.count > 0 {
                section(p.mutualTitle) {
                    HStack(spacing: 10) {
                        if !p.mutualFriends.people.isEmpty {
                            AvatarStack(people: p.mutualFriends.people, size: 30, overlap: 8, maxCount: 3)
                                .accessibilityHidden(true)
                        }
                        if let line = p.mutualLine {
                            Text(line)
                                .sqFont(14)
                                .foregroundStyle(Theme.text2)
                        }
                    }
                }
            }
            if !p.openPlans.isEmpty {
                section(p.openPlans.count == 1 ? "OPEN PLAN" : "OPEN PLANS") {
                    VStack(spacing: 10) {
                        ForEach(p.openPlans) { post in
                            ForumPostCard(post: post, isBusy: busyPlanIds.contains(post.id), error: planErrors[post.id],
                                          showsAuthor: false) { act(on: post) }
                        }
                    }
                }
            }
        }
        .frame(maxWidth: .infinity)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: actionError)
    }

    /// "86% match" with the reasons under it; nothing when there's neither.
    @ViewBuilder private func matchBlock(_ p: PublicProfile) -> some View {
        if p.compatibility != nil || !p.matchReasons.isEmpty {
            VStack(spacing: 6) {
                if let match = p.compatibility {
                    ProfileMatchPill(percent: match)
                }
                VStack(spacing: 2) {
                    ForEach(p.matchReasons, id: \.self) { reason in
                        Text(reason)
                            .sqFont(14)
                            .foregroundStyle(Theme.text2)
                    }
                }
            }
            .multilineTextAlignment(.center)
            .accessibilityElement(children: .combine)
        }
    }

    /// A cream box with a small heading (like the Event sheet's "WHO'S IN").
    private func section<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title)
                .sqFont(12, .bold, relativeTo: .caption)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .accessibilityAddTraits(.isHeader)
            content()
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    // MARK: Actions

    private func actions(_ p: PublicProfile) -> some View {
        HStack(spacing: 8) {
            ForEach(PersonProfileAction.actions(for: p.relation), id: \.self) { action in
                actionButton(action, p)
                    .opacity(pending == action ? 0.6 : 1)
                    .transition(.opacity)
            }
        }
        .disabled(pending != nil || messaging)
        .animation(Motion.quick, value: p.relation)
        .animation(Motion.quick, value: pending)
    }

    @ViewBuilder private func actionButton(_ action: PersonProfileAction, _ p: PublicProfile) -> some View {
        switch action {
        case .add:
            Button(action.title) { add(p) }
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 44, radius: 12, fontSize: 15))
        case .requested:
            // Faint without its check while the request is on its way; then tap to withdraw it.
            Button {
                if let requestId = p.requestId { confirming = .withdraw(requestId: requestId, person: p.person) }
            } label: {
                HStack(spacing: 6) {
                    if pending != .requested {
                        AnimatedCheck(lineWidth: 2.8, delay: 0.1)
                            .frame(width: 13, height: 13)
                            .transition(.opacity)
                    }
                    Text(action.title)
                }
            }
            .buttonStyle(.sq(fill: Theme.successBg, foreground: Theme.success, height: 44, radius: 12, fontSize: 15))
            .accessibilityValue(pending == .requested ? "Sending" : "")
            .accessibilityHint(pending == .requested ? "" : "Withdraws your friend request")
        case .accept:
            Button(action.title) { answer(p, accept: true) }
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 44, radius: 12, fontSize: 15))
        case .decline:
            Button(action.title) { answer(p, accept: false) }
                .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.text2, height: 44, radius: 12, fontSize: 15))
        case .friends:
            Menu {
                Button(role: .destructive) { confirming = .remove(p.person) } label: {
                    Label("Remove friend", systemImage: "person.badge.minus")
                }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "checkmark")
                        .font(.system(size: 12, weight: .bold))
                    Text(action.title)
                    Image(systemName: "chevron.down")
                        .font(.system(size: 11, weight: .bold))
                }
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.sageInk)
                .frame(maxWidth: .infinity, minHeight: 44)
                .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            }
            .buttonStyle(.plain)
            .accessibilityLabel(action.title)
            .accessibilityHint("Shows Remove friend")
        case .message:
            Button { message(p.person) } label: {
                SocialBusyLabel(isBusy: messaging, color: Theme.sageInk, dotSize: 5) { Text(action.title) }
            }
            .buttonStyle(.sq(fill: .white, foreground: Theme.sageInk, border: Theme.sage, borderWidth: 1.5, height: 44,
                             radius: 12, fontSize: 15))
            .accessibilityLabel(action.title)
            .accessibilityValue(messaging ? "Opening" : "")
        }
    }

    private var confirmShown: Binding<Bool> {
        Binding(get: { confirming != nil }, set: { if !$0 { confirming = nil } })
    }

    private func perform(_ item: PersonProfileConfirm) {
        switch item {
        case .withdraw(let requestId, _): withdraw(requestId: requestId)
        case .remove(let person): removeFriend(person)
        }
    }

    private func add(_ p: PublicProfile) {
        update(to: .requested, failure: "Couldn't send the request. Try again.") {
            $0.relation = .outgoing
            $0.requestId = nil
        } work: {
            let request = try await env.api.sendFriendRequest(userId: p.person.id)
            return { $0.requestId = request.id }
        }
    }

    private func withdraw(requestId: String) {
        update(to: .add, failure: "Couldn't withdraw the request. Try again.") {
            $0.relation = .none
            $0.requestId = nil
        } work: {
            try await env.api.cancelFriendRequest(id: requestId)
            return nil
        }
    }

    /// Accept (they're a friend at once) or decline (back to "Add friend") their request.
    private func answer(_ p: PublicProfile, accept: Bool) {
        guard let requestId = p.requestId else { return }
        update(to: accept ? .friends : .add,
               failure: accept ? "Couldn't accept the request. Try again." : "Couldn't decline the request. Try again.") {
            $0.relation = accept ? .friend : .none
            $0.requestId = nil
        } work: {
            if accept {
                try await env.api.acceptFriendRequest(id: requestId)
            } else {
                try await env.api.declineFriendRequest(id: requestId)
            }
            return nil
        }
    }

    private func removeFriend(_ person: PersonRef) {
        update(to: .add, failure: "Couldn't remove \(person.firstName). Try again.") {
            $0.relation = .none
            $0.statusLine = nil
        } work: {
            try await env.api.removeFriend(id: person.id)
            return nil
        }
    }

    /// A friend action: `change` shows at once with `action`'s button faint; `work` asks the server
    /// and returns what to fill in from its answer. Success tells the screen underneath and reloads
    /// the profile quietly (a new friend's status line and plans); failure puts it back with
    /// `failure` as a short note.
    private func update(to action: PersonProfileAction, failure: String, change: (inout PublicProfile) -> Void,
                        work: @escaping () async throws -> ((inout PublicProfile) -> Void)?) {
        guard case .loaded(let before) = profile, pending == nil else { return }
        var after = before
        change(&after)
        withMotion(Motion.arrive) {
            profile = .loaded(after)
            pending = action
            actionError = nil
        }
        Task {
            do {
                let confirm = try await work()
                withMotion(Motion.arrive) {
                    if let confirm, case .loaded(var current) = profile {
                        confirm(&current)
                        profile = .loaded(current)
                    }
                    pending = nil
                }
                changed?()
                await refresh()
            } catch {
                withMotion(Motion.arrive) {
                    profile = .loaded(before)
                    pending = nil
                    actionError = authMessage(for: error, fallback: failure)
                }
            }
        }
    }

    private func message(_ person: PersonRef) {
        guard !messaging else { return }
        withMotion(Motion.quick) {
            messaging = true
            actionError = nil
        }
        Task {
            do {
                let thread = try await env.api.startDM(userId: person.id)
                messaging = false
                openChat(thread.id, isGroup: false)
            } catch {
                withMotion {
                    messaging = false
                    actionError = authMessage(for: error, fallback: "Couldn't open the chat. Try again.")
                }
            }
        }
    }

    /// Chats show over the tabs, under any sheet: this sheet (and one under it) closes first.
    private func openChat(_ threadId: String, isGroup: Bool) {
        close()
        leave?()
        router.openThread(threadId, isGroup: isGroup)
    }

    // MARK: Open plans

    /// A plan card's button, as in the Forum: ask to join (or take the request back), and once
    /// you're in, open the plan's chat.
    private func act(on post: ForumPost) {
        switch post.joinStatus {
        case .none:
            run(post) {
                let result = try await env.api.requestToJoin(postId: post.id)
                applyJoin(result, to: post.id)
                if result.status == .joined {
                    joinedItineraryId = result.itineraryId
                    // So the plan shows on Home and its chat in Groups.
                    Task { await env.reloadAll() }
                }
                changed?()
            }
        case .requested:
            run(post) {
                try await env.api.cancelJoinRequest(postId: post.id)
                updatePlan(post.id) { $0.joinStatus = .none }
                changed?()
            }
        case .joined:
            if let threadId = joinThreadIds[post.id] ?? post.threadId {
                openChat(threadId, isGroup: true)
            } else {
                close()
                leave?()
                router.select(.groups)
            }
        case .full, .closed:
            break
        }
    }

    private func applyJoin(_ result: JoinResult, to postId: String) {
        if let threadId = result.threadId { joinThreadIds[postId] = threadId }
        updatePlan(postId) { $0.joinStatus = result.status }
    }

    /// Runs a card's action with loading dots in its button; a failure rises in under it.
    private func run(_ post: ForumPost, _ work: @escaping () async throws -> Void) {
        guard !busyPlanIds.contains(post.id) else { return }
        busyPlanIds.insert(post.id)
        if planErrors[post.id] != nil { withMotion(Motion.quick) { planErrors[post.id] = nil } }
        Task {
            do {
                try await work()
            } catch {
                withMotion { planErrors[post.id] = error.socialMessage }
            }
            withMotion(Motion.arrive) { _ = busyPlanIds.remove(post.id) }
        }
    }

    private func updatePlan(_ postId: String, _ change: (inout ForumPost) -> Void) {
        guard case .loaded(var current) = profile, let index = current.openPlans.firstIndex(where: { $0.id == postId }) else { return }
        change(&current.openPlans[index])
        withMotion(Motion.arrive) { profile = .loaded(current) }
    }

    /// The Forum offers Muse right after a join; here it waits until this sheet has closed (Muse's
    /// sheet shows over the tabs), and skips it while another sheet is still open under this one.
    private func offerTicketsAfterJoin() {
        guard let itineraryId = joinedItineraryId, leave == nil else { return }
        joinedItineraryId = nil
        let router = router, env = env
        Task { await router.offerAgentCheckout(itineraryId: itineraryId, env: env, delay: .milliseconds(400)) }
    }

    // MARK: Loading

    private func load() async {
        if !profile.isLoading { withMotion { profile = .loading } }
        let result = await Loadable.run { try await env.api.profile(userId: route.personId) }
        guard !Task.isCancelled else { return }
        withMotion { profile = result }
    }

    /// The server's copy after a change, without a loader; keeps what's shown if it fails.
    private func refresh() async {
        guard let fresh = try? await env.api.profile(userId: route.personId) else { return }
        withMotion { profile = .loaded(fresh) }
    }

    /// This person answering (or sending) a request, or their line changing, reloads the profile;
    /// a host answering your join request moves its card along.
    private func listen() async {
        for await event in env.realtime.subscribe() {
            switch event {
            case .friendStatus(let userId, _) where userId == route.personId && pending == nil:
                await refresh()
            case .friendRequest(let request) where request.person.id == route.personId && pending == nil:
                await refresh()
            case .joinUpdate(let postId, let result):
                applyJoin(result, to: postId)
            default:
                break
            }
        }
    }
}

/// A destructive profile action waiting for confirmation.
private enum PersonProfileConfirm {
    case withdraw(requestId: String, person: PersonRef)
    case remove(PersonRef)

    var title: String {
        switch self {
        case .withdraw(_, let person): "Withdraw your friend request to \(person.name)?"
        case .remove(let person): "Remove \(person.name) from your friends?"
        }
    }

    var actionLabel: String {
        switch self {
        case .withdraw: "Withdraw request"
        case .remove: "Remove friend"
        }
    }

    var message: String? {
        switch self {
        case .withdraw: nil
        case .remove: "You can add them again anytime."
        }
    }
}

/// The profile while it loads: its lines, the match pill, the buttons and a section, shimmering.
/// Reads "Loading".
private struct PersonProfileSkeleton: View {
    var body: some View {
        VStack(spacing: 16) {
            VStack(spacing: 9) {
                SkeletonBlock(width: 140, height: 13)
                SkeletonBlock(width: 200, height: 13)
            }
            Capsule()
                .fill(Theme.skeleton)
                .frame(width: 104, height: 30)
            HStack(spacing: 8) {
                SkeletonBlock(height: 44, radius: 12)
                SkeletonBlock(height: 44, radius: 12)
            }
            VStack(alignment: .leading, spacing: 10) {
                SkeletonBlock(width: 44, height: 10, color: Theme.skeletonOnCream)
                HStack(spacing: 6) {
                    ForEach([78, 104, 70], id: \.self) { width in
                        SkeletonBlock(width: CGFloat(width), height: 27, radius: 10, color: .white)
                    }
                }
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

#Preview {
    Color.clear
        .personProfileSheet(.constant(PersonProfileRoute(MockPeople.maya)))
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

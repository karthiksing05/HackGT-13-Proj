import AuthenticationServices
import Observation
import SwiftUI

/// Account › Me data. Owned by `AccountView` so it survives Me ⇄ Friends switches; every refresh
/// keeps what's on screen and swaps the fresh data in with animation.
@Observable
final class AccountMeModel {
    var taste: Loadable<TasteProfile> = .loading
    var links: Loadable<AccountLinks> = .loading
    var history: Loadable<[PastEvent]> = .loading

    /// Loads (or quietly reloads) all three cards in parallel.
    func load(_ env: AppEnvironment) async {
        async let t: Void = loadTaste(env)
        async let l: Void = loadLinks(env)
        async let h: Void = loadHistory(env)
        _ = await (t, l, h)
    }

    func loadTaste(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.tasteProfile() }
        // Keep what's on screen if a background refresh fails.
        guard result.value != nil || taste.value == nil else { return }
        withMotion(Motion.gentle) { taste = result }
    }

    func loadLinks(_ env: AppEnvironment) async {
        let result = await Loadable.run {
            async let integrations = env.api.integrations()
            async let cards = env.api.paymentMethods()
            return AccountLinks(integrations: try await integrations, cards: try await cards)
        }
        guard result.value != nil || links.value == nil else { return }
        withMotion { links = result }
    }

    func loadHistory(_ env: AppEnvironment) async {
        let result = await Loadable.run {
            try await env.api.pastEvents(unratedOnly: false)
                .filter { $0.rating != nil }
                .sorted { $0.date > $1.date }
                .prefix(3)
                .map { $0 }
        }
        guard result.value != nil || history.value == nil else { return }
        withMotion { history = result }
    }
}

/// What the "Connected" card is built from.
struct AccountLinks {
    var integrations: [Integration]
    var cards: [PaymentMethod]
}

/// Account › Me: taste profile, connected services, rated past sidequests, sign out.
/// No stats row and no app-color option (GUI_PLAN.md §7.10).
///
/// First load: each card shows a skeleton of its own rows, then the rows arrive (taste bars grow
/// from zero, one after another). Calendars connect and disconnect from the Connected card.
struct AccountMeSection: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    let model: AccountMeModel

    @State private var signingOut = false
    /// Rows animate in when their data arrives while this section exists (not when it's rebuilt
    /// around data that was already loaded).
    @State private var animateTaste: Bool
    @State private var animateLinks: Bool
    @State private var animateHistory: Bool
    /// The calendar being connected or disconnected.
    @State private var working: CalendarProvider?
    /// Just connected here: its "Connected" gets a check that draws in.
    @State private var justConnected: CalendarProvider?
    /// Calendars touched here stay listed (as "Not connected" after a disconnect) so you can undo.
    @State private var touched: Set<CalendarProvider> = []
    @State private var confirmDisconnect: CalendarProvider?
    @State private var choosingCalendar = false
    @State private var linkError: String?

    init(model: AccountMeModel) {
        self.model = model
        _animateTaste = State(initialValue: model.taste.value == nil)
        _animateLinks = State(initialValue: model.links.value == nil)
        _animateHistory = State(initialValue: model.history.value == nil)
    }

    /// Skeletons only shimmer while Account is the visible tab.
    private var onScreen: Bool { router.tab == .account }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            sectionTitle("Taste profile")
            tasteCard

            sectionTitle("Connected")
            connectionsCard

            HStack(alignment: .firstTextBaseline) {
                SectionHeader(title: "Past sidequests")
                Spacer(minLength: 8)
                Button("See all & rate") { router.showPast() }
                    .buttonStyle(.sqLink(size: 14))
                    .authHitHeight(30)
            }
            .authLineHeight(1.35, size: 19, mono: true)
            .padding(.top, 22)
            .padding(.bottom, 8)
            .padding(.horizontal, Metrics.side)
            historyCard

            signOutButton
                .padding(.top, 20)
                .padding(.bottom, 28)
                .padding(.horizontal, Metrics.side)
        }
        // Refresh whenever Account comes on screen (tabs stay alive in the background).
        .task(id: router.tab == .account) {
            guard router.tab == .account else { return }
            await model.load(env)
        }
        .sqReloadable("account.me") { await model.load(env) }
        .confirmationDialog(disconnectTitle, isPresented: disconnectShown, titleVisibility: .visible,
                            presenting: confirmDisconnect) { provider in
            Button("Disconnect", role: .destructive) { toggle(provider, connect: false) }
        } message: { _ in
            Text("We'll stop reading your free/busy times. You can connect it again anytime.")
        }
        .confirmationDialog("Connect a calendar", isPresented: $choosingCalendar, titleVisibility: .visible) {
            ForEach(CalendarProvider.allCases) { provider in
                Button(provider.name) { toggle(provider, connect: true) }
            }
        }
    }

    private func sectionTitle(_ title: String) -> some View {
        SectionHeader(title: title)
            .authLineHeight(1.35, size: 19, mono: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.top, 22)
            .padding(.bottom, 8)
            .padding(.horizontal, Metrics.side)
    }

    // MARK: Taste profile

    private var tasteCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            AuthLoadable(state: model.taste, shimmers: onScreen, retry: { Task { await model.loadTaste(env) } }) {
                AccountTasteSkeleton()
            } content: { profile in
                VStack(alignment: .leading, spacing: 10) {
                    if profile.bars.isEmpty {
                        Text("Rate a few sidequests and your taste profile shows up here.")
                            .sqFont(14)
                            .foregroundStyle(Theme.text3)
                    } else {
                        ForEach(Array(profile.bars.enumerated()), id: \.element.id) { index, bar in
                            AccountTasteBarRow(bar: bar, index: index, grows: animateTaste)
                        }
                    }
                }
                .onAppear { animateTaste = false }
            }
            Text("Starts from your setup answers, then learns from your ratings. Each new sidequest mixes this with the mood you describe.")
                .sqFont(12)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
                .fixedSize(horizontal: false, vertical: true)
            Button("Redo setup questions") { router.setupRedo = SetupEntry(step: 3) }
                .buttonStyle(.sqTintPill)
                .authHitHeight(32)
        }
        .padding(Metrics.cardPadding)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .padding(.horizontal, Metrics.side)
    }

    // MARK: Connected

    private var connectionsCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            SetupCard {
                AuthLoadable(state: model.links, shimmers: onScreen, retry: { Task { await model.loadLinks(env) } }) {
                    AccountLinksSkeleton()
                } content: { links in
                    let rows = AccountConnection.rows(integrations: links.integrations, cards: links.cards, keep: touched)
                    VStack(spacing: 0) {
                        ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                            if index > 0 { RowDivider() }
                            connectionRow(row)
                                .authArrive(index, animated: animateLinks)
                                .sqTransition(.rise)
                        }
                    }
                    .onAppear { animateLinks = false }
                }
            }
            if let linkError {
                AuthErrorText(message: linkError)
                    .padding(.horizontal, 4)
                    .sqTransition(.rise)
            }
        }
        .padding(.horizontal, Metrics.side)
    }

    @ViewBuilder private func connectionRow(_ row: AccountConnection) -> some View {
        switch row.action {
        case .none:
            connectionLabel(row)
        case .calendar(let provider):
            Button {
                if row.connected {
                    confirmDisconnect = provider
                } else {
                    toggle(provider, connect: true)
                }
            } label: {
                connectionLabel(row, working: working == provider, check: justConnected == provider && row.connected)
            }
            .buttonStyle(.sqPressable)
            .disabled(working != nil)
            .accessibilityHint(row.connected ? "Double-tap to disconnect" : "Double-tap to connect")
        case .pickCalendar:
            Button {
                choosingCalendar = true
            } label: {
                connectionLabel(row)
            }
            .buttonStyle(.sqPressable)
            .disabled(working != nil)
            .accessibilityHint("Double-tap to connect a calendar")
        }
    }

    /// "Google Calendar ··· Connected" (15pt name, 14 Semibold status in its color).
    private func connectionLabel(_ row: AccountConnection, working: Bool = false, check: Bool = false) -> some View {
        HStack(spacing: 10) {
            Text(row.name)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
            Spacer(minLength: 8)
            ZStack(alignment: .trailing) {
                HStack(spacing: 5) {
                    if check {
                        AnimatedCheck(lineWidth: 2.8, delay: 0.15)
                            .frame(width: 13, height: 13)
                            .transition(.opacity)
                    }
                    Text(row.status)
                        .sqFont(14, .semibold)
                }
                .foregroundStyle(row.color)
                .opacity(working ? 0 : 1)
                if working {
                    LoadingDots(color: row.color, dotSize: 5)
                        .sqTransition(.pop)
                }
            }
        }
        .authLineHeight(1.35, size: 15)
        .padding(.vertical, 13)
        .padding(.horizontal, 14)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(working ? "In progress" : "")
    }

    private var disconnectTitle: String {
        confirmDisconnect.map { "Disconnect \($0.name)?" } ?? ""
    }

    private var disconnectShown: Binding<Bool> {
        Binding(get: { confirmDisconnect != nil }, set: { if !$0 { confirmDisconnect = nil } })
    }

    // MARK: Past sidequests

    private var historyCard: some View {
        SetupCard {
            AuthLoadable(state: model.history, shimmers: onScreen, retry: { Task { await model.loadHistory(env) } }) {
                AccountHistorySkeleton()
            } content: { events in
                VStack(spacing: 0) {
                    if events.isEmpty {
                        EmptyStateView(message: "Rate a sidequest you went on and it shows up here.", minHeight: 96)
                    } else {
                        ForEach(Array(events.enumerated()), id: \.element.id) { index, event in
                            if index > 0 { RowDivider() }
                            AccountHistoryRow(event: event, when: whenLine(event))
                                .authArrive(index, animated: animateHistory)
                                .sqTransition(.rise)
                        }
                    }
                }
                .onAppear { animateHistory = false }
            }
        }
        .padding(.horizontal, Metrics.side)
    }

    /// "Sep 19 · with 2 others"
    private func whenLine(_ event: PastEvent) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.timeZone = env.clock.timeZone
        formatter.setLocalizedDateFormatFromTemplate("MMMd")
        return "\(formatter.string(from: event.date)) · \(event.company)"
    }

    // MARK: Sign out

    private var signOutButton: some View {
        Button {
            withMotion(Motion.quick) { signingOut = true }
            Task {
                await env.signOut()
                router.signedOut()
            }
        } label: {
            ZStack {
                Text("Sign out").opacity(signingOut ? 0 : 1)
                if signingOut {
                    LoadingDots(color: Theme.danger)
                        .sqTransition(.pop)
                }
            }
        }
        .buttonStyle(.sq(fill: .white, foreground: Theme.danger, height: 48, fontSize: 16))
        .disabled(signingOut)
        .accessibilityLabel("Sign out")
        .accessibilityValue(signingOut ? "In progress" : "")
    }

    // MARK: Calendars

    /// Connects (OAuth via the backend) or disconnects a calendar, then reloads the list: the status
    /// shows `LoadingDots` meanwhile and "Connected" gets a check that draws in.
    private func toggle(_ provider: CalendarProvider, connect: Bool) {
        withMotion(Motion.quick) {
            working = provider
            linkError = nil
            justConnected = nil
            touched.insert(provider)
        }
        Task {
            do {
                if connect {
                    try await CalendarConnector.connect(provider, env: env)
                } else {
                    try await env.api.disconnectIntegration(provider)
                }
                let integrations = try await env.api.integrations()
                withMotion(Motion.arrive) {
                    let cards = model.links.value?.cards ?? []
                    model.links = .loaded(AccountLinks(integrations: integrations, cards: cards))
                    working = nil
                    if connect { justConnected = provider }
                }
            } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
                // Closed the sign-in sheet: nothing changed.
                withMotion(Motion.quick) { working = nil }
            } catch {
                withMotion {
                    working = nil
                    linkError = authMessage(for: error, fallback: connect ? "Couldn't connect \(provider.name). Try again."
                                                                            : "Couldn't disconnect \(provider.name). Try again.")
                }
            }
        }
    }
}

// MARK: - Rows

/// "Outdoors" in an 84pt column + an 8pt sage bar that grows from zero when it first loads.
private struct AccountTasteBarRow: View {
    let bar: TasteBar
    let index: Int
    @State private var grown: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(bar: TasteBar, index: Int, grows: Bool) {
        self.bar = bar
        self.index = index
        _grown = State(initialValue: !grows)
    }

    var body: some View {
        HStack(spacing: 10) {
            Text(bar.label)
                .sqFont(14)
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .frame(width: 84, alignment: .leading)
                .authLineHeight(1.35, size: 14)
            GeometryReader { proxy in
                Rectangle()
                    .fill(Theme.sage)
                    .frame(width: proxy.size.width * (grown ? min(max(bar.value, 0), 1) : 0))
            }
            .frame(height: 8)
            .background(Theme.line)
            .clipShape(Capsule())
        }
        .onAppear {
            guard !grown else { return }
            if reduceMotion {
                grown = true
            } else {
                withAnimation(Motion.gentle.delay(0.12 + Motion.stagger(index, step: 0.06))) { grown = true }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(bar.label)
        .accessibilityValue("\(Int((bar.value * 100).rounded())) percent")
    }
}

/// A rated past sidequest: title, "Sep 19 · with 2 others", 14pt stars.
private struct AccountHistoryRow: View {
    let event: PastEvent
    let when: String

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 0) {
                Text(event.title)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Text(when)
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            StarRow(rating: event.rating?.stars ?? 0, size: 14, spacing: 2)
        }
        .padding(.vertical, 13)
        .padding(.horizontal, 14)
        .accessibilityElement(children: .combine)
    }
}

/// One "Connected" row: name, status and its color; calendar rows can be tapped.
struct AccountConnection: Identifiable, Hashable {
    enum Action: Hashable {
        case none
        /// Tap to connect, or to disconnect (after confirming).
        case calendar(CalendarProvider)
        /// No calendar yet: tap to pick one to connect.
        case pickCalendar
    }

    let name: String
    let status: String
    let color: Color
    var action: Action = .none
    var connected = false
    var id: String { name }

    /// Calendars and the card come from the server; location and voice describe how the app works.
    /// `keep`: calendars to list even when not connected (just disconnected here).
    static func rows(integrations: [Integration], cards: [PaymentMethod], keep: Set<CalendarProvider> = []) -> [AccountConnection] {
        var rows = integrations.filter { $0.connected || keep.contains($0.provider) }.map {
            AccountConnection(name: $0.provider.name, status: $0.connected ? "Connected" : "Not connected",
                              color: $0.connected ? Theme.success : Theme.text2, action: .calendar($0.provider),
                              connected: $0.connected)
        }
        if rows.isEmpty {
            rows.append(AccountConnection(name: "Calendar", status: "Not connected", color: Theme.text2, action: .pickCalendar))
        }
        rows.append(AccountConnection(name: "Location", status: "While planning", color: Theme.text2))
        if let card = cards.first(where: \.isDefault) ?? cards.first {
            rows.append(AccountConnection(name: "\(card.brand) •••• \(card.last4)", status: "Agent checkout on", color: Theme.success))
        } else {
            rows.append(AccountConnection(name: "Card", status: "Not added", color: Theme.text2))
        }
        rows.append(AccountConnection(name: "Voice input", status: "On", color: Theme.success))
        return rows
    }
}

// MARK: - Skeletons

/// Five label + bar rows, like the taste profile (18.9pt rows, 10pt apart).
private struct AccountTasteSkeleton: View {
    private let labels: [CGFloat] = [64, 40, 28, 50, 70]
    private let bars: [CGFloat] = [0.8, 0.68, 0.52, 0.62, 0.28]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(0..<5, id: \.self) { index in
                HStack(spacing: 10) {
                    SkeletonBlock(width: labels[index], height: 12)
                        .frame(width: 84, alignment: .leading)
                    GeometryReader { proxy in
                        SkeletonBlock(width: proxy.size.width * bars[index], height: 8, radius: 4)
                    }
                    .frame(height: 8)
                }
                .frame(height: 14 * 1.35)
            }
        }
    }
}

/// Four name ··· status rows, like the Connected card.
private struct AccountLinksSkeleton: View {
    private let widths: [(CGFloat, CGFloat)] = [(120, 74), (66, 96), (104, 118), (84, 24)]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<widths.count, id: \.self) { index in
                if index > 0 { RowDivider() }
                HStack {
                    SkeletonBlock(width: widths[index].0, height: 12)
                    Spacer(minLength: 8)
                    SkeletonBlock(width: widths[index].1, height: 12)
                }
                .frame(height: 15 * 1.35)
                .padding(.vertical, 13)
                .padding(.horizontal, 14)
            }
        }
    }
}

/// Three title + date rows with a row of stars, like Past sidequests.
private struct AccountHistorySkeleton: View {
    private let widths: [(CGFloat, CGFloat)] = [(150, 104), (124, 92), (164, 110)]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<widths.count, id: \.self) { index in
                if index > 0 { RowDivider() }
                HStack(spacing: 10) {
                    VStack(alignment: .leading, spacing: 9) {
                        SkeletonBlock(width: widths[index].0, height: 12)
                        SkeletonBlock(width: widths[index].1, height: 10)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    SkeletonBlock(width: 78, height: 12)
                }
                .frame(height: 15 * 1.35 + 12 * 1.35)
                .padding(.vertical, 13)
                .padding(.horizontal, 14)
            }
        }
    }
}

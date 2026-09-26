import AuthenticationServices
import Observation
import SwiftUI
import UIKit

/// Account › Me data. Owned by `AccountView` so it survives Me ⇄ Friends switches; every refresh
/// keeps what's on screen and swaps the fresh data in with animation.
@Observable
final class AccountMeModel {
    var taste: Loadable<TasteProfile> = .loading
    var integrations: Loadable<[Integration]> = .loading
    var cards: Loadable<[PaymentMethod]> = .loading
    var history: Loadable<[PastEvent]> = .loading
    /// Cards being removed here: a reload meanwhile must not bring them back.
    var removingCards: Set<String> = []
    /// Location and voice input permissions (read from the phone, not the server).
    let permissions = AccountPermissions()
    /// Facebook: the Connected row and the Facebook sheet.
    let facebook = FacebookModel()
    var showsFacebook = false

    /// Loads (or quietly reloads) every card in parallel.
    func load(_ env: AppEnvironment) async {
        async let t: Void = loadTaste(env)
        async let i: Void = loadIntegrations(env)
        async let c: Void = loadCards(env)
        async let h: Void = loadHistory(env)
        async let p: Void = loadPreferencesIfNeeded(env)
        async let f: Void = facebook.load(env)
        _ = await (t, i, c, h, p, f)
    }

    func loadTaste(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.tasteProfile() }
        // Keep what's on screen if a background refresh fails.
        guard result.value != nil || taste.value == nil else { return }
        withMotion(Motion.gentle) { taste = result }
    }

    func loadIntegrations(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.integrations() }
        guard result.value != nil || integrations.value == nil else { return }
        withMotion { integrations = result }
    }

    func loadCards(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.paymentMethods() }
        guard let list = result.value else {
            if cards.value == nil { withMotion { cards = result } }
            return
        }
        withMotion { cards = .loaded(list.filter { !removingCards.contains($0.id) }) }
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

    /// Instant checkout lives in the preferences (normally loaded with the session).
    func loadPreferencesIfNeeded(_ env: AppEnvironment) async {
        guard env.preferences == nil, let saved = try? await env.api.preferences() else { return }
        if env.preferences == nil { env.preferences = saved }
    }
}

/// Account › Me: taste profile, home base (when the account has one), connected services,
/// payments, rated past sidequests, sign out. No stats row and no app-color option (GUI_PLAN.md §7.10).
///
/// First load: each card shows a skeleton of its own rows, then the rows arrive (taste bars grow
/// from zero, one after another). Calendars connect and disconnect from the Connected card, which
/// also shows the phone's real Location and voice permissions (tap to allow, or to open Settings).
struct AccountMeSection: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.openURL) private var openURL
    @Environment(\.scenePhase) private var scenePhase
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
    /// Calendars touched here stay listed (as "Connect" after a disconnect) so you can undo.
    @State private var touched: Set<CalendarProvider> = []
    @State private var confirmDisconnect: CalendarProvider?
    @State private var choosingCalendar = false
    @State private var linkError: String?

    init(model: AccountMeModel) {
        self.model = model
        _animateTaste = State(initialValue: model.taste.value == nil)
        _animateLinks = State(initialValue: model.integrations.value == nil)
        _animateHistory = State(initialValue: model.history.value == nil)
    }

    /// Skeletons only shimmer while Account is the visible tab.
    private var onScreen: Bool { router.tab == .account }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            sectionTitle("Taste profile")
            tasteCard

            if let home = env.user?.homeBase {
                sectionTitle("Home base")
                homeBaseCard(home)
            }

            sectionTitle("Connected")
            connectionsCard

            sectionTitle("Payments")
            AccountPaymentsCard(model: model, shimmers: onScreen)
                .padding(.horizontal, Metrics.side)

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
            model.permissions.start()
            await model.load(env)
        }
        .sqReloadable("account.me") {
            model.permissions.refresh()
            await model.load(env)
        }
        // Back from Settings (or a system prompt): show what changed.
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { withMotion { model.permissions.refresh() } }
        }
        .confirmationDialog(disconnectTitle, isPresented: disconnectShown, titleVisibility: .visible,
                            presenting: confirmDisconnect) { provider in
            Button("Disconnect", role: .destructive) { toggle(provider, connect: false) }
        } message: { _ in
            Text("We'll stop reading your free/busy times.")
        }
        .confirmationDialog("Connect a calendar", isPresented: $choosingCalendar, titleVisibility: .visible) {
            ForEach(CalendarProvider.allCases) { provider in
                Button(provider.name) { toggle(provider, connect: true) }
            }
        }
        .sqSheet(isPresented: facebookShown, style: FacebookSheet.style, onDismiss: facebookClosed) {
            FacebookSheet(model: model.facebook) { model.showsFacebook = false }
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
        VStack(alignment: .leading, spacing: 14) {
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
            Button("Redo setup questions") { router.setupRedo = SetupEntry(step: 3) }
                .buttonStyle(.sqTintPill)
                .authHitHeight(32)
        }
        .padding(Metrics.cardPadding)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .padding(.horizontal, Metrics.side)
    }

    // MARK: Home base

    /// Where plans start and the Forum looks first (set by the server): a pin, the place, and the
    /// city it plans in. Nothing to tap; there's no way to move it from the app yet.
    private func homeBaseCard(_ home: Place) -> some View {
        HStack(spacing: 12) {
            SocialGlyph(kind: .pin, size: 20, lineWidth: 2)
                .foregroundStyle(Theme.sageInk)
            VStack(alignment: .leading, spacing: 0) {
                Text(home.name)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                if let city = env.user?.cityLabel {
                    Text(city)
                        .sqFont(12)
                        .foregroundStyle(Theme.text3)
                        .authLineHeight(1.35, size: 12)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 13)
        .padding(.horizontal, 14)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .padding(.horizontal, Metrics.side)
        .accessibilityElement(children: .combine)
    }

    // MARK: Connected

    private var connectionsCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            SetupCard {
                AuthLoadable(state: model.integrations, shimmers: onScreen, retry: { Task { await model.loadIntegrations(env) } }) {
                    AccountLinksSkeleton()
                } content: { integrations in
                    let rows = connectedRows(integrations)
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
            if let message = linkError ?? (model.showsFacebook ? nil : model.facebook.message) {
                AuthErrorText(message: message)
                    .padding(.horizontal, 4)
                    .sqTransition(.rise)
            }
        }
        .padding(.horizontal, Metrics.side)
    }

    /// Every row of the Connected card, in order. To add a connection, append its row here.
    private func connectedRows(_ integrations: [Integration]) -> [AccountConnection] {
        var rows = integrations.filter { $0.connected || touched.contains($0.provider) }.map(calendarRow)
        if rows.isEmpty {
            rows.append(AccountConnection(id: "calendar", name: "Calendar", status: "Connect", color: Theme.sageInk,
                                          action: { choosingCalendar = true }, hint: "Double-tap to connect a calendar",
                                          disabled: working != nil))
        }
        rows.append(facebookRow())
        rows.append(permissionRow(.location, name: "Location", state: model.permissions.location))
        rows.append(permissionRow(.voice, name: "Voice input", state: model.permissions.voice))
        return rows
    }

    private func calendarRow(_ integration: Integration) -> AccountConnection {
        let provider = integration.provider
        let connected = integration.connected
        return AccountConnection(
            id: provider.rawValue, name: provider.name,
            status: connected ? "Connected" : "Connect",
            color: connected ? Theme.success : Theme.sageInk,
            action: {
                if connected { confirmDisconnect = provider } else { toggle(provider, connect: true) }
            },
            hint: connected ? "Double-tap to disconnect" : "Double-tap to connect",
            working: working == provider,
            check: justConnected == provider && connected,
            disabled: working != nil)
    }

    /// Allowed: green, tap opens Settings to change it. Never asked: "Turn on" shows the system
    /// prompt. Turned off: "Turn on in Settings". Restricted: just says so.
    private func permissionRow(_ kind: AccountPermissions.Kind, name: String, state: AccountPermissionState) -> AccountConnection {
        let tap = { model.permissions.handleTap(kind, openSettings: openSettings) }
        let working = model.permissions.asking == kind
        switch state {
        case .allowed(let how):
            return AccountConnection(id: name, name: name, status: how, color: Theme.success, action: tap,
                                     hint: "Opens Settings to change it", working: working)
        case .notAsked:
            return AccountConnection(id: name, name: name, status: "Turn on", color: Theme.sageInk, action: tap,
                                     hint: "Asks for permission", working: working)
        case .denied:
            return AccountConnection(id: name, name: name, status: "Turn on in Settings", color: Theme.sageInk, action: tap,
                                     hint: "Opens Settings", working: working)
        case .restricted:
            return AccountConnection(id: name, name: name, status: "Restricted", color: Theme.text2)
        }
    }

    /// Facebook: not connected → Facebook's page, the import, then the sheet with what was found;
    /// connected (or asking for a new sign-in) → the sheet.
    private func facebookRow() -> AccountConnection {
        let facebook = model.facebook
        let (status, color): (String, Color) = switch facebook.connection {
        case .loading: ("", Theme.text3)
        case .failed: ("Try again", Theme.sageInk)
        case .loaded(let connection):
            connection.needsReconnect ? ("Reconnect", Theme.sageInk)
                : connection.connected ? ("Connected", Theme.success) : ("Connect", Theme.sageInk)
        }
        return AccountConnection(
            id: "facebook", name: "Facebook", status: status, color: color,
            action: {
                switch facebook.connection {
                case .failed:
                    Task { await facebook.load(env) }
                case .loaded(let connection) where connection.connected:
                    model.showsFacebook = true
                default:
                    Task {
                        if await facebook.connect(env) { model.showsFacebook = true }
                    }
                }
            },
            hint: facebook.isConnected ? "Shows what was imported" : "Fills in your likes from Pages you like",
            working: facebook.connection.isLoading || facebook.work == .connecting,
            disabled: facebook.work != nil)
    }

    private var facebookShown: Binding<Bool> {
        Binding(get: { model.showsFacebook }, set: { model.showsFacebook = $0 })
    }

    /// Errors from the sheet stay in the sheet; new likes may change the taste profile.
    private func facebookClosed() {
        model.facebook.message = nil
        if model.facebook.applied { Task { await model.loadTaste(env) } }
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        openURL(url)
    }

    @ViewBuilder private func connectionRow(_ row: AccountConnection) -> some View {
        if let action = row.action {
            Button(action: action) {
                connectionLabel(row)
            }
            .buttonStyle(.sqPressable)
            .disabled(row.disabled || row.working)
            .accessibilityHint(row.hint ?? "")
        } else {
            connectionLabel(row)
        }
    }

    /// "Google Calendar ··· Connected" (15pt name, 14 Semibold status in its color).
    private func connectionLabel(_ row: AccountConnection) -> some View {
        HStack(spacing: 10) {
            Text(row.name)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
            Spacer(minLength: 8)
            ZStack(alignment: .trailing) {
                HStack(spacing: 5) {
                    if row.check {
                        AnimatedCheck(lineWidth: 2.8, delay: 0.15)
                            .frame(width: 13, height: 13)
                            .transition(.opacity)
                    }
                    Text(row.status)
                        .sqFont(14, .semibold)
                        .contentTransition(.opacity)
                }
                .foregroundStyle(row.color)
                .opacity(row.working ? 0 : 1)
                if row.working {
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
        .accessibilityValue(row.working ? "In progress" : "")
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
                    model.integrations = .loaded(integrations)
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

/// One row of Account › Connected: a name, its status on the right, and what a tap does.
struct AccountConnection: Identifiable {
    let id: String
    let name: String
    let status: String
    var color: Color = Theme.text2
    /// nil: the row isn't tappable.
    var action: (() -> Void)? = nil
    /// VoiceOver hint for the tap.
    var hint: String? = nil
    /// Its request (or system prompt) is running: the status turns into dots.
    var working = false
    /// Just connected here: a check draws in before the status.
    var check = false
    var disabled = false
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

/// Three name ··· status rows, like the Connected card.
private struct AccountLinksSkeleton: View {
    private let widths: [(CGFloat, CGFloat)] = [(120, 74), (66, 96), (84, 24)]

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

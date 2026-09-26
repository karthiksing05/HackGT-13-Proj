import SwiftUI

/// Account › Me: taste profile, connected services, rated past sidequests, sign out.
/// No stats row and no app-color option (GUI_PLAN.md §7.10).
struct AccountMeSection: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var taste: Loadable<TasteProfile> = .loading
    @State private var connections: Loadable<[AccountConnection]> = .loading
    @State private var history: Loadable<[PastEvent]> = .loading
    @State private var signingOut = false

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
            await load()
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
            LoadableView(state: taste, minHeight: 120, retry: { Task { await loadTaste() } }) { profile in
                if profile.bars.isEmpty {
                    Text("Rate a few sidequests and your taste profile shows up here.")
                        .sqFont(14)
                        .foregroundStyle(Theme.text3)
                } else {
                    ForEach(profile.bars) { AccountTasteBarRow(bar: $0) }
                }
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
        SetupCard {
            LoadableView(state: connections, minHeight: 120, retry: { Task { await loadConnections() } }) { rows in
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                    if index > 0 { RowDivider() }
                    HStack(spacing: 10) {
                        Text(row.name)
                            .sqFont(15)
                            .foregroundStyle(Theme.ink)
                        Spacer(minLength: 8)
                        Text(row.status)
                            .sqFont(14, .semibold)
                            .foregroundStyle(row.color)
                    }
                    .authLineHeight(1.35, size: 15)
                    .padding(.vertical, 13)
                    .padding(.horizontal, 14)
                    .accessibilityElement(children: .combine)
                }
            }
        }
        .padding(.horizontal, Metrics.side)
    }

    // MARK: Past sidequests

    private var historyCard: some View {
        SetupCard {
            LoadableView(state: history, minHeight: 120, retry: { Task { await loadHistory() } }) { events in
                if events.isEmpty {
                    EmptyStateView(message: "Rate a sidequest you went on and it shows up here.", minHeight: 96)
                } else {
                    ForEach(Array(events.enumerated()), id: \.element.id) { index, event in
                        if index > 0 { RowDivider() }
                        AccountHistoryRow(event: event, when: whenLine(event))
                    }
                }
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
            signingOut = true
            Task {
                await env.signOut()
                signingOut = false
                router.signedOut()
            }
        } label: {
            ZStack {
                Text("Sign out").opacity(signingOut ? 0 : 1)
                if signingOut { ProgressView().tint(Theme.danger) }
            }
        }
        .buttonStyle(.sq(fill: .white, foreground: Theme.danger, height: 48, fontSize: 16))
        .disabled(signingOut)
    }

    // MARK: Loading

    private func load() async {
        async let t: Void = loadTaste()
        async let c: Void = loadConnections()
        async let h: Void = loadHistory()
        _ = await (t, c, h)
    }

    private func loadTaste() async {
        let result = await Loadable.run { try await env.api.tasteProfile() }
        // Keep what's on screen if a background refresh fails.
        if result.value != nil || taste.value == nil { taste = result }
    }

    private func loadConnections() async {
        let result = await Loadable.run {
            async let integrations = env.api.integrations()
            async let cards = env.api.paymentMethods()
            return AccountConnection.rows(integrations: try await integrations, cards: try await cards)
        }
        if result.value != nil || connections.value == nil { connections = result }
    }

    private func loadHistory() async {
        let result = await Loadable.run {
            try await env.api.pastEvents(unratedOnly: false)
                .filter { $0.rating != nil }
                .sorted { $0.date > $1.date }
                .prefix(3)
                .map { $0 }
        }
        if result.value != nil || history.value == nil { history = result }
    }
}

// MARK: - Rows

/// "Outdoors" in an 84pt column + an 8pt sage bar.
private struct AccountTasteBarRow: View {
    let bar: TasteBar

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
                    .frame(width: proxy.size.width * min(max(bar.value, 0), 1))
            }
            .frame(height: 8)
            .background(Theme.line)
            .clipShape(Capsule())
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

/// One "Connected" row: name, status and its color.
struct AccountConnection: Identifiable, Hashable {
    let name: String
    let status: String
    let color: Color
    var id: String { name }

    /// Calendars and the card come from the server; location and voice describe how the app works.
    static func rows(integrations: [Integration], cards: [PaymentMethod]) -> [AccountConnection] {
        var rows = integrations.filter(\.connected).map {
            AccountConnection(name: $0.provider.name, status: "Connected", color: Theme.success)
        }
        if rows.isEmpty {
            rows.append(AccountConnection(name: "Calendar", status: "Not connected", color: Theme.text2))
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

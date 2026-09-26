import AuthenticationServices
import SwiftUI

/// Setup · 2 "Connect your calendar": Google / Outlook rows (free/busy only), via the backend's
/// OAuth (`CalendarConnector`), then the list is reloaded from `GET /integrations`.
///
/// Loading: two skeleton rows shaped like the real ones. Connect: the pill shows `LoadingDots` while
/// the connection and the reload run, then turns into "Connected" with its check drawing in.
struct SetupConnectStep: View {
    @Environment(AppEnvironment.self) private var env
    @Bindable var draft: SetupDraft
    @State private var working: CalendarProvider?
    @State private var actionError: String?
    /// Rows rise in when the list arrives while this step is on screen (not when coming back to it).
    @State private var animateRows: Bool

    init(draft: SetupDraft) {
        _draft = Bindable(wrappedValue: draft)
        _animateRows = State(initialValue: draft.integrations.value == nil)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "Connect your calendar",
                         subtitle: "We only read when you're busy or free, so we can plan around your schedule and spot gaps.")
            SetupCard {
                AuthLoadable(state: draft.integrations, minHeight: 136, retry: { Task { await load() } }) {
                    SetupConnectSkeleton()
                } content: { list in
                    VStack(spacing: 0) {
                        ForEach(Array(CalendarProvider.allCases.enumerated()), id: \.element) { index, provider in
                            row(provider, connected: list.first { $0.provider == provider }?.connected ?? false)
                                .authArrive(index, animated: animateRows)
                            RowDivider(color: Theme.cream)
                        }
                    }
                }
            }
            .padding(.top, 8)
            if let actionError {
                ErrorBox(messages: [actionError])
                    .sqTransition(.rise)
            }
            Text("We never post to your calendar or read event details without asking. You can disconnect anytime in Account.")
                .sqFont(13)
                .foregroundStyle(Theme.ink)
                .authLineHeight(1.4, size: 13)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.vertical, 12)
                .padding(.horizontal, 14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .task {
            if draft.integrations.value == nil { await load() }
        }
    }

    private func row(_ provider: CalendarProvider, connected: Bool) -> some View {
        HStack(spacing: 12) {
            CalendarGlyph(size: 22)
                .foregroundStyle(provider.setupTileIcon)
                .frame(width: 40, height: 40)
                .background(provider.setupTileFill, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text(provider.name)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Text(connected ? "Connected · reading free/busy only" : "Not connected")
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            connectButton(provider, connected: connected)
        }
        .padding(14)
    }

    private func connectButton(_ provider: CalendarProvider, connected: Bool) -> some View {
        let isWorking = working == provider
        return Button {
            toggle(provider, connected: connected)
        } label: {
            HStack(spacing: 4) {
                if connected {
                    // Prototype: 14pt check, stroke 2.8 in a 24-unit box — drawn in on connect.
                    AnimatedCheck(lineWidth: 2.8, delay: 0.15)
                        .frame(width: 14, height: 14)
                        .transition(.opacity)
                }
                Text(connected ? "Connected" : "Connect")
            }
            // Hidden (not removed) while working, so the pill keeps its width under the dots.
            .foregroundStyle(isWorking ? Color.clear : connected ? Theme.success : Theme.ink)
            .overlay {
                if isWorking {
                    LoadingDots(color: connected ? Theme.success : Theme.ink, dotSize: 5)
                        .sqTransition(.pop)
                }
            }
        }
        .buttonStyle(.sqPill(fill: connected ? Theme.successBg : Theme.sage,
                             foreground: connected ? Theme.success : Theme.ink))
        .authHitHeight(34)
        .disabled(working != nil)
        .accessibilityLabel(connected ? "\(provider.name) connected" : "Connect \(provider.name)")
        .accessibilityValue(isWorking ? "In progress" : "")
        .accessibilityHint(connected ? "Double-tap to disconnect" : "")
    }

    private func load() async {
        let result = await Loadable.run { try await env.api.integrations() }
        withMotion { draft.integrations = result }
    }

    private func toggle(_ provider: CalendarProvider, connected: Bool) {
        withMotion(Motion.quick) {
            working = provider
            actionError = nil
        }
        Task {
            do {
                if connected {
                    try await env.api.disconnectIntegration(provider)
                } else {
                    try await CalendarConnector.connect(provider, env: env)
                }
                let list = try await env.api.integrations()
                // The pill morphs (dots → "Connected" + check), the subtitle and the footer button's
                // label cross-fade, all together.
                withMotion(Motion.arrive) {
                    draft.integrations = .loaded(list)
                    working = nil
                }
            } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
                // Closed the sign-in sheet: nothing changed.
                withMotion(Motion.quick) { working = nil }
            } catch {
                withMotion {
                    working = nil
                    actionError = authMessage(for: error, fallback: "Couldn't connect \(provider.name). Try again.")
                }
            }
        }
    }
}

/// Two rows shaped like the calendar rows: a 40pt tile, name + status lines, a pill.
private struct SetupConnectSkeleton: View {
    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<2, id: \.self) { index in
                HStack(spacing: 12) {
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .fill(Theme.skeleton)
                        .frame(width: 40, height: 40)
                    VStack(alignment: .leading, spacing: 8) {
                        SkeletonBlock(width: index == 0 ? 128 : 116, height: 13)
                        SkeletonBlock(width: 84, height: 10)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    SkeletonBlock(width: 86, height: 34, radius: 17)
                }
                .padding(14)
                RowDivider(color: Theme.cream)
            }
        }
    }
}

private extension CalendarProvider {
    /// Prototype tile colors (one-offs): Google blue, Outlook blue.
    var setupTileFill: Color {
        switch self {
        case .google: Color(hex: 0xE8F0FE)
        case .outlook: Color(hex: 0xE6F1FA)
        }
    }

    var setupTileIcon: Color {
        switch self {
        case .google: Color(hex: 0x1D4ED8)
        case .outlook: Color(hex: 0x0E5A8A)
        }
    }
}

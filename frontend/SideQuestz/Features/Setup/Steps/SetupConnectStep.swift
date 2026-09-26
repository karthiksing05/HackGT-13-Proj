import AuthenticationServices
import SwiftUI

/// Setup · 2 "Connect your calendar": Google / Outlook rows (free/busy only), via the backend's
/// OAuth (`CalendarConnector`), then the list is reloaded from `GET /integrations`.
struct SetupConnectStep: View {
    @Environment(AppEnvironment.self) private var env
    @Bindable var draft: SetupDraft
    @State private var working: CalendarProvider?
    @State private var actionError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "Connect your calendar",
                         subtitle: "We only read when you're busy or free, so we can plan around your schedule and spot gaps.")
            SetupCard {
                LoadableView(state: draft.integrations, minHeight: 136, retry: { Task { await load() } }) { list in
                    ForEach(CalendarProvider.allCases) { provider in
                        row(provider, connected: list.first { $0.provider == provider }?.connected ?? false)
                        RowDivider(color: Theme.cream)
                    }
                }
            }
            .padding(.top, 8)
            if let actionError {
                ErrorBox(messages: [actionError])
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
        Button {
            toggle(provider, connected: connected)
        } label: {
            HStack(spacing: 4) {
                if working == provider {
                    ProgressView().controlSize(.small).tint(connected ? Theme.success : Theme.ink)
                } else if connected {
                    // Prototype: 14pt check, stroke 2.8 in a 24-unit box.
                    CheckGlyph(lineWidth: 2.8).frame(width: 14, height: 14)
                }
                Text(connected ? "Connected" : "Connect")
            }
        }
        .buttonStyle(.sqPill(fill: connected ? Theme.successBg : Theme.sage,
                             foreground: connected ? Theme.success : Theme.ink))
        .authHitHeight(34)
        .disabled(working != nil)
        .accessibilityLabel(connected ? "\(provider.name) connected" : "Connect \(provider.name)")
        .accessibilityHint(connected ? "Double-tap to disconnect" : "")
    }

    private func load() async {
        draft.integrations = await .run { try await env.api.integrations() }
    }

    private func toggle(_ provider: CalendarProvider, connected: Bool) {
        working = provider
        actionError = nil
        Task {
            defer { working = nil }
            do {
                if connected {
                    try await env.api.disconnectIntegration(provider)
                } else {
                    try await CalendarConnector.connect(provider, env: env)
                }
                draft.integrations = .loaded(try await env.api.integrations())
            } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
                // Closed the sign-in sheet: nothing changed.
            } catch {
                actionError = authMessage(for: error, fallback: "Couldn't connect \(provider.name). Try again.")
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

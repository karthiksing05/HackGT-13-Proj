import SwiftUI

/// Facebook's colors (one-offs, like the calendar tiles' Google and Outlook blues).
enum FacebookBrand {
    static let blue = Color(hex: 0x0866FF)
    static let tint = Color(hex: 0xE7F0FF)
}

/// Facebook's "f" in its blue on a light-blue tile, the size of the Setup calendar tiles.
struct FacebookGlyph: View {
    var size: CGFloat = 40

    var body: some View {
        Text("f")
            .font(.system(size: size * 0.6, weight: .heavy, design: .rounded))
            .foregroundStyle(FacebookBrand.blue)
            .offset(y: size * 0.03)
            .frame(width: size, height: size)
            .background(FacebookBrand.tint, in: RoundedRectangle(cornerRadius: size / 4, style: .continuous))
            .accessibilityHidden(true)
    }
}

// MARK: - Setup › Connect

/// Setup · 2, under the calendars: "FILL IN YOUR LIKES" · Facebook. Connect opens Facebook's page and
/// imports right away; Setup then fills the likes you haven't rated on the next step. Tapping
/// "Connected" disconnects (and Setup takes back what it filled that you haven't changed).
struct FacebookSetupSection: View {
    @Environment(AppEnvironment.self) private var env
    /// The import made during this setup (nil: not connected here).
    let imported: FacebookImport?
    let onImported: (FacebookImport) -> Void
    let onDisconnected: () -> Void

    @State private var working = false
    @State private var error: String?

    private var connected: Bool { imported != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupEyebrow(text: "FILL IN YOUR LIKES")
                .padding(.top, 6)
            SetupCard { row }
            if let error {
                ErrorBox(messages: [error])
                    .sqTransition(.rise)
            }
            Text("Reads Pages you like and your city. Never posts.")
                .sqFont(13)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 13)
                .fixedSize(horizontal: false, vertical: true)
        }
        .task { await restore() }
    }

    private var row: some View {
        HStack(spacing: 12) {
            FacebookGlyph()
            VStack(alignment: .leading, spacing: 0) {
                Text("Facebook")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Text(subtitle)
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
                    .contentTransition(.opacity)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            pill
        }
        .padding(14)
    }

    private var subtitle: String {
        guard let imported else { return "Not connected" }
        // The pill already says "Connected".
        return imported.likedPages == 1 ? "1 liked Page" : "\(imported.likedPages) liked Pages"
    }

    /// The same pill as the calendar rows: dots while working, then "Connected" with a check.
    private var pill: some View {
        Button(action: toggle) {
            HStack(spacing: 4) {
                if connected {
                    AnimatedCheck(lineWidth: 2.8, delay: 0.15)
                        .frame(width: 14, height: 14)
                        .transition(.opacity)
                }
                Text(connected ? "Connected" : "Connect")
            }
            // Hidden (not removed) while working, so the pill keeps its width under the dots.
            .foregroundStyle(working ? Color.clear : connected ? Theme.success : Theme.ink)
            .overlay {
                if working {
                    LoadingDots(color: connected ? Theme.success : Theme.ink, dotSize: 5)
                        .sqTransition(.pop)
                }
            }
        }
        .buttonStyle(.sqPill(fill: connected ? Theme.successBg : Theme.sage,
                             foreground: connected ? Theme.success : Theme.ink))
        .authHitHeight(34)
        .disabled(working)
        .accessibilityLabel(connected ? "Facebook connected" : "Connect Facebook")
        .accessibilityValue(working ? "In progress" : "")
        .accessibilityHint(connected ? "Double-tap to disconnect" : "Fills in your likes from Pages you like")
    }

    /// A setup that was left and resumed may already have Facebook connected: use its last import.
    private func restore() async {
        guard imported == nil,
              let connection = try? await env.api.facebookConnection(),
              connection.connected, !connection.needsReconnect, let last = connection.lastImport,
              imported == nil, !working else { return }
        withMotion(Motion.arrive) { onImported(last) }
    }

    private func toggle() {
        withMotion(Motion.quick) {
            working = true
            error = nil
        }
        Task {
            do {
                if connected {
                    try await env.api.disconnectFacebook()
                    withMotion(Motion.arrive) {
                        onDisconnected()
                        working = false
                    }
                } else if let result = try await FacebookConnector.connect(env: env) {
                    // The pill morphs (dots → "Connected" + check) and the subtitle cross-fades.
                    withMotion(Motion.arrive) {
                        onImported(result)
                        working = false
                    }
                } else {
                    // Closed Facebook's page or said no there: nothing changed.
                    withMotion(Motion.quick) { working = false }
                }
            } catch {
                withMotion {
                    working = false
                    self.error = authMessage(for: error, fallback: connected ? "Couldn't disconnect Facebook. Try again."
                                                                               : "Couldn't connect Facebook. Try again.")
                }
            }
        }
    }
}

// MARK: - Setup › What do you enjoy?

/// Setup · 3, after a Facebook import filled some likes: "Filled in from Facebook. Change anything."
struct FacebookFilledNote: View {
    var body: some View {
        HStack(spacing: 10) {
            FacebookGlyph(size: 24)
            Text("Filled in from Facebook. Change anything.")
                .sqFont(13, .semibold)
                .foregroundStyle(Theme.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .accessibilityElement(children: .combine)
    }
}

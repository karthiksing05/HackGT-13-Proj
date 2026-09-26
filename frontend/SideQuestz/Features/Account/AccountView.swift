import SwiftUI

/// Account tab (GUI_PLAN.md §7.10): profile header, your status, then Me | Friends.
struct AccountView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @State private var showPhoto = false

    var body: some View {
        @Bindable var router = router
        ScrollView {
            VStack(spacing: 0) {
                AccountHeader { showPhoto = true }
                    .padding(.horizontal, Metrics.side)
                    .designTopPadding(60)
                    .padding(.bottom, 8)
                AccountStatusCard()
                    .padding(.top, 12)
                    .padding(.horizontal, Metrics.side)
                SQSegmentedControl(selection: $router.accountSegment,
                                   options: [(.me, "Me"), (.friends, "Friends")],
                                   accessibilityLabel: "Account sections")
                    .padding(.top, 12)
                    .padding(.horizontal, Metrics.side)
                switch router.accountSegment {
                case .me:
                    AccountMeSection()
                case .friends:
                    FriendsView()
                }
            }
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Theme.cream.ignoresSafeArea())
        .sqSheet(isPresented: $showPhoto) {
            PhotoSheet(initials: env.user?.initials ?? "JL", color: avatarColor) { showPhoto = false }
        }
        .task { await openLaunchRoute() }
    }

    private var avatarColor: Binding<AvatarColor> {
        Binding(get: { env.user?.avatarColor ?? .ink }, set: { env.user?.avatarColor = $0 })
    }

    /// `-SQRoute account/photo` opens the Photo sheet (the router already picked the segment).
    private func openLaunchRoute() async {
        guard let parts = router.consumeLaunch("account"), parts.first == "photo" else { return }
        try? await Task.sleep(nanoseconds: 350_000_000)
        showPhoto = true
    }
}

// MARK: - Header

/// Avatar 84 with a 20pt status dot, "Edit photo" pill, name, "@handle · school".
private struct AccountHeader: View {
    @Environment(AppEnvironment.self) private var env
    let onEditPhoto: () -> Void

    var body: some View {
        VStack(spacing: 8) {
            CurrentUserAvatar(size: 84, statusSize: 20, statusBorderWidth: 3, statusOffset: -2)
                .accessibilityElement()
                .accessibilityLabel("Your photo, status \(env.user?.status.label ?? PresenceStatus.open.label)")
            Button(action: onEditPhoto) {
                HStack(spacing: 5) {
                    Image(systemName: "camera").font(.system(size: 11.5, weight: .medium))
                    Text("Edit photo")
                }
            }
            .buttonStyle(.sqPill(fill: .white, foreground: Theme.sageInk, height: 30, fontSize: 13, horizontalPadding: 12))
            .authHitHeight(30)
            Text(env.user?.name ?? "")
                .sqFont(24, .bold)
                .foregroundStyle(Theme.ink)
                .authLineHeight(1.35, size: 24)
                .accessibilityAddTraits(.isHeader)
            if !handleLine.isEmpty {
                Text(handleLine)
                    .sqFont(14)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 14)
            }
        }
        .frame(maxWidth: .infinity)
    }

    private var handleLine: String {
        [env.user?.username.map { "@\($0)" }, env.user?.school].compactMap { $0 }.joined(separator: " · ")
    }
}

// MARK: - Your status

/// Open / Online / Not free (selected: white + 2pt ring in the dot color) and what it means.
private struct AccountStatusCard: View {
    @Environment(AppEnvironment.self) private var env

    private var current: PresenceStatus { env.user?.status ?? .open }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("YOUR STATUS")
                .sqFont(12, .bold, relativeTo: .caption)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
                .accessibilityAddTraits(.isHeader)
            HStack(spacing: 8) {
                ForEach(PresenceStatus.allCases) { status in
                    button(status)
                }
            }
            Text(current.description)
                .sqFont(13)
                .foregroundStyle(Theme.text2)
                .authLineHeight(1.35, size: 13)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .sensoryFeedback(.selection, trigger: current)
    }

    private func button(_ status: PresenceStatus) -> some View {
        let selected = status == current
        return Button {
            guard !selected else { return }
            Task { await env.setStatus(status) }
        } label: {
            HStack(spacing: 6) {
                StatusDot(color: status.color, size: 10)
                Text(status.label)
                    .sqFont(14, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
            }
            .frame(maxWidth: .infinity)
            .frame(height: 40)
            .background(selected ? Color.white : Theme.cream, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay {
                if selected {
                    // box-shadow 0 0 0 2px: a 2pt ring just outside the button.
                    RoundedRectangle(cornerRadius: 13, style: .continuous)
                        .stroke(status.color, lineWidth: 2)
                        .padding(-1)
                }
            }
            .frame(minHeight: Metrics.minTouch)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .authHitHeight(40)
        .accessibilityLabel(status.label)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

#Preview {
    AccountView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

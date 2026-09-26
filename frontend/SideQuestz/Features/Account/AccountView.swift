import SwiftUI

/// Account tab (GUI_PLAN.md §7.10): profile header, your status, then Me | Friends.
///
/// The Me and Friends data live here (`AccountMeModel`, `AccountFriendsModel`), so switching
/// segments cross-fades straight to what was already loaded and refreshes it quietly. Pull down to
/// reload everything (`sqPullToRefresh`).
struct AccountView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @State private var showPhoto = false
    @State private var me = AccountMeModel()
    @State private var friends = AccountFriendsModel()

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
                // A ZStack so the two sections cross-fade in place.
                ZStack(alignment: .top) {
                    switch router.accountSegment {
                    case .me:
                        AccountMeSection(model: me)
                            .transition(.opacity)
                    case .friends:
                        FriendsView(model: friends)
                            .transition(.opacity)
                    }
                }
                .animation(Motion.standard, value: router.accountSegment)
            }
        }
        .sqPullToRefresh()
        .background(Theme.cream.ignoresSafeArea())
        .sqSheet(isPresented: $showPhoto) {
            PhotoSheet(initials: env.user?.initials ?? "", color: avatarColor) { showPhoto = false }
        }
        .task { await openLaunchRoute() }
        // New friend requests and friends' status lines arrive live, whichever segment is showing.
        .task { await friends.listen(env) }
    }

    private var avatarColor: Binding<AvatarColor> {
        Binding(get: { env.user?.avatarColor ?? .ink }, set: { env.user?.avatarColor = $0 })
    }

    /// `-SQRoute account/photo` opens the Photo sheet, `account/facebook` the Facebook sheet (the
    /// router already picked the segment).
    private func openLaunchRoute() async {
        guard let parts = router.consumeLaunch("account"), let sheet = parts.first, ["photo", "facebook"].contains(sheet) else { return }
        try? await Task.sleep(nanoseconds: 350_000_000)
        if sheet == "photo" {
            showPhoto = true
        } else {
            // The demo starts disconnected: connect first so the sheet shows an import.
            if env.isMock, !me.facebook.isConnected { await me.facebook.connect(env) }
            me.showsFacebook = true
        }
    }
}

// MARK: - Header

/// Avatar 84 with a 20pt status dot, "Edit photo" pill, name, "@handle · school" (the city when
/// the account has no school).
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
        [env.user?.username.map { "@\($0)" }, env.user?.school ?? env.user?.cityLabel].compactMap { $0 }.joined(separator: " · ")
    }
}

// MARK: - Your status

/// Open to all / Friends only / Busy (selected: white + 2pt ring in the dot color) and what it means.
/// Labels, colors and descriptions come from `PresenceStatus`; each pill is as wide as its label
/// plus an equal share of the spare room (stacked when the text is too large for one row).
///
/// Optimistic: a tap moves the ring (it slides over) and updates the text at once; the ring stays
/// faint until the server confirms, and snaps back with a short note if it doesn't.
private struct AccountStatusCard: View {
    @Environment(AppEnvironment.self) private var env
    @Namespace private var ringSpace
    /// The status waiting for the server.
    @State private var pending: PresenceStatus?
    /// What the server last confirmed (where a failed change rolls back to).
    @State private var confirmed: PresenceStatus?
    /// Only the newest tap's answer counts when taps overlap.
    @State private var latestRequest = 0
    @State private var errorText: String?

    private var current: PresenceStatus { env.user?.status ?? .open }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("YOUR STATUS")
                .sqFont(12, .bold, relativeTo: .caption)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
                .accessibilityAddTraits(.isHeader)
            AccountStatusPillsLayout(spacing: 8) {
                ForEach(PresenceStatus.allCases) { status in
                    button(status)
                }
            }
            Text(current.description)
                .sqFont(13)
                .foregroundStyle(Theme.text2)
                .authLineHeight(1.35, size: 13)
                .fixedSize(horizontal: false, vertical: true)
                .contentTransition(.opacity)
            if let errorText {
                AuthErrorText(message: errorText)
                    .sqTransition(.rise)
            }
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
            select(status)
        } label: {
            HStack(spacing: 6) {
                StatusDot(color: status.color, size: 10)
                    .sqBounce(when: selected, scale: 1.4)
                Text(status.label)
                    .sqFont(14, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            .padding(.horizontal, 10)
            .frame(maxWidth: .infinity)
            .frame(minHeight: 40)
            .background(selected ? Color.white : Theme.cream, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay {
                if selected {
                    // box-shadow 0 0 0 2px: a 2pt ring just outside the button. Faint while pending.
                    RoundedRectangle(cornerRadius: 13, style: .continuous)
                        .stroke(status.color, lineWidth: 2)
                        .padding(-1)
                        .opacity(pending == status ? 0.4 : 1)
                        .matchedGeometryEffect(id: "ring", in: ringSpace)
                }
            }
            .frame(minHeight: Metrics.minTouch)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .authHitHeight(40)
        .accessibilityLabel(status.label)
        .accessibilityValue(pending == status ? "Saving" : "")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private func select(_ status: PresenceStatus) {
        guard status != current else { return }
        if pending == nil { confirmed = current }
        latestRequest += 1
        let request = latestRequest
        withMotion(Motion.quick) {
            env.user?.status = status
            pending = status
            errorText = nil
        }
        Task {
            do {
                let updated = try await env.api.updateMe(UserPatch(status: status))
                confirmed = updated.status
                guard request == latestRequest else { return }
                withMotion(Motion.quick) {
                    env.user = updated
                    pending = nil
                }
            } catch {
                guard request == latestRequest else { return }
                withMotion {
                    env.user?.status = confirmed ?? status
                    pending = nil
                    errorText = "Couldn't update your status. Try again."
                }
            }
        }
    }
}

/// The three status pills in one row: each gets its natural width (dot, label, 10pt padding) plus an
/// equal share of what's left, so the padding looks even whatever the labels say. When they don't
/// fit they squeeze a little (up to 8%: the labels scale down), and past that they stack full width.
private struct AccountStatusPillsLayout: Layout {
    var spacing: CGFloat

    private enum Arrangement {
        case row([CGFloat])
        case column
    }

    private func arrangement(width: CGFloat?, subviews: Subviews) -> Arrangement {
        let ideals = subviews.map { $0.sizeThatFits(.unspecified).width }
        guard let width, !ideals.isEmpty else { return .row(ideals) }
        let gaps = spacing * CGFloat(ideals.count - 1)
        let natural = ideals.reduce(0, +)
        let room = width - gaps
        if natural <= room {
            let extra = (room - natural) / CGFloat(ideals.count)
            return .row(ideals.map { $0 + extra })
        }
        if natural * 0.92 <= room {
            return .row(ideals.map { $0 * room / natural })
        }
        return .column
    }

    private func rowHeight(_ subviews: Subviews, widths: [CGFloat]) -> CGFloat {
        zip(subviews, widths).map { $0.sizeThatFits(ProposedViewSize(width: $1, height: nil)).height }.max() ?? 0
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        switch arrangement(width: proposal.width, subviews: subviews) {
        case .row(let widths):
            let total = widths.reduce(0, +) + spacing * CGFloat(max(0, widths.count - 1))
            return CGSize(width: proposal.width ?? total, height: rowHeight(subviews, widths: widths))
        case .column:
            let width = proposal.width ?? 0
            let heights = subviews.map { $0.sizeThatFits(ProposedViewSize(width: width, height: nil)).height }
            return CGSize(width: width, height: heights.reduce(0, +) + spacing * CGFloat(max(0, heights.count - 1)))
        }
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        switch arrangement(width: bounds.width, subviews: subviews) {
        case .row(let widths):
            let height = rowHeight(subviews, widths: widths)
            var x = bounds.minX
            for (subview, width) in zip(subviews, widths) {
                subview.place(at: CGPoint(x: x, y: bounds.minY), anchor: .topLeading,
                              proposal: ProposedViewSize(width: width, height: height))
                x += width + spacing
            }
        case .column:
            var y = bounds.minY
            for subview in subviews {
                let size = subview.sizeThatFits(ProposedViewSize(width: bounds.width, height: nil))
                subview.place(at: CGPoint(x: bounds.minX, y: y), anchor: .topLeading,
                              proposal: ProposedViewSize(width: bounds.width, height: size.height))
                y += size.height + spacing
            }
        }
    }
}

#Preview {
    AccountView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

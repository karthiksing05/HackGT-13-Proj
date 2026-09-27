import SwiftUI

/// Account › Me › "Your tickets": how many are coming up ("2 upcoming"), and a tap opens the list.
/// It loads the list whenever Account comes on screen (the screen and this count share it).
struct TicketsAccountRow: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    let model: TicketsModel
    /// The placeholder only shimmers while Account is the visible tab.
    var shimmers = true

    private var upcoming: Int? { model.upcomingCount(now: env.clock.now) }

    var body: some View {
        Button {
            router.showTickets()
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "ticket")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(Theme.sageInk)
                    .frame(width: 36, height: 36)
                    .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                Text("Your tickets")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Spacer(minLength: 8)
                count
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.mutedIcon)
            }
            .padding(.vertical, 10)
            .padding(.horizontal, 14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel("Your tickets")
        .accessibilityValue(countText ?? "")
        .accessibilityHint("Shows every ticket you've bought")
        .accessibilityIdentifier("account.tickets")
        // Refresh whenever Account comes on screen (tabs stay alive in the background).
        .task(id: router.tab == .account) {
            guard router.tab == .account else { return }
            await model.load(env)
        }
        .sqReloadable("account.tickets") { await model.load(env) }
    }

    /// "2 upcoming" / "None upcoming"; a placeholder bar until the list is in, nothing if it failed.
    @ViewBuilder private var count: some View {
        ZStack(alignment: .trailing) {
            if let countText, let upcoming {
                Text(countText)
                    .sqFont(14, upcoming > 0 ? .semibold : .regular)
                    .foregroundStyle(upcoming > 0 ? Theme.sageInk : Theme.text3)
                    .contentTransition(.numericText())
                    .transition(.opacity)
            } else if model.tickets.isLoading {
                SkeletonBlock(width: 70, height: 10)
                    .sqShimmer(active: shimmers)
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: countText)
    }

    private var countText: String? {
        upcoming.map { $0 == 0 ? "None upcoming" : "\($0) upcoming" }
    }
}
